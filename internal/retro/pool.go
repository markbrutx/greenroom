package retro

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

type ColumnSet struct {
	Name    string   `toml:"name" json:"name"`
	Columns []string `toml:"columns" json:"columns"`
}

type Pool struct {
	Warmups []string    `toml:"warmups"`
	Sets    []ColumnSet `toml:"sets"`
}

func LoadPool(override string) (*Pool, error) {
	raw := defaultPool
	source := "embedded pool.toml"
	if override != "" {
		b, err := os.ReadFile(override)
		if err != nil {
			return nil, fmt.Errorf("GREENROOM_TEMPLATES: %w", err)
		}
		raw, source = b, override
	}
	var p Pool
	if _, err := toml.Decode(string(raw), &p); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	if err := p.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	return &p, nil
}

func (p *Pool) validate() error {
	if len(p.Warmups) == 0 {
		return errors.New("needs at least one warm-up question")
	}
	if len(p.Sets) == 0 {
		return errors.New("needs at least one column set")
	}
	seen := map[string]bool{}
	for i, s := range p.Sets {
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("set %d has no name", i+1)
		}
		if seen[s.Name] {
			return fmt.Errorf("set %q appears twice", s.Name)
		}
		seen[s.Name] = true
		if len(s.Columns) < 1 || len(s.Columns) > maxColumns {
			return fmt.Errorf("set %q needs 1 to %d columns", s.Name, maxColumns)
		}
		for j, c := range s.Columns {
			c = clip(strings.TrimSpace(c), maxTitle)
			if c == "" {
				return fmt.Errorf("set %q has an empty column", s.Name)
			}
			p.Sets[i].Columns[j] = c
		}
	}
	for i, w := range p.Warmups {
		p.Warmups[i] = clip(strings.TrimSpace(w), maxWarmup)
		if p.Warmups[i] == "" {
			return fmt.Errorf("warm-up question %d is empty", i+1)
		}
	}
	return nil
}

func (p *Pool) Set(name string) (ColumnSet, bool) {
	for _, s := range p.Sets {
		if s.Name == name {
			return s, true
		}
	}
	return ColumnSet{}, false
}

func (p *Pool) RandomSet(not []string) ColumnSet {
	cur := strings.Join(not, "\x00")
	for tries := 0; tries < 20; tries++ {
		s := pick(p.Sets)
		if strings.Join(s.Columns, "\x00") != cur || len(p.Sets) == 1 {
			return s
		}
	}
	return p.Sets[0]
}

func (p *Pool) RandomWarmup(not string) string {
	for tries := 0; tries < 20; tries++ {
		w := pick(p.Warmups)
		if w != not || len(p.Warmups) == 1 {
			return w
		}
	}
	return p.Warmups[0]
}

func (p *Pool) SetNames() []string {
	out := make([]string, len(p.Sets))
	for i, s := range p.Sets {
		out[i] = s.Name
	}
	return out
}

func pick[T any](xs []T) T {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(xs))))
	if err != nil {
		return xs[0]
	}
	return xs[n.Int64()]
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
