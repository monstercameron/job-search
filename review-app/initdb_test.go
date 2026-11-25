package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// A first run with -init must give a database every page and tool can use.
func TestInitJobsBuildsAWorkingFreshDatabase(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "fresh.sqlite3"))+"?mode=rwc")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < 2; i++ { // twice: -init must be safe on an existing database
		for _, step := range []func(*sql.DB) error{initJobs, migrateTracker, migrateIndustry, migrateProfile, migrateNextUp, migrateMetrics} {
			if err = step(db); err != nil {
				t.Fatalf("pass %d: %v", i+1, err)
			}
		}
	}
	if _, err = db.Exec(`INSERT INTO jobs(company,title,url,date_found,posting_date) VALUES('Acme','Engineer','https://example.com/1','2026-11-24','2026-11-20')`); err != nil {
		t.Fatal(err)
	}
	p, err := loadNextPage(db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 1 || p.Job.Title != "Engineer" || p.Job.Posted == "" {
		t.Errorf("next up on a fresh database: total %d, job %q, posted %q", p.Total, p.Job.Title, p.Job.Posted)
	}
}

// The education rule follows the Profile: with no education entry on the
// résumé, a degree or an education section is refused; with one, both pass.
func TestEducationRuleFollowsTheProfile(t *testing.T) {
	text := goodResume + "\nEDUCATION\nB.S. Computer Science, State University\n"
	hasEducationError := func(c ResumeCheck) bool {
		for _, e := range c.Errors {
			if strings.Contains(e, "EDUCATION") || strings.Contains(e, "B.S.") {
				return true
			}
		}
		return false
	}
	without := ProfilePage{F: map[string]string{"name": "Alex Rivera"}, Education: []ProfileEntry{{Title: "B.S. Computer Science", OnResume: false}}}
	if !hasEducationError(checkResume(text, without, "", "")) {
		t.Error("education kept off the résumé, but a degree was allowed")
	}
	with := ProfilePage{F: map[string]string{"name": "Alex Rivera"}, Education: []ProfileEntry{{Title: "B.S. Computer Science", OnResume: true}}}
	if c := checkResume(text, with, "", ""); hasEducationError(c) {
		t.Errorf("education is on the résumé, but it was refused: %v", c.Errors)
	}
}

// An employer that goes by its initials needs no spelling out.
func TestEmployerInitialsAreNotFlaggedAsAcronyms(t *testing.T) {
	// Without the "NWT (Routing Platform)" role line, which already
	// reads as spelled out, the bullet's "NWT" is a bare acronym.
	text := strings.Replace(goodResume, "NWT (Routing Platform)", "NWT", 1)
	flagged := func(p ProfilePage) bool {
		for _, w := range checkResume(text, p, "", "").Warnings {
			if strings.Contains(w, "Spell out") && strings.Contains(w, "NWT") {
				return true
			}
		}
		return false
	}
	if !flagged(ProfilePage{F: map[string]string{"name": "Alex Rivera"}}) {
		t.Fatal("NWT should be flagged when it is not an employer in the Profile")
	}
	if flagged(ProfilePage{F: map[string]string{"name": "Alex Rivera"}, Experience: []ProfileEntry{{Title: "Staff Engineer", Org: "NWT"}}}) {
		t.Error("NWT is an employer in the Profile, but it was flagged")
	}
}
