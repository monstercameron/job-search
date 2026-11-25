package main

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func migrateTracker(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS applications (id INTEGER PRIMARY KEY, job_id INTEGER NOT NULL UNIQUE REFERENCES jobs(id), status TEXT NOT NULL DEFAULT 'Research', applied_date TEXT, follow_up_date TEXT, contact TEXT, notes TEXT, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
 CREATE TABLE IF NOT EXISTS interviews (id INTEGER PRIMARY KEY, application_id INTEGER NOT NULL REFERENCES applications(id), scheduled_at TEXT, kind TEXT, interviewer TEXT, outcome TEXT, notes TEXT, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
 CREATE TABLE IF NOT EXISTS communications (id INTEGER PRIMARY KEY, application_id INTEGER NOT NULL REFERENCES applications(id), occurred_at TEXT, channel TEXT, direction TEXT, contact TEXT, notes TEXT, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
 CREATE TABLE IF NOT EXISTS application_documents (id INTEGER PRIMARY KEY, application_id INTEGER NOT NULL REFERENCES applications(id), kind TEXT NOT NULL, title TEXT NOT NULL, content TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
 CREATE TABLE IF NOT EXISTS tracker_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
 CREATE TABLE IF NOT EXISTS application_events (id INTEGER PRIMARY KEY, application_id INTEGER NOT NULL REFERENCES applications(id), occurred_at TEXT NOT NULL, kind TEXT NOT NULL, detail TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS idx_interviews_application ON interviews(application_id); CREATE INDEX IF NOT EXISTS idx_communications_application ON communications(application_id); CREATE INDEX IF NOT EXISTS idx_documents_application ON application_documents(application_id); CREATE INDEX IF NOT EXISTS idx_events_application ON application_events(application_id);`)
	if err != nil {
		return err
	}
	rows, err := db.Query("PRAGMA table_info(applications)")
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, nn, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&cid, &name, &typ, &nn, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, name := range []string{"feeling", "feeling_notes"} {
		if !columns[name] {
			if _, err = db.Exec("ALTER TABLE applications ADD COLUMN " + name + " TEXT"); err != nil {
				return err
			}
		}
	}
	return nil
}

// isLocalHost reports whether a request's Host header names this machine's
// loopback interface. A DNS-rebinding page arrives with its own domain here.
func isLocalHost(hostHeader string) bool {
	host, _, err := net.SplitHostPort(hostHeader)
	if err != nil {
		host = hostHeader
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// localHostOnly refuses every request, reads included, whose Host is not
// loopback, so another website cannot read tracker data via DNS rebinding.
func localHostOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalHost(r.Host) {
			http.Error(w, "Local host required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// nullIfEmpty stores a blank form value as NULL so "IS NULL" queries work.
func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// validDate accepts an empty value or a YYYY-MM-DD date.
func validDate(s string) bool {
	if strings.TrimSpace(s) == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// Only accept writes from this browser's localhost origin; never transmit emails.
func localPost(r *http.Request) bool {
	if !isLocalHost(r.Host) {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, r.Host)
}
func positive(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	if n < 1 {
		return 0
	}
	return n
}
func checkedUpdate(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("record not found")
	}
	return nil
}

func logTracker(err error) { log.Println("tracker save:", err) }
func readRecords(db *sql.DB, q string, id int64) ([]TrackerRecord, error) {
	rows, err := db.Query(q, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []TrackerRecord
	for rows.Next() {
		var x TrackerRecord
		if err = rows.Scan(&x.ID, &x.A, &x.B, &x.C, &x.D, &x.E); err != nil {
			return nil, err
		}
		list = append(list, x)
	}
	return list, rows.Err()
}
func resumeTips(resume string) []string {
	if strings.TrimSpace(resume) == "" {
		return []string{"Paste your résumé to run these checks."}
	}
	lower := strings.ToLower(resume)
	var tips []string
	if !strings.Contains(resume, "@") {
		tips = append(tips, "Check that your email contact is included.")
	}
	if !strings.Contains(lower, "experience") && !strings.Contains(lower, "employment") {
		tips = append(tips, "Consider a clear Experience heading for work history.")
	}
	if !strings.Contains(lower, "skills") && !strings.Contains(lower, "technologies") {
		tips = append(tips, "Consider a clear Skills heading for relevant technologies.")
	}
	if !strings.Contains(lower, "education") && !strings.Contains(lower, "training") {
		tips = append(tips, "Consider an Education or Training section that accurately reflects your background.")
	}
	hasDigit := false
	for _, c := range resume {
		if c >= '0' && c <= '9' {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		tips = append(tips, "Include dates and measurable outcomes where accurate and useful.")
	}
	if len(tips) == 0 {
		tips = append(tips, "Basic headings and contact checks passed. Review your document's layout and relevance manually.")
	}
	return tips
}
func compareSkills(skills, resume string) (string, string) {
	if strings.TrimSpace(skills) == "" {
		return "", "No structured skills recorded for this job."
	}
	if strings.TrimSpace(resume) == "" {
		return "", "Add a base résumé to compare it with the job's recorded skills."
	}
	parts := strings.FieldsFunc(skills, func(r rune) bool { return r == ';' || r == '\n' || r == ',' })
	var yes, no []string
	lower := strings.ToLower(resume)
	for _, part := range parts {
		s := strings.TrimSpace(part)
		if s == "" {
			continue
		}
		if containsTerm(lower, strings.ToLower(s)) {
			yes = append(yes, s)
		} else {
			no = append(no, s)
		}
	}
	return strings.Join(yes, ", "), strings.Join(no, ", ")
}

// containsTerm reports whether term occurs in text as a whole term, not inside
// a longer word: "java" does not match "javascript", "go" does not match
// "google", and "c" does not match "c#" or "c++". A term that begins or ends
// with punctuation (".net") is not boundary-checked on that side, so ".net"
// still matches "asp.net". Both arguments must already be lower-cased.
func containsTerm(text, term string) bool {
	if term == "" {
		return false
	}
	first, _ := utf8.DecodeRuneInString(term)
	last, _ := utf8.DecodeLastRuneInString(term)
	for from := 0; from <= len(text)-len(term); {
		i := strings.Index(text[from:], term)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(term)
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		after, _ := utf8.DecodeRuneInString(text[end:])
		leftOK := start == 0 || !termRune(first) || !termRune(before)
		rightOK := end == len(text) || !termRune(last) || !termRune(after)
		if leftOK && rightOK {
			return true
		}
		_, size := utf8.DecodeRuneInString(text[start:])
		from = start + size
	}
	return false
}

// termRune is a character that continues a skill name: letters, digits, and
// the "+" and "#" of C++ and C#.
func termRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '+' || r == '#'
}
