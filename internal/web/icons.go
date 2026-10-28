package web

import "html/template"

var icons = map[string]string{
	"dice":     `<rect x="4" y="4" width="16" height="16" rx="4"/><circle cx="9" cy="9" r="1.1" fill="currentColor"/><circle cx="15" cy="15" r="1.1" fill="currentColor"/><circle cx="15" cy="9" r="1.1" fill="currentColor"/><circle cx="9" cy="15" r="1.1" fill="currentColor"/>`,
	"smile":    `<circle cx="12" cy="12" r="8.5"/><path d="M8.8 14.2a4 4 0 0 0 6.4 0"/><circle cx="9.3" cy="10" r=".9" fill="currentColor"/><circle cx="14.7" cy="10" r=".9" fill="currentColor"/>`,
	"share":    `<circle cx="9.5" cy="8.5" r="3.5"/><path d="M3.5 19.5a6 6 0 0 1 12 0"/><path d="M18.5 8v6M15.5 11h6"/>`,
	"copy":     `<rect x="8.5" y="8.5" width="11" height="11" rx="2.5"/><path d="M15.5 8.5V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v7.5a2 2 0 0 0 2 2h2.5"/>`,
	"download": `<path d="M12 4v11"/><path d="M7.5 10.5 12 15l4.5-4.5"/><path d="M5 19.5h14"/>`,
	"sparkle":  `<path d="M12 3.5l1.9 5.1 5.1 1.9-5.1 1.9L12 17.5l-1.9-5.1L5 10.5l5.1-1.9z"/><path d="M18.5 15.5l.7 1.8 1.8.7-1.8.7-.7 1.8-.7-1.8-1.8-.7 1.8-.7z"/>`,
	"close":    `<path d="M6.5 6.5l11 11M17.5 6.5l-11 11"/>`,
	"lock":     `<rect x="5" y="10.5" width="14" height="9.5" rx="2.5"/><path d="M8.5 10.5V8a3.5 3.5 0 0 1 7 0v2.5"/>`,
	"globe":    `<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17M12 3.5c2.4 2.6 3.5 5.4 3.5 8.5s-1.1 5.9-3.5 8.5c-2.4-2.6-3.5-5.4-3.5-8.5s1.1-5.9 3.5-8.5z"/>`,
	"key":      `<circle cx="8" cy="15" r="4"/><path d="M11 12l8-8M16 7l2.5 2.5M14 9l2 2"/>`,
	"sun":      `<circle cx="12" cy="12" r="4"/><path d="M12 3v2M12 19v2M3 12h2M19 12h2M5.6 5.6l1.4 1.4M17 17l1.4 1.4M5.6 18.4 7 17M17 7l1.4-1.4"/>`,
	"moon":     `<path d="M19.5 14.5A7.5 7.5 0 0 1 9.5 4.5a7.5 7.5 0 1 0 10 10z"/>`,
	"trash":    `<path d="M4.5 7h15M9.5 7V4.5h5V7M6.5 7l1 12.5h9l1-12.5"/>`,
}

func icon(name string) template.HTML {
	p, ok := icons[name]
	if !ok {
		return ""
	}
	return template.HTML(`<svg class="i i-` + name + `" viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">` + p + `</svg>`)
}

func Icon(name string) template.HTML { return icon(name) }
