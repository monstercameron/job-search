package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Light tools for applying from Claude Desktop, Claude Code or ChatGPT. The
// assistant does the applying itself (reads the posting, fills the employer's
// form in a browser, shows the user before submitting); these tools give it the
// facts, check and export the tailored résumé, and start the tracking.

// dataDir is the folder holding the database; exports go under it.
var dataDir = "."

type mcpTailored struct {
	JobID   int64  `json:"job_id" jsonschema:"Job ID from search_jobs or get_application_kit"`
	Title   string `json:"title,omitempty" jsonschema:"Version name, for example 'Acme Platform Engineer'; defaults to the company and title"`
	Content string `json:"content" jsonschema:"The full tailored résumé as plain text in the layout get_application_kit describes"`
	Variant string `json:"variant,omitempty" jsonschema:"Short label of the résumé version being tested, for example 'Platform engineer' or 'AI focus'; the Metrics page compares outcomes per label. Reuse the exact label for every application that uses the same version"`
}

type mcpTrack struct {
	JobID         int64  `json:"job_id"`
	Status        string `json:"status" jsonschema:"Preparing (started, not sent) or Applied (the user saw the employer's confirmation)"`
	AppliedDate   string `json:"applied_date,omitempty" jsonschema:"YYYY-MM-DD; defaults to today when status is Applied"`
	FollowUpDate  string `json:"follow_up_date,omitempty" jsonschema:"YYYY-MM-DD; defaults to 7 days after applying"`
	Via           string `json:"via,omitempty" jsonschema:"Where it was sent, for example 'Lever application form'"`
	ResumeTitle   string `json:"resume_title,omitempty" jsonschema:"Name of the résumé version that was uploaded"`
	Notes         string `json:"notes,omitempty" jsonschema:"Confirmation number, answers worth remembering, or anything the user said"`
	ResumeVariant string `json:"resume_variant,omitempty" jsonschema:"Label of the résumé version for the Metrics page A/B comparison, the same label passed to save_tailored_resume; empty keeps the current label"`
}

func jobFields(ctx context.Context, db *sql.DB, id int64) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT * FROM jobs WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("job %d not found", id)
	}
	return scanRowMap(rows, cols)
}

func entryMaps(list []ProfileEntry) []map[string]any {
	out := []map[string]any{}
	for _, e := range list {
		m := map[string]any{"title": e.Title, "on_resume": e.OnResume, "bullets": splitLines(e.Bullets)}
		for k, v := range map[string]string{"org": e.Org, "team": e.Place, "dates": dateRange(e.Start, e.End), "url": e.URL} {
			if v != "" {
				m[k] = v
			}
		}
		if e.Section == "skill" {
			m["items"] = splitItems(e.Bullets)
			delete(m, "bullets")
		}
		out = append(out, m)
	}
	return out
}

