package main

import (
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	_ "github.com/ncruces/go-sqlite3/driver"
)

//go:embed static/* static/htmlex/*
var assets embed.FS

type Job struct {
	ID          string
	Company     string
	Title       string
	Location    string
	URL         string
	Status      string
	Priority    string
	Arrangement string
	Employment  string
	Industry    string
	Salary      string
	Score       string
	Confidence  string
	Badges      []string
	Notes       string
	Details     []Detail

	// Display fields for the web pages.
	HasScore   bool
	ScoreN     int
	Parts      []ScorePart
	ScoreNote  string
	ScoreMeta  string
	Groups     []DetailGroup
	WorkType   string
	NeedsCheck bool
	Posting    string
	AppStatus  string
	FitNotes   string
	Questions  string
	Posted     string // when the employer published the listing, with its age
	Added      string // when the job was added to the tracker
	Weak       string
	Dismissed  bool // marked "Not interested" in Next up
}
type Detail struct{ Label, Value string }
type DetailGroup struct {
	Title string
	Items []Detail
}

// ScorePart is one rubric component; Max is its share of the 100 points.
type ScorePart struct {
	Label    string
	Got, Max int
}

func (p ScorePart) Pct() int {
	if p.Max <= 0 {
		return 0
	}
	return min(100, max(0, p.Got*100/p.Max))
}

type PageLink struct {
	Number  int
	URL     string
	Current bool
}
type SortOption struct{ Value, Menu, Label string }
type Page struct {
	Nav                                                         Nav
	Jobs                                                        []Job
	Count                                                       int
	Q, Status, Priority, Arrangement, Industry, Sort, Direction string
	Statuses, Priorities, Arrangements, Industries              []string
	Workplace, JobType                                          []string
	Workplaces, JobTypes                                        []Choice
	PageNumber, TotalPages, Start, End                          int
	PreviousURL, NextURL, ListURL, SortLabel                    string
	PageLinks                                                   []PageLink
	SortOptions                                                 []SortOption
	FilterCount                                                 int
	RefineCount                                                 int  // filters inside the "Filters and sort" disclosure
	Fragment                                                    bool // answering an HTMLeX request: focus the results heading
}

var scoreFields = []struct {
	key, label string
	max        int
}{{"score_role", "Role direction", 30}, {"score_qualifications", "Qualifications", 25}, {"score_location", "Location", 20}, {"score_pay", "Pay", 15}, {"score_availability", "Still open", 10}}

// detailGroups orders the job details by the question each answers.
var detailGroups = []struct {
	title  string
	fields []struct{ key, label string }
}{
	{"Check before applying", []struct{ key, label string }{{"gaps_questions", "Questions to check"}, {"posting_status", "Posting status"}, {"location_eligibility", "Location eligibility"}, {"compensation_notes", "Pay notes"}, {"next_action", "Next step"}}},
	{"Why it may fit", []struct{ key, label string }{{"fit_notes", "Fit"}}},
	{"The role", []struct{ key, label string }{{"seniority", "Level"}, {"category", "Category"}, {"experience_required", "Experience"}, {"education_requirement", "Education"}, {"skills_required", "Required skills"}, {"skills_preferred", "Nice to have"}, {"responsibilities", "Responsibilities"}, {"benefits", "Benefits"}}},
	{"Notes and record", []struct{ key, label string }{{"notes", "Notes"}, {"contact", "Contact"}, {"source", "Source"}, {"posting_date", "Listing posted"}, {"application_deadline", "Deadline"}, {"date_found", "Added to tracker"}, {"last_checked", "Last checked"}, {"applied_date", "Applied"}, {"follow_up_date", "Follow-up"}, {"resume_version", "Résumé version"}}},
}

var dateKeys = map[string]bool{"posting_date": true, "application_deadline": true, "date_found": true, "last_checked": true, "applied_date": true, "follow_up_date": true}

