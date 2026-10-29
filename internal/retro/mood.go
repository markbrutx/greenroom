package retro

import (
	"context"
	"math"
	"sync"
	"time"
)

type Face struct {
	Score int    `json:"score"`
	Emoji string `json:"emoji"`
	Label string `json:"label"`
}

var Faces = []Face{
	{1, "😞", "Unhappy"},
	{2, "🙁", "Not great"},
	{3, "😐", "Okay"},
	{4, "🙂", "Good"},
	{5, "😄", "Great"},
}

const (
	MoodQuestion  = "How happy are you with the last sprint?"
	autoRevealMin = 2
	revealDelay   = 3 * time.Second
)

type Mood struct {
	Votes int   `json:"votes"`
	Shown bool  `json:"shown"`
	Dist  []int `json:"dist,omitempty"`
	Mine  int   `json:"mine,omitempty"`
}

func summarize(votes map[int64]int, shown bool, me int64) Mood {
	m := Mood{Votes: len(votes), Shown: shown, Mine: votes[me]}
	if shown {
		m.Dist = make([]int, len(Faces))
		for _, v := range votes {
			if v >= 1 && v <= len(Faces) {
				m.Dist[v-1]++
			}
		}
	}
	return m
}

func (m Mood) Average() float64 {
	sum, n := 0, 0
	for i, c := range m.Dist {
		sum += (i + 1) * c
		n += c
	}
	if n == 0 {
		return 0
	}
	return math.Round(float64(sum)/float64(n)*10) / 10
}

func everyoneVoted(here []int64, votes map[int64]int) bool {
	if len(votes) < autoRevealMin || len(here) == 0 {
		return false
	}
	for _, id := range here {
		if _, ok := votes[id]; !ok {
			return false
		}
	}
	return true
}

type reveals struct {
	mu     sync.Mutex
	timers map[int64]*time.Timer
}

func (r *reveals) set(roomID int64, t *time.Timer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old := r.timers[roomID]; old != nil {
		old.Stop()
	}
	if t == nil {
		delete(r.timers, roomID)
		return
	}
	if r.timers == nil {
		r.timers = map[int64]*time.Timer{}
	}
	r.timers[roomID] = t
}

func (s *Service) sendMood(ctx context.Context, room *Room, me int64, autoReveal bool) (Mood, error) {
	votes, err := s.repo.Moods(ctx, room.ID)
	if err != nil {
		return Mood{}, err
	}
	if autoReveal && !room.MoodShown && everyoneVoted(s.app.Hub.HereIDs(topic(room.Code)), votes) {
		code := room.Code
		s.reveals.set(room.ID, time.AfterFunc(revealDelay, func() { s.autoReveal(code) }))
	} else {
		s.reveals.set(room.ID, nil)
	}
	s.app.Hub.Send(topic(room.Code), map[string]any{"t": "mood", "mood": summarize(votes, room.MoodShown, 0)})
	return summarize(votes, room.MoodShown, me), nil
}

func (s *Service) autoReveal(code string) {
	ctx := context.Background()
	room, err := s.repo.RoomByCode(ctx, code)
	if err != nil || room.MoodShown {
		return
	}
	votes, err := s.repo.Moods(ctx, room.ID)
	if err != nil || !everyoneVoted(s.app.Hub.HereIDs(topic(code)), votes) {
		return
	}
	if err := s.repo.ShowMoods(ctx, room.ID); err != nil {
		s.app.Log.Error("reveal happiness", "err", err, "room", code)
		return
	}
	s.app.Hub.Send(topic(code), map[string]any{"t": "mood", "mood": summarize(votes, true, 0)})
}
