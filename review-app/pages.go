package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type TrackerJob struct {
	ID                                                        int64
	Company, Title, Status, Applied, Followup, Contact, Notes string
	Feeling, FeelingNotes, LastActivity                       string
}

// TimelineEvent is one dated entry on an application's activity timeline.
// Kind is "message" or "interview" when the entry can be edited in place.
type TimelineEvent struct {
	When, Display, Title, Detail, Gap, Kind string
	Upcoming                                bool
	Rec                                     TrackerRecord
	date                                    time.Time
}
type TrackerRecord struct {
	ID            int64
	A, B, C, D, E string
}

// Step is one stage on the application page's stage track.
type Step struct {
	Label         string
	Done, Current bool
}

// AppGroup is one section of the Applications page.
type AppGroup struct {
	Title, Note string
	Jobs        []TrackerJob
}

type TrackerPage struct {
	Nav                                     Nav
	Job                                     TrackerJob
	Info                                    Job
	Groups                                  []AppGroup
	Total                                   int
	Resumes, Letters                        []TrackerRecord
	Base, Resume, ResumeTitle               string
	Matched, Missing                        []string
	MatchNote                               string
	ActiveID                                int64
	HasApp, HasBase                         bool
	ResumeTips                              []string
	Timeline                                []TimelineEvent
	Statuses                                []string
	Back, BackLabel, Saved, SavedText, Self string
	FollowDue, Closed                       bool
	Steps                                   []Step
	Fragment                                bool // rendered as an HTMLeX fragment: the saved banner takes focus
}

var statusOrder = []string{"Research", "Interested", "Preparing", "Applied", "Screening", "Interviewing", "Offer", "Accepted", "Rejected", "Withdrawn", "Closed"}
var statusStage = map[string]string{"Research": "later", "Interested": "later", "Preparing": "later", "Applied": "active", "Screening": "active", "Interviewing": "active", "Offer": "active", "Accepted": "closed", "Rejected": "closed", "Withdrawn": "closed", "Closed": "closed"}

var savedMessages = map[string]string{"application": "Status saved.", "interview": "Interview saved.", "message": "Message logged.", "document": "Document saved.", "base": "Base résumé saved."}

func backLabel(back string) string {
	switch {
	case strings.HasPrefix(back, "/tracker"):
		return "Back to applications"
	case strings.HasPrefix(back, "/track?"):
		return "Back to the application"
	case back == "/resume":
		return "Back to résumé"
	case back == "/profile":
		return "Back to profile"
	case strings.HasPrefix(back, "/next"):
		return "Back to next up"
	default:
		return "Back to jobs"
	}
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, ", ")
}

