package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Résumé files for upload. Both renderers read the same plain-text layout
// (see resumeFormat) and produce single-column, text-based files: standard
// fonts, no tables, columns, images, headers or footers, so applicant tracking
// systems extract every word in reading order.

const resumeFormat = `Plain text, one item per line:
line 1: your name
line 2: headline (title and focus)
line 3: contact details separated by " | " (location | email | phone | website | GitHub | LinkedIn)
section headings on their own line in capitals: SUMMARY, WORK EXPERIENCE, SKILLS, PROJECTS
role lines: "Title, Company (Team) | Mon YYYY – Mon YYYY" or "– Present"
bullets: lines starting with "- "
skills: one group per line, "Group: item, item, item"`

type resumeLine struct {
	Kind string // name, headline, contact, heading, role, bullet, text, blank
	Text string
}

var headingRe = regexp.MustCompile(`^[A-Z][A-Z &/,-]{2,40}$`)

func isHeading(s string) bool {
	letters := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters >= 3 && headingRe.MatchString(s)
}

// parseResume classifies each line of the résumé text.
func parseResume(text string) []resumeLine {
	var out []resumeLine
	seenHeading := false
	head := 0
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		s := strings.TrimSpace(raw)
		switch {
		case s == "":
			out = append(out, resumeLine{Kind: "blank"})
		case isHeading(s):
			seenHeading = true
			out = append(out, resumeLine{"heading", s})
		case !seenHeading && head == 0:
			head++
			out = append(out, resumeLine{"name", s})
		case !seenHeading && (strings.Contains(s, "@") || strings.Count(s, " | ") >= 1):
			head++
			out = append(out, resumeLine{"contact", s})
		case !seenHeading:
			head++
			out = append(out, resumeLine{"headline", s})
		case strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "• ") || strings.HasPrefix(s, "* "):
			out = append(out, resumeLine{"bullet", strings.TrimSpace(s[strings.Index(s, " "):])})
		case strings.Contains(s, " | "):
			out = append(out, resumeLine{"role", s})
		default:
			out = append(out, resumeLine{"text", s})
		}
	}
	return out
}

// --- PDF ------------------------------------------------------------------

// helvetica holds Helvetica advance widths (1/1000 em) for ASCII 32..126.
var helvetica = [95]int{278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556,
	1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556,
	333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556,
	556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584}

// winAnsi maps the characters a résumé commonly uses outside ASCII to the
// WinAnsiEncoding byte the standard PDF fonts expect.
var winAnsi = map[rune]byte{'€': 0x80, '‚': 0x82, '„': 0x84, '…': 0x85, '‘': 0x91, '’': 0x92, '“': 0x93, '”': 0x94, '•': 0x95, '–': 0x96, '—': 0x97, '™': 0x99}

// encodable reports whether r can be drawn with a standard PDF font.
func encodable(r rune) bool {
	_, ok := winAnsi[r]
	return ok || (r >= 32 && r <= 126) || (r >= 0xA0 && r <= 0xFF)
}

// helveticaBold holds Helvetica-Bold advance widths for ASCII 32..126.
var helveticaBold = [95]int{278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611,
	975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556,
	333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611,
	611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584}

func runeWidth(r rune, bold bool) float64 {
	w := 556.0
	switch {
	case r >= 32 && r <= 126 && bold:
		return float64(helveticaBold[r-32])
	case r >= 32 && r <= 126:
		return float64(helvetica[r-32])
	case r == '—':
		w = 1000
	case r == '•':
		w = 350
	case r == '‘' || r == '’':
		w = 222
	case r == '“' || r == '”':
		w = 333
	}
	if bold {
		w *= 1.07
	}
	return w
}

func textWidth(s string, size float64, bold bool) float64 {
	t := 0.0
	for _, r := range s {
		t += runeWidth(r, bold)
	}
	return t * size / 1000
}

