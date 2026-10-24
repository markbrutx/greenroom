package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type Dialect string

const (
	SQLite   Dialect = "sqlite"
	Postgres Dialect = "postgres"
)

type DB struct {
	raw     *sql.DB
	Dialect Dialect
}

func Now() int64 { return time.Now().UnixMilli() }

func Open(ctx context.Context, rawURL string) (*DB, error) {
	var (
		db  *DB
		err error
	)
	switch {
	case strings.HasPrefix(rawURL, "sqlite:"):
		db, err = openSQLite(strings.TrimPrefix(rawURL, "sqlite:"))
	case strings.HasPrefix(rawURL, "postgres://"), strings.HasPrefix(rawURL, "postgresql://"):
		var raw *sql.DB
		raw, err = sql.Open("pgx", rawURL)
		if err == nil {
			raw.SetMaxOpenConns(10)
			raw.SetMaxIdleConns(5)
			raw.SetConnMaxIdleTime(5 * time.Minute)
			db = &DB{raw: raw, Dialect: Postgres}
		}
	default:
		return nil, errors.New("database url must start with sqlite: or postgres://")
	}
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.raw.PingContext(pctx); err != nil {
		db.raw.Close()
		return nil, fmt.Errorf("database ping: %w", err)
	}
	return db, nil
}

func openSQLite(p string) (*DB, error) {
	p = strings.TrimPrefix(p, "//")
	if p == "" {
		return nil, errors.New("sqlite: empty path")
	}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Set("_txlock", "immediate")
	raw, err := sql.Open("sqlite", "file:"+p+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	raw.SetMaxOpenConns(4)
	raw.SetMaxIdleConns(4)
	return &DB{raw: raw, Dialect: SQLite}, nil
}

func (db *DB) Close() error { return db.raw.Close() }

func (db *DB) Ping(ctx context.Context) error { return db.raw.PingContext(ctx) }

func (db *DB) Rebind(q string) string {
	if db.Dialect != Postgres {
		return q
	}
	var b strings.Builder
	b.Grow(len(q) + 8)
	n, quoted := 0, false
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case c == '\'':
			quoted = !quoted
			b.WriteByte(c)
		case c == '?' && !quoted:
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func (db *DB) Exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return db.raw.ExecContext(ctx, db.Rebind(q), args...)
}

func (db *DB) Query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return db.raw.QueryContext(ctx, db.Rebind(q), args...)
}

func (db *DB) QueryRow(ctx context.Context, q string, args ...any) *sql.Row {
	return db.raw.QueryRowContext(ctx, db.Rebind(q), args...)
}

type Tx struct {
	tx *sql.Tx
	db *DB
}

func (db *DB) Begin(ctx context.Context) (*Tx, error) {
	tx, err := db.raw.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx, db: db}, nil
}

func (t *Tx) Exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, t.db.Rebind(q), args...)
}

func (t *Tx) Query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, t.db.Rebind(q), args...)
}

func (t *Tx) QueryRow(ctx context.Context, q string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, t.db.Rebind(q), args...)
}

func (t *Tx) Commit() error   { return t.tx.Commit() }
func (t *Tx) Rollback() error { return t.tx.Rollback() }

func (db *DB) Backup(ctx context.Context, file string) error {
	if db.Dialect != SQLite {
		return errors.New("backup works for SQLite only; for Postgres use pg_dump")
	}
	_, err := db.raw.ExecContext(ctx, "VACUUM INTO ?", file)
	return err
}

func (db *DB) Migrate(ctx context.Context, component string, fsys fs.FS) error {
	if _, err := db.raw.ExecContext(ctx, `create table if not exists schema_migrations (
		component text not null,
		version integer not null,
		name text not null,
		applied_at bigint not null,
		primary key (component, version))`); err != nil {
		return fmt.Errorf("schema_migrations: %w", err)
	}
	files, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return err
	}
	type mig struct {
		ver  int
		name string
	}
	var list []mig
	for _, f := range files {
		base := path.Base(f)
		num, _, ok := strings.Cut(base, "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil || v <= 0 {
			return fmt.Errorf("migration %s: name must start with a positive number and _", base)
		}
		list = append(list, mig{v, base})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ver < list[j].ver })
	for i := 1; i < len(list); i++ {
		if list[i].ver == list[i-1].ver {
			return fmt.Errorf("migration %s: duplicate version %d", list[i].name, list[i].ver)
		}
	}
	for _, m := range list {
		var n int
		if err := db.QueryRow(ctx, `select count(*) from schema_migrations where component = ? and version = ?`, component, m.ver).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := fs.ReadFile(fsys, m.name)
		if err != nil {
			return err
		}
		if err := db.apply(ctx, component, m.ver, m.name, string(body)); err != nil {
			return fmt.Errorf("migration %s/%s: %w", component, m.name, err)
		}
	}
	return nil
}

func (db *DB) apply(ctx context.Context, component string, ver int, name, body string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range Statements(db.expand(body)) {
		if _, err := tx.tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%w\n%s", err, stmt)
		}
	}
	if _, err := tx.Exec(ctx, `insert into schema_migrations (component, version, name, applied_at) values (?, ?, ?, ?)`,
		component, ver, name, Now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) expand(s string) string {
	var r *strings.Replacer
	if db.Dialect == Postgres {
		r = strings.NewReplacer("{{ID}}", "bigserial primary key", "{{BOOL}}", "boolean", "{{TRUE}}", "true", "{{FALSE}}", "false")
	} else {
		r = strings.NewReplacer("{{ID}}", "integer primary key autoincrement", "{{BOOL}}", "integer", "{{TRUE}}", "1", "{{FALSE}}", "0")
	}
	return r.Replace(s)
}

func Statements(s string) []string {
	var out []string
	var cur strings.Builder
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
		if strings.HasSuffix(t, ";") {
			out = append(out, strings.TrimSuffix(strings.TrimSpace(cur.String()), ";"))
			cur.Reset()
		}
	}
	if rest := strings.TrimSpace(cur.String()); rest != "" {
		out = append(out, rest)
	}
	return out
}
