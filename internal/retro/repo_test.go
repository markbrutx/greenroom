package retro

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/markbrutx/moored/internal/storetest"
)

func newRepo(t *testing.T) *Repo {
	t.Helper()
	db := storetest.Open(t)
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), "rooms", sub); err != nil {
		t.Fatal(err)
	}
	return NewRepo(db)
}

func newRoom(t *testing.T, r *Repo, code string, open bool) (*Room, *Member) {
	t.Helper()
	room, host, err := r.CreateRoom(context.Background(), code, "secret-"+code, open, "How was it?", []string{"Good", "Bad"}, "Maya")
	if err != nil {
		t.Fatal(err)
	}
	return room, host
}

func TestCodes(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c, err := NewCode()
		if err != nil {
			t.Fatal(err)
		}
		if !ValidCode(c) {
			t.Fatalf("bad code %q", c)
		}
		seen[c] = true
	}
	if len(seen) < 199 {
		t.Fatalf("codes repeat too often: %d unique of 200", len(seen))
	}
	cases := map[string]string{
		"abc-defg-hij":                           "abc-defg-hij",
		"  ABC-DEFG-HIJ ":                        "abc-defg-hij",
		"abcdefghij":                             "abc-defg-hij",
		"https://retro.example.com/abc-defg-hij": "abc-defg-hij",
		"https://retro.example.com/abc-defg-hij?host=x": "abc-defg-hij",
		"/abc-defg-hij": "abc-defg-hij",
	}
	for in, want := range cases {
		if got, ok := ParseCode(in); !ok || got != want {
			t.Errorf("ParseCode(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "abc", "abc-defg-hi1", "abc_defg_hij", "../etc/passwd"} {
		if _, ok := ParseCode(bad); ok {
			t.Errorf("ParseCode(%q) must fail", bad)
		}
	}
}

func TestCreateRoomAndCodeClash(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	room, host := newRoom(t, r, "abc-defg-hij", false)
	if !host.Host || host.Status != Admitted || host.Name != "Maya" {
		t.Fatalf("host member: %+v", host)
	}
	got, err := r.RoomByCode(ctx, "abc-defg-hij")
	if err != nil || got.ID != room.ID || got.Open || strings.Join(got.Cols, "|") != "Good|Bad" || got.HostSecret != "secret-abc-defg-hij" {
		t.Fatalf("room: %v %+v", err, got)
	}
	if _, _, err := r.CreateRoom(ctx, "abc-defg-hij", "x", true, "q", []string{"A"}, "Sam"); !errors.Is(err, ErrCodeTaken) {
		t.Fatalf("a taken code must be refused: %v", err)
	}
	if _, err := r.RoomByCode(ctx, "zzz-zzzz-zzz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing room: %v", err)
	}
}

func TestJoinNamesAndStatus(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	room, _ := newRoom(t, r, "abc-defg-hij", false)
	if _, err := r.Join(ctx, room.ID, 0, "maya", Pending); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("names are unique without case: %v", err)
	}
	jonas, err := r.Join(ctx, room.ID, 0, "Jonas", Pending)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.SetStatus(ctx, room.ID, jonas.ID, Admitted, Removed); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a pending member cannot be removed: %v", err)
	}
	denied, err := r.SetStatus(ctx, room.ID, jonas.ID, Pending, Denied)
	if err != nil || denied.Status != Denied {
		t.Fatalf("deny: %v %+v", err, denied)
	}
	other, err := r.Join(ctx, room.ID, 0, "JONAS", Pending)
	if err != nil {
		t.Fatalf("a denied name is free again: %v", err)
	}
	if _, err := r.Join(ctx, room.ID, jonas.ID, "Jonas", Pending); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("asking again under a name now in use: %v", err)
	}
	again, err := r.Join(ctx, room.ID, jonas.ID, "Jonas B", Pending)
	if err != nil || again.ID != jonas.ID {
		t.Fatalf("ask again keeps the member: %v %+v", err, again)
	}
	ids, err := r.AdmitAll(ctx, room.ID)
	if err != nil || len(ids) != 2 {
		t.Fatalf("admit all: %v %v", err, ids)
	}
	members, _ := r.Members(ctx, room.ID, Admitted)
	if len(members) != 3 || !members[0].Host || members[1].ID != other.ID || members[2].Name != "Jonas B" {
		t.Fatalf("members, host first then by name: %+v", members)
	}
	host, _ := r.HostMember(ctx, room.ID)
	if _, err := r.SetStatus(ctx, room.ID, host.ID, Admitted, Removed); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the host cannot be removed: %v", err)
	}
	second, _ := newRoom(t, r, "xyz-defg-hij", true)
	if _, err := r.Member(ctx, second.ID, jonas.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a member of one room is not a member of another: %v", err)
	}
}

