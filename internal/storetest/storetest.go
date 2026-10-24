package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/markbrutx/greenroom/internal/store"
)

const PostgresEnv = "GREENROOM_TEST_POSTGRES_URL"

func Open(t testing.TB) *store.DB {
	t.Helper()
	ctx := context.Background()
	if base := os.Getenv(PostgresEnv); base != "" {
		return openPostgres(t, ctx, base)
	}
	db, err := store.Open(ctx, "sqlite:"+filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func openPostgres(t testing.TB, ctx context.Context, base string) *store.DB {
	admin, err := store.Open(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 6)
	rand.Read(b)
	schema := "t_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "create schema "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		admin.Exec(context.Background(), "drop schema "+schema+" cascade")
		admin.Close()
	})
	return db
}