// sortColumns maps a sort key to the SQL expression ordered on. Priority and
// status sort by rank (High first; pipeline order), not alphabetically.
// Salary sorts on an annualized minimum so hourly contract rates (80/hour)
// compare with yearly salaries; a missing period is treated as yearly.
var sortColumns = map[string]string{
	"score":    "score_total",
	"company":  "company",
	"title":    "title",
	"industry": "industry",
	"location": "location",
	"salary":   "(CASE lower(coalesce(salary_period,'')) WHEN 'hour' THEN salary_min*2080 WHEN 'hourly' THEN salary_min*2080 WHEN 'week' THEN salary_min*52 WHEN 'month' THEN salary_min*12 ELSE salary_min END)",
	"priority": "(CASE WHEN trim(coalesce(priority,'')) = '' THEN NULL WHEN priority = 'High' THEN 1 WHEN priority = 'Medium' THEN 2 WHEN priority = 'Low' THEN 3 ELSE 4 END)",
	"status":   "(CASE WHEN trim(coalesce(status,'')) = '' THEN NULL WHEN status = 'Research' THEN 1 WHEN status = 'Interested' THEN 2 WHEN status = 'Preparing' THEN 3 WHEN status = 'Applied' THEN 4 WHEN status = 'Screening' THEN 5 WHEN status = 'Interviewing' THEN 6 WHEN status = 'Offer' THEN 7 WHEN status = 'Accepted' THEN 8 WHEN status = 'Rejected' THEN 9 WHEN status = 'Withdrawn' THEN 10 WHEN status = 'Closed' THEN 11 ELSE 12 END)",
	"found":    "date_found",
	"posted":   "posting_date",
	"checked":  "last_checked",
}

// sortOptions is the order and wording of the "Sort by" menu. Each sort has a
// natural direction (best match, highest pay and newest first), used when the
// request names none.
var sortOptions = []SortOption{{"score", "Best match", "best match"}, {"salary", "Highest pay", "highest pay"}, {"priority", "Priority", "priority"}, {"status", "Application stage", "application stage"}, {"posted", "Newest listing", "newest listing"}, {"found", "Recently added", "recently added"}, {"checked", "Recently checked", "recently checked"}, {"company", "Company, A to Z", "company, A to Z"}, {"title", "Title, A to Z", "title, A to Z"}, {"location", "Location, A to Z", "location, A to Z"}, {"industry", "Company market, A to Z", "company market, A to Z"}}
var descendingByDefault = map[string]bool{"score": true, "salary": true, "posted": true, "found": true, "checked": true}

// money renders a pay range plainly: "$115k–$159k a year", "$80–$100 an hour".
func money(m map[string]string) string {
	a, b := m["salary_min"], m["salary_max"]
	if a == "" && b == "" {
		return "Not listed"
	}
	curr := strings.TrimSpace(m["currency"])
	period := strings.ToLower(strings.TrimSpace(m["salary_period"]))
	amount := func(s string) string {
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return s
		}
		text := strconv.FormatFloat(n, 'f', -1, 64)
		if n >= 1000 {
			text = strconv.FormatFloat(float64(int(n/100+0.5))/10, 'f', -1, 64) + "k"
		}
		if curr == "USD" || curr == "" {
			return "$" + text
		}
		return text
	}
	out := amount(a)
	if a == "" {
		out = "up to " + amount(b)
	} else if b != "" && b != a {
		out += "–" + amount(b)
	}
	if curr != "" && curr != "USD" {
		out += " " + curr
	}
	switch period {
	case "hour", "hourly":
		return out + " an hour"
	case "month":
		return out + " a month"
	case "week":
		return out + " a week"
	default:
		return out + " a year"
	}
}

// badges shows the workplace and job-type categories the filters use (see
// filters.go), so a row tagged Remote is exactly a row the Remote filter keeps.
// Unknown values stay unlabelled.
func badges(m map[string]string) []string {
	var out []string
	for _, v := range classifyWorkplace(m["work_arrangement"], m["location"]) {
		out = append(out, choiceLabel(workplaceChoices, v))
	}
	for _, v := range classifyJobType(m["employment_type"], m["notes"]) {
		if v == "contract" {
			out = append(out, "Contract")
		}
	}
	return out
}

