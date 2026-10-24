package retro

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

func (t *Tool) export(w http.ResponseWriter, r *http.Request) {
	id, ok := retroID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s, err := t.load(r, id)
	if errors.Is(err, ErrNotFound) {
		t.fail(w, http.StatusNotFound, "This retro does not exist.")
		return
	}
	if err != nil {
		t.dbFail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="retro-`+t.isoDate(s.created)+`.md"`)
	}
	w.Write([]byte(Markdown(s, t.isoDate(s.created))))
}

func Markdown(s *State, date string) string {
	var b strings.Builder
	title := "Retro " + date
	if s.Title != "" {
		title = s.Title + " (" + date + ")"
	}
	b.WriteString("# " + title + "\n")

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
		b.WriteString("- **" + c.Author + "**: " + oneLine(c.Body) + reactionSuffix(c) + "\n")
	}

	if len(s.Carry) > 0 {
		b.WriteString("\n## From earlier retros\n\n")
		for _, c := range s.Carry {
			mark := " "
			if c.Done {
				mark = "x"
			}
			meta := "from " + c.FromStr
			if c.Owner != "" {
				meta = c.Owner + ", " + meta
			}
			b.WriteString("- [" + mark + "] " + oneLine(c.Body) + " (" + meta + ")\n")
		}
	}

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
		owner := ""
		if c.Owner != "" {
			owner = " (" + c.Owner + ")"
		}
		b.WriteString("- [ ] " + oneLine(c.Body) + owner + reactionSuffix(c) + "\n")
	}
	return b.String()
}

func reactionSuffix(c Card) string {
	var parts []string
	for _, e := range Emojis {
		if n := len(c.R[e]); n > 0 {
			parts = append(parts, e+strconv.Itoa(n))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "  " + strings.Join(parts, " ")
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
