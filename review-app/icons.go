package main

import (
	"html/template"
	"sort"
	"strings"
)

// Icons are line glyphs on a 24-unit grid with a 1.75 stroke, round caps and
// straight runs, to sit beside Bahnschrift's DIN-style letters. Each one is a
// <symbol> in a sprite written once per page, and {{icon "name"}} places it.
// Every icon stands next to a text label, so all of them are aria-hidden.
var iconPaths = map[string]string{
	"jobs":      `<rect x="3" y="7" width="18" height="13" rx="2"/><path d="M9 7V5.5A1.5 1.5 0 0 1 10.5 4h3A1.5 1.5 0 0 1 15 5.5V7M3 12.5h18M10.5 12.5v2h3v-2"/>`,
	"apps":      `<rect x="5" y="4.5" width="14" height="16.5" rx="2"/><rect x="9" y="3" width="6" height="3.5" rx="1"/><path d="m9 13 2 2 4-4.5"/>`,
	"profile":   `<circle cx="12" cy="8.5" r="4"/><path d="M4.5 20.5c.6-3.6 3.6-5.8 7.5-5.8s6.9 2.2 7.5 5.8"/>`,
	"resume":    `<path d="M6 3.5h8l4.5 4.5v12.5H6z"/><path d="M14 3.5V8h4.5M9 12.5h6.5M9 16h4.5"/>`,
	"search":    `<circle cx="10.5" cy="10.5" r="6.5"/><path d="m15.5 15.5 5 5"/>`,
	"filter":    `<path d="M4 5h16l-6 7.5V19l-4 1.5v-8z"/>`,
	"pin":       `<path d="M12 21s-6.5-5.8-6.5-11.5a6.5 6.5 0 0 1 13 0C18.5 15.2 12 21 12 21z"/><circle cx="12" cy="9.5" r="2.3"/>`,
	"pay":       `<rect x="2.5" y="6" width="19" height="12" rx="2"/><circle cx="12" cy="12" r="2.5"/><path d="M6 9.5v5M18 9.5v5"/>`,
	"clock":     `<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/>`,
	"external":  `<path d="M14 4h6v6M20 4l-8.5 8.5M18 14v4.5a1.5 1.5 0 0 1-1.5 1.5h-11A1.5 1.5 0 0 1 4 18.5v-11A1.5 1.5 0 0 1 5.5 6H10"/>`,
	"check":     `<path d="m5 12.5 4.5 4.5L19 7.5"/>`,
	"mail":      `<rect x="3" y="5" width="18" height="14" rx="2"/><path d="m3.5 7 8.5 6.5L20.5 7"/>`,
	"calendar":  `<rect x="3.5" y="5" width="17" height="15.5" rx="2"/><path d="M3.5 10h17M8 3v4M16 3v4"/>`,
	"plus":      `<path d="M12 5v14M5 12h14"/>`,
	"download":  `<path d="M12 4v11M7 10l5 5 5-5M5 20h14"/>`,
	"up":        `<path d="M12 19V5M6 11l6-6 6 6"/>`,
	"down":      `<path d="M12 5v14M6 13l6 6 6-6"/>`,
	"left":      `<path d="m14.5 6-6 6 6 6"/>`,
	"right":     `<path d="m9.5 6 6 6-6 6"/>`,
	"trash":     `<path d="M4 7h16M9 7V4.5h6V7M6 7l1 13h10l1-13M10 11v5.5M14 11v5.5"/>`,
	"alert":     `<path d="M12 4 21 19.5H3z"/><path d="M12 10v4.5M12 17v.01"/>`,
	"bell":      `<path d="M6 16.5V11a6 6 0 0 1 12 0v5.5l1.5 1.5h-15z"/><path d="M10 20.5a2 2 0 0 0 4 0"/>`,
	"progress":  `<circle cx="6" cy="18" r="2"/><circle cx="18" cy="6" r="2"/><path d="M8 18h7.5a3 3 0 0 0 0-6h-7a3 3 0 0 1 0-6H16"/>`,
	"bookmark":  `<path d="M7 3.5h10v17l-5-4-5 4z"/>`,
	"archive":   `<rect x="3" y="4" width="18" height="5" rx="1"/><path d="M5 9v10a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V9M10 13h4"/>`,
	"summary":   `<path d="M4 6h16M4 10h16M4 14h11M4 18h13"/>`,
	"skills":    `<rect x="4" y="4" width="6.5" height="6.5" rx="1.5"/><rect x="13.5" y="4" width="6.5" height="6.5" rx="1.5"/><rect x="4" y="13.5" width="6.5" height="6.5" rx="1.5"/><path d="M16.75 13.5v6.5M13.5 16.75H20"/>`,
	"code":      `<path d="m8 7-5 5 5 5M16 7l5 5-5 5M13.5 4.5l-3 15"/>`,
	"cap":       `<path d="M2.5 9.5 12 5l9.5 4.5L12 14z"/><path d="M6.5 11.5V16c0 1.5 2.5 3 5.5 3s5.5-1.5 5.5-3v-4.5M21.5 9.5v5"/>`,
	"phone":     `<path d="M5.5 4H9l1.5 4.5-2.2 1.4a11 11 0 0 0 5.8 5.8l1.4-2.2L20 15v3.5a1.5 1.5 0 0 1-1.5 1.5C10.5 20 4 13.5 4 5.5A1.5 1.5 0 0 1 5.5 4z"/>`,
	"globe":     `<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17M12 3.5c2.3 2.5 3.5 5.3 3.5 8.5s-1.2 6-3.5 8.5c-2.3-2.5-3.5-5.3-3.5-8.5s1.2-6 3.5-8.5z"/>`,
	"link":      `<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1"/>`,
	"target":    `<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="4.5"/><circle cx="12" cy="12" r=".8" fill="currentColor"/>`,
	"shield":    `<path d="M12 3.5 19 6v5.5c0 4.4-3 8-7 9.2-4-1.2-7-4.8-7-9.2V6z"/><path d="m9 12 2 2 4-4.2"/>`,
	"copy":      `<rect x="8.5" y="8.5" width="12" height="12" rx="2"/><path d="M15.5 8.5V5a1.5 1.5 0 0 0-1.5-1.5H5A1.5 1.5 0 0 0 3.5 5v9A1.5 1.5 0 0 0 5 15.5h3.5"/>`,
	"flag":      `<path d="M5.5 21V4M5.5 4.5h12l-2.5 4 2.5 4h-12"/>`,
	"activity":  `<path d="M3 12h4l3-7 4 14 3-7h4"/>`,
	"hourglass": `<path d="M7 3.5h10M7 20.5h10M8 3.5c0 4.5 8 5 8 8.5s-8 4-8 8.5M16 3.5c0 4.5-8 5-8 8.5s8 4 8 8.5"/>`,
	"checklist": `<path d="m4 6.5 1.5 1.5 3-3M4 13.5 5.5 15l3-3M11.5 7h8.5M11.5 14h8.5M4.5 19.5h.01M11.5 19.5h8.5"/>`,
	"play":      `<rect x="3" y="5.5" width="18" height="13" rx="3"/><path d="M10.5 9.5v5l4.5-2.5z"/>`,
	"note":      `<path d="M5 4h14v11l-5 5H5z"/><path d="M14 20v-5h5M8.5 9h7M8.5 12.5h4"/>`,
	"dismiss":   `<circle cx="12" cy="12" r="8.5"/><path d="m6 6 12 12"/>`,
	"next":      `<path d="m5 6 6 6-6 6M13 6l6 6-6 6"/>`,
	"close":     `<path d="m6 6 12 12M18 6 6 18"/>`,
	"metrics":   `<path d="M4 4v16h16"/><path d="M8.5 16v-4M12.5 16V8M16.5 16v-6"/>`,
}

