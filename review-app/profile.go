package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ProfileField is one single-value fact on the Profile page, such as the
// email address or the lowest acceptable salary.
type ProfileField struct {
	Key, Label, Hint, Group, Type, Auto, Value string
	Long                                       bool
}

var profileFieldDefs = []ProfileField{
	{Key: "name", Label: "Full name", Group: "basics", Auto: "name"},
	{Key: "headline", Label: "Headline", Group: "basics", Hint: "The line under your name: your title and focus."},
	{Key: "location", Label: "Location", Group: "basics", Auto: "address-level2"},
	{Key: "email", Label: "Email", Group: "basics", Type: "email", Auto: "email"},
	{Key: "phone", Label: "Phone", Group: "basics", Type: "tel", Auto: "tel"},
	{Key: "mailing_address", Label: "Mailing address", Group: "basics", Auto: "street-address", Hint: "For application forms only; never copied into the résumé text."},
	{Key: "website", Label: "Website", Group: "basics", Auto: "url"},
	{Key: "github", Label: "GitHub", Group: "basics"},
	{Key: "linkedin", Label: "LinkedIn", Group: "basics"},
	{Key: "devpost", Label: "Devpost", Group: "basics", Hint: "Shown on this page only; not copied into the résumé text."},
	{Key: "youtube", Label: "YouTube", Group: "basics", Hint: "Shown on this page only; not copied into the résumé text."},
	{Key: "summary", Label: "Summary", Group: "summary", Long: true, Hint: "Three or four sentences: who you are, what you build, what you want next."},
	{Key: "target_roles", Label: "Roles you want", Group: "search", Long: true},
	{Key: "avoid_roles", Label: "Roles to avoid", Group: "search"},
	{Key: "workplace", Label: "Workplace", Group: "search", Hint: "Remote, hybrid, onsite, or a mix."},
	{Key: "relocation", Label: "Relocation", Group: "search"},
	{Key: "commute", Label: "Longest commute", Group: "search"},
	{Key: "current_pay", Label: "Current pay", Group: "search", Long: true, Hint: "For comparing offers. Private: never copied into the résumé text."},
	{Key: "salary_floor", Label: "Lowest base salary", Group: "search"},
	{Key: "contract", Label: "Contract work", Group: "search"},
	{Key: "authorization", Label: "Work authorization", Group: "search"},
	{Key: "availability", Label: "Can start", Group: "search"},
	{Key: "accuracy_notes", Label: "Claims to keep accurate", Group: "accuracy", Long: true, Hint: "One per line. Shown beside your profile as a reminder when you tailor a résumé."},
}

// ProfileEntry is one repeatable item: a role, a skill group, a project, or
// a school or course. Bullets holds one line per bullet (for skills, a
// comma-separated list).
type ProfileEntry struct {
	ID                                          int64
	Section, Title, Org, Place, Start, End, URL string
	Bullets                                     string
	OnResume, First, Last                       bool
}

// profileSections names each repeatable section once, in page order.
var profileSections = []struct{ Key, Title, Noun string }{
	{"experience", "Experience", "role"},
	{"skill", "Skills", "skill group"},
	{"project", "Projects", "project"},
	{"education", "Education and training", "entry"},
}

func sectionNoun(section string) string {
	for _, s := range profileSections {
		if s.Key == section {
			return s.Noun
		}
	}
	return ""
}

type ProfilePage struct {
	Nav                                     Nav
	F                                       map[string]string
	Basics, Summary, Search, Accuracy       []ProfileField
	Experience, Skills, Projects, Education []ProfileEntry
	Saved, SavedText, Text                  string
	HasBase                                 bool
	Tips                                    []string
	Notes                                   []string
	Fragment                                bool
}

var profileSaved = map[string]string{
	"basics": "Contact details saved.", "summary": "Summary saved.", "search": "Search preferences saved.",
	"accuracy": "Notes saved.", "experience": "Experience saved.", "skill": "Skills saved.",
	"project": "Projects saved.", "education": "Education saved.",
}