// postingNeedsCheck is true unless the stored posting status says the listing
// was seen open; blank, "recheck", "unverified" and similar all need a look.
func postingNeedsCheck(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	if s == "" {
		return true
	}
	for _, w := range []string{"recheck", "unverified", "unconfirmed", "stale", "verify", "inaccessible"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// jobFromRow turns one jobs row (plus app_status) into display data.
func jobFromRow(m map[string]string) Job {
	job := Job{ID: m["id"], Company: m["company"], Title: m["title"], Industry: m["industry"], Location: m["location"], URL: m["url"], Status: m["status"], Priority: m["priority"], Arrangement: m["work_arrangement"], Employment: m["employment_type"], Salary: money(m), Score: m["score_total"], Confidence: m["score_confidence"], Badges: badges(m), Notes: m["notes"], Posting: m["posting_status"], AppStatus: m["app_status"], FitNotes: m["fit_notes"], Questions: m["gaps_questions"], Posted: dateAge(m["posting_date"]), Added: dateAge(m["date_found"])}
	if strings.TrimSpace(job.Industry) == "" {
		job.Industry = "Unknown"
	}
	if n, err := strconv.Atoi(m["score_total"]); err == nil {
		job.HasScore, job.ScoreN = true, n
		for _, f := range scoreFields {
			got, _ := strconv.Atoi(m[f.key])
			job.Parts = append(job.Parts, ScorePart{f.label, got, f.max})
		}
		weakest := 100
		for _, part := range job.Parts {
			if part.Pct() < weakest {
				weakest, job.Weak = part.Pct(), strings.ToLower(part.Label)
			}
		}
		if weakest >= 60 {
			job.Weak = ""
		}
		job.ScoreNote = m["score_explanation"]
		var meta []string
		if c := m["score_confidence"]; c != "" {
			meta = append(meta, strings.ToLower(c)+" confidence in the research behind it")
		}
		if d := m["score_as_of"]; d != "" {
			meta = append(meta, "scored "+fmtDate(d))
		}
		if v := m["score_version"]; v != "" {
			meta = append(meta, "rubric version "+v)
		}
		if len(meta) > 0 {
			meta[0] = strings.ToUpper(meta[0][:1]) + meta[0][1:]
			job.ScoreMeta = strings.Join(meta, "; ") + "."
		}
	}
	job.NeedsCheck = postingNeedsCheck(job.Posting)
	job.Dismissed = m["dismissed"] == "1"
	var types []string
	for _, b := range job.Badges {
		if b != "Contract" {
			types = append(types, b)
		}
	}
	if e := strings.TrimSpace(job.Employment); e != "" {
		types = append(types, e)
	} else if len(job.Badges) > 0 && job.Badges[len(job.Badges)-1] == "Contract" {
		types = append(types, "Contract")
	}
	job.WorkType = strings.Join(types, ", ")
	for _, g := range detailGroups {
		group := DetailGroup{Title: g.title}
		for _, f := range g.fields {
			if val := strings.TrimSpace(m[f.key]); val != "" {
				if dateKeys[f.key] {
					val = fmtDate(val)
				}
				group.Items = append(group.Items, Detail{f.label, val})
				job.Details = append(job.Details, Detail{f.label, val})
			}
		}
		if len(group.Items) > 0 {
			job.Groups = append(job.Groups, group)
		}
	}
	return job
}

func scanRowMap(rows *sql.Rows, cols []string) (map[string]string, error) {
	raw := make([]any, len(cols))
	ptr := make([]any, len(cols))
	for i := range raw {
		ptr[i] = &raw[i]
	}
	if err := rows.Scan(ptr...); err != nil {
		return nil, err
	}
	m := map[string]string{}
	for i, key := range cols {
		if raw[i] != nil {
			m[key] = fmt.Sprint(raw[i])
		}
	}
	return m, nil
}

const jobSelect = "SELECT jobs.*, (SELECT a.status FROM applications a WHERE a.job_id = jobs.id) AS app_status, EXISTS(SELECT 1 FROM job_dismissals d WHERE d.job_id = jobs.id) AS dismissed FROM jobs"

// loadJob reads one job for the application page.
func loadJob(db *sql.DB, id int64) (Job, error) {
	rows, err := db.Query(jobSelect+" WHERE id = ?", id)
	if err != nil {
		return Job{}, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return Job{}, err
	}
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return Job{}, err
		}
		return Job{}, sql.ErrNoRows
	}
	m, err := scanRowMap(rows, cols)
	if err != nil {
		return Job{}, err
	}
	return jobFromRow(m), nil
}

