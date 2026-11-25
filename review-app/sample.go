package main

import (
	"database/sql"
	"fmt"
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
		if s.appStatus == "" {
			continue
		}
		jobID, _ := res.LastInsertId()
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
