package web

import (
	"html/template"
	"sync"
)

var (
	iconsMu sync.RWMutex
	icons   = map[string]string{
		"sun":    `<circle cx="12" cy="12" r="4"/><path d="M12 2.5v2M12 19.5v2M2.5 12h2M19.5 12h2M5.3 5.3l1.4 1.4M17.3 17.3l1.4 1.4M5.3 18.7l1.4-1.4M17.3 6.7l1.4-1.4"/>`,
		"moon":   `<path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z"/>`,
		"logout": `<path d="M14 4h4a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-4"/><path d="M9 16l-4-4 4-4"/><path d="M5 12h11"/>`,
		"plus":   `<path d="M12 5v14M5 12h14"/>`,
		"dice":   `<rect x="4" y="4" width="16" height="16" rx="4"/><circle cx="9" cy="9" r="1.1" fill="currentColor"/><circle cx="15" cy="15" r="1.1" fill="currentColor"/><circle cx="15" cy="9" r="1.1" fill="currentColor"/><circle cx="9" cy="15" r="1.1" fill="currentColor"/>`,
		"clock":  `<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/>`,
		"eye":    `<path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z"/><circle cx="12" cy="12" r="3"/>`,
		"eyeoff": `<path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z"/><circle cx="12" cy="12" r="3"/><path d="M4 4l16 16"/>`,
		"sort":   `<path d="M7 4v16M4 17l3 3 3-3M13 6h7M13 12h5M13 18h3"/>`,
		"copy":   `<rect x="8" y="8" width="12" height="12" rx="2.5"/><path d="M16 8V6.5A2.5 2.5 0 0 0 13.5 4h-7A2.5 2.5 0 0 0 4 6.5v7A2.5 2.5 0 0 0 6.5 16H8"/>`,
		"x":      `<path d="M6.5 6.5l11 11M17.5 6.5l-11 11"/>`,
		"smile":  `<circle cx="12" cy="12" r="8.5"/><path d="M8.8 14.2a4 4 0 0 0 6.4 0"/><circle cx="9.3" cy="10" r=".9" fill="currentColor"/><circle cx="14.7" cy="10" r=".9" fill="currentColor"/>`,
		"board":  `<rect x="3" y="4" width="18" height="16" rx="3"/><path d="M9 4v16M15 4v16M5.5 8h1.5M11 8h2M17 8h1.5"/>`,
		"checks": `<path d="M4 7.5l2 2 3.5-3.5M12.5 8H20M4 15.5l2 2 3.5-3.5M12.5 16H20"/>`,
		"arrow":  `<path d="M5 12h14M13 6l6 6-6 6"/>`,
		"stop":   `<rect x="6.5" y="6.5" width="11" height="11" rx="2.5"/>`,
		"lock":   `<rect x="5" y="10.5" width="14" height="9.5" rx="2.5"/><path d="M8.5 10.5V8a3.5 3.5 0 0 1 7 0v2.5"/>`,
		"people": `<circle cx="9" cy="9" r="3.2"/><path d="M3.5 19a5.5 5.5 0 0 1 11 0"/><circle cx="16.5" cy="8.5" r="2.6"/><path d="M15.5 13.6A4.6 4.6 0 0 1 20.5 18"/>`,
		"pencil": `<path d="M15.5 5.5l3 3L9 18H6v-3z"/><path d="M13.5 7.5l3 3"/>`,
	}
)

func RegisterIcon(name, paths string) {
	iconsMu.Lock()
	icons[name] = paths
	iconsMu.Unlock()
}

func icon(name string) template.HTML {
	iconsMu.RLock()
	p, ok := icons[name]
	iconsMu.RUnlock()
	if !ok {
		return ""
	}
	return template.HTML(`<svg class="i" viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">` + p + `</svg>`)
}

func Icon(name string) template.HTML { return icon(name) }