func addApplyTools(s *mcp.Server, db *sql.DB) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	mcp.AddTool(s, &mcp.Tool{Name: "get_application_kit", Annotations: readOnly, Description: "Everything needed to apply to one saved job: the posting details, the candidate's contact details and standard answers, the full profile (including entries kept off the résumé), accuracy rules that must never be broken, ATS résumé rules, the résumé text layout, and tailored versions already saved. Call this first. It does not apply or send anything."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpJobID) (*mcp.CallToolResult, map[string]any, error) {
		job, err := jobFields(ctx, db, in.JobID)
		if err != nil {
			return nil, nil, err
		}
		p, err := loadProfile(db)
		if err != nil {
			return nil, nil, err
		}
		links := map[string]string{}
		for _, k := range []string{"website", "github", "linkedin", "devpost", "youtube"} {
			if v := linkFor(p.F[k]); v != "" {
				links[k] = v
			}
		}
		first, last, _ := strings.Cut(strings.TrimSpace(p.F["name"]), " ")
		candidate := map[string]any{"name": p.F["name"], "first_name": first, "last_name": last, "email": p.F["email"], "phone": p.F["phone"], "mailing_address": p.F["mailing_address"], "location": p.F["location"], "headline": p.F["headline"], "links": links}
		var missing []string
		for _, k := range []string{"phone", "mailing_address"} {
			if strings.TrimSpace(p.F[k]) == "" {
				missing = append(missing, k)
			}
		}
		answers := map[string]string{
			"work_authorization":  p.F["authorization"],
			"availability":        p.F["availability"],
			"relocation":          p.F["relocation"],
			"workplace":           p.F["workplace"],
			"contract_work":       p.F["contract"],
			"salary_expectations": "Ask the user. Lowest base they set: " + orDash(p.F["salary_floor"]) + ". Posting range: " + orDash(job["compensation_notes"]) + ". Do not disclose current pay unless the user says to.",
			"twitter_or_x":        "Leave blank. Never enter the user's X account.",
			"education":           "Leave optional education fields blank. If a required field asks, answer truthfully from the Profile's education entries (education_private), including entries kept off the résumé. Never claim a degree, certificate or graduation the Profile does not list.",
			"demographics":        "Voluntary EEO, gender, race, veteran and disability questions: ask the user or choose 'decline to self-identify'. Never guess.",
		}
		app := map[string]any{}
		var aid int64
		var status, applied string
		if e := db.QueryRowContext(ctx, "SELECT id,status,COALESCE(applied_date,'') FROM applications WHERE job_id=?", in.JobID).Scan(&aid, &status, &applied); e == nil {
			app["status"], app["applied_date"] = status, applied
			rows, e := db.QueryContext(ctx, "SELECT id,kind,title,updated_at FROM application_documents WHERE application_id=? ORDER BY id DESC", aid)
			if e == nil {
				docs, _ := rowsAsMaps(rows)
				app["documents"] = docs
			}
		} else if e != sql.ErrNoRows {
			return nil, nil, e
		}
		posting := map[string]string{}
		for _, k := range []string{"id", "company", "title", "url", "location", "location_eligibility", "work_arrangement", "employment_type", "seniority", "experience_required", "skills_required", "skills_preferred", "responsibilities", "compensation_notes", "fit_notes", "gaps_questions", "posting_status", "industry", "notes"} {
			if v := strings.TrimSpace(job[k]); v != "" {
				posting[k] = v
			}
		}
		return nil, map[string]any{
			"job":                  posting,
			"candidate":            candidate,
			"missing_from_profile": missing,
			"standard_answers":     answers,
			"summary":              p.F["summary"],
			"target_roles":         p.F["target_roles"],
			"experience":           entryMaps(p.Experience),
			"skills":               entryMaps(p.Skills),
			"projects":             entryMaps(p.Projects),
			"education_private":    entryMaps(p.Education),
			"accuracy_rules":       p.Notes,
			"resume_rules":         atsRules,
			"resume_layout":        resumeFormat,
			"current_base_resume":  profileText(p),
			"application":          app,
			"steps": []string{
				"1. Read the posting at job.url and note its exact required wording.",
				"2. Write a tailored résumé from the profile only, following accuracy_rules and resume_rules, in resume_layout. Call save_tailored_resume; fix every error it returns and resave. Show the user the warnings worth acting on.",
				"3. Call track_application with status Preparing.",
				"4. Open job.url, fill the form from candidate and standard_answers, upload the PDF that save_tailored_resume exported. Ask the user for anything missing (for example phone, mailing address, referral, non-compete questions).",
				"5. Show the user the filled form and let them submit it themselves; CAPTCHAs and the final submit are theirs.",
				"6. When the user confirms it went through, call track_application with status Applied.",
			},
		}, nil
	})

	mcp.AddTool(s, &mcp.Tool{Name: "save_tailored_resume", Description: "Check a tailored résumé against the accuracy rules (no degree, no X account, no invented figures or employers) and ATS rules, save it as this job's résumé version, and export a text-based PDF, DOCX and TXT for upload. Errors block the save: fix them and call again. Returns warnings, keyword coverage against the posting, and the file paths."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpTailored) (*mcp.CallToolResult, map[string]any, error) {
		job, err := jobFields(ctx, db, in.JobID)
		if err != nil {
			return nil, nil, err
		}
		p, err := loadProfile(db)
		if err != nil {
			return nil, nil, err
		}
		check := checkResume(in.Content, p, job["skills_required"], job["skills_preferred"])
		if !check.OK {
			return nil, map[string]any{"saved": false, "check": check}, nil
		}
		title := strings.TrimSpace(in.Title)
		if title == "" {
			title = strings.TrimSpace(job["company"] + " " + job["title"])
		}
		docID, err := upsertResumeDoc(ctx, db, in.JobID, title, in.Content)
		if err != nil {
			return nil, nil, err
		}
		if v := normalizeVariant(in.Variant); v != "" {
			if err = setVariant(db, in.JobID, v); err != nil {
				return nil, nil, err
			}
		}
		files, err := exportResume(dataDir, p.F["name"], job["company"], in.JobID, in.Content)
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"saved": true, "document_id": docID, "title": title, "files": files,
			"view": fmt.Sprintf("http://127.0.0.1:8080/track?id=%d#documents", in.JobID), "check": check}, nil
	})

	mcp.AddTool(s, &mcp.Tool{Name: "track_application", Description: "Start or update tracking for a job application. Use status Preparing when work on an application begins, and Applied only after the user has seen the employer's confirmation that it was submitted. Applied sets the applied date (default today), a follow-up date (default a week later), and logs the submission in the activity timeline. Never submits anything itself."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpTrack) (*mcp.CallToolResult, map[string]any, error) {
		return trackApplication(ctx, db, in)
	})
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "not recorded"
	}
	return s
}

