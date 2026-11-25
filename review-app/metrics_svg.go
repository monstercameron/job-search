package main

import (
	"fmt"
	"html"
	"html/template"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Inline SVG charts, drawn in Go. Colours come from CSS classes in style.css
// (.c-slate, .c-good, ...), so the sheet owns the palette. Every chart carries
// <title> and an aria-label, and the page also offers the numbers as a table.
// The viewBox is 400 wide and the element is not scaled up past 440px, so type
// stays close to its drawn size from a phone to a desktop column.

const svgW = 400.0

type bar struct {
	Label, Value string
	N            float64
	Class        string
}

type col struct {
	Label, Short, Class string
	N                   int
	ShowLabel           bool
}

type intervalRow struct {
	Label, Class string
	K, N         int
}

func esc(s string) string { return html.EscapeString(s) }

func trunc(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n-1])) + "…"
}

func f1(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }

func svgOpen(title, desc string, h float64) *strings.Builder {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="chart" viewBox="0 0 %s %s" width="%s" role="img" aria-label="%s"><title>%s</title>`,
		f1(svgW), f1(h), f1(svgW), esc(title+". "+desc), esc(title))
	return &b
}

// hbarsSVG draws one labelled bar per row: the name and value on one line, the
// bar under it. Bars start at zero, so length is the count.
func hbarsSVG(title string, bars []bar, max float64) template.HTML {
	const rowH = 38.0
	h := rowH*float64(len(bars)) + 4
	var parts []string
	for _, b := range bars {
		parts = append(parts, b.Label+" "+b.Value)
	}
	sb := svgOpen(title, strings.Join(parts, "; "), h)
	if max <= 0 {
		max = 1
	}
	for i, b := range bars {
		y := rowH*float64(i) + 2
		fmt.Fprintf(sb, `<text class="c-label" x="0" y="%s">%s<title>%s</title></text>`, f1(y+13), esc(trunc(b.Label, 38)), esc(b.Label))
		fmt.Fprintf(sb, `<text class="c-value" x="%s" y="%s" text-anchor="end">%s</text>`, f1(svgW), f1(y+13), esc(b.Value))
		fmt.Fprintf(sb, `<rect class="c-track" x="0" y="%s" width="%s" height="3" rx="1.5"/>`, f1(y+27), f1(svgW))
		if b.N > 0 {
			w := math.Max(b.N/max*svgW, 4)
			r := math.Min(4, w/2)
			fmt.Fprintf(sb, `<path class="c-%s" d="M0,%s H%s Q%s,%s %s,%s V%s Q%s,%s %s,%s H0Z"/>`, b.Class,
				f1(y+19), f1(w-r), f1(w), f1(y+19), f1(w), f1(y+19+r), f1(y+27-r), f1(w), f1(y+27), f1(w-r), f1(y+27))
		}
	}
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}

func niceStep(raw float64) float64 {
	if raw <= 1 {
		return 1
	}
	exp := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 5, 10} {
		if m*exp >= raw {
			return m * exp
		}
	}
	return 10 * exp
}

// columnsSVG draws a column chart with a labelled count axis, a value above
// each column and a label under the columns marked ShowLabel.
func columnsSVG(title string, cols []col) template.HTML {
	const (
		left, right, top, bottom = 30.0, 4.0, 20.0, 26.0
		plotH                    = 150.0
	)
	h := top + plotH + bottom
	maxN := 1
	var parts []string
	for _, c := range cols {
		maxN = max(maxN, c.N)
		parts = append(parts, c.Label+" "+strconv.Itoa(c.N))
	}
	step := niceStep(float64(maxN) / 4)
	topV := math.Ceil(float64(maxN)/step) * step
	sb := svgOpen(title, strings.Join(parts, "; "), h)
	plotW := svgW - left - right
	for v := 0.0; v <= topV+0.001; v += step {
		y := top + plotH - v/topV*plotH
		cls := "c-grid"
		if v == 0 {
			cls = "c-axis"
		}
		fmt.Fprintf(sb, `<line class="%s" x1="%s" x2="%s" y1="%s" y2="%s"/>`, cls, f1(left), f1(svgW-right), f1(y), f1(y))
		fmt.Fprintf(sb, `<text class="c-tick" x="%s" y="%s" text-anchor="end">%s</text>`, f1(left-6), f1(y+4), strconv.Itoa(int(v)))
	}
	slot := plotW / float64(len(cols))
	bw := math.Min(slot*0.72, 34)
	for i, c := range cols {
		cx := left + slot*(float64(i)+0.5)
		x := cx - bw/2
		if c.N > 0 {
			bh := float64(c.N) / topV * plotH
			y := top + plotH - bh
			r := math.Min(4, bh)
			r = math.Min(r, bw/2)
			fmt.Fprintf(sb, `<path class="c-%s" d="M%s,%s V%s Q%s,%s %s,%s H%s Q%s,%s %s,%s V%sZ"><title>%s: %d</title></path>`, c.Class,
				f1(x), f1(top+plotH), f1(y+r), f1(x), f1(y), f1(x+r), f1(y), f1(x+bw-r), f1(x+bw), f1(y), f1(x+bw), f1(y+r), f1(top+plotH), esc(c.Label), c.N)
			fmt.Fprintf(sb, `<text class="c-value" x="%s" y="%s" text-anchor="middle">%d</text>`, f1(cx), f1(y-5), c.N)
		}
		if c.ShowLabel {
			fmt.Fprintf(sb, `<text class="c-tick" x="%s" y="%s" text-anchor="middle">%s</text>`, f1(cx), f1(top+plotH+17), esc(c.Short))
		}
	}
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}

// intervalSVG draws, per row, a dot at the observed rate and a line for its 95%
// Wilson interval on a shared 0 to 100% axis. A row with no applications shows
// words instead of marks, so an empty arm is never drawn as a zero rate.
func intervalSVG(title string, rows []intervalRow) template.HTML {
	const (
		rowH        = 58.0
		left, right = 6.0, 10.0
		axisH       = 26.0
		dot         = 6.0
	)
	h := rowH*float64(len(rows)) + axisH + 4
	var parts []string
	for _, r := range rows {
		if r.N == 0 {
			parts = append(parts, r.Label+": no applications")
			continue
		}
		lo, hi := wilson(r.K, r.N)
		parts = append(parts, fmt.Sprintf("%s: %d of %d heard back, %s%%, 95%% range %s%% to %s%%", r.Label, r.K, r.N, strconv.Itoa(int(math.Round(100*float64(r.K)/float64(r.N)))), pctF(lo), pctF(hi)))
	}
	sb := svgOpen(title, strings.Join(parts, "; "), h)
	px := func(f float64) float64 { return left + f*(svgW-left-right) }
	for _, t := range []float64{0, .25, .5, .75, 1} {
		fmt.Fprintf(sb, `<line class="c-grid" x1="%s" x2="%s" y1="0" y2="%s"/>`, f1(px(t)), f1(px(t)), f1(h-axisH))
		anchor := "middle"
		if t == 0 {
			anchor = "start"
		} else if t == 1 {
			anchor = "end"
		}
		x := px(t)
		if t == 0 {
			x = 0
		} else if t == 1 {
			x = svgW
		}
		fmt.Fprintf(sb, `<text class="c-tick" x="%s" y="%s" text-anchor="%s">%d%%</text>`, f1(x), f1(h-8), anchor, int(t*100))
	}
	fmt.Fprintf(sb, `<line class="c-axis" x1="%s" x2="%s" y1="%s" y2="%s"/>`, f1(left), f1(svgW-right), f1(h-axisH), f1(h-axisH))
	for i, r := range rows {
		y := rowH * float64(i)
		fmt.Fprintf(sb, `<text class="c-label" x="0" y="%s">%s<title>%s</title></text>`, f1(y+16), esc(trunc(r.Label, 26)), esc(r.Label))
		if r.N == 0 {
			fmt.Fprintf(sb, `<text class="c-value" x="%s" y="%s" text-anchor="end">no applications</text>`, f1(svgW), f1(y+16))
			continue
		}
		p := float64(r.K) / float64(r.N)
		lo, hi := wilson(r.K, r.N)
		fmt.Fprintf(sb, `<text class="c-value" x="%s" y="%s" text-anchor="end">%d of %d · %s</text>`, f1(svgW), f1(y+16), r.K, r.N, pctStr(r.K, r.N))
		cy := y + 38
		fmt.Fprintf(sb, `<line class="c-whisker c-%s" x1="%s" x2="%s" y1="%s" y2="%s"/>`, r.Class, f1(px(lo)), f1(px(hi)), f1(cy), f1(cy))
		fmt.Fprintf(sb, `<line class="c-cap c-%s" x1="%s" x2="%s" y1="%s" y2="%s"/><line class="c-cap c-%s" x1="%s" x2="%s" y1="%s" y2="%s"/>`,
			r.Class, f1(px(lo)), f1(px(lo)), f1(cy-5), f1(cy+5), r.Class, f1(px(hi)), f1(px(hi)), f1(cy-5), f1(cy+5))
		fmt.Fprintf(sb, `<circle class="c-dot c-%s" cx="%s" cy="%s" r="%s"><title>%s: %d of %d (%s%%), 95%% range %s%% to %s%%</title></circle>`, r.Class, f1(px(p)), f1(cy), f1(dot), esc(r.Label), r.K, r.N, strconv.Itoa(int(math.Round(100*p))), pctF(lo), pctF(hi))
	}
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}
