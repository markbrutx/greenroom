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

	_ "modernc.org/sqlite"
)

type DB struct {
	raw *sql.DB
}

var ErrLegacy = errors.New("this database was written by Greenroom 0.2 or older; 0.3 starts with an empty database: " +
	"move the file aside (keep it as a backup) or point GREENROOM_DATABASE_URL at a new file")

func Now() int64 { return time.Now().UnixMilli() }

func Open(ctx context.Context, rawURL string) (*DB, error) {
	p, ok := strings.CutPrefix(rawURL, "sqlite:")
	if !ok {
		return nil, errors.New("GREENROOM_DATABASE_URL must start with sqlite:, for example sqlite:/data/greenroom.db")
	}
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
	db := &DB{raw: raw}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := raw.PingContext(pctx); err != nil {
		raw.Close()
		return nil, fmt.Errorf("database ping: %w", err)
	}
	if err := db.checkLegacy(pctx); err != nil {
		raw.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) checkLegacy(ctx context.Context) error {
	var n int
	err := db.raw.QueryRowContext(ctx, `select count(*) from sqlite_master where type = 'table' and name in ('retros', 'people')`).Scan(&n)
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}
	if n > 0 {
		return ErrLegacy
	}
	return nil
}

func (db *DB) Close() error { return db.raw.Close() }

func (db *DB) Ping(ctx context.Context) error { return db.raw.PingContext(ctx) }

func (db *DB) Exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return db.raw.ExecContext(ctx, q, args...)
}

func (db *DB) Query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return db.raw.QueryContext(ctx, q, args...)
}

func (db *DB) QueryRow(ctx context.Context, q string, args ...any) *sql.Row {
	return db.raw.QueryRowContext(ctx, q, args...)
}

type Tx struct{ tx *sql.Tx }

func (db *DB) Begin(ctx context.Context) (*Tx, error) {
	tx, err := db.raw.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx}, nil
}

func (t *Tx) Exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, q, args...)
}

func (t *Tx) Query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, q, args...)
}

func (t *Tx) QueryRow(ctx context.Context, q string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, q, args...)
}

func (t *Tx) Commit() error   { return t.tx.Commit() }
func (t *Tx) Rollback() error { return t.tx.Rollback() }

func (db *DB) Backup(ctx context.Context, file string) error {
	_, err := db.raw.ExecContext(ctx, "VACUUM INTO ?", file)
	return err
}

func (db *DB) Migrate(ctx context.Context, component string, fsys fs.FS) error {
	if _, err := db.raw.ExecContext(ctx, `create table if not exists schema_migrations (
		component text not null,
		version integer not null,
		name text not null,
		applied_at integer not null,
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
	for _, stmt := range Statements(body) {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("%w\n%s", err, stmt)
		}
	}
	if _, err := tx.Exec(ctx, `insert into schema_migrations (component, version, name, applied_at) values (?, ?, ?, ?)`,
		component, ver, name, Now()); err != nil {
		return err
	}
	return tx.Commit()
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