func distinctValues(db *sql.DB, q string) ([]string, error) {
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func query(db *sql.DB, r *http.Request) (Page, error) {
	v := r.URL.Query()
	p := Page{Q: strings.TrimSpace(v.Get("q")), Status: v.Get("status"), Priority: v.Get("priority"), Arrangement: v.Get("arrangement"), Industry: v.Get("industry"), Sort: v.Get("sort"), Direction: v.Get("direction"), SortOptions: sortOptions}
	if _, ok := sortColumns[p.Sort]; !ok {
		p.Sort = "score"
	}
	if p.Direction != "asc" && p.Direction != "desc" {
		p.Direction = "asc"
		if descendingByDefault[p.Sort] {
			p.Direction = "desc"
		}
	}
	for _, o := range sortOptions {
		if o.Value == p.Sort {
			p.SortLabel = o.Label
		}
	}
	if (p.Direction == "desc") != descendingByDefault[p.Sort] {
		p.SortLabel += ", reversed"
	}
	p.Workplace, p.JobType = pickChoices(workplaceChoices, v["workplace"]), pickChoices(jobTypeChoices, v["type"])
	for _, f := range []string{p.Q, p.Status, p.Priority, p.Arrangement, p.Industry, strings.Join(p.Workplace, ","), strings.Join(p.JobType, ",")} {
		if f != "" {
			p.FilterCount++
		}
	}
	for _, f := range []string{p.Status, p.Priority, p.Arrangement, p.Industry} {
		if f != "" {
			p.RefineCount++
		}
	}
	var e error
	for _, d := range []struct {
		col  string
		dest *[]string
	}{{"status", &p.Statuses}, {"priority", &p.Priorities}, {"work_arrangement", &p.Arrangements}} {
		if *d.dest, e = distinctValues(db, "SELECT DISTINCT "+d.col+" FROM jobs WHERE "+d.col+" IS NOT NULL AND trim("+d.col+") <> '' ORDER BY "+d.col); e != nil {
			return p, e
		}
	}
	if p.Industries, e = distinctValues(db, `SELECT DISTINCT COALESCE(NULLIF(trim(industry),''),'Unknown') AS market FROM jobs ORDER BY market COLLATE NOCASE`); e != nil {
		return p, e
	}
	where := []string{"1=1"}
	args := []any{}
	if p.Q != "" {
		where = append(where, "(company LIKE ? OR title LIKE ? OR coalesce(notes,'') LIKE ? OR coalesce(skills_required,'') LIKE ? OR coalesce(fit_notes,'') LIKE ? OR coalesce(location,'') LIKE ?)")
		for i := 0; i < 6; i++ {
			args = append(args, "%"+p.Q+"%")
		}
	}
	for _, f := range []struct{ col, val string }{{"status", p.Status}, {"priority", p.Priority}, {"work_arrangement", p.Arrangement}} {
		if f.val != "" {
			where = append(where, "coalesce("+f.col+",'') = ?")
			args = append(args, f.val)
		}
	}
	if p.Industry != "" {
		where = append(where, "COALESCE(NULLIF(trim(industry),''),'Unknown') = ?")
		args = append(args, p.Industry)
	}
	ids, places, types, e := categoryFilter(db, p.Workplace, p.JobType)
	if e != nil {
		return p, e
	}
	p.Workplaces, p.JobTypes = places, types
	if len(p.Workplace) > 0 || len(p.JobType) > 0 {
		if len(ids) == 0 {
			where = append(where, "0=1")
		} else {
			where = append(where, "id IN (?"+strings.Repeat(",?", len(ids)-1)+")")
			args = append(args, ids...)
		}
	}
	const pageSize = 25
	if e := db.QueryRow("SELECT COUNT(*) FROM jobs WHERE "+strings.Join(where, " AND "), args...).Scan(&p.Count); e != nil {
		return p, e
	}
	p.TotalPages = max(1, (p.Count+pageSize-1)/pageSize)
	p.PageNumber, _ = strconv.Atoi(v.Get("page"))
	p.PageNumber = min(max(p.PageNumber, 1), p.TotalPages)
	if p.Count > 0 {
		p.Start = (p.PageNumber-1)*pageSize + 1
		p.End = min(p.PageNumber*pageSize, p.Count)
	}
	pageURL := func(n int) string {
		values := url.Values{}
		for key, value := range map[string]string{"q": p.Q, "status": p.Status, "priority": p.Priority, "arrangement": p.Arrangement, "industry": p.Industry} {
			if value != "" {
				values.Set(key, value)
			}
		}
		setChoices(values, "workplace", p.Workplace)
		setChoices(values, "type", p.JobType)
		if p.Sort != "score" {
			values.Set("sort", p.Sort)
		}
		if (p.Direction == "desc") != descendingByDefault[p.Sort] {
			values.Set("direction", p.Direction)
		}
		if n > 1 {
			values.Set("page", strconv.Itoa(n))
		}
		if len(values) == 0 {
			return "/"
		}
		return "/?" + values.Encode()
	}
	p.ListURL = pageURL(p.PageNumber)
	if p.PageNumber > 1 {
		p.PreviousURL = pageURL(p.PageNumber - 1)
	}
	if p.PageNumber < p.TotalPages {
		p.NextURL = pageURL(p.PageNumber + 1)
	}
	first := max(p.PageNumber-3, 1)
	last := min(first+6, p.TotalPages)
	if last-first < 6 {
		first = max(last-6, 1)
	}
	for n := first; n <= last; n++ {
		p.PageLinks = append(p.PageLinks, PageLink{Number: n, URL: pageURL(n), Current: n == p.PageNumber})
	}
	order := sortColumns[p.Sort]
	direction := "ASC"
	if p.Direction == "desc" {
		direction = "DESC"
	} // Blank fields stay at the end in either direction.
	q := jobSelect + " WHERE " + strings.Join(where, " AND ") + " ORDER BY (" + order + " IS NULL OR " + order + " = '') ASC, " + order + " COLLATE NOCASE " + direction + ", company ASC, id ASC LIMIT ? OFFSET ?"
	queryArgs := append(append([]any{}, args...), pageSize, (p.PageNumber-1)*pageSize)
	rows, e := db.Query(q, queryArgs...)
	if e != nil {
		return p, e
	}
	defer rows.Close()
	cols, e := rows.Columns()
	if e != nil {
		return p, e
	}
	for rows.Next() {
		m, e := scanRowMap(rows, cols)
		if e != nil {
			return p, e
		}
		p.Jobs = append(p.Jobs, jobFromRow(m))
	}
	if e = rows.Err(); e != nil {
		return p, e
	}
	return p, nil
}

// render writes a page template, logging (not exposing) any template error.
func render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if e := pages.ExecuteTemplate(w, name, data); e != nil {
		log.Printf("render %s: %v", name, e)
	}
}