// upsertResumeDoc saves the version under the job's application (creating the
// application row if needed), replacing a version with the same name.
func upsertResumeDoc(ctx context.Context, db *sql.DB, jobID int64, title, content string) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO applications(job_id,status) SELECT id,COALESCE(NULLIF(status,''),'Research') FROM jobs WHERE id=? ON CONFLICT(job_id) DO NOTHING`, jobID); err != nil {
		return 0, err
	}
	var aid, docID int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM applications WHERE job_id=?", jobID).Scan(&aid); err != nil {
		return 0, err
	}
	err = tx.QueryRowContext(ctx, "SELECT id FROM application_documents WHERE application_id=? AND kind='Resume' AND title=?", aid, title).Scan(&docID)
	switch {
	case err == sql.ErrNoRows:
		res, e := tx.ExecContext(ctx, "INSERT INTO application_documents(kind,title,content,application_id) VALUES('Resume',?,?,?)", title, content, aid)
		if e != nil {
			return 0, e
		}
		docID, err = res.LastInsertId()
	case err == nil:
		_, err = tx.ExecContext(ctx, "UPDATE application_documents SET content=?,updated_at=CURRENT_TIMESTAMP WHERE id=?", content, docID)
	}
	if err != nil {
		return 0, err
	}
	return docID, tx.Commit()
}

func trackApplication(ctx context.Context, db *sql.DB, in mcpTrack) (*mcp.CallToolResult, map[string]any, error) {
	if in.Status != "Preparing" && in.Status != "Applied" {
		return nil, nil, fmt.Errorf("status must be Preparing or Applied")
	}
	now := time.Now()
	applied, follow := strings.TrimSpace(in.AppliedDate), strings.TrimSpace(in.FollowUpDate)
	for _, d := range []string{applied, follow} {
		if !validDate(d) {
			return nil, nil, fmt.Errorf("dates must use YYYY-MM-DD")
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	var company, title string
	if err = tx.QueryRowContext(ctx, "SELECT company,title FROM jobs WHERE id=?", in.JobID).Scan(&company, &title); err != nil {
		return nil, nil, fmt.Errorf("job %d not found", in.JobID)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO applications(job_id,status) SELECT id,COALESCE(NULLIF(status,''),'Research') FROM jobs WHERE id=? ON CONFLICT(job_id) DO NOTHING`, in.JobID); err != nil {
		return nil, nil, err
	}
	var aid int64
	var old, oldApplied, oldFollow string
	if err = tx.QueryRowContext(ctx, "SELECT id,status,COALESCE(applied_date,''),COALESCE(follow_up_date,'') FROM applications WHERE job_id=?", in.JobID).Scan(&aid, &old, &oldApplied, &oldFollow); err != nil {
		return nil, nil, err
	}
	status := in.Status
	// Never move an application backwards: Preparing on an already-applied job
	// (or later stage) keeps the later stage.
	if statusRank(old) > statusRank(status) {
		status = old
	}
	if in.Status == "Applied" {
		if applied == "" {
			applied = oldApplied
		}
		if applied == "" {
			applied = now.Format("2006-01-02")
		}
		if follow == "" {
			follow = oldFollow
		}
		if follow == "" {
			t, _ := time.Parse("2006-01-02", applied)
			follow = t.AddDate(0, 0, 7).Format("2006-01-02")
		}
	} else {
		applied, follow = oldApplied, firstNonEmpty(follow, oldFollow)
	}
	stamp := now.Format("2006-01-02T15:04")
	if _, err = tx.ExecContext(ctx, "UPDATE applications SET status=?,applied_date=?,follow_up_date=?,updated_at=CURRENT_TIMESTAMP WHERE id=?", status, nullIfEmpty(applied), nullIfEmpty(follow), aid); err != nil {
		return nil, nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE jobs SET status=?,applied_date=?,follow_up_date=? WHERE id=?", status, nullIfEmpty(applied), nullIfEmpty(follow), in.JobID); err != nil {
		return nil, nil, err
	}
	variant := normalizeVariant(in.ResumeVariant)
	if variant != "" {
		if _, err = tx.ExecContext(ctx, "UPDATE applications SET resume_variant=? WHERE id=?", variant, aid); err != nil {
			return nil, nil, err
		}
	}
	if status != old {
		if _, err = tx.ExecContext(ctx, "INSERT INTO application_events(application_id,occurred_at,kind,detail) VALUES(?,?,?,?)", aid, stamp, "Status changed", old+" → "+status); err != nil {
			return nil, nil, err
		}
	}
	if in.Status == "Applied" && old != "Applied" {
		var parts []string
		parts = append(parts, "Application submitted")
		if v := strings.TrimSpace(in.Via); v != "" {
			parts[0] += " via " + v
		}
		if v := strings.TrimSpace(in.ResumeTitle); v != "" {
			parts = append(parts, "Résumé version: "+v)
		}
		if variant != "" {
			parts = append(parts, "Variant: "+variant)
		}
		if v := strings.TrimSpace(in.Notes); v != "" {
			parts = append(parts, v)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO communications(occurred_at,channel,direction,contact,notes,application_id) VALUES(?,?,?,?,?,?)`, stamp, "Application", "Sent", company, strings.Join(parts, ". ")+".", aid); err != nil {
			return nil, nil, err
		}
	} else if v := strings.TrimSpace(in.Notes); v != "" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO communications(occurred_at,channel,direction,contact,notes,application_id) VALUES(?,?,?,?,?,?)`, stamp, "Note", "", company, v, aid); err != nil {
			return nil, nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	return nil, map[string]any{"job_id": in.JobID, "job": title + " at " + company, "status": status, "applied_date": applied, "follow_up_date": follow,
		"view": "http://127.0.0.1:8080/track?id=" + strconv.FormatInt(in.JobID, 10)}, nil
}

// statusRank is the status's place in the pipeline; unknown statuses rank 0.
func statusRank(s string) int {
	for i, x := range statusOrder {
		if x == s {
			return i
		}
	}
	return 0
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// registerResumeFiles serves a saved résumé version as an upload-ready file:
// /resume-file?id=<document id>&format=pdf|docx.
func registerResumeFiles(db *sql.DB) {
	http.HandleFunc("/resume-file", func(w http.ResponseWriter, r *http.Request) {
		id := positive(r.URL.Query().Get("id"))
		var content, name string
		err := db.QueryRow(`SELECT d.content, COALESCE((SELECT value FROM profile_fields WHERE key='name'),'') FROM application_documents d WHERE d.id=? AND d.kind='Resume'`, id).Scan(&content, &name)
		if err != nil {
			http.Error(w, "Résumé version not found", http.StatusNotFound)
			return
		}
		base := fileSlug(name)
		if base == "" {
			base = "Resume"
		} else {
			base += "-Resume"
		}
		switch r.URL.Query().Get("format") {
		case "docx":
			data, e := resumeDOCX(content, name+" résumé", name)
			if e != nil {
				http.Error(w, "Could not build the file", 500)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
			w.Header().Set("Content-Disposition", `attachment; filename="`+base+`.docx"`)
			w.Write(data)
		default:
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", `inline; filename="`+base+`.pdf"`)
			w.Write(resumePDF(content, name+" résumé", name))
		}
	})
}

// serveMCPHTTP serves the same tools over Streamable HTTP for ChatGPT, which
// only connects to remote servers. Every request must carry the secret path
// /mcp/<token>; the token is made once and kept in tracker_settings. Because
// the secret path is the guard, the SDK's localhost Host check is turned off
// so a tunnel (cloudflared, ngrok) can forward requests with its own Host.
func serveMCPHTTP(db *sql.DB, addr string) error {
	var token string
	err := db.QueryRow("SELECT value FROM tracker_settings WHERE key='mcp_http_token'").Scan(&token)
	if err == sql.ErrNoRows || (err == nil && len(token) < 32) {
		b := make([]byte, 24)
		if _, err = rand.Read(b); err != nil {
			return err
		}
		token = hex.EncodeToString(b)
		_, err = db.Exec(`INSERT INTO tracker_settings(key,value) VALUES('mcp_http_token',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=CURRENT_TIMESTAMP`, token)
	}
	if err != nil {
		return err
	}
	srv := newMCPServer(db)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{DisableLocalhostProtection: true})
	secret := "/mcp/" + token
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != secret {
			http.NotFound(w, r)
			return
		}
		handler.ServeHTTP(w, r)
	})
	log.Printf("MCP over HTTP: http://%s%s (keep this path secret; anyone with it can read and change your job search data)", addr, secret)
	return http.ListenAndServe(addr, mux)
}
