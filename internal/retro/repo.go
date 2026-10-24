package retro

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/markbrutx/greenroom/internal/store"
)

var ErrNotFound = errors.New("not found")

type Repo struct{ db *store.DB }

func NewRepo(db *store.DB) *Repo { return &Repo{db: db} }

type Retro struct {
	ID        int64
	CreatedAt int64
	Warmup    string
	Cols      []string
}

type Card struct {
	ID     int64               `json:"id"`
	Col    string              `json:"col"`
	Author string              `json:"author"`
	Body   string              `json:"body"`
	Owner  string              `json:"owner"`
	Done   bool                `json:"done"`
	Ver    int64               `json:"ver"`
	Lock   string              `json:"lock,omitempty"`
	LockMs int64               `json:"lockMs,omitempty"`
	R      map[string][]string `json:"r,omitempty"`
}

type Carry struct {
	ID      int64  `json:"id"`
	Body    string `json:"body"`
	Owner   string `json:"owner"`
	Done    bool   `json:"done"`
	RetroID int64  `json:"retro"`
	From    int64  `json:"-"`
	FromStr string `json:"from"`
}

type Summary struct {
	ID        int64
	CreatedAt int64
	Cols      []string
	Cards     int
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (r *Repo) CreateRetro(ctx context.Context, warmup string, cols []string) (int64, error) {
	b, _ := json.Marshal(cols)
	var id int64
	err := r.db.QueryRow(ctx, `insert into retros (created_at, warmup, cols) values (?, ?, ?) returning id`,
		store.Now(), warmup, string(b)).Scan(&id)
	return id, err
}

func (r *Repo) Retro(ctx context.Context, id int64) (*Retro, error) {
	x := &Retro{ID: id}
	var cols string
	err := r.db.QueryRow(ctx, `select created_at, warmup, cols from retros where id = ?`, id).
		Scan(&x.CreatedAt, &x.Warmup, &cols)
	if err != nil {
		return nil, notFound(err)
	}
	if err := json.Unmarshal([]byte(cols), &x.Cols); err != nil {
		return nil, err
	}
	return x, nil
}

func (r *Repo) List(ctx context.Context, limit int) ([]Summary, error) {
	rows, err := r.db.Query(ctx, `select r.id, r.created_at, r.cols,
		(select count(*) from cards c where c.retro_id = r.id and not c.deleted and c.body <> '')
		from retros r order by r.id desc limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		var s Summary
		var cols string
		if err := rows.Scan(&s.ID, &s.CreatedAt, &cols, &s.Cards); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(cols), &s.Cols)
		out = append(out, s)
	}
	return out, rows.Err()
}

const cardCols = `id, col, author, body, owner, done, ver`

func scanCard(sc interface{ Scan(...any) error }) (Card, error) {
	var c Card
	err := sc.Scan(&c.ID, &c.Col, &c.Author, &c.Body, &c.Owner, &c.Done, &c.Ver)
	return c, err
}

func (r *Repo) Cards(ctx context.Context, retroID int64) ([]Card, error) {
	rows, err := r.db.Query(ctx, `select `+cardCols+` from cards where retro_id = ? and not deleted order by id`, retroID)
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
		where c.retro_id = ? and not c.deleted order by x.created_at, x.who`, retroID)
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

func (r *Repo) Carry(ctx context.Context, retroID int64) ([]Carry, error) {
	rows, err := r.db.Query(ctx, `select c.id, c.body, c.owner, c.done, c.retro_id, r.created_at from cards c
		join retros r on r.id = c.retro_id
		where c.col = 'a' and not c.deleted and c.body <> '' and c.retro_id < ?
		  and (not c.done or c.done_retro = ?) order by c.id`, retroID, retroID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Carry{}
	for rows.Next() {
		var c Carry
		if err := rows.Scan(&c.ID, &c.Body, &c.Owner, &c.Done, &c.RetroID, &c.From); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repo) AddCard(ctx context.Context, retroID int64, col, author string) (Card, error) {
	now := store.Now()
	row := r.db.QueryRow(ctx, `insert into cards (retro_id, col, author, created_at, updated_at) values (?, ?, ?, ?, ?)
		returning `+cardCols, retroID, col, author, now, now)
	c, err := scanCard(row)
	return c, err
}

func (r *Repo) SetText(ctx context.Context, retroID, cardID int64, body string) (Card, error) {
	row := r.db.QueryRow(ctx, `update cards set body = ?, ver = ver + 1, updated_at = ?
		where id = ? and retro_id = ? and not deleted returning `+cardCols, body, store.Now(), cardID, retroID)
	c, err := scanCard(row)
	return c, notFound(err)
}

func (r *Repo) SetOwner(ctx context.Context, retroID, cardID int64, owner string) (Card, error) {
	row := r.db.QueryRow(ctx, `update cards set owner = ?, ver = ver + 1, updated_at = ?
		where id = ? and retro_id = ? and col = 'a' and not deleted returning `+cardCols, owner, store.Now(), cardID, retroID)
	c, err := scanCard(row)
	return c, notFound(err)
}

func (r *Repo) DeleteCard(ctx context.Context, retroID, cardID int64) error {
	res, err := r.db.Exec(ctx, `update cards set deleted = ?, ver = ver + 1, updated_at = ? where id = ? and retro_id = ? and not deleted`,
		true, store.Now(), cardID, retroID)
	return affected(res, err)
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

func (r *Repo) SetCarryDone(ctx context.Context, retroID, cardID int64, done bool) error {
	var doneRetro, doneAt sql.NullInt64
	if done {
		doneRetro = sql.NullInt64{Int64: retroID, Valid: true}
		doneAt = sql.NullInt64{Int64: store.Now(), Valid: true}
	}
	res, err := r.db.Exec(ctx, `update cards set done = ?, done_retro = ?, done_at = ?, ver = ver + 1, updated_at = ?
		where id = ? and col = 'a' and retro_id < ? and not deleted`,
		done, doneRetro, doneAt, store.Now(), cardID, retroID)
	return affected(res, err)
}

func (r *Repo) CardInRetro(ctx context.Context, retroID, cardID int64) (bool, error) {
	var n int
	err := r.db.QueryRow(ctx, `select count(*) from cards where id = ? and retro_id = ? and not deleted`, cardID, retroID).Scan(&n)
	return n > 0, err
}

func (r *Repo) React(ctx context.Context, retroID, cardID int64, who, emoji string, on bool) (bool, error) {
	ok, err := r.CardInRetro(ctx, retroID, cardID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, ErrNotFound
	}
	var res sql.Result
	if on {
		res, err = r.db.Exec(ctx, `insert into reactions (card_id, who, emoji, created_at) values (?, ?, ?, ?)
			on conflict do nothing`, cardID, who, emoji, store.Now())
	} else {
		res, err = r.db.Exec(ctx, `delete from reactions where card_id = ? and who = ? and emoji = ?`, cardID, who, emoji)
	}
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (r *Repo) BoardCardCount(ctx context.Context, retroID int64) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `select count(*) from cards where retro_id = ? and not deleted and col not in ('a', 'w')`, retroID).Scan(&n)
	return n, err
}

func (r *Repo) SetCols(ctx context.Context, retroID int64, cols []string) error {
	b, _ := json.Marshal(cols)
	return affected(r.db.Exec(ctx, `update retros set cols = ? where id = ?`, string(b), retroID))
}

func (r *Repo) SetWarmup(ctx context.Context, retroID int64, warmup string) error {
	return affected(r.db.Exec(ctx, `update retros set warmup = ? where id = ?`, warmup, retroID))
}