// problemPage is the page shown when a form cannot be saved because of
// something the person can fix: what to change, and a link back to the form.
type problemPage struct {
	Nav                       Nav
	Message, FixURL, FixLabel string
}

func formProblem(w http.ResponseWriter, db *sql.DB, message, fixURL, fixLabel string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusBadRequest)
	data := problemPage{Nav: loadNav(db, "", "Could not save"), Message: message, FixURL: fixURL, FixLabel: fixLabel}
	if e := pages.ExecuteTemplate(w, "problem", data); e != nil {
		log.Printf("render problem: %v", e)
	}
}

// jobsHandler serves the Jobs page: the full page for navigations, the
// #jobs-view fragment (plus title and Push-Url) for HTMLeX requests.
func jobsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		p, e := query(db, r)
		if e != nil {
			log.Printf("query: %v", e)
			http.Error(w, "Could not read the jobs database", 500)
			return
		}
		title := "Jobs"
		if p.Q != "" {
			title += " matching “" + p.Q + "”"
		}
		if p.PageNumber > 1 {
			title += fmt.Sprintf(", page %d of %d", p.PageNumber, p.TotalPages)
		}
		p.Nav = loadNav(db, "jobs", title)
		if isFragmentRequest(r) {
			p.Fragment = true
			writeFragments(w, "Push-Url", p.ListURL,
				Fragment{Target: "#jobs-view(innerHTML)", Name: "jobsview", Data: p},
				Fragment{Target: "title(innerHTML)", HTML: template.HTML(template.HTMLEscapeString(title + " · Job search"))})
			return
		}
		render(w, "page", p)
	}
}