func registerTracker(db *sql.DB) {
	http.HandleFunc("/tracker", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		rows, err := db.Query(`SELECT j.id,j.company,j.title,a.status,COALESCE(a.applied_date,''),COALESCE(a.follow_up_date,''),COALESCE(a.contact,''),COALESCE(a.notes,''),COALESCE(a.feeling,''),COALESCE(a.feeling_notes,''),
			COALESCE((SELECT MAX(t) FROM (SELECT occurred_at t FROM communications WHERE application_id=a.id UNION ALL SELECT occurred_at FROM application_events WHERE application_id=a.id UNION ALL SELECT scheduled_at FROM interviews WHERE application_id=a.id AND scheduled_at <= strftime('%Y-%m-%dT%H:%M','now','localtime'))),'')
			FROM applications a JOIN jobs j ON j.id=a.job_id ORDER BY CASE WHEN a.follow_up_date IS NULL OR a.follow_up_date='' THEN 1 ELSE 0 END, a.follow_up_date, j.company`)
		if err != nil {
			http.Error(w, "Could not load applications", 500)
			return
		}
		defer rows.Close()
		p := TrackerPage{Nav: loadNav(db, "apps", "Applications")}
		due := AppGroup{Title: "Follow up now", Note: "The follow-up date you set has arrived."}
		active := AppGroup{Title: "In progress", Note: "Applied and waiting, or in interviews."}
		later := AppGroup{Title: "Not applied yet", Note: "Tracked while you research or prepare."}
		closed := AppGroup{Title: "Closed", Note: "Accepted, rejected, withdrawn, or closed."}
		today := time.Now().Format("2006-01-02")
		for rows.Next() {
			var j TrackerJob
			if err = rows.Scan(&j.ID, &j.Company, &j.Title, &j.Status, &j.Applied, &j.Followup, &j.Contact, &j.Notes, &j.Feeling, &j.FeelingNotes, &j.LastActivity); err != nil {
				http.Error(w, "Could not read applications", 500)
				return
			}
			p.Total++
			stage := statusStage[j.Status]
			switch {
			case stage != "closed" && j.Followup != "" && j.Followup <= today:
				due.Jobs = append(due.Jobs, j)
			case stage == "active":
				active.Jobs = append(active.Jobs, j)
			case stage == "closed":
				closed.Jobs = append(closed.Jobs, j)
			default:
				later.Jobs = append(later.Jobs, j)
			}
		}
		if rows.Err() != nil {
			http.Error(w, "Could not read applications", 500)
			return
		}
		for _, g := range []AppGroup{due, active, later, closed} {
			if len(g.Jobs) > 0 {
				p.Groups = append(p.Groups, g)
			}
		}
		render(w, "dashboard", p)
	})

	http.HandleFunc("/resume", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		p := TrackerPage{Nav: loadNav(db, "resume", "Résumé"), Back: safeBack(r.URL.Query().Get("back"))}
		if p.Back != "" {
			p.BackLabel = backLabel(p.Back)
		}
		if err := db.QueryRow("SELECT value FROM tracker_settings WHERE key='base_resume'").Scan(&p.Base); err != nil && err != sql.ErrNoRows {
			http.Error(w, "Could not read résumé", 500)
			return
		}
		p.HasBase = strings.TrimSpace(p.Base) != ""
		p.ResumeTips = resumeTips(p.Base)
		p.Saved = r.URL.Query().Get("saved")
		p.SavedText = savedMessages[p.Saved]
		render(w, "resume", p)
	})

	http.HandleFunc("/track", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		id := positive(r.URL.Query().Get("id"))
		if id == 0 {
			http.NotFound(w, r)
			return
		}
		p, code, msg := loadTrackPage(db, id, r.URL.Query().Get("back"), r.URL.Query().Get("saved"), positive(r.URL.Query().Get("doc")))
		if code == http.StatusNotFound {
			http.NotFound(w, r)
			return
		}
		if code != 0 {
			http.Error(w, msg, code)
			return
		}
		render(w, "track", p)
	})

	http.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		if !localPost(r) {
			http.Error(w, "Local browser origin required", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		if parseForm(r) != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		kind := r.FormValue("kind")
		back := safeBack(r.FormValue("back"))
		frag := isFragmentRequest(r)
		if kind == "base" {
			// The base résumé belongs to no single job.
			if _, err := db.Exec(`INSERT INTO tracker_settings(key,value) VALUES('base_resume',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=CURRENT_TIMESTAMP`, r.FormValue("content")); err != nil {
				logTracker(err)
				http.Error(w, "Could not save the résumé", 500)
				return
			}
			if frag {
				p := TrackerPage{Nav: loadNav(db, "resume", "Résumé"), Back: back, Base: r.FormValue("content"), Saved: "base", SavedText: savedMessages["base"], Fragment: true}
				if back != "" {
					p.BackLabel = backLabel(back)
				}
				p.HasBase = strings.TrimSpace(p.Base) != ""
				p.ResumeTips = resumeTips(p.Base)
				self := "/resume"
				if back != "" {
					self += "?back=" + url.QueryEscape(back)
				}
				writeFragments(w, "Replace-Url", self, Fragment{Target: "#resume-view(innerHTML)", Name: "resumeview", Data: p})
				return
			}
			target := "/resume?saved=base"
			if back != "" {
				target += "&back=" + url.QueryEscape(back)
			}
			http.Redirect(w, r, target+"#saved", http.StatusSeeOther)
			return
		}
		id := positive(r.FormValue("job_id"))
		if id == 0 {
			http.Error(w, "Invalid job", 400)
			return
		}
		tx, err := db.Begin()
		if err != nil {
			http.Error(w, "Database busy", 500)
			return
		}
		defer tx.Rollback()
		var exists int
		if err = tx.QueryRow("SELECT 1 FROM jobs WHERE id=?", id).Scan(&exists); err != nil {
			http.Error(w, "Job not found", 404)
			return
		}
		_, err = tx.Exec(`INSERT INTO applications(job_id,status) SELECT id,COALESCE(NULLIF(status,''),'Research') FROM jobs WHERE id=? ON CONFLICT(job_id) DO NOTHING`, id)
		var aid int64
		if err == nil {
			err = tx.QueryRow("SELECT id FROM applications WHERE job_id=?", id).Scan(&aid)
		}
		rec := positive(r.FormValue("record_id"))
		fixURL := fmt.Sprintf("/track?id=%d", id)
		if back != "" {
			fixURL += "&back=" + url.QueryEscape(back)
		}
		switch {
		case err != nil:
		case kind == "application":
			status := r.FormValue("status")
			if statusStage[status] == "" {
				badRequest(w, frag, "Invalid status")
				return
			}
			feeling := r.FormValue("feeling")
			if feeling != "" && feeling != "Low" && feeling != "Medium" && feeling != "High" && feeling != "Unsure" {
				badRequest(w, frag, "Invalid feeling")
				return
			}
			applied, followup := r.FormValue("applied"), r.FormValue("followup")
			if !validDate(applied) || !validDate(followup) {
				const msg = "A date is not in the form YYYY-MM-DD, for example 2026-11-24. Correct the Follow up on or Date applied field and save again."
				if frag {
					writeFormError(w, msg)
					return
				}
				formProblem(w, db, msg, fixURL+"#status", "Back to the application")
				return
			}
			var previousStatus, previousFeeling, previousFeelingNotes string
			err = tx.QueryRow("SELECT status,COALESCE(feeling,''),COALESCE(feeling_notes,'') FROM applications WHERE id=?", aid).Scan(&previousStatus, &previousFeeling, &previousFeelingNotes)
			if err == nil {
				_, err = tx.Exec(`UPDATE applications SET status=?,applied_date=?,follow_up_date=?,contact=?,notes=?,feeling=?,feeling_notes=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, status, nullIfEmpty(applied), nullIfEmpty(followup), r.FormValue("contact"), r.FormValue("notes"), feeling, r.FormValue("feeling_notes"), aid)
			}
			if err == nil {
				_, err = tx.Exec("UPDATE jobs SET status=?,applied_date=?,follow_up_date=? WHERE id=?", status, nullIfEmpty(applied), nullIfEmpty(followup), id)
			}
			if err == nil && status != previousStatus {
				_, err = tx.Exec("INSERT INTO application_events(application_id,occurred_at,kind,detail) VALUES(?,?,?,?)", aid, time.Now().Format("2006-01-02T15:04"), "Status changed", previousStatus+" → "+status)
			}
			if err == nil && (feeling != previousFeeling || r.FormValue("feeling_notes") != previousFeelingNotes) {
				detail := feeling
				if detail == "" {
					detail = "Not set"
				}
				if note := strings.TrimSpace(r.FormValue("feeling_notes")); note != "" {
					detail += ". " + note
				}
				_, err = tx.Exec("INSERT INTO application_events(application_id,occurred_at,kind,detail) VALUES(?,?,?,?)", aid, time.Now().Format("2006-01-02T15:04"), "Confidence updated", detail)
			}
		case kind == "interview":
			vals := []any{r.FormValue("when"), r.FormValue("type"), r.FormValue("person"), r.FormValue("outcome"), r.FormValue("notes")}
			if rec == 0 {
				_, err = tx.Exec(`INSERT INTO interviews(scheduled_at,kind,interviewer,outcome,notes,application_id) VALUES(?,?,?,?,?,?)`, append(vals, aid)...)
			} else {
				res, e := tx.Exec(`UPDATE interviews SET scheduled_at=?,kind=?,interviewer=?,outcome=?,notes=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND application_id=?`, append(vals, rec, aid)...)
				err = checkedUpdate(res, e)
			}
		case kind == "message":
			vals := []any{r.FormValue("when"), r.FormValue("channel"), r.FormValue("direction"), r.FormValue("person"), r.FormValue("notes")}
			if rec == 0 {
				_, err = tx.Exec(`INSERT INTO communications(occurred_at,channel,direction,contact,notes,application_id) VALUES(?,?,?,?,?,?)`, append(vals, aid)...)
			} else {
				res, e := tx.Exec(`UPDATE communications SET occurred_at=?,channel=?,direction=?,contact=?,notes=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND application_id=?`, append(vals, rec, aid)...)
				err = checkedUpdate(res, e)
			}
		case kind == "document":
			docType := r.FormValue("doc_type")
			if docType != "Resume" && docType != "Cover letter" {
				badRequest(w, frag, "Invalid document type")
				return
			}
			title := strings.TrimSpace(r.FormValue("title"))
			if title == "" {
				const msg = "The name field was blank. Give the résumé version or cover letter a name and save again."
				if frag {
					writeFormError(w, msg)
					return
				}
				formProblem(w, db, msg, fixURL+"#documents", "Back to the application")
				return
			}
			if rec == 0 {
				_, err = tx.Exec(`INSERT INTO application_documents(kind,title,content,application_id) VALUES(?,?,?,?)`, docType, title, r.FormValue("content"), aid)
			} else {
				res, e := tx.Exec(`UPDATE application_documents SET kind=?,title=?,content=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND application_id=?`, docType, title, r.FormValue("content"), rec, aid)
				err = checkedUpdate(res, e)
			}
		default:
			badRequest(w, frag, "Invalid action")
			return
		}
		if err != nil {
			logTracker(err)
			http.Error(w, "Could not save; check the database", 500)
			return
		}
		if err = tx.Commit(); err != nil {
			http.Error(w, "Could not save", 500)
			return
		}
		if frag {
			p, code, msg := loadTrackPage(db, id, back, kind, 0)
			if code != 0 {
				http.Error(w, msg, code)
				return
			}
			p.Fragment = true
			self := fmt.Sprintf("/track?id=%d", id)
			if back != "" {
				self += "&back=" + url.QueryEscape(back)
			}
			writeFragments(w, "Replace-Url", self,
				Fragment{Target: "#track-view(innerHTML)", Name: "trackview", Data: p},
				Fragment{Target: "#tabs(innerHTML)", Name: "tabs", Data: p.Nav})
			return
		}
		target := fmt.Sprintf("/track?id=%d&saved=%s", id, kind)
		if back != "" {
			target += "&back=" + url.QueryEscape(back)
		}
		http.Redirect(w, r, target+"#saved", http.StatusSeeOther)
	})
}

// loadTrackPage reads everything the application page shows. A non-zero code
// is the HTTP status to answer with, and msg its text.
func loadTrackPage(db *sql.DB, id int64, back, saved string, doc int64) (p TrackerPage, code int, msg string) {
	info, err := loadJob(db, id)
	if err == sql.ErrNoRows {
		return p, http.StatusNotFound, ""
	}
	if err != nil {
		return p, 500, "Could not read job"
	}
	p = TrackerPage{Info: info, Statuses: statusOrder}
	p.Nav = loadNav(db, "apps", info.Title)
	p.Back = safeBack(back)
	if p.Back == "" {
		p.Back = "/"
	}
	p.BackLabel = backLabel(p.Back)
	p.Saved = saved
	p.SavedText = savedMessages[p.Saved]
	p.Self = fmt.Sprintf("/track?id=%d", id)
	err = db.QueryRow(`SELECT j.id,j.company,j.title,COALESCE(a.status,j.status,'Research'),COALESCE(a.applied_date,''),COALESCE(a.follow_up_date,''),COALESCE(a.contact,''),COALESCE(a.notes,''),COALESCE(a.feeling,''),COALESCE(a.feeling_notes,''),CASE WHEN a.id IS NULL THEN 0 ELSE 1 END FROM jobs j LEFT JOIN applications a ON a.job_id=j.id WHERE j.id=?`, id).Scan(&p.Job.ID, &p.Job.Company, &p.Job.Title, &p.Job.Status, &p.Job.Applied, &p.Job.Followup, &p.Job.Contact, &p.Job.Notes, &p.Job.Feeling, &p.Job.FeelingNotes, &p.HasApp)
	if err != nil {
		return p, 500, "Could not read application"
	}
	p.Closed = statusStage[p.Job.Status] == "closed"
	reached := false
	for i := len(statusOrder) - 1; i >= 0; i-- {
		s := statusOrder[i]
		if statusStage[s] == "closed" {
			continue
		}
		cur := s == p.Job.Status
		if cur {
			reached = true
		}
		p.Steps = append([]Step{{Label: s, Current: cur, Done: reached && !cur}}, p.Steps...)
	}
	p.FollowDue = p.Job.Followup != "" && p.Job.Followup <= time.Now().Format("2006-01-02") && statusStage[p.Job.Status] != "closed"
	var aid int64
	err = db.QueryRow("SELECT id FROM applications WHERE job_id=?", id).Scan(&aid)
	if err != nil && err != sql.ErrNoRows {
		return p, 500, "Could not read application"
	}
	if aid != 0 {
		interviews, err := readRecords(db, `SELECT id,COALESCE(scheduled_at,''),COALESCE(kind,''),COALESCE(interviewer,''),COALESCE(outcome,''),COALESCE(notes,'') FROM interviews WHERE application_id=? ORDER BY scheduled_at DESC,id DESC`, aid)
		if err != nil {
			return p, 500, "Could not read interviews"
		}
		messages, err := readRecords(db, `SELECT id,COALESCE(occurred_at,''),COALESCE(channel,''),COALESCE(direction,''),COALESCE(contact,''),COALESCE(notes,'') FROM communications WHERE application_id=? ORDER BY occurred_at DESC,id DESC`, aid)
		if err != nil {
			return p, 500, "Could not read messages"
		}
		docs, err := readRecords(db, `SELECT id,kind,title,content,updated_at,'' FROM application_documents WHERE application_id=? ORDER BY updated_at DESC,id DESC`, aid)
		if err != nil {
			return p, 500, "Could not read documents"
		}
		for _, d := range docs {
			if d.A == "Resume" {
				p.Resumes = append(p.Resumes, d)
			} else {
				p.Letters = append(p.Letters, d)
			}
		}
		p.Timeline, err = buildTimeline(db, aid, p.Job.Applied, interviews, messages)
		if err != nil {
			return p, 500, "Could not read timeline"
		}
	}
	err = db.QueryRow("SELECT value FROM tracker_settings WHERE key='base_resume'").Scan(&p.Base)
	if err != nil && err != sql.ErrNoRows {
		return p, 500, "Could not read résumé"
	}
	p.HasBase = strings.TrimSpace(p.Base) != ""
	p.Resume, p.ResumeTitle = p.Base, "your base résumé"
	p.ActiveID = doc
	for _, d := range p.Resumes {
		if d.ID == p.ActiveID {
			p.Resume, p.ResumeTitle = d.C, "“"+d.B+"”"
		}
	}
	var skills string
	if err = db.QueryRow("SELECT COALESCE(skills_required,'') FROM jobs WHERE id=?", id).Scan(&skills); err != nil {
		return p, 500, "Could not read skills"
	}
	matched, missing := compareSkills(skills, p.Resume)
	if strings.TrimSpace(skills) == "" || strings.TrimSpace(p.Resume) == "" {
		p.MatchNote = missing
	} else {
		p.Matched, p.Missing = splitList(matched), splitList(missing)
	}
	return p, 0, ""
}

func buildTimeline(db *sql.DB, applicationID int64, applied string, interviews, messages []TrackerRecord) ([]TimelineEvent, error) {
	var events []TimelineEvent
	add := func(e TimelineEvent) {
		if t, _, ok := parseWhen(e.When); ok {
			e.date = t
		}
		events = append(events, e)
	}
	if applied != "" {
		add(TimelineEvent{When: applied, Title: "Applied", Detail: "Application date you recorded."})
	}
	for _, m := range messages {
		title := strings.TrimSpace(m.B)
		if title == "" {
			title = "Message"
		}
		switch strings.ToLower(m.C) {
		case "received":
			title += " received"
		case "sent":
			title += " sent"
		}
		if m.D != "" {
			if strings.HasSuffix(title, "received") {
				title += " from " + m.D
			} else {
				title += " to " + m.D
			}
		}
		add(TimelineEvent{When: m.A, Title: title, Detail: m.E, Kind: "message", Rec: m})
	}
	for _, i := range interviews {
		title := "Interview"
		if i.B != "" {
			title = i.B
		}
		if i.C != "" {
			title += " with " + i.C
		}
		detail := ""
		if i.D != "" {
			detail = "Outcome: " + i.D
		}
		if i.E != "" {
			if detail != "" {
				detail += "\n"
			}
			detail += i.E
		}
		add(TimelineEvent{When: i.A, Title: title, Detail: detail, Kind: "interview", Rec: i})
	}
	rows, err := db.Query("SELECT occurred_at,kind,detail FROM application_events WHERE application_id=? ORDER BY id", applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var when, kind, detail string
		if err = rows.Scan(&when, &kind, &detail); err != nil {
			return nil, err
		}
		add(TimelineEvent{When: when, Title: kind, Detail: detail})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].date.IsZero() {
			return false
		}
		if events[j].date.IsZero() {
			return true
		}
		return events[i].date.Before(events[j].date)
	})
	now := time.Now()
	var previous time.Time
	for i := range events {
		if events[i].date.IsZero() {
			events[i].Display = "Date not recorded"
			continue
		}
		d := events[i].date
		events[i].Display = fmtDate(events[i].When)
		events[i].Upcoming = d.After(now)
		day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
		if !previous.IsZero() {
			switch days := int(day.Sub(previous).Hours() / 24); days {
			case 0:
				events[i].Gap = "same day"
			case 1:
				events[i].Gap = "1 day later"
			default:
				events[i].Gap = fmt.Sprintf("%d days later", days)
			}
		}
		previous = day
	}
	return events, nil
}

// badRequest answers a malformed /save: a flash alert for HTMLeX forms, a
// plain 400 for navigations.
func badRequest(w http.ResponseWriter, frag bool, msg string) {
	if frag {
		writeFormError(w, msg)
		return
	}
	http.Error(w, msg, 400)
}
