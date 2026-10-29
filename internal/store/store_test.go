package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/markbrutx/moored/internal/store"
	"github.com/markbrutx/moored/internal/storetest"
	_ "modernc.org/sqlite"
)

func TestStatements(t *testing.T) {
	got := store.Statements("create table a (x int);\n\n-- note\ncreate index i on a (x)\n  where x > 0;\nselect 1")
	if len(got) != 3 || got[1] != "create index i on a (x)\n  where x > 0" || got[2] != "select 1" {
		t.Fatalf("unexpected split: %#v", got)
	}
}

func TestMigrateOnceAndInOrder(t *testing.T) {
	db := storetest.Open(t)
	ctx := context.Background()
	fsys := fstest.MapFS{
		"0002_more.sql": {Data: []byte("alter table things add column n integer not null default 0;")},
		"0001_init.sql": {Data: []byte("create table things (id integer primary key autoincrement, ok integer not null default 0, at integer not null);")},
	}
	for i := 0; i < 2; i++ {
		if err := db.Migrate(ctx, "demo", fsys); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	var id int64
	if err := db.QueryRow(ctx, `insert into things (ok, at, n) values (?, ?, ?) returning id`, true, store.Now(), 3).Scan(&id); err != nil {
		t.Fatal(err)
	}
	var ok bool
	var n int64
	if err := db.QueryRow(ctx, `select ok, n from things where id = ?`, id).Scan(&ok, &n); err != nil {
		t.Fatal(err)
	}
	if !ok || n != 3 {
		t.Fatalf("got ok=%v n=%d", ok, n)
	}
	var count int
	db.QueryRow(ctx, `select count(*) from schema_migrations where component = ?`, "demo").Scan(&count)
	if count != 2 {
		t.Fatalf("schema_migrations has %d rows for demo, want 2", count)
	}
}

func TestMigrateRejectsBadNames(t *testing.T) {
	db := storetest.Open(t)
	err := db.Migrate(context.Background(), "bad", fstest.MapFS{"init.sql": {Data: []byte("select 1;")}})
	if err == nil {
		t.Fatal("a migration without a number must be rejected")
	}
}

func TestOpenRejectsOtherURLs(t *testing.T) {
	if _, err := store.Open(context.Background(), "postgres://localhost/moored"); err == nil {
		t.Fatal("only sqlite: URLs are accepted")
	}
}

func TestOpenRefusesLegacyDatabase(t *testing.T) {
	file := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`create table retros (id integer primary key)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	if _, err := store.Open(context.Background(), "sqlite:"+file); !errors.Is(err, store.ErrLegacy) {
		t.Fatalf("a 0.2 database must be refused, got %v", err)
	}
}
