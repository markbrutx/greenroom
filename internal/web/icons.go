package web

import (
	"html/template"
	"sync"
)

var (
	iconsMu sync.RWMutex
	icons   = map[string]string{
		"logout": `<path d="M14 4h4a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-4"/><path d="M9 16l-4-4 4-4"/><path d="M5 12h11"/>`,
		"dice":   `<rect x="4" y="4" width="16" height="16" rx="4"/><circle cx="9" cy="9" r="1.1" fill="currentColor"/><circle cx="15" cy="15" r="1.1" fill="currentColor"/><circle cx="15" cy="9" r="1.1" fill="currentColor"/><circle cx="9" cy="15" r="1.1" fill="currentColor"/>`,
		"smile":  `<circle cx="12" cy="12" r="8.5"/><path d="M8.8 14.2a4 4 0 0 0 6.4 0"/><circle cx="9.3" cy="10" r=".9" fill="currentColor"/><circle cx="14.7" cy="10" r=".9" fill="currentColor"/>`,
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
