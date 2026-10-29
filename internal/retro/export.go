package retro

import (
	"net/http"
	"strconv"
	"strings"
)

func (s *Service) export(w http.ResponseWriter, r *http.Request, room *Room, m *Member) {
	st, err := s.load(r, room, m)
	if err != nil {
		s.dbFail(w, r, err)
		return
	}
	date := s.isoDate(st.created)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="retro-`+date+`-`+room.Code+`.md"`)
	}
	w.Write([]byte(Markdown(st, date)))
}

func Participants(s *State) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name, label string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, label)
	}
	for _, p := range s.Members {
		if p.Host {
			add(p.Name, p.Name+" (host)")
		}
	}
	for _, p := range s.Members {
		add(p.Name, p.Name)
	}
	for _, c := range s.Cards {
		if strings.TrimSpace(c.Body) != "" {
			add(c.Author, c.Author)
		}
	}
	return out
}

func Markdown(s *State, date string) string {
	var b strings.Builder
	b.WriteString("# Team retro, " + date + "\n\n")
	people := Participants(s)
	b.WriteString("Participants (" + strconv.Itoa(len(people)) + "): " + strings.Join(people, ", ") + "\n\n")
	b.WriteString("Notes from a team retrospective. Each card shows its author in parentheses; " +
		"emoji with a number are reaction counts from the team. Actions list the owner when one was picked.\n")

	byCol := map[string][]Card{}
	for _, c := range s.Cards {
		if strings.TrimSpace(c.Body) != "" {
			byCol[c.Col] = append(byCol[c.Col], c)
		}
	}

	b.WriteString("\n## Warm-up: " + oneLine(s.Warmup) + "\n\n")
	if len(byCol["w"]) == 0 {
		b.WriteString("_No answers._\n")
	}
	for _, c := range byCol["w"] {
		b.WriteString("- " + oneLine(c.Body) + " (" + c.Author + ")" + reactionSuffix(c) + "\n")
	}

	b.WriteString(happiness(s.Mood))

	for i, h := range s.Cols {
		b.WriteString("\n## " + oneLine(h) + "\n\n")
		cards := byCol[strconv.Itoa(i)]
		if len(cards) == 0 {
			b.WriteString("_Nothing here._\n")
		}
		for _, c := range cards {
			b.WriteString("- " + oneLine(c.Body) + " (" + c.Author + ")" + reactionSuffix(c) + "\n")
		}
	}

	b.WriteString("\n## Actions\n\n")
	if len(byCol["a"]) == 0 {
		b.WriteString("_No actions._\n")
	}
	for _, c := range byCol["a"] {
		owner := "owner: nobody yet"
		if c.Owner != "" {
			owner = "owner: " + c.Owner
		}
		b.WriteString("- [ ] " + oneLine(c.Body) + " (" + owner + ")" + reactionSuffix(c) + "\n")
	}
	return b.String()
}

func happiness(m Mood) string {
	if m.Votes == 0 {
		return ""
	}
	head := "\n## Happiness: " + MoodQuestion + "\n\n"
	if !m.Shown {
		return head + "_" + plural(m.Votes, "vote") + ", results not shown yet._\n"
	}
	var parts []string
	for i := len(Faces) - 1; i >= 0; i-- {
		if n := m.Dist[i]; n > 0 {
			f := Faces[i]
			parts = append(parts, f.Emoji+" "+f.Label+" ("+strconv.Itoa(f.Score)+"): "+strconv.Itoa(n))
		}
	}
	return head + "Average " + strconv.FormatFloat(m.Average(), 'f', 1, 64) + " out of 5 from " +
		plural(m.Votes, "anonymous vote") + ". " + strings.Join(parts, ", ") + "\n"
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func reactionSuffix(c Card) string {
	var parts []string
	for _, e := range Emojis {
		if n := len(c.R[e]); n > 0 {
			parts = append(parts, e+" "+strconv.Itoa(n))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, ", ")
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
