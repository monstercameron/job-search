package main

import (
	"fmt"
	"regexp"
	"strings"
)

// atsRules is what the agent is told before writing a tailored résumé. They
// follow current applicant-tracking-system practice (Workday, Greenhouse,
// Lever, Ashby, iCIMS parse text in reading order and match the posting's
// own wording) plus the user's accuracy rules, which outrank everything here.
// Sources: Jobscan, "The Ultimate ATS Guide" (dates, acronyms, headings,
// margins, fonts, PDF) and Lever, "Understanding Resume Parsing" (regional
// locations are matched to the nearest structured city).
var atsRules = []string{
	"Truth first: use only facts in the profile (experience, skills, projects, accuracy notes). Never invent employers, titles, dates, figures, degrees or skills. A keyword you cannot back with a real line in the profile stays out.",
	"Mirror the posting's exact wording where it is true: if the job says \"TypeScript\" and \"LLM APIs\", use those words, not only synonyms. Put the most important required skills in the summary, the skills section and at least one bullet each.",
	"Spell out each acronym once next to its short form, for example \"large language model (LLM)\" and \"continuous integration (CI)\": some parsers, Lever and Taleo among them, index only the form that is written.",
	"Use standard section headings only: SUMMARY, WORK EXPERIENCE, SKILLS, PROJECTS, and EDUCATION only when the Profile puts education on the résumé. ATS parsers look for these words; creative headings get misfiled.",
	"One column, reverse-chronological. No tables, text boxes, columns, icons, photos, graphics, skill bars, or contact details in a header or footer.",
	"Each role line: \"Title, Company (Team) | Mon YYYY – Mon YYYY\" with the same date format everywhere. Use \"Present\" for the current role. Never a year alone: parsers compute years of experience from these dates.",
	"Bullets start with a strong past- or present-tense verb, say what you built or changed and with what technology, and stay under about two lines (230 characters). No first person (\"I\", \"my\").",
	"Lead each role with the bullets most relevant to this posting; cut or shorten the rest. 3 to 6 bullets for recent roles, 1 to 3 for older ones.",
	"Summary: 2 to 4 lines naming the target role, years of experience and the posting's top requirements you actually meet.",
	"Length: one page if it fits, never more than two (roughly 450 to 800 words).",
	"Plain characters only: letters, digits, standard punctuation, en dashes. No emoji or decorative bullets; the renderer adds bullets.",
	"Contact line: location as a real city and state (not a county or metro area, which Lever maps to the nearest city and can misplace), email, phone, website, GitHub, LinkedIn. Never X or Twitter.",
	"No keyword stuffing, hidden or white text, or pasted job description. Recruiters read the result, and many systems flag it.",
	"Export is a text-based PDF and DOCX from this text; upload the PDF unless the form asks for Word.",
}

