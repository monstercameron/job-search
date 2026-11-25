package main

import "database/sql"

// jobsSchema is the leads table every other table hangs off. The app normally
// opens an existing database read-write and refuses a missing file, so a
// mistyped -db path fails instead of starting an empty tracker. Run once with
// -init to create a new database; the tracker, profile, Next up and metrics
// tables are then added by the usual startup migrations.
const jobsSchema = `CREATE TABLE IF NOT EXISTS jobs (id INTEGER PRIMARY KEY, company TEXT NOT NULL, title TEXT NOT NULL,
	category TEXT, location TEXT, salary_min INTEGER, salary_max INTEGER, currency TEXT, url TEXT NOT NULL UNIQUE, notes TEXT,
	status TEXT NOT NULL DEFAULT 'Research', date_found TEXT NOT NULL, last_checked TEXT, applied_date TEXT, follow_up_date TEXT,
	work_arrangement TEXT, employment_type TEXT, seniority TEXT, city TEXT, region TEXT, country TEXT, location_eligibility TEXT,
	salary_period TEXT, compensation_notes TEXT, experience_required TEXT, education_requirement TEXT, skills_required TEXT,
	skills_preferred TEXT, responsibilities TEXT, benefits TEXT, fit_notes TEXT, gaps_questions TEXT, priority TEXT,
	posting_status TEXT, posting_date TEXT, application_deadline TEXT, source TEXT, contact TEXT, next_action TEXT,
	resume_version TEXT, updated_at TEXT, score_total INTEGER, score_role INTEGER, score_qualifications INTEGER,
	score_location INTEGER, score_pay INTEGER, score_availability INTEGER, score_confidence TEXT, score_explanation TEXT,
	score_as_of TEXT, score_version TEXT, industry TEXT)`

// initJobs creates the jobs table when it is missing. It never changes an
// existing table, so -init is safe to pass against a database in use.
func initJobs(db *sql.DB) error {
	_, err := db.Exec(jobsSchema)
	return err
}
