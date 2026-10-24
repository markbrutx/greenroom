package retro

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/markbrutx/greenroom/internal/store"
	"github.com/markbrutx/greenroom/internal/storetest"
)

func newRepo(t *testing.T) *Repo {
	t.Helper()
	db := storetest.Open(t)
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), Slug, sub); err != nil {
		t.Fatal(err)
	}
	return NewRepo(db)
}

func TestCreateAndLoadRetro(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	id, err := r.CreateRetro(ctx, "Sprint 42", "How was it?", []string{"Good", "Bad"})
	if err != nil {
		t.Fatal(err)
	}
	x, err := r.Retro(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if x.Title != "Sprint 42" || x.Warmup != "How was it?" || strings.Join(x.Cols, "|") != "Good|Bad" || x.Hidden || x.TimerEnd != 0 {
		t.Fatalf("unexpected retro: %+v", x)
	}
	if _, err := r.Retro(ctx, id+100); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing retro: got %v", err)
	}
	list, err := r.List(ctx, 10)
	if err != nil || len(list) != 1 || list[0].Title != "Sprint 42" {
		t.Fatalf("list: %v %+v", err, list)
	}
}

func TestCardVersionGrowsOnEveryUpdate(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	id, _ := r.CreateRetro(ctx, "", "q", []string{"A"})
	c, err := r.AddCard(ctx, id, "a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if c.Ver != 0 || c.Author != "alice" || c.Col != "a" {
		t.Fatalf("new card: %+v", c)
	}
	c1, err := r.SetText(ctx, id, c.ID, "Write docs")
	if err != nil || c1.Ver != 1 || c1.Body != "Write docs" {
		t.Fatalf("text: %v %+v", err, c1)
	}
	c2, err := r.SetOwner(ctx, id, c.ID, "bob")
	if err != nil || c2.Ver != 2 || c2.Owner != "bob" || c2.Body != "Write docs" {
		t.Fatalf("owner: %v %+v", err, c2)
	}
	c3, err := r.SetText(ctx, id, c.ID, "Write the docs")
	if err != nil || c3.Ver != 3 || c3.Owner != "bob" {
		t.Fatalf("text again: %v %+v", err, c3)
	}
	other, _ := r.CreateRetro(ctx, "", "q", []string{"A"})
	if _, err := r.SetText(ctx, other, c.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("text through another retro: %v", err)
	}
	board, _ := r.AddCard(ctx, id, "0", "alice")
	if _, err := r.SetOwner(ctx, id, board.ID, "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("owner on a board card must fail: %v", err)
	}
	if err := r.DeleteCard(ctx, id, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SetText(ctx, id, c.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("text on deleted card: %v", err)
	}
	if err := r.DeleteCard(ctx, id, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestReactionsAreIdempotent(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	id, _ := r.CreateRetro(ctx, "", "q", []string{"A"})
	c, _ := r.AddCard(ctx, id, "0", "alice")
	r.SetText(ctx, id, c.ID, "Nice")

	steps := []struct {
		who, emoji  string
		on, changed bool
	}{
		{"bob", "👍", true, true},
		{"bob", "👍", true, false},
		{"carol", "👍", true, true},
		{"bob", "🎉", true, true},
		{"bob", "🎉", false, true},
		{"bob", "🎉", false, false},
	}
	for i, s := range steps {
		changed, err := r.React(ctx, id, c.ID, s.who, s.emoji, s.on)
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if changed != s.changed {
			t.Fatalf("step %d: changed=%v want %v", i, changed, s.changed)
		}
	}
	cards, err := r.Cards(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	got := cards[0].R
	if len(got["👍"]) != 2 || len(got["🎉"]) != 0 {
		t.Fatalf("reactions: %#v", got)
	}

	other, _ := r.CreateRetro(ctx, "", "q", []string{"A"})
	if _, err := r.React(ctx, other, c.ID, "bob", "👍", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("react through another retro: %v", err)
	}
	r.DeleteCard(ctx, id, c.ID)
	if _, err := r.React(ctx, id, c.ID, "bob", "❤️", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("react on deleted card: %v", err)
	}
}

func TestCarryOverAndDone(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	first, _ := r.CreateRetro(ctx, "", "q", []string{"A"})
	keep, _ := r.AddCard(ctx, first, "a", "alice")
	r.SetText(ctx, first, keep.ID, "Fix the build")
	empty, _ := r.AddCard(ctx, first, "a", "alice")
	finish, _ := r.AddCard(ctx, first, "a", "alice")
	r.SetText(ctx, first, finish.ID, "Update docs")
	_ = empty

	second, _ := r.CreateRetro(ctx, "", "q", []string{"A"})
	carry, err := r.Carry(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if len(carry) != 2 {
		t.Fatalf("carry: %+v", carry)
	}
	if err := r.SetCarryDone(ctx, second, finish.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := r.SetCarryDone(ctx, first, keep.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("done from the same retro must fail: %v", err)
	}
	carry, _ = r.Carry(ctx, second)
	if len(carry) != 2 || !carry[1].Done {
		t.Fatalf("done action stays visible where it was ticked: %+v", carry)
	}

	third, _ := r.CreateRetro(ctx, "", "q", []string{"A"})
	carry, _ = r.Carry(ctx, third)
	if len(carry) != 1 || carry[0].ID != keep.ID {
		t.Fatalf("later retro sees only unfinished actions: %+v", carry)
	}
	open, err := r.OpenActions(ctx)
	if err != nil || len(open) != 1 || open[0].ID != keep.ID {
		t.Fatalf("open actions: %v %+v", err, open)
	}
	if err := r.SetActionDone(ctx, keep.ID, true); err != nil {
		t.Fatal(err)
	}
	if open, _ := r.OpenActions(ctx); len(open) != 0 {
		t.Fatalf("open actions after done: %+v", open)
	}
	if err := r.SetActionDone(ctx, empty.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an empty action cannot be done: %v", err)
	}
	n, _, err := r.Stats(ctx)
	if err != nil || n != 0 {
		t.Fatalf("stats: %v %d", err, n)
	}
}

func TestTimerHiddenTitleColumns(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	id, _ := r.CreateRetro(ctx, "", "q", []string{"A", "B"})
	end := store.Now() + 60000
	if err := r.SetTimer(ctx, id, end); err != nil {
		t.Fatal(err)
	}
	if err := r.SetHidden(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if err := r.SetTitle(ctx, id, "Q3 review"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetCols(ctx, id, []string{"Keep", "Drop"}); err != nil {
		t.Fatal(err)
	}
	x, _ := r.Retro(ctx, id)
	if x.TimerEnd != end || !x.Hidden || x.Title != "Q3 review" || x.Cols[1] != "Drop" {
		t.Fatalf("retro: %+v", x)
	}
	on, left := timerLeft(x.TimerEnd)
	if !on || left <= 0 || left > 60000 {
		t.Fatalf("timer left: %v %d", on, left)
	}
	if on, left := timerLeft(store.Now() - 5000); !on || left != 0 {
		t.Fatalf("finished timer: %v %d", on, left)
	}
	if err := r.SetHidden(ctx, id+99, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden on missing retro: %v", err)
	}
	if n, _ := r.BoardCardCount(ctx, id); n != 0 {
		t.Fatalf("board count %d", n)
	}
	r.AddCard(ctx, id, "a", "x")
	r.AddCard(ctx, id, "w", "x")
	r.AddCard(ctx, id, "1", "x")
	if n, _ := r.BoardCardCount(ctx, id); n != 1 {
		t.Fatalf("board count ignores warm-up and actions, got %d", n)
	}
}

func TestMarkdownExport(t *testing.T) {
	s := &State{
		Title:  "Sprint 42",
		Warmup: "One word?",
		Cols:   []string{"Good", "Bad"},
		Cards: []Card{
			{ID: 1, Col: "w", Author: "alice", Body: "Calm"},
			{ID: 2, Col: "0", Author: "bob", Body: "Fast\nreviews", R: map[string][]string{"👍": {"a", "b", "c"}, "❤️": {"a"}}},
			{ID: 3, Col: "a", Author: "bob", Body: "Pair more", Owner: "Everyone"},
			{ID: 4, Col: "1", Author: "bob", Body: "  "},
		},
		Carry: []Carry{{ID: 9, Body: "Old thing", Owner: "carol", Done: true, FromStr: "Oct 1"}},
	}
	md := Markdown(s, "2026-10-24")
	for _, want := range []string{
		"# Sprint 42 (2026-10-24)",
		"## Warm-up: One word?",
		"- **alice**: Calm",
		"- [x] Old thing (carol, from Oct 1)",
		"- Fast reviews (bob)  👍3 ❤️1",
		"## Bad\n\n_Nothing here._",
		"- [ ] Pair more (Everyone)",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("export lacks %q:\n%s", want, md)
		}
	}
}

func TestPoolValidation(t *testing.T) {
	p, err := LoadPool("")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Sets) < 15 || len(p.Warmups) < 60 {
		t.Fatalf("pool too small: %d sets, %d warm-ups", len(p.Sets), len(p.Warmups))
	}
	cur := p.Sets[0].Columns
	for i := 0; i < 20; i++ {
		if strings.Join(p.RandomSet(cur).Columns, "|") == strings.Join(cur, "|") {
			t.Fatal("random set repeated the current one")
		}
	}
	bad := &Pool{Warmups: []string{"q"}, Sets: []ColumnSet{{Name: "x", Columns: []string{}}}}
	if bad.validate() == nil {
		t.Fatal("a set without columns must be rejected")
	}
}