func migrateProfile(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS profile_fields (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
 CREATE TABLE IF NOT EXISTS profile_entries (id INTEGER PRIMARY KEY, section TEXT NOT NULL, position INTEGER NOT NULL DEFAULT 0, title TEXT NOT NULL DEFAULT '', org TEXT NOT NULL DEFAULT '', place TEXT NOT NULL DEFAULT '', start_date TEXT NOT NULL DEFAULT '', end_date TEXT NOT NULL DEFAULT '', url TEXT NOT NULL DEFAULT '', bullets TEXT NOT NULL DEFAULT '', on_resume INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
 CREATE INDEX IF NOT EXISTS idx_profile_entries_section ON profile_entries(section, position);`)
	return err
}

func loadProfile(db *sql.DB) (ProfilePage, error) {
	p := ProfilePage{F: map[string]string{}}
	rows, err := db.Query("SELECT key, value FROM profile_fields")
	if err != nil {
		return p, err
	}
	for rows.Next() {
		var k, v string
		if err = rows.Scan(&k, &v); err != nil {
			rows.Close()
			return p, err
		}
		p.F[k] = v
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return p, err
	}
	for _, f := range profileFieldDefs {
		f.Value = p.F[f.Key]
		switch f.Group {
		case "basics":
			p.Basics = append(p.Basics, f)
		case "summary":
			p.Summary = append(p.Summary, f)
		case "search":
			p.Search = append(p.Search, f)
		case "accuracy":
			p.Accuracy = append(p.Accuracy, f)
		}
	}
	p.Notes = splitLines(p.F["accuracy_notes"])
	erows, err := db.Query(`SELECT id, section, title, org, place, start_date, end_date, url, bullets, on_resume FROM profile_entries ORDER BY section, position, id`)
	if err != nil {
		return p, err
	}
	defer erows.Close()
	for erows.Next() {
		var e ProfileEntry
		if err = erows.Scan(&e.ID, &e.Section, &e.Title, &e.Org, &e.Place, &e.Start, &e.End, &e.URL, &e.Bullets, &e.OnResume); err != nil {
			return p, err
		}
		switch e.Section {
		case "experience":
			p.Experience = append(p.Experience, e)
		case "skill":
			p.Skills = append(p.Skills, e)
		case "project":
			p.Projects = append(p.Projects, e)
		case "education":
			p.Education = append(p.Education, e)
		}
	}
	if err = erows.Err(); err != nil {
		return p, err
	}
	for _, list := range [][]ProfileEntry{p.Experience, p.Skills, p.Projects, p.Education} {
		if len(list) > 0 {
			list[0].First = true
			list[len(list)-1].Last = true
		}
	}
	p.Text = profileText(p)
	return p, nil
}

// profileText renders the profile as plain résumé text, leaving out entries
// marked as not for the résumé.
func profileText(p ProfilePage) string {
	var b strings.Builder
	line := func(s string) { b.WriteString(strings.TrimRight(s, " ") + "\n") }
	if p.F["name"] != "" {
		line(p.F["name"])
	}
	if p.F["headline"] != "" {
		line(p.F["headline"])
	}
	var contact []string
	for _, k := range []string{"location", "email", "phone", "website", "github", "linkedin"} {
		if v := strings.TrimSpace(p.F[k]); v != "" {
			contact = append(contact, v)
		}
	}
	if len(contact) > 0 {
		line(strings.Join(contact, " | "))
	}
	if s := strings.TrimSpace(p.F["summary"]); s != "" {
		line("\nSUMMARY")
		line(s)
	}
	section := func(title string, list []ProfileEntry, write func(ProfileEntry)) {
		first := true
		for _, e := range list {
			if !e.OnResume {
				continue
			}
			if first {
				line("\n" + title)
				first = false
			}
			write(e)
		}
	}
	section("EXPERIENCE", p.Experience, func(e ProfileEntry) {
		head := e.Title
		if e.Org != "" {
			head += ", " + e.Org
		}
		if e.Place != "" {
			head += " (" + e.Place + ")"
		}
		if d := dateRange(e.Start, e.End); d != "" {
			head += " | " + d
		}
		line(head)
		for _, l := range splitLines(e.Bullets) {
			line("- " + l)
		}
	})
	section("SKILLS", p.Skills, func(e ProfileEntry) {
		line(e.Title + ": " + strings.Join(splitItems(e.Bullets), ", "))
	})
	section("PROJECTS", p.Projects, func(e ProfileEntry) {
		head := e.Title
		if e.URL != "" {
			head += " (" + e.URL + ")"
		}
		line(head)
		for _, l := range splitLines(e.Bullets) {
			line("- " + l)
		}
	})
	section("EDUCATION AND TRAINING", p.Education, func(e ProfileEntry) {
		head := e.Title
		if e.Org != "" {
			head += ", " + e.Org
		}
		if d := dateRange(e.Start, e.End); d != "" {
			head += " | " + d
		}
		line(head)
	})
	return strings.TrimSpace(b.String())
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "-•*"))
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func splitItems(s string) []string {
	var out []string
	depth := 0 // commas inside parentheses belong to one item: "runtimes (a, b)"
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		return depth == 0 && (r == ',' || r == '\n' || r == '·')
	}) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// parseMonth accepts "2025" or "2025-01".
func parseMonth(s string) (time.Time, bool, bool) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse("2006-01", s); err == nil {
		return t, true, true
	}
	if t, err := time.Parse("2006", s); err == nil {
		return t, false, true
	}
	return time.Time{}, false, false
}

func validMonth(s string) bool {
	_, _, ok := parseMonth(s)
	return strings.TrimSpace(s) == "" || ok
}

// fmtMonth shows "2025-01" as "Jan 2025" and "2020" as "2020".
func fmtMonth(s string) string {
	t, hasMonth, ok := parseMonth(s)
	if !ok {
		return s
	}
	if hasMonth {
		return t.Format("Jan 2006")
	}
	return t.Format("2006")
}

func dateRange(start, end string) string {
	switch {
	case start == "" && end == "":
		return ""
	case end == "":
		return fmtMonth(start) + " – Present"
	case start == "":
		return fmtMonth(end)
	}
	return fmtMonth(start) + " – " + fmtMonth(end)
}

// tenure is the length of a role, "1 yr 10 mos", counted to this month when
// the role has no end. Year-only dates are too coarse to count, so they give "".
func tenure(start, end string) string {
	s, sm, ok := parseMonth(start)
	if !ok || !sm {
		return ""
	}
	e := time.Now()
	if end != "" {
		var em bool
		if e, em, ok = parseMonth(end); !ok || !em {
			return ""
		}
	}
	months := (e.Year()-s.Year())*12 + int(e.Month()-s.Month())
	if end == "" {
		months++ // the current month counts
	}
	if months <= 0 {
		return ""
	}
	y, m := months/12, months%12
	var parts []string
	if y > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", y, map[bool]string{true: "yr", false: "yrs"}[y == 1]))
	}
	if m > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", m, map[bool]string{true: "mo", false: "mos"}[m == 1]))
	}
	return strings.Join(parts, " ")
}

// linkFor turns "github.com/x" or "www.site.com" into an https URL for display
// links; anything that is not a plain host/path is left unlinked.
func linkFor(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " <>\"'") {
		return ""
	}
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || !strings.Contains(u.Host, ".") {
		return ""
	}
	return s
}

func registerProfile(db *sql.DB) {
	http.HandleFunc("/profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		p, err := profilePage(db, r.URL.Query().Get("saved"), false)
		if err != nil {
			logTracker(err)
			http.Error(w, "Could not read the profile", 500)
			return
		}
		render(w, "profile", p)
	})

	http.HandleFunc("/profile/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		if !localPost(r) {
			http.Error(w, "Local browser origin required", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if parseForm(r) != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		fragment := isFragmentRequest(r)
		saved, err := saveProfile(db, r)
		if err != nil {
			if fe, ok := err.(formError); ok {
				if fragment {
					writeFormError(w, string(fe))
				} else {
					formProblem(w, db, string(fe), "/profile", "Back to the profile")
				}
				return
			}
			logTracker(err)
			http.Error(w, "Could not save; check the database", 500)
			return
		}
		if saved == "to-base" {
			http.Redirect(w, r, "/resume?saved=base&back=/profile#saved", http.StatusSeeOther)
			return
		}
		if fragment {
			p, err := profilePage(db, saved, true)
			if err != nil {
				logTracker(err)
				http.Error(w, "Could not read the profile", 500)
				return
			}
			writeFragments(w, "Replace-Url", "/profile", Fragment{Target: "#profile-view(innerHTML)", Name: "profileview", Data: p})
			return
		}
		http.Redirect(w, r, "/profile?saved="+saved+"#saved", http.StatusSeeOther)
	})
}

// profilePage loads everything the Profile page and its fragment need.
func profilePage(db *sql.DB, saved string, fragment bool) (ProfilePage, error) {
	p, err := loadProfile(db)
	if err != nil {
		return p, err
	}
	p.Nav = loadNav(db, "profile", "Profile")
	p.Saved = saved
	p.SavedText = profileSaved[saved]
	p.Fragment = fragment
	var base string
	if err = db.QueryRow("SELECT value FROM tracker_settings WHERE key='base_resume'").Scan(&base); err != nil && err != sql.ErrNoRows {
		return p, err
	}
	p.HasBase = strings.TrimSpace(base) != ""
	p.Tips = resumeTips(p.Text)
	return p, nil
}

type formError string

func (e formError) Error() string { return string(e) }

// saveProfile applies one Profile form and returns the section it saved,
// which is also the anchor the page returns to.
func saveProfile(db *sql.DB, r *http.Request) (string, error) {
	switch action := r.FormValue("action"); action {
	case "fields":
		group := r.FormValue("group")
		if profileSaved[group] == "" {
			return "", formError("Unknown section")
		}
		tx, err := db.Begin()
		if err != nil {
			return "", err
		}
		defer tx.Rollback()
		for _, f := range profileFieldDefs {
			if f.Group != group {
				continue
			}
			if _, err = tx.Exec(`INSERT INTO profile_fields(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`, f.Key, strings.TrimSpace(r.FormValue(f.Key))); err != nil {
				return "", err
			}
		}
		return group, tx.Commit()
	case "entry":
		section := r.FormValue("section")
		if sectionNoun(section) == "" {
			return "", formError("Unknown section")
		}
		title := strings.TrimSpace(r.FormValue("title"))
		if title == "" {
			return "", formError("The name field was blank. Give the " + sectionNoun(section) + " a name and save again.")
		}
		start, end := strings.TrimSpace(r.FormValue("start")), strings.TrimSpace(r.FormValue("end"))
		if !validMonth(start) || !validMonth(end) {
			return "", formError("A date is not in the form 2025-01 or 2025. Correct the Started or Ended field and save again.")
		}
		onResume := r.FormValue("on_resume") == "1"
		vals := []any{title, strings.TrimSpace(r.FormValue("org")), strings.TrimSpace(r.FormValue("place")), start, end, strings.TrimSpace(r.FormValue("url")), strings.TrimSpace(r.FormValue("bullets")), onResume}
		if id := positive(r.FormValue("id")); id != 0 {
			res, err := db.Exec(`UPDATE profile_entries SET title=?, org=?, place=?, start_date=?, end_date=?, url=?, bullets=?, on_resume=?, updated_at=CURRENT_TIMESTAMP WHERE id=? AND section=?`, append(vals, id, section)...)
			return section, checkedUpdate(res, err)
		}
		_, err := db.Exec(`INSERT INTO profile_entries(title, org, place, start_date, end_date, url, bullets, on_resume, section, position) VALUES(?,?,?,?,?,?,?,?,?, (SELECT COALESCE(MAX(position),0)+1 FROM profile_entries WHERE section=?))`, append(vals, section, section)...)
		return section, err
	case "delete":
		var section string
		id := positive(r.FormValue("id"))
		if err := db.QueryRow("SELECT section FROM profile_entries WHERE id=?", id).Scan(&section); err != nil {
			return "", formError("That entry no longer exists")
		}
		_, err := db.Exec("DELETE FROM profile_entries WHERE id=?", id)
		return section, err
	case "move":
		id := positive(r.FormValue("id"))
		var section string
		var pos int
		if err := db.QueryRow("SELECT section, position FROM profile_entries WHERE id=?", id).Scan(&section, &pos); err != nil {
			return "", formError("That entry no longer exists")
		}
		tx, err := db.Begin()
		if err != nil {
			return "", err
		}
		defer tx.Rollback()
		// Renumber the section first so equal positions cannot block a swap.
		rows, err := tx.Query("SELECT id FROM profile_entries WHERE section=? ORDER BY position, id", section)
		if err != nil {
			return "", err
		}
		var ids []int64
		for rows.Next() {
			var x int64
			if err = rows.Scan(&x); err != nil {
				rows.Close()
				return "", err
			}
			ids = append(ids, x)
		}
		rows.Close()
		for i, x := range ids {
			if x != id {
				continue
			}
			j := i - 1
			if r.FormValue("dir") == "down" {
				j = i + 1
			}
			if j >= 0 && j < len(ids) {
				ids[i], ids[j] = ids[j], ids[i]
			}
			break
		}
		for i, x := range ids {
			if _, err = tx.Exec("UPDATE profile_entries SET position=? WHERE id=?", i+1, x); err != nil {
				return "", err
			}
		}
		return section, tx.Commit()
	case "to-base":
		p, err := loadProfile(db)
		if err != nil {
			return "", err
		}
		if p.Text == "" {
			return "", formError("The profile is empty")
		}
		_, err = db.Exec(`INSERT INTO tracker_settings(key,value) VALUES('base_resume',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=CURRENT_TIMESTAMP`, p.Text)
		return "to-base", err
	default:
		return "", formError("Unknown action " + action)
	}
}