// wrap breaks s into lines no wider than width points.
func wrap(s string, size, width float64, bold bool) []string {
	var lines []string
	cur := ""
	for _, w := range strings.Fields(s) {
		try := w
		if cur != "" {
			try = cur + " " + w
		}
		if cur != "" && textWidth(try, size, bold) > width {
			lines = append(lines, cur)
			cur = w
			continue
		}
		cur = try
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func pdfString(s string) string {
	var b strings.Builder
	b.WriteByte('(')
	for _, r := range s {
		var c byte
		switch {
		case r >= 32 && r <= 126:
			c = byte(r)
		case winAnsi[r] != 0:
			c = winAnsi[r]
		case r >= 0xA0 && r <= 0xFF:
			c = byte(r)
		default:
			c = '?'
		}
		if c == '(' || c == ')' || c == '\\' {
			b.WriteByte('\\')
		}
		if c >= 0x80 {
			fmt.Fprintf(&b, "\\%03o", c)
		} else {
			b.WriteByte(c)
		}
	}
	b.WriteByte(')')
	return b.String()
}

// layoutPDF lays the résumé out on US Letter pages and returns each page's
// content stream. Project names and skill-group labels ("AI:") are bold.
func layoutPDF(text string) []string {
	const pageW, pageH, margin = 612.0, 792.0, 54.0 // 0.75 in: the Jobscan ATS guide's minimum
	const body, lead = 10.0, 12.6
	width := pageW - 2*margin
	var pages []string
	var cur strings.Builder
	y := pageH - margin
	newPage := func() {
		pages = append(pages, cur.String())
		cur.Reset()
		y = pageH - margin
	}
	draw := func(s string, x, size float64, bold bool) {
		font := "F1"
		if bold {
			font = "F2"
		}
		fmt.Fprintf(&cur, "BT /%s %.1f Tf %.2f %.2f Td %s Tj ET\n", font, size, x, y, pdfString(s))
	}
	need := func(h float64) {
		if y-h < margin {
			newPage()
		}
	}
	section := ""
	for _, l := range parseResume(text) {
		switch l.Kind {
		case "blank":
			y -= 2
		case "name":
			need(26)
			y -= 18
			draw(l.Text, margin, 20, true)
			y -= 6
		case "headline", "contact":
			size := 11.0
			if l.Kind == "contact" {
				size = 9.5
			}
			for _, w := range wrap(l.Text, size, width, false) {
				need(size * 1.3)
				y -= size * 1.3
				draw(w, margin, size, false)
			}
		case "heading":
			section = l.Text
			need(34)
			y -= 15
			draw(l.Text, margin, 11.5, true)
			y -= 4
			fmt.Fprintf(&cur, "0.6 w %.2f %.2f m %.2f %.2f l S\n", margin, y, pageW-margin, y)
			y -= 1
		case "role":
			for _, w := range wrap(l.Text, 10.5, width, true) {
				need(lead + 2)
				y -= lead + 2
				draw(w, margin, 10.5, true)
			}
		case "bullet":
			for i, w := range wrap(l.Text, body, width-14, false) {
				need(lead)
				y -= lead
				if i == 0 {
					draw("•", margin+3, body, false)
				}
				draw(w, margin+14, body, false)
			}
		default:
			// "Group: items" lines get a bold label; plain lines in PROJECTS
			// are project names and are bold throughout.
			label, _, hasLabel := strings.Cut(l.Text, ": ")
			hasLabel = hasLabel && len(label) <= 32 && !strings.Contains(label, "http")
			bold := strings.Contains(section, "PROJECT") && !hasLabel
			for i, w := range wrap(l.Text, body, width, bold) {
				need(lead)
				y -= lead
				if i == 0 && hasLabel && strings.HasPrefix(w, label+":") {
					draw(label+":", margin, body, true)
					draw(strings.TrimPrefix(w, label+":"), margin+textWidth(label+":", body, true), body, false)
					continue
				}
				draw(w, margin, body, bold)
			}
		}
	}
	return append(pages, cur.String())
}

// tdRe captures the baseline of each text draw ("x y Td").
var tdRe = regexp.MustCompile(`[\d.]+ ([\d.]+) Td `)

// pdfPages reports how many pages the résumé fills and how many text lines
// land on the last page, so a one-line spill onto page two can be flagged.
func pdfPages(text string) (pages, lastLines int) {
	ps := layoutPDF(text)
	ys := map[string]bool{}
	for _, m := range tdRe.FindAllStringSubmatch(ps[len(ps)-1], -1) {
		ys[m[1]] = true
	}
	return len(ps), len(ys)
}

// resumePDF renders the résumé as a single-column, text-based PDF.
func resumePDF(text, title, author string) []byte {
	pages := layoutPDF(text)
	var buf bytes.Buffer
	var offsets []int
	obj := func(body string) {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	n := len(pages)
	kids := make([]string, n)
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", 6+2*i)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	obj(fmt.Sprintf("<< /Title %s /Author %s /Creator (Job review) /CreationDate (D:%s) >>", pdfString(title), pdfString(author), time.Now().Format("20060102150405")))
	for i, content := range pages {
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R /F2 4 0 R >> >> /Contents %d 0 R >>", 7+2*i))
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content))
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 5 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return buf.Bytes()
}

// --- DOCX -----------------------------------------------------------------

func xmlText(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			if r == '\t' || r == '\n' || r >= 0x20 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// resumeDOCX renders the résumé as a minimal Word document: Arial, one
// column, real paragraphs, headings as bold text with a bottom border.
func resumeDOCX(text, title, author string) ([]byte, error) {
	var body strings.Builder
	para := func(s string, size int, bold bool, before, after int, extra string) {
		b := ""
		if bold {
			b = "<w:b/>"
		}
		fmt.Fprintf(&body, `<w:p><w:pPr><w:spacing w:before="%d" w:after="%d"/>%s</w:pPr><w:r><w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial" w:cs="Arial"/>%s<w:sz w:val="%d"/></w:rPr><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, before, after, extra, b, size, xmlText(s))
	}
	for _, l := range parseResume(text) {
		switch l.Kind {
		case "blank":
		case "name":
			para(l.Text, 40, true, 0, 60, "")
		case "headline":
			para(l.Text, 22, false, 0, 20, "")
		case "contact":
			para(l.Text, 19, false, 0, 40, "")
		case "heading":
			para(l.Text, 23, true, 240, 80, `<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="808080"/></w:pBdr>`)
		case "role":
			para(l.Text, 21, true, 140, 40, "")
		case "bullet":
			para("•\t"+l.Text, 20, false, 0, 30, `<w:tabs><w:tab w:val="left" w:pos="284"/></w:tabs><w:ind w:left="284" w:hanging="284"/>`)
		default:
			para(l.Text, 20, false, 0, 40, "")
		}
	}
	document := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body.String() +
		`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1080" w:right="1080" w:bottom="1080" w:left="1080" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr></w:body></w:document>`
	files := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/></Relationships>`},
		{"docProps/core.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>` + xmlText(title) + `</dc:title><dc:creator>` + xmlText(author) + `</dc:creator></cp:coreProperties>`},
		{"word/document.xml", document},
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := z.Create(f.name)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write([]byte(f.body)); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fileSlug keeps letters and digits for folder names: "GoodLeap, LLC" → "GoodLeap-LLC".
func fileSlug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if r < 128 {
				b.WriteRune(r)
				dash = false
				continue
			}
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// exportResume writes "<First>-<Last>-Resume.pdf/.docx/.txt" into
// applications/<job id>-<company>/ next to the database, replacing older
// exports, and returns the paths.
func exportResume(dir, name, company string, jobID int64, text string) (map[string]string, error) {
	folder := filepath.Join(dir, "applications", fmt.Sprintf("%d-%s", jobID, fileSlug(company)))
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return nil, err
	}
	base := fileSlug(name)
	if base == "" {
		base = "Resume"
	} else {
		base += "-Resume"
	}
	docx, err := resumeDOCX(text, name+" résumé", name)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for ext, data := range map[string][]byte{".pdf": resumePDF(text, name+" résumé", name), ".docx": docx, ".txt": []byte(strings.ReplaceAll(text, "\n", "\r\n"))} {
		p := filepath.Join(folder, base+ext)
		tmp := p + ".tmp"
		if err = os.WriteFile(tmp, data, 0o644); err != nil {
			return nil, err
		}
		if err = os.Rename(tmp, p); err != nil {
			return nil, fmt.Errorf("could not replace %s (is it open in another program?): %w", p, err)
		}
		out[strings.TrimPrefix(ext, ".")] = p
	}
	return out, nil
}