var (
	degreeRe  = regexp.MustCompile(`(?i)\bbachelor|\bmaster'?s\s+(degree|of|in)\b|\bassociate'?s?\s+degree|\b(b\.s\.|b\.a\.|m\.s\.|a\.s\.|a\.a\.|ph\.d)|\bmba\b|\bdegree\s+in\b|\bgraduated\b`)
	twitterRe = regexp.MustCompile(`(?i)\b(x\.com|twitter)\b`)
	figureRe  = regexp.MustCompile(`\$\s?\d[\d,.]*\s?[kKmMbB]?|\d[\d,.]*\s?%|\b\d{1,3}(,\d{3})+\b|\b\d+\.\d+[xX]\b|\b\d+[xX]\b`)
	emailRe   = regexp.MustCompile(`[^\s@|]+@[^\s@|]+\.[a-zA-Z]{2,}`)
	phoneRe   = regexp.MustCompile(`\(?\d{3}\)?[\s.-]?\d{3}[\s.-]?\d{4}`)
	pronounRe = regexp.MustCompile(`(?i)(^|\s)(i|my|me|i'm|i've)\s`)
	regionRe  = regexp.MustCompile(`(?i)\b(county|metro(politan)?|area|region)\b`)
	acronymRe = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,5}\b`)
	yearOnly  = regexp.MustCompile(`^(19|20)\d{2}$`)
)

// wellKnownAcronyms are searched in their short form, so they need no long form.
var wellKnownAcronyms = map[string]bool{
	"AI": true, "API": true, "SQL": true, "HTML": true, "CSS": true, "REST": true, "JSON": true, "XML": true,
	"HTTP": true, "HTTPS": true, "URL": true, "UI": true, "UX": true, "IT": true, "HR": true, "US": true,
	"USA": true, "PDF": true, "NET": true, "AWS": true, "CI": true, "CD": true, "QA": true, "IDE": true,
	"CEO": true, "CTO": true, "VP": true, "OS": true, "CPU": true, "GPU": true, "PHP": true,
}

// educationOnResume is true when the Profile puts at least one education
// entry on the résumé. Without one the résumé may hold no education section
// and no degree, so a tailored version cannot claim schooling the owner left
// off on purpose or does not have.
func educationOnResume(p ProfilePage) bool {
	for _, e := range p.Education {
		if e.OnResume {
			return true
		}
	}
	return false
}

// employerAcronym is true when an all-caps word is part of an employer or
// title in the Profile's experience, such as a company that goes by its
// initials; those need no spelling out.
func employerAcronym(a string, p ProfilePage) bool {
	for _, e := range p.Experience {
		if strings.Contains(e.Org, a) || strings.Contains(e.Title, a) {
			return true
		}
	}
	return false
}

// roleDates returns the date range after the last " | " of a role line, split
// into its start and end.
func roleDates(line string) []string {
	i := strings.LastIndex(line, "|")
	if i < 0 {
		return nil
	}
	return strings.FieldsFunc(line[i+1:], func(r rune) bool { return r == '–' || r == '-' })
}

var standardHeadings = map[string]bool{
	"SUMMARY": true, "PROFESSIONAL SUMMARY": true, "EXPERIENCE": true, "WORK EXPERIENCE": true,
	"PROFESSIONAL EXPERIENCE": true, "SKILLS": true, "TECHNICAL SKILLS": true, "PROJECTS": true,
	"SELECTED PROJECTS": true, "CERTIFICATIONS": true, "AWARDS": true, "VOLUNTEER EXPERIENCE": true,
	"TEACHING": true, "PUBLICATIONS": true,
}

// ResumeCheck is the result of checking a tailored résumé.
type ResumeCheck struct {
	OK                 bool     `json:"ok"`
	Errors             []string `json:"errors"`
	Warnings           []string `json:"warnings"`
	Words              int      `json:"words"`
	MatchedSkills      []string `json:"matched_job_skills"`
	MissingInProfile   []string `json:"missing_but_in_profile"`
	MissingNotProfile  []string `json:"missing_and_not_in_profile"`
	PreferredMatched   []string `json:"matched_preferred_skills"`
	PreferredInProfile []string `json:"preferred_missing_but_in_profile"`
	Pages              int      `json:"pdf_pages"`
}

// profileCorpus is every fact the profile holds, on or off the résumé, lower-cased.
func profileCorpus(p ProfilePage) string {
	var b strings.Builder
	for _, k := range []string{"name", "headline", "location", "summary", "target_roles", "accuracy_notes"} {
		b.WriteString(p.F[k] + "\n")
	}
	for _, list := range [][]ProfileEntry{p.Experience, p.Skills, p.Projects, p.Education} {
		for _, e := range list {
			fmt.Fprintf(&b, "%s\n%s\n%s\n%s %s\n%s\n", e.Title, e.Org, e.Place, e.Start, e.End, e.Bullets)
			fmt.Fprintf(&b, "%s\n", dateRange(e.Start, e.End))
		}
	}
	return strings.ToLower(b.String())
}

func splitSkills(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == '\n' || r == ',' }) {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// checkResume applies the hard accuracy rules (errors block saving) and the
// ATS guidance (warnings), and reports which of the job's skills it covers.
func checkResume(text string, p ProfilePage, required, preferred string) ResumeCheck {
	c := ResumeCheck{Errors: []string{}, Warnings: []string{}, MatchedSkills: []string{}, MissingInProfile: []string{}, MissingNotProfile: []string{}, PreferredMatched: []string{}, PreferredInProfile: []string{}}
	errf := func(f string, a ...any) { c.Errors = append(c.Errors, fmt.Sprintf(f, a...)) }
	warnf := func(f string, a ...any) { c.Warnings = append(c.Warnings, fmt.Sprintf(f, a...)) }
	lines := parseResume(text)
	lower := strings.ToLower(text)
	corpus := profileCorpus(p)
	c.Words = len(strings.Fields(text))

	if strings.TrimSpace(text) == "" {
		errf("The résumé is empty.")
		return c
	}
	headings := map[string]bool{}
	header := ""
	section := ""
	for _, l := range lines {
		switch l.Kind {
		case "name":
			if n := strings.TrimSpace(p.F["name"]); n != "" && !strings.EqualFold(l.Text, n) {
				warnf("The first line should be your name exactly as in the profile (%q), so parsers file it as the name.", n)
			}
			header += l.Text + "\n"
		case "headline":
			header += l.Text + "\n"
		case "contact":
			header += l.Text + "\n"
			if loc := strings.TrimSpace(strings.Split(l.Text, "|")[0]); regionRe.MatchString(loc) {
				warnf("The contact line gives a region (%q). Use a real city and state, such as \"Austin, TX\": Lever matches regions to the nearest city it knows and can misplace you.", loc)
			}
		case "heading":
			section = l.Text
			headings[l.Text] = true
			if strings.Contains(l.Text, "EDUCATION") && !educationOnResume(p) {
				errf("Remove the %s section: your Profile keeps education off the résumé, so it has no education section and lists no degree.", l.Text)
			} else if !standardHeadings[l.Text] {
				warnf("%q is not a standard heading; ATS parsers expect SUMMARY, EXPERIENCE, SKILLS or PROJECTS.", l.Text)
			}
		case "bullet":
			if len(l.Text) > 230 {
				warnf("Shorten the bullet starting %q (%d characters; keep under about 230).", clip(l.Text, 50), len(l.Text))
			}
			if pronounRe.MatchString(" " + l.Text + " ") {
				warnf("Drop first person from the bullet starting %q.", clip(l.Text, 50))
			}
		case "role":
			for _, d := range roleDates(l.Text) {
				if yearOnly.MatchString(strings.TrimSpace(d)) {
					warnf("The role line %q gives a year without a month. Use \"Mon YYYY\" on every role; parsers compute years of experience from these dates. Add the month to the Profile if you know it.", clip(l.Text, 80))
					break
				}
			}
			if strings.Contains(section, "EXPERIENCE") && !roleInProfile(l.Text, p) {
				warnf("The role line %q does not match a company in your profile's experience. Check it is real and spelled as in the profile.", clip(l.Text, 80))
			}
		}
	}
	var short []string
	shortSeen := map[string]bool{}
	for _, l := range lines {
		if l.Kind != "bullet" && l.Kind != "text" {
			continue
		}
		for _, a := range acronymRe.FindAllString(l.Text, -1) {
			if wellKnownAcronyms[a] || employerAcronym(a, p) || shortSeen[a] || strings.Contains(text, "("+a+")") || strings.Contains(text, a+" (") {
				continue
			}
			shortSeen[a] = true
			short = append(short, a)
		}
	}
	if len(short) > 0 {
		warnf("Spell out once, next to the short form: %s. For example \"large language model (LLM)\"; some parsers index only the form that is written.", strings.Join(short, ", "))
	}
	if m := degreeRe.FindString(text); m != "" && !educationOnResume(p) {
		errf("Remove %q: your Profile has no education on the résumé, so never claim or imply a degree.", m)
	}
	if m := twitterRe.FindString(text); m != "" {
		errf("Remove %q: never list or link your X account.", m)
	}
	for _, r := range text {
		if !encodable(r) && r != '\n' && r != '\r' && r != '\t' {
			errf("Replace the character %q: it cannot be rendered in the PDF and may confuse parsers. Use plain punctuation.", string(r))
			break
		}
	}
	if strings.Contains(text, "\t") || regexp.MustCompile(`\S {3,}\S`).MatchString(text) {
		warnf("Tabs or runs of spaces look like a table or columns. Use one column; parsers read across columns and scramble them.")
	}
	if !headings["EXPERIENCE"] && !headings["WORK EXPERIENCE"] && !headings["PROFESSIONAL EXPERIENCE"] {
		errf("Add an EXPERIENCE section heading on its own line.")
	}
	if !headings["SKILLS"] && !headings["TECHNICAL SKILLS"] {
		warnf("Add a SKILLS section; ATS keyword matching leans on it.")
	}
	if !emailRe.MatchString(header) {
		errf("Put your email on the contact line under your name (not only in a footer).")
	}
	if !phoneRe.MatchString(header) {
		if strings.TrimSpace(p.F["phone"]) == "" {
			warnf("No phone number: add one to the Profile (Contact details); most application forms and recruiters expect it on the résumé.")
		} else {
			warnf("Add your phone number (%s) to the contact line.", p.F["phone"])
		}
	}
	pages, last := pdfPages(text)
	c.Pages = pages
	switch {
	case pages > 2:
		warnf("The PDF runs to %d pages. Cut to two at most: drop older or less relevant bullets and projects.", pages)
	case pages == 2 && last <= 8:
		warnf("Only %d line(s) spill onto page 2. Trim a bullet or two so it fits on one page.", last)
	}
	switch {
	case c.Words > 850:
		warnf("%d words is likely over two pages. Cut older or less relevant bullets (aim for 450 to 800).", c.Words)
	case c.Words < 250:
		warnf("%d words is thin for a senior role. Add relevant bullets from the profile (aim for 450 to 800).", c.Words)
	}
	seen := map[string]bool{}
	for _, f := range figureRe.FindAllString(text, -1) {
		key := strings.ToLower(strings.TrimSpace(f))
		if seen[key] {
			continue
		}
		seen[key] = true
		if !strings.Contains(corpus, key) && !strings.Contains(corpus, strings.ReplaceAll(key, ",", "")) {
			warnf("The figure %q is not in your profile. Use only figures you can back up and are allowed to share.", f)
		}
	}
	for _, s := range splitSkills(required) {
		switch {
		case anyAlternative(lower, s):
			c.MatchedSkills = append(c.MatchedSkills, s)
		case anyAlternative(corpus, s):
			c.MissingInProfile = append(c.MissingInProfile, s)
		default:
			c.MissingNotProfile = append(c.MissingNotProfile, s)
		}
	}
	for _, s := range splitSkills(preferred) {
		if anyAlternative(lower, s) {
			c.PreferredMatched = append(c.PreferredMatched, s)
		} else if anyAlternative(corpus, s) {
			c.PreferredInProfile = append(c.PreferredInProfile, s)
		}
	}
	if len(c.MissingInProfile) > 0 {
		warnf("Required skills your profile supports but this résumé omits: %s. Add them where they are true.", strings.Join(c.MissingInProfile, "; "))
	}
	c.OK = len(c.Errors) == 0
	return c
}

var alternativeSplit = regexp.MustCompile(`(?i)\s+(and/or|or|and)\s+|/|\(|\)`)

// anyAlternative matches a posting phrase such as "TypeScript and/or Python"
// or "AWS or equivalent cloud" when the whole phrase or any one alternative
// appears as a whole term in text (already lower-cased).
func anyAlternative(text, phrase string) bool {
	p := strings.ToLower(strings.TrimSpace(phrase))
	if containsTerm(text, p) {
		return true
	}
	for _, alt := range alternativeSplit.Split(p, -1) {
		alt = strings.TrimSpace(alt)
		if len(alt) >= 2 && alt != "equivalent" && containsTerm(text, alt) {
			return true
		}
	}
	return false
}

func roleInProfile(line string, p ProfilePage) bool {
	l := strings.ToLower(line)
	for _, e := range p.Experience {
		for _, org := range strings.FieldsFunc(e.Org, func(r rune) bool { return r == ',' || r == '(' || r == ')' }) {
			if o := strings.ToLower(strings.TrimSpace(org)); len(o) >= 3 && strings.Contains(l, o) {
				return true
			}
		}
	}
	return false
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
