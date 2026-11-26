package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Sample jobs let a first-time visitor to the browser build try the app
// before bringing their own data. The employers are invented and the links
// go to example.com.

type sampleJob struct {
	company, title, location, arrangement, industry, skills, fit, gaps, priority, notes string
	salaryMin, salaryMax                                                                int
	posted                                                                              int // days ago
	role, quals, place, pay, open                                                       int // score parts, out of 30, 25, 20, 15, 10
	appStatus                                                                           string
	applied                                                                             int // days ago, for an application
}

var sampleJobs = []sampleJob{
	{company: "Brightwater Analytics", title: "Senior Backend Engineer (Go)", location: "Remote, United States", arrangement: "Remote", industry: "Data and analytics",
		skills: "Go; PostgreSQL; Kubernetes; gRPC", fit: "Go services and Postgres match recent work; the team owns its own on-call.", gaps: "Ask how much of the role is migration work.",
		priority: "High", notes: "Small platform team, reports to the head of engineering.", salaryMin: 165000, salaryMax: 195000, posted: 3, role: 27, quals: 22, place: 20, pay: 13, open: 10},
	{company: "Pinecrest Health", title: "Full Stack Engineer, Patient Tools", location: "Denver, CO", arrangement: "Hybrid", industry: "Health care",
		skills: "TypeScript; React; Go; accessibility", fit: "Accessible front ends and a Go API, both in recent projects.", gaps: "Three days a week in the office; check the commute.",
		priority: "Medium", salaryMin: 140000, salaryMax: 170000, posted: 9, role: 23, quals: 20, place: 12, pay: 11, open: 9, appStatus: "Applied", applied: 6},
	{company: "Juniper Freight", title: "Platform Engineer", location: "Remote, US time zones", arrangement: "Remote", industry: "Logistics",
		skills: "Terraform; AWS; Go; observability", fit: "Infrastructure as code and service tooling.", gaps: "Posting asks for AWS certification as a plus.",
		priority: "Medium", salaryMin: 150000, salaryMax: 175000, posted: 14, role: 22, quals: 18, place: 20, pay: 12, open: 7},
	{company: "Civic Lantern", title: "Software Engineer, Public Data", location: "Remote", arrangement: "Remote", industry: "Government technology",
		skills: "Python; Go; SQL; open data", fit: "Mission-driven team publishing open data; SQL-heavy work.", gaps: "Pay range is lower than the others.",
		priority: "High", salaryMin: 125000, salaryMax: 145000, posted: 5, role: 25, quals: 21, place: 20, pay: 8, open: 10, appStatus: "Interviewing", applied: 15},
	{company: "Copperleaf Energy", title: "Staff Engineer, Grid Software", location: "Austin, TX", arrangement: "On-site", industry: "Energy",
		skills: "Rust; Go; distributed systems; time series", fit: "Distributed systems depth; time-series storage is new.", gaps: "Staff scope: confirm the expectations for leading without managing.",
		priority: "Low", salaryMin: 190000, salaryMax: 230000, posted: 21, role: 20, quals: 15, place: 6, pay: 15, open: 6},
	{company: "Tidewater Learning", title: "Backend Engineer, Courses", location: "Remote, North America", arrangement: "Remote", industry: "Education",
		skills: "Go; MySQL; REST APIs; caching", fit: "API and caching work close to past projects.", gaps: "Contract to hire for the first six months.",
		priority: "Medium", salaryMin: 60, salaryMax: 75, posted: 2, role: 21, quals: 22, place: 20, pay: 9, open: 10},
}

// sampleProfiles give each sample job its company's headquarters, where
// the role can be done from and what it asks for, in sampleJobs order.
// Requirements are "kind:Name", with a trailing "?" for preferred and a
// trailing "!" for one the sample profile lacks.
type sampleProfile struct {
	hq        [3]string // city, region, country
	size, own string
	places    []placeIn
	reqs      []string
	years     int
	degree    string
	level     string
}

var sampleProfiles = []sampleProfile{
	{[3]string{"Seattle", "WA", "US"}, "51-200", "venture", []placeIn{{Kind: "remote", Country: "US"}},
		[]string{"language:Go", "platform:PostgreSQL", "platform:Kubernetes!", "practice:gRPC", "practice:On-call?"}, 5, "none", "senior"},
	{[3]string{"Denver", "CO", "US"}, "201-500", "private", []placeIn{{Kind: "office", Country: "US", Region: "CO", City: "Denver"}},
		[]string{"language:TypeScript", "framework:React", "language:Go", "practice:Accessibility", "domain:Healthcare?!"}, 3, "equivalent", "mid"},
	{[3]string{"Chicago", "IL", "US"}, "1001-5000", "public", []placeIn{{Kind: "remote", Country: "US"}},
		[]string{"tool:Terraform", "platform:AWS", "language:Go", "practice:Observability", "certification:AWS Certified Solutions Architect?!"}, 4, "bachelor", "mid"},
	{[3]string{"Washington", "DC", "US"}, "11-50", "nonprofit", []placeIn{{Kind: "remote", Country: "US", Region: "DC"}, {Kind: "remote", Country: "US", Region: "MD"}, {Kind: "remote", Country: "US", Region: "VA"}},
		[]string{"language:Python", "language:Go", "language:SQL", "domain:Open data?"}, 2, "none", "mid"},
	{[3]string{"Austin", "TX", "US"}, "501-1000", "private", []placeIn{{Kind: "office", Country: "US", Region: "TX", City: "Austin"}},
		[]string{"language:Rust!", "language:Go", "practice:Distributed systems", "platform:Time-series databases?!"}, 8, "bachelor", "staff"},
	{[3]string{"Vancouver", "BC", "CA"}, "201-500", "venture", []placeIn{{Kind: "remote", Country: "NORTH_AMERICA"}},
		[]string{"language:Go", "platform:MySQL", "practice:REST APIs", "practice:Caching?"}, 3, "preferred", "mid"},
}