func TestRoomFull(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	room, _ := newRoom(t, r, "abc-defg-hij", false)
	for i := 1; i < maxMembers; i++ {
		if _, err := r.Join(ctx, room.ID, 0, "guest "+strings.Repeat("x", i), Pending); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Join(ctx, room.ID, 0, "one more", Pending); !errors.Is(err, ErrFull) {
		t.Fatalf("room must be full: %v", err)
	}
}

func TestCardsAndHardDelete(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	room, _ := newRoom(t, r, "abc-defg-hij", false)
	c, err := r.AddCard(ctx, room.ID, "a", "Maya")
	if err != nil || c.Ver != 0 {
		t.Fatalf("add: %v %+v", err, c)
	}
	c1, err := r.SetText(ctx, room.ID, c.ID, "Write docs")
	if err != nil || c1.Ver != 1 || c1.Body != "Write docs" {
		t.Fatalf("text: %v %+v", err, c1)
	}
	c2, err := r.SetOwner(ctx, room.ID, c.ID, "Jonas")
	if err != nil || c2.Ver != 2 || c2.Owner != "Jonas" {
		t.Fatalf("owner: %v %+v", err, c2)
	}
	board, _ := r.AddCard(ctx, room.ID, "0", "Maya")
	if _, err := r.SetOwner(ctx, room.ID, board.ID, "Jonas"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("owner on a board card: %v", err)
	}
	other, _ := newRoom(t, r, "xyz-defg-hij", false)
	if _, err := r.SetText(ctx, other.ID, c.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("text through another room: %v", err)
	}
	if _, err := r.React(ctx, other.ID, c.ID, "Maya", "👍", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("react through another room: %v", err)
	}
	if changed, err := r.React(ctx, room.ID, c.ID, "Maya", "👍", true); err != nil || !changed {
		t.Fatalf("react: %v %v", err, changed)
	}
	if changed, _ := r.React(ctx, room.ID, c.ID, "Maya", "👍", true); changed {
		t.Fatal("a second identical reaction changes nothing")
	}
	cards, _ := r.Cards(ctx, room.ID)
	if len(cards) != 2 || len(cards[0].R["👍"]) != 1 {
		t.Fatalf("cards: %+v", cards)
	}
	if n, _ := r.BoardCardCount(ctx, room.ID); n != 1 {
		t.Fatalf("board count: %d", n)
	}
	if err := r.DeleteCard(ctx, room.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteCard(ctx, room.ID, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if err := r.DeleteRoom(ctx, room.ID); err != nil {
		t.Fatal(err)
	}
	var left int
	r.db.QueryRow(ctx, `select (select count(*) from cards where room_id = ?) + (select count(*) from members where room_id = ?) + (select count(*) from reactions)`, room.ID, room.ID).Scan(&left)
	if left != 0 {
		t.Fatalf("deleting a room must remove its members, cards and reactions, %d rows left", left)
	}
}

func TestIdleRooms(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	old, _ := newRoom(t, r, "abc-defg-hij", false)
	fresh, _ := newRoom(t, r, "xyz-defg-hij", false)
	if err := r.SetActiveAt(ctx, old.ID, 1000); err != nil {
		t.Fatal(err)
	}
	idle, err := r.Idle(ctx, 2000)
	if err != nil || len(idle) != 1 || idle[0].ID != old.ID {
		t.Fatalf("idle: %v %+v", err, idle)
	}
	if err := r.Touch(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	if idle, _ := r.Idle(ctx, 2000); len(idle) != 0 {
		t.Fatal("a touch makes the room active again")
	}
	_ = fresh
}
