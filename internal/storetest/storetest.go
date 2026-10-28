package storetest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/markbrutx/greenroom/internal/store"
)

func Open(t testing.TB) *store.DB {
	t.Helper()
	db, err := store.Open(context.Background(), "sqlite:"+filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
