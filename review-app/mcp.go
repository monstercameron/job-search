package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The MCP server uses stdio and the same SQLite database as the web app. It
// never sends email or submits applications. Tool errors become MCP errors.
type mcpSearch struct {
	Query       string `json:"query,omitempty" jsonschema:"Search titles, company names, location, skills, and notes"`
	Status      string `json:"status,omitempty" jsonschema:"Exact application status filter"`
	Priority    string `json:"priority,omitempty" jsonschema:"Exact priority filter"`
	Arrangement string `json:"arrangement,omitempty" jsonschema:"Exact work arrangement filter"`
	Industry    string `json:"industry,omitempty" jsonschema:"Exact company industry filter, or Unknown"`
	Sort        string `json:"sort,omitempty" jsonschema:"score, company, title, location, salary, priority, status, found, or checked"`
	Direction   string `json:"direction,omitempty" jsonschema:"asc or desc; when omitted, score, salary, found and checked sort descending and the rest ascending"`
	Page        int    `json:"page,omitempty" jsonschema:"One-based page of 25 results"`
}
type mcpJobID struct {
	JobID int64 `json:"job_id" jsonschema:"Existing job ID"`
}
type mcpNewJob struct {
	Company         string `json:"company" jsonschema:"Employer name, or explicitly label it undisclosed"`
	Title           string `json:"title" jsonschema:"Job title"`
	URL             string `json:"url" jsonschema:"Actual HTTP or HTTPS job posting or source URL"`
	Location        string `json:"location,omitempty"`
	Category        string `json:"category,omitempty"`
	Industry        string `json:"industry,omitempty" jsonschema:"Company market or industry; omit if unknown"`
	WorkArrangement string `json:"work_arrangement,omitempty"`
	EmploymentType  string `json:"employment_type,omitempty"`
	SkillsRequired  string `json:"skills_required,omitempty"`
	Notes           string `json:"notes,omitempty"`
	Source          string `json:"source,omitempty"`
	PostingStatus   string `json:"posting_status,omitempty"`
	FitNotes        string `json:"fit_notes,omitempty"`
	GapsQuestions   string `json:"gaps_questions,omitempty"`
	PostingDate     string `json:"posting_date,omitempty" jsonschema:"Date the employer published the listing, YYYY-MM-DD, taken from the posting itself; omit if the posting does not say"`
}
type mcpUpdateJob struct {
	JobID  int64          `json:"job_id"`
	Fields map[string]any `json:"fields" jsonschema:"Fields to change. Allowed fields include company, title, url, location, category, industry, notes, priority, work_arrangement, employment_type, skills_required, posting_status, posting_date (when the employer published the listing, YYYY-MM-DD), application_deadline (YYYY-MM-DD), fit_notes, gaps_questions, salary_min, salary_max, currency, salary_period, source, and next_action. Use null to clear optional fields."`
}
type mcpApplication struct {
	JobID         int64   `json:"job_id"`
	Status        *string `json:"status,omitempty" jsonschema:"Research, Interested, Preparing, Applied, Screening, Interviewing, Offer, Accepted, Rejected, Withdrawn, or Closed"`
	AppliedDate   *string `json:"applied_date,omitempty" jsonschema:"YYYY-MM-DD; empty clears"`
	FollowUpDate  *string `json:"follow_up_date,omitempty" jsonschema:"YYYY-MM-DD; empty clears"`
	Contact       *string `json:"contact,omitempty"`
	Notes         *string `json:"notes,omitempty"`
	Feeling       *string `json:"feeling,omitempty" jsonschema:"User's personal confidence: Low, Medium, High, Unsure, or empty; do not infer without the user"`
	FeelingNotes  *string `json:"feeling_notes,omitempty"`
	ResumeVariant *string `json:"resume_variant,omitempty" jsonschema:"Label of the résumé version this application went out with, for the Metrics page A/B comparison; empty clears"`
}
type mcpInterview struct {
	JobID    int64   `json:"job_id"`
	RecordID int64   `json:"record_id,omitempty" jsonschema:"Existing interview ID to edit; omit to create"`
	When     *string `json:"when,omitempty" jsonschema:"Local date-time YYYY-MM-DDTHH:MM"`
	Type     *string `json:"type,omitempty"`
	Person   *string `json:"person,omitempty"`
	Outcome  *string `json:"outcome,omitempty"`
	Notes    *string `json:"notes,omitempty"`
}
type mcpCommunication struct {
	JobID     int64   `json:"job_id"`
	RecordID  int64   `json:"record_id,omitempty" jsonschema:"Existing communication ID to edit; omit to create"`
	When      *string `json:"when,omitempty" jsonschema:"Local date-time YYYY-MM-DDTHH:MM"`
	Channel   *string `json:"channel,omitempty"`
	Direction *string `json:"direction,omitempty"`
	Person    *string `json:"person,omitempty"`
	Notes     *string `json:"notes,omitempty"`
}
type mcpDocument struct {
	JobID    int64   `json:"job_id"`
	RecordID int64   `json:"record_id,omitempty" jsonschema:"Existing document ID to edit; omit to create"`
	Type     *string `json:"type,omitempty" jsonschema:"Resume or Cover letter"`
	Title    *string `json:"title,omitempty"`
	Content  *string `json:"content,omitempty"`
}
type mcpBase struct {
	JobID   int64  `json:"job_id" jsonschema:"Any existing job ID"`
	Content string `json:"content" jsonschema:"Full base resume text; setting replaces the old value"`
}
type mcpCompare struct {
	JobID  int64   `json:"job_id"`
	Resume *string `json:"resume,omitempty" jsonschema:"Text to compare; omit to compare the saved base resume"`
}

func validWebURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}
func merge(base string, patch *string) string {
	if patch != nil {
		return *patch
	}
	return base
}
func rowsAsMaps(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	cols, e := rows.Columns()
	if e != nil {
		return nil, e
	}
	result := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range vals {
			ptr[i] = &vals[i]
		}
		if e = rows.Scan(ptr...); e != nil {
			return nil, e
		}
		m := map[string]any{}
		for i, key := range cols {
			if b, ok := vals[i].([]byte); ok {
				m[key] = string(b)
			} else {
				m[key] = vals[i]
			}
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
func mcpSave(v url.Values) error {
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/save", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://127.0.0.1:8080")
	w := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		return fmt.Errorf("save failed (%d): %s", w.Code, strings.TrimSpace(w.Body.String()))
	}
	return nil
}
func savedResult(jobID int64, kind string) map[string]any {
	return map[string]any{"saved": true, "job_id": jobID, "kind": kind}
}
func put(v url.Values, k string, p *string, old string) { v.Set(k, merge(old, p)) }

func runMCP(db *sql.DB) error {
	return newMCPServer(db).Run(context.Background(), &mcp.StdioTransport{})
}

// newMCPServer builds the tool set shared by the stdio and HTTP transports.
func newMCPServer(db *sql.DB) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "job-search-tracker", Version: "1.1.0"}, &mcp.ServerOptions{Instructions: "Job search tracker for one person. To apply to a job: get_application_kit, then save_tailored_resume, then track_application. When you save or read a job, record its company's headquarters with set_company and its locations and requirements with set_job_profile; list_profile_gaps shows what is missing. Never invent facts, never claim a degree, never enter the user's X/Twitter account, and let the user submit applications themselves."})
	mcp.AddTool(s, &mcp.Tool{Name: "search_jobs", Description: "Search and filter saved jobs. Results are paginated 25 per page."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpSearch) (*mcp.CallToolResult, map[string]any, error) {
		v := url.Values{}
		for k, x := range map[string]string{"q": in.Query, "status": in.Status, "priority": in.Priority, "arrangement": in.Arrangement, "industry": in.Industry, "sort": in.Sort, "direction": in.Direction} {
			if x != "" {
				v.Set(k, x)
			}
		}
		if in.Page > 0 {
			v.Set("page", strconv.Itoa(in.Page))
		}
		r := httptest.NewRequest("GET", "http://127.0.0.1:8080/?"+v.Encode(), nil)
		p, e := query(db, r)
		if e != nil {
			return nil, nil, e
		}
		return nil, map[string]any{"count": p.Count, "page": p.PageNumber, "total_pages": p.TotalPages, "jobs": p.Jobs}, nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "get_job", Description: "Read all stored job fields and its application, interviews, communications, documents, and timeline. This may contain private user data."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpJobID) (*mcp.CallToolResult, map[string]any, error) {
		if in.JobID < 1 {
			return nil, nil, fmt.Errorf("invalid job_id")
		}
		rows, e := db.QueryContext(ctx, "SELECT * FROM jobs WHERE id=?", in.JobID)
		if e != nil {
			return nil, nil, e
		}
		jobs, e := rowsAsMaps(rows)
		if e != nil {
			return nil, nil, e
		}
		if len(jobs) == 0 {
			return nil, nil, fmt.Errorf("job not found")
		}
		out := map[string]any{"job": jobs[0]}
		if out["profile"], e = jobProfile(ctx, db, in.JobID); e != nil {
			return nil, nil, e
		}
		rows, e = db.QueryContext(ctx, "SELECT * FROM applications WHERE job_id=?", in.JobID)
		if e != nil {
			return nil, nil, e
		}
		apps, e := rowsAsMaps(rows)
		if e != nil {
			return nil, nil, e
		}
		if len(apps) == 0 {
			return nil, out, nil
		}
		out["application"] = apps[0]
		aid := apps[0]["id"]
		for _, table := range []string{"interviews", "communications", "application_documents", "application_events"} {
			rows, e = db.QueryContext(ctx, "SELECT * FROM "+table+" WHERE application_id=? ORDER BY id", aid)
			if e != nil {
				return nil, nil, e
			}
			data, err := rowsAsMaps(rows)
			if err != nil {
				return nil, nil, err
			}
			out[table] = data
		}
		return nil, out, nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "create_job", Description: "Save a new job lead with its actual source URL. Does not apply for a job. Duplicate URLs are rejected."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpNewJob) (*mcp.CallToolResult, map[string]any, error) {
		if strings.TrimSpace(in.Company) == "" || strings.TrimSpace(in.Title) == "" || !validWebURL(in.URL) {
			return nil, nil, fmt.Errorf("company, title, and a valid HTTP(S) source URL are required")
		}
		in.PostingDate = strings.TrimSpace(in.PostingDate)
		if !validDate(in.PostingDate) {
			return nil, nil, fmt.Errorf("posting_date must be YYYY-MM-DD")
		}
		var posted any
		if in.PostingDate != "" {
			posted = in.PostingDate
		}
		now := time.Now().Format("2006-01-02")
		res, e := db.ExecContext(ctx, `INSERT INTO jobs(company,title,url,location,category,industry,work_arrangement,employment_type,skills_required,notes,source,posting_status,fit_notes,gaps_questions,posting_date,date_found,last_checked,status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'Research')`, in.Company, in.Title, in.URL, in.Location, in.Category, in.Industry, in.WorkArrangement, in.EmploymentType, in.SkillsRequired, in.Notes, in.Source, in.PostingStatus, in.FitNotes, in.GapsQuestions, posted, now, now)
		if e != nil {
			return nil, nil, e
		}
		id, e := res.LastInsertId()
		return nil, map[string]any{"job_id": id, "created": true, "score": "not yet rated"}, e
	})
	mcp.AddTool(s, &mcp.Tool{Name: "update_job", Description: "Update explicitly supplied job details without changing application status. Omitted fields are preserved."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpUpdateJob) (*mcp.CallToolResult, map[string]any, error) {
		allowed := map[string]bool{}
		for _, k := range strings.Fields("company title url location category industry notes priority work_arrangement employment_type seniority city region country location_eligibility skills_required skills_preferred responsibilities benefits compensation_notes experience_required education_requirement fit_notes gaps_questions salary_min salary_max currency salary_period source posting_status posting_date application_deadline contact next_action") {
			allowed[k] = true
		}
		if in.JobID < 1 || len(in.Fields) == 0 {
			return nil, nil, fmt.Errorf("job_id and at least one field are required")
		}
		sets := []string{}
		args := []any{}
		for k, v := range in.Fields {
			if !allowed[k] {
				return nil, nil, fmt.Errorf("field %q is not allowed", k)
			}
			if k == "salary_min" || k == "salary_max" {
				if v != nil {
					f, ok := v.(float64)
					if !ok || f < 0 || f != float64(int64(f)) {
						return nil, nil, fmt.Errorf("%s must be a nonnegative integer or null", k)
					}
				}
			} else if v != nil {
				if _, ok := v.(string); !ok {
					return nil, nil, fmt.Errorf("%s must be a string or null", k)
				}
			}
			if (k == "company" || k == "title" || k == "url") && (v == nil || strings.TrimSpace(v.(string)) == "") {
				return nil, nil, fmt.Errorf("%s cannot be empty", k)
			}
			if k == "url" && !validWebURL(v.(string)) {
				return nil, nil, fmt.Errorf("url must be a valid HTTP(S) URL")
			}
			if (k == "posting_date" || k == "application_deadline") && v != nil && !validDate(strings.TrimSpace(v.(string))) {
				return nil, nil, fmt.Errorf("%s must be YYYY-MM-DD", k)
			}
			sets = append(sets, k+"=?")
			args = append(args, v)
		}
		args = append(args, in.JobID)
		res, e := db.ExecContext(ctx, "UPDATE jobs SET "+strings.Join(sets, ",")+" WHERE id=?", args...)
		if e = checkedUpdate(res, e); e != nil {
			return nil, nil, e
		}
		return nil, savedResult(in.JobID, "job"), nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "list_applications", Description: "List applications with their statuses, feelings, and follow-up dates."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]any, error) {
		rows, e := db.QueryContext(ctx, `SELECT j.id AS job_id,j.company,j.title,a.* FROM applications a JOIN jobs j ON j.id=a.job_id ORDER BY CASE WHEN a.follow_up_date IS NULL OR a.follow_up_date='' THEN 1 ELSE 0 END,a.follow_up_date,j.company`)
		if e != nil {
			return nil, nil, e
		}
		a, e := rowsAsMaps(rows)
		return nil, map[string]any{"applications": a}, e
	})
	mcp.AddTool(s, &mcp.Tool{Name: "update_application", Description: "Create or update application status, dates, notes, and the user's self-reported feeling. Omitted fields are preserved; this does not submit an application."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpApplication) (*mcp.CallToolResult, map[string]any, error) {
		if in.JobID < 1 {
			return nil, nil, fmt.Errorf("invalid job_id")
		}
		if in.Status != nil {
			valid := map[string]bool{"Research": true, "Interested": true, "Preparing": true, "Applied": true, "Screening": true, "Interviewing": true, "Offer": true, "Accepted": true, "Rejected": true, "Withdrawn": true, "Closed": true}
			if !valid[*in.Status] {
				return nil, nil, fmt.Errorf("invalid status")
			}
		}
		if in.Feeling != nil && *in.Feeling != "" && *in.Feeling != "Low" && *in.Feeling != "Medium" && *in.Feeling != "High" && *in.Feeling != "Unsure" {
			return nil, nil, fmt.Errorf("invalid feeling")
		}
		for _, pair := range []struct {
			n string
			p *string
		}{{"applied_date", in.AppliedDate}, {"follow_up_date", in.FollowUpDate}} {
			if pair.p != nil && *pair.p != "" {
				if _, err := time.Parse("2006-01-02", *pair.p); err != nil {
					return nil, nil, fmt.Errorf("%s must use YYYY-MM-DD", pair.n)
				}
			}
		}
		tx, e := db.BeginTx(ctx, nil)
		if e != nil {
			return nil, nil, e
		}
		defer tx.Rollback()
		var exists int
		if e = tx.QueryRowContext(ctx, "SELECT 1 FROM jobs WHERE id=?", in.JobID).Scan(&exists); e != nil {
			return nil, nil, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO applications(job_id,status) SELECT id,COALESCE(NULLIF(status,''),'Research') FROM jobs WHERE id=? ON CONFLICT(job_id) DO NOTHING`, in.JobID); e != nil {
			return nil, nil, e
		}
		var aid int64
		var oldStatus, oldFeeling, oldFeelingNotes string
		if e = tx.QueryRowContext(ctx, "SELECT id,status,COALESCE(feeling,''),COALESCE(feeling_notes,'') FROM applications WHERE job_id=?", in.JobID).Scan(&aid, &oldStatus, &oldFeeling, &oldFeelingNotes); e != nil {
			return nil, nil, e
		}
		sets := []string{}
		args := []any{}
		for _, p := range []struct {
			name  string
			value *string
		}{{"status", in.Status}, {"applied_date", in.AppliedDate}, {"follow_up_date", in.FollowUpDate}, {"contact", in.Contact}, {"notes", in.Notes}, {"feeling", in.Feeling}, {"feeling_notes", in.FeelingNotes}, {"resume_variant", in.ResumeVariant}} {
			if p.value != nil {
				sets = append(sets, p.name+"=?")
				if p.name == "resume_variant" {
					args = append(args, nullIfEmpty(normalizeVariant(*p.value)))
				} else if p.name == "applied_date" || p.name == "follow_up_date" {
					args = append(args, nullIfEmpty(*p.value)) // empty clears to NULL
				} else {
					args = append(args, *p.value)
				}
			}
		}
		if len(sets) > 0 {
			args = append(args, aid)
			if _, e = tx.ExecContext(ctx, "UPDATE applications SET "+strings.Join(sets, ",")+",updated_at=CURRENT_TIMESTAMP WHERE id=?", args...); e != nil {
				return nil, nil, e
			}
		}
		jobSets := []string{}
		jobArgs := []any{}
		for _, p := range []struct {
			name  string
			value *string
		}{{"status", in.Status}, {"applied_date", in.AppliedDate}, {"follow_up_date", in.FollowUpDate}} {
			if p.value != nil {
				jobSets = append(jobSets, p.name+"=?")
				if p.name == "status" {
					jobArgs = append(jobArgs, *p.value)
				} else {
					jobArgs = append(jobArgs, nullIfEmpty(*p.value)) // empty clears to NULL
				}
			}
		}
		if len(jobSets) > 0 {
			jobArgs = append(jobArgs, in.JobID)
			if _, e = tx.ExecContext(ctx, "UPDATE jobs SET "+strings.Join(jobSets, ",")+" WHERE id=?", jobArgs...); e != nil {
				return nil, nil, e
			}
		}
		when := time.Now().Format("2006-01-02T15:04")
		if in.Status != nil && *in.Status != oldStatus {
			if _, e = tx.ExecContext(ctx, "INSERT INTO application_events(application_id,occurred_at,kind,detail) VALUES(?,?,?,?)", aid, when, "Status changed", oldStatus+" → "+*in.Status); e != nil {
				return nil, nil, e
			}
		}
		newFeeling, newNotes := merge(oldFeeling, in.Feeling), merge(oldFeelingNotes, in.FeelingNotes)
		if newFeeling != oldFeeling || newNotes != oldFeelingNotes {
			detail := newFeeling
			if detail == "" {
				detail = "Not set"
			}
			if strings.TrimSpace(newNotes) != "" {
				detail += " — " + strings.TrimSpace(newNotes)
			}
			if _, e = tx.ExecContext(ctx, "INSERT INTO application_events(application_id,occurred_at,kind,detail) VALUES(?,?,?,?)", aid, when, "Confidence updated", detail); e != nil {
				return nil, nil, e
			}
		}
		if e = tx.Commit(); e != nil {
			return nil, nil, e
		}
		return nil, savedResult(in.JobID, "application"), nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "save_interview", Description: "Log or edit an interview, preserving omitted fields on edits. Date-times are local YYYY-MM-DDTHH:MM."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpInterview) (*mcp.CallToolResult, map[string]any, error) {
		if in.RecordID > 0 {
			if in.When != nil {
				if err := validateLocalTime(*in.When); err != nil {
					return nil, nil, err
				}
			}
			if err := patchTrackerRecord(ctx, db, "interviews", in.RecordID, in.JobID, map[string]*string{"scheduled_at": in.When, "kind": in.Type, "interviewer": in.Person, "outcome": in.Outcome, "notes": in.Notes}); err != nil {
				return nil, nil, err
			}
			return nil, savedResult(in.JobID, "interview"), nil
		}
		// New interview: omitted fields start blank.
		v := url.Values{"job_id": {strconv.FormatInt(in.JobID, 10)}, "kind": {"interview"}}
		put(v, "when", in.When, "")
		put(v, "type", in.Type, "")
		put(v, "person", in.Person, "")
		put(v, "outcome", in.Outcome, "")
		put(v, "notes", in.Notes, "")
		if err := validateLocalTime(v.Get("when")); err != nil {
			return nil, nil, err
		}
		if err := mcpSave(v); err != nil {
			return nil, nil, err
		}
		return nil, savedResult(in.JobID, "interview"), nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "save_communication", Description: "Log or edit a communication; this records notes only and never sends email or messages."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpCommunication) (*mcp.CallToolResult, map[string]any, error) {
		if in.RecordID > 0 {
			if in.When != nil {
				if err := validateLocalTime(*in.When); err != nil {
					return nil, nil, err
				}
			}
			if err := patchTrackerRecord(ctx, db, "communications", in.RecordID, in.JobID, map[string]*string{"occurred_at": in.When, "channel": in.Channel, "direction": in.Direction, "contact": in.Person, "notes": in.Notes}); err != nil {
				return nil, nil, err
			}
			return nil, savedResult(in.JobID, "communication"), nil
		}
		// New communication: omitted fields start blank.
		v := url.Values{"job_id": {strconv.FormatInt(in.JobID, 10)}, "kind": {"message"}}
		put(v, "when", in.When, "")
		put(v, "channel", in.Channel, "")
		put(v, "direction", in.Direction, "")
		put(v, "person", in.Person, "")
		put(v, "notes", in.Notes, "")
		if err := validateLocalTime(v.Get("when")); err != nil {
			return nil, nil, err
		}
		if err := mcpSave(v); err != nil {
			return nil, nil, err
		}
		return nil, savedResult(in.JobID, "communication"), nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "save_document", Description: "Create or update a local resume or cover-letter draft. Never send a document externally or invent qualifications."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpDocument) (*mcp.CallToolResult, map[string]any, error) {
		var typ, title, content string
		if in.RecordID > 0 {
			err := db.QueryRowContext(ctx, `SELECT d.kind,d.title,d.content FROM application_documents d JOIN applications a ON a.id=d.application_id WHERE d.id=? AND a.job_id=?`, in.RecordID, in.JobID).Scan(&typ, &title, &content)
			if err != nil {
				return nil, nil, err
			}
		}
		v := url.Values{"job_id": {strconv.FormatInt(in.JobID, 10)}, "kind": {"document"}}
		if in.RecordID > 0 {
			v.Set("record_id", strconv.FormatInt(in.RecordID, 10))
		}
		put(v, "doc_type", in.Type, typ)
		put(v, "title", in.Title, title)
		put(v, "content", in.Content, content)
		if err := mcpSave(v); err != nil {
			return nil, nil, err
		}
		return nil, savedResult(in.JobID, "document"), nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "set_base_resume", Description: "Replace the locally stored base resume with user-provided text. This does not transmit the resume."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpBase) (*mcp.CallToolResult, map[string]any, error) {
		v := url.Values{"job_id": {strconv.FormatInt(in.JobID, 10)}, "kind": {"base"}, "content": {in.Content}}
		if err := mcpSave(v); err != nil {
			return nil, nil, err
		}
		return nil, savedResult(in.JobID, "base_resume"), nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "compare_resume", Description: "Compare recorded job skills with supplied or locally saved resume text. Literal keyword matching only; not an ATS prediction."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpCompare) (*mcp.CallToolResult, map[string]any, error) {
		var skills, resume string
		if e := db.QueryRowContext(ctx, "SELECT COALESCE(skills_required,'') FROM jobs WHERE id=?", in.JobID).Scan(&skills); e != nil {
			return nil, nil, e
		}
		if in.Resume != nil {
			resume = *in.Resume
		} else {
			e := db.QueryRowContext(ctx, "SELECT value FROM tracker_settings WHERE key='base_resume'").Scan(&resume)
			if e != nil && e != sql.ErrNoRows {
				return nil, nil, e
			}
		}
		matched, missing := compareSkills(skills, resume)
		return nil, map[string]any{"matched": matched, "missing_or_needs_review": missing, "tips": resumeTips(resume), "limitations": "Literal text comparison, not an ATS score or prediction."}, nil
	})
	addApplyTools(s, db)
	addInsightTools(s, db)
	return s
}
func validateLocalTime(value string) error {
	if value == "" {
		return nil
	}
	if _, e := time.Parse("2006-01-02T15:04", value); e != nil {
		return fmt.Errorf("date-time must use YYYY-MM-DDTHH:MM")
	}
	return nil
}

func patchTrackerRecord(ctx context.Context, db *sql.DB, table string, recordID, jobID int64, fields map[string]*string) error {
	if table != "interviews" && table != "communications" {
		return fmt.Errorf("invalid record type")
	}
	sets := []string{}
	args := []any{}
	for name, value := range fields {
		if value != nil {
			sets = append(sets, name+"=?")
			args = append(args, *value)
		}
	}
	if len(sets) == 0 {
		return fmt.Errorf("no fields to update")
	}
	args = append(args, recordID, jobID)
	res, e := db.ExecContext(ctx, "UPDATE "+table+" SET "+strings.Join(sets, ",")+",updated_at=CURRENT_TIMESTAMP WHERE id=? AND application_id IN (SELECT id FROM applications WHERE job_id=?)", args...)
	return checkedUpdate(res, e)
}

// Keep json referenced when SDK schemas use maps containing interface values.
var _ = json.Valid