// loadSampleJobs fills an empty database with the sample jobs and two
// applications. It refuses a database that already has jobs.
func loadSampleJobs(db *sql.DB) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow("SELECT count(*) FROM jobs").Scan(&n); err != nil {
		return 0, err
	}
	if n > 0 {
		return 0, fmt.Errorf("sample jobs go only into an empty tracker; this one has %d jobs", n)
	}
	day := func(ago int) string { return time.Now().AddDate(0, 0, -ago).Format("2006-01-02") }
	today := day(0)
	for i, s := range sampleJobs {
		period := "year"
		if s.salaryMin < 1000 {
			period = "hour"
		}
		total := s.role + s.quals + s.place + s.pay + s.open
		confidence := "Medium"
		if total >= 85 {
			confidence = "High"
		}
		status := "Research"
		if s.appStatus != "" {
			status = s.appStatus
		}
		res, err := tx.Exec(`INSERT INTO jobs(company, title, url, location, work_arrangement, employment_type, industry, skills_required, fit_notes, gaps_questions,
			priority, notes, salary_min, salary_max, currency, salary_period, posting_date, date_found, last_checked, posting_status, status, source,
			score_total, score_role, score_qualifications, score_location, score_pay, score_availability, score_confidence, score_explanation, score_as_of)
			VALUES(?,?,?,?,?,'Full-time',?,?,?,?,?,?,?,?,'USD',?,?,?,?,'Open - sample listing',?,'Sample data',?,?,?,?,?,?,?,?,?)`,
			s.company, s.title, fmt.Sprintf("https://example.com/jobs/sample-%d", i+1), s.location, s.arrangement, s.industry, s.skills, s.fit, s.gaps,
			s.priority, nullIfEmpty(s.notes), s.salaryMin, s.salaryMax, period, day(s.posted), day(s.posted/2), today, status,
			total, s.role, s.quals, s.place, s.pay, s.open, confidence, "A sample score: role "+fmt.Sprint(s.role)+"/30, qualifications "+fmt.Sprint(s.quals)+"/25.", today)
		if err != nil {
			return 0, err
		}
		jobID, _ := res.LastInsertId()
		if i < len(sampleProfiles) {
			if err = saveSampleProfile(tx, jobID, s.company, today, sampleProfiles[i]); err != nil {
				return 0, err
			}
		}
		if s.appStatus == "" {
			continue
		}
		app, err := tx.Exec(`INSERT INTO applications(job_id, status, applied_date, follow_up_date, notes) VALUES(?,?,?,?,?)`,
			jobID, s.appStatus, day(s.applied), day(s.applied-8), "Sample application.")
		if err != nil {
			return 0, err
		}
		appID, _ := app.LastInsertId()
		if _, err = tx.Exec(`INSERT INTO communications(application_id, occurred_at, channel, direction, contact, notes) VALUES(?,?,'Email','Received','Recruiter','Thanks for applying; we will be in touch.')`,
			appID, day(s.applied-1)+"T10:00"); err != nil {
			return 0, err
		}
	}
	return len(sampleJobs), tx.Commit()
}

func saveSampleProfile(tx *sql.Tx, jobID int64, company, today string, p sampleProfile) error {
	if _, err := tx.Exec(`INSERT INTO companies(name, hq_city, hq_region, hq_country, size_band, ownership) VALUES(?,?,?,?,?,?) ON CONFLICT(name) DO NOTHING`,
		company, p.hq[0], p.hq[1], p.hq[2], p.size, p.own); err != nil {
		return err
	}
	for _, l := range p.places {
		if _, err := tx.Exec(`INSERT INTO job_locations(job_id, kind, country, region, city) VALUES(?,?,?,?,?)`, jobID, l.Kind, l.Country, l.Region, l.City); err != nil {
			return err
		}
	}
	for _, r := range p.reqs {
		kind, name, _ := strings.Cut(r, ":")
		has := "yes"
		if strings.HasSuffix(name, "!") {
			name, has = strings.TrimSuffix(name, "!"), "no"
		}
		required := !strings.HasSuffix(name, "?")
		name = strings.TrimSuffix(name, "?")
		if _, err := tx.Exec(`INSERT INTO job_requirements(job_id, kind, name, required, owner_has) VALUES(?,?,?,?,?)`, jobID, kind, name, required, has); err != nil {
			return err
		}
	}
	_, err := tx.Exec(`UPDATE jobs SET years_min=?, degree_level=?, seniority_level=?, profiled_at=? WHERE id=?`, p.years, p.degree, p.level, today, jobID)
	return err
}
