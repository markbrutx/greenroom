package retro

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/markbrutx/moored/internal/store"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrNameTaken = errors.New("name taken")
	ErrFull      = errors.New("room full")
	ErrCodeTaken = errors.New("code taken")
)

const (
	Pending  = "pending"
	Admitted = "admitted"
	Denied   = "denied"
	Removed  = "removed"

	maxMembers  = 100
	touchEvery  = 60 * 1000
	activeQuery = `update rooms set active_at = ? where id = ? and active_at < ?`
)

type Repo struct{ db *store.DB }

func NewRepo(db *store.DB) *Repo { return &Repo{db: db} }

type Room struct {
	ID         int64
	Code       string
	HostSecret string
	Open       bool
	Warmup     string
	Cols       []string
	CreatedAt  int64
	ActiveAt   int64
	MoodShown  bool
}

type Member struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Host   bool   `json:"host,omitempty"`
	Status string `json:"-"`
	RoomID int64  `json:"-"`
}

type Card struct {
	ID     int64               `json:"id"`
	Col    string              `json:"col"`
	Author string              `json:"author"`
	Body   string              `json:"body"`
	Owner  string              `json:"owner"`
	Ver    int64               `json:"ver"`
	Lock   string              `json:"lock,omitempty"`
	LockMs int64               `json:"lockMs,omitempty"`
	R      map[string][]string `json:"r,omitempty"`
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func nameKey(name string) string { return strings.ToLower(name) }

func (r *Repo) CreateRoom(ctx context.Context, code, secret string, open bool, warmup string, cols []string, host string) (*Room, *Member, error) {
	b, _ := json.Marshal(cols)
	now := store.Now()
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(ctx, `select count(*) from rooms where code = ?`, code).Scan(&n); err != nil {
		return nil, nil, err
	}
	if n > 0 {
		return nil, nil, ErrCodeTaken
	}
	room := &Room{Code: code, HostSecret: secret, Open: open, Warmup: warmup, Cols: cols, CreatedAt: now, ActiveAt: now}
	if err := tx.QueryRow(ctx, `insert into rooms (code, host_secret, open, warmup, cols, created_at, active_at)
		values (?, ?, ?, ?, ?, ?, ?) returning id`, code, secret, open, warmup, string(b), now, now).Scan(&room.ID); err != nil {
		return nil, nil, err
	}
	m := &Member{Name: host, Host: true, Status: Admitted, RoomID: room.ID}
	if err := tx.QueryRow(ctx, `insert into members (room_id, name, name_key, host, status, created_at, seen_at)
		values (?, ?, ?, ?, ?, ?, ?) returning id`, room.ID, host, nameKey(host), true, Admitted, now, now).Scan(&m.ID); err != nil {
		return nil, nil, err
	}
	return room, m, tx.Commit()
}

const roomCols = `id, code, host_secret, open, warmup, cols, created_at, active_at, mood_shown`

func scanRoom(sc interface{ Scan(...any) error }) (*Room, error) {
	x := &Room{}
	var cols string
	if err := sc.Scan(&x.ID, &x.Code, &x.HostSecret, &x.Open, &x.Warmup, &cols, &x.CreatedAt, &x.ActiveAt, &x.MoodShown); err != nil {
		return nil, notFound(err)
	}
	if err := json.Unmarshal([]byte(cols), &x.Cols); err != nil {
		return nil, err
	}
	return x, nil
}

func (r *Repo) RoomByCode(ctx context.Context, code string) (*Room, error) {
	return scanRoom(r.db.QueryRow(ctx, `select `+roomCols+` from rooms where code = ?`, code))
}

func (r *Repo) Touch(ctx context.Context, roomID int64) error {
	now := store.Now()
	_, err := r.db.Exec(ctx, activeQuery, now, roomID, now-touchEvery)
	return err
}

func (r *Repo) SetActiveAt(ctx context.Context, roomID, at int64) error {
	return affected(r.db.Exec(ctx, `update rooms set active_at = ? where id = ?`, at, roomID))
}

func (r *Repo) SetOpen(ctx context.Context, roomID int64, open bool) error {
	return affected(r.db.Exec(ctx, `update rooms set open = ? where id = ?`, open, roomID))
}

func (r *Repo) DeleteRoom(ctx context.Context, roomID int64) error {
	return affected(r.db.Exec(ctx, `delete from rooms where id = ?`, roomID))
}

func (r *Repo) Idle(ctx context.Context, before int64) ([]Room, error) {
	rows, err := r.db.Query(ctx, `select `+roomCols+` from rooms where active_at < ? order by id`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Room
	for rows.Next() {
		x, err := scanRoom(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

const memberCols = `id, room_id, name, host, status`

func scanMember(sc interface{ Scan(...any) error }) (*Member, error) {
	m := &Member{}
	if err := sc.Scan(&m.ID, &m.RoomID, &m.Name, &m.Host, &m.Status); err != nil {
		return nil, notFound(err)
	}
	return m, nil
}

func (r *Repo) Member(ctx context.Context, roomID, id int64) (*Member, error) {
	return scanMember(r.db.QueryRow(ctx, `select `+memberCols+` from members where id = ? and room_id = ?`, id, roomID))
}

func (r *Repo) HostMember(ctx context.Context, roomID int64) (*Member, error) {
	return scanMember(r.db.QueryRow(ctx, `select `+memberCols+` from members where room_id = ? and host order by id limit 1`, roomID))
}

func (r *Repo) Members(ctx context.Context, roomID int64, status string) ([]Member, error) {
	rows, err := r.db.Query(ctx, `select `+memberCols+` from members where room_id = ? and status = ? order by host desc, name_key`, roomID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *Repo) Join(ctx context.Context, roomID, existing int64, name, status string) (*Member, error) {
	now := store.Now()
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var taken, total int
	if err := tx.QueryRow(ctx, `select
		coalesce(sum(case when name_key = ? and id <> ? then 1 else 0 end), 0), count(*)
		from members where room_id = ? and status in ('pending', 'admitted')`, nameKey(name), existing, roomID).Scan(&taken, &total); err != nil {
		return nil, err
	}
	if taken > 0 {
		return nil, ErrNameTaken
	}
	m := &Member{Name: name, Status: status, RoomID: roomID}
	if existing > 0 {
		row := tx.QueryRow(ctx, `update members set name = ?, name_key = ?, status = ?, seen_at = ?
			where id = ? and room_id = ? and not host returning id`, name, nameKey(name), status, now, existing, roomID)
		if err := row.Scan(&m.ID); err != nil {
			return nil, notFound(err)
		}
	} else {
		if total >= maxMembers {
			return nil, ErrFull
		}
		if err := tx.QueryRow(ctx, `insert into members (room_id, name, name_key, host, status, created_at, seen_at)
			values (?, ?, ?, ?, ?, ?, ?) returning id`, roomID, name, nameKey(name), false, status, now, now).Scan(&m.ID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `update rooms set active_at = ? where id = ?`, now, roomID); err != nil {
		return nil, err
	}
	return m, tx.Commit()
}

func (r *Repo) SetStatus(ctx context.Context, roomID, id int64, from, to string) (*Member, error) {
	return scanMember(r.db.QueryRow(ctx, `update members set status = ? where id = ? and room_id = ? and status = ? and not host
		returning `+memberCols, to, id, roomID, from))
}

func (r *Repo) AdmitAll(ctx context.Context, roomID int64) ([]int64, error) {
	rows, err := r.db.Query(ctx, `update members set status = 'admitted' where room_id = ? and status = 'pending' returning id`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *Repo) Seen(ctx context.Context, id int64) error {
	now := store.Now()
	_, err := r.db.Exec(ctx, `update members set seen_at = ? where id = ? and seen_at < ?`, now, id, now-touchEvery)
	return err
}

func (r *Repo) SetCols(ctx context.Context, roomID int64, cols []string) error {
	b, _ := json.Marshal(cols)
	return affected(r.db.Exec(ctx, `update rooms set cols = ? where id = ?`, string(b), roomID))
}

func (r *Repo) SetWarmup(ctx context.Context, roomID int64, warmup string) error {
	return affected(r.db.Exec(ctx, `update rooms set warmup = ? where id = ?`, warmup, roomID))
}

const cardCols = `id, col, author, body, owner, ver`

func scanCard(sc interface{ Scan(...any) error }) (Card, error) {
	var c Card
	err := sc.Scan(&c.ID, &c.Col, &c.Author, &c.Body, &c.Owner, &c.Ver)
	return c, err
}

func (r *Repo) Cards(ctx context.Context, roomID int64) ([]Card, error) {
	rows, err := r.db.Query(ctx, `select `+cardCols+` from cards where room_id = ? order by id`, roomID)
	if err != nil {
		return nil, err
	}
	cards := []Card{}
	index := map[int64]int{}
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		c.R = map[string][]string{}
		index[c.ID] = len(cards)
		cards = append(cards, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = r.db.Query(ctx, `select x.card_id, x.who, x.emoji from reactions x
		join cards c on c.id = x.card_id
		where c.room_id = ? order by x.created_at, x.who`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var who, emoji string
		if err := rows.Scan(&id, &who, &emoji); err != nil {
			return nil, err
		}
		if i, ok := index[id]; ok {
			cards[i].R[emoji] = append(cards[i].R[emoji], who)
		}
	}
	return cards, rows.Err()
}

func (r *Repo) AddCard(ctx context.Context, roomID int64, col, author string) (Card, error) {
	now := store.Now()
	row := r.db.QueryRow(ctx, `insert into cards (room_id, col, author, created_at, updated_at) values (?, ?, ?, ?, ?)
		returning `+cardCols, roomID, col, author, now, now)
	return scanCard(row)
}

func (r *Repo) SetText(ctx context.Context, roomID, cardID int64, body string) (Card, error) {
	row := r.db.QueryRow(ctx, `update cards set body = ?, ver = ver + 1, updated_at = ?
		where id = ? and room_id = ? returning `+cardCols, body, store.Now(), cardID, roomID)
	c, err := scanCard(row)
	return c, notFound(err)
}

func (r *Repo) SetOwner(ctx context.Context, roomID, cardID int64, owner string) (Card, error) {
	row := r.db.QueryRow(ctx, `update cards set owner = ?, ver = ver + 1, updated_at = ?
		where id = ? and room_id = ? and col = 'a' returning `+cardCols, owner, store.Now(), cardID, roomID)
	c, err := scanCard(row)
	return c, notFound(err)
}

func (r *Repo) DeleteCard(ctx context.Context, roomID, cardID int64) error {
	return affected(r.db.Exec(ctx, `delete from cards where id = ? and room_id = ?`, cardID, roomID))
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) React(ctx context.Context, roomID, cardID int64, who, emoji string, on bool) (bool, error) {
	var n int
	if err := r.db.QueryRow(ctx, `select count(*) from cards where id = ? and room_id = ?`, cardID, roomID).Scan(&n); err != nil {
		return false, err
	}
	if n == 0 {
		return false, ErrNotFound
	}
	var res sql.Result
	var err error
	if on {
		res, err = r.db.Exec(ctx, `insert into reactions (card_id, who, emoji, created_at) values (?, ?, ?, ?)
			on conflict do nothing`, cardID, who, emoji, store.Now())
	} else {
		res, err = r.db.Exec(ctx, `delete from reactions where card_id = ? and who = ? and emoji = ?`, cardID, who, emoji)
	}
	if err != nil {
		return false, err
	}
	n64, err := res.RowsAffected()
	return n64 > 0, err
}

func (r *Repo) SetMood(ctx context.Context, roomID, memberID int64, score int) error {
	if score == 0 {
		_, err := r.db.Exec(ctx, `delete from moods where member_id = ? and room_id = ?
			and exists (select 1 from rooms where id = ? and not mood_shown)`, memberID, roomID, roomID)
		return err
	}
	return affected(r.db.Exec(ctx, `insert into moods (member_id, room_id, score, created_at)
		select ?, ?, ?, ? where exists (select 1 from rooms where id = ? and not mood_shown)
		on conflict (member_id) do update set score = excluded.score`, memberID, roomID, score, store.Now(), roomID))
}

func (r *Repo) Moods(ctx context.Context, roomID int64) (map[int64]int, error) {
	rows, err := r.db.Query(ctx, `select m.member_id, m.score from moods m
		join members x on x.id = m.member_id
		where m.room_id = ? and x.status = 'admitted'`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var score int
		if err := rows.Scan(&id, &score); err != nil {
			return nil, err
		}
		out[id] = score
	}
	return out, rows.Err()
}

func (r *Repo) ShowMoods(ctx context.Context, roomID int64) error {
	return affected(r.db.Exec(ctx, `update rooms set mood_shown = 1 where id = ?`, roomID))
}

func (r *Repo) ResetMoods(ctx context.Context, roomID int64) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(ctx, `delete from moods where room_id = ?`, roomID); err != nil {
		return err
	}
	if err := affected(tx.Exec(ctx, `update rooms set mood_shown = 0 where id = ?`, roomID)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repo) BoardCardCount(ctx context.Context, roomID int64) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `select count(*) from cards where room_id = ? and col not in ('a', 'w')`, roomID).Scan(&n)
	return n, err
}

func (r *Repo) Counts(ctx context.Context) (rooms, members, cards int, err error) {
	err = r.db.QueryRow(ctx, `select (select count(*) from rooms), (select count(*) from members), (select count(*) from cards)`).
		Scan(&rooms, &members, &cards)
	return
}