// iconSprite is the hidden <svg> holding every symbol, emitted once per page.
var iconSprite = func() template.HTML {
	names := make([]string, 0, len(iconPaths))
	for n := range iconPaths {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(`<svg class="sprite" aria-hidden="true" focusable="false"><defs>`)
	for _, n := range names {
		b.WriteString(`<symbol id="i-` + n + `" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">` + iconPaths[n] + `</symbol>`)
	}
	b.WriteString(`</defs></svg>`)
	return template.HTML(b.String())
}()

// icon returns the markup that places one glyph. An unknown name renders
// nothing, so a typo cannot inject markup or break the page.
func icon(name string) template.HTML {
	if _, ok := iconPaths[name]; !ok {
		return ""
	}
	return template.HTML(`<svg class="i i-` + name + `" aria-hidden="true" focusable="false"><use href="#i-` + name + `"></use></svg>`)
}

// groupIcon picks the glyph for an Applications group heading.
func groupIcon(title string) template.HTML {
	switch title {
	case "Follow up now":
		return icon("bell")
	case "In progress":
		return icon("hourglass")
	case "Not applied yet":
		return icon("bookmark")
	case "Closed":
		return icon("archive")
	}
	return ""
}

// contactIcon picks the glyph for a profile contact link by its field key.
func contactIcon(key string) template.HTML {
	switch key {
	case "website":
		return icon("globe")
	case "github":
		return icon("code")
	case "linkedin":
		return icon("profile")
	case "youtube":
		return icon("play")
	}
	return icon("link")
}
