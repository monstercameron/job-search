package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Next up shows one job at a time from the queue of jobs still worth applying
// to: best match first, then priority. A job leaves the queue as soon as it
// has been applied to (any stage from Applied on, an applied date, or a closed
// application), or when the owner marks it "Not interested", so the queue only ever
// holds work still to do.

// migrateNextUp adds the table of jobs taken out of Next up. It is separate
// from the job's status, so dismissing never changes the application pipeline
// and putting a job back is a single delete.
func migrateNextUp(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS job_dismissals (job_id INTEGER PRIMARY KEY REFERENCES jobs(id), dismissed_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`)
	return err
}

// nextUpWhere keeps jobs that have not been applied to, are not closed and
// were not dismissed. The application's stage wins over the job's own status.
const nextUpWhere = `coalesce((SELECT a.status FROM applications a WHERE a.job_id = jobs.id), jobs.status, '') NOT IN ('Applied','Screening','Interviewing','Offer','Accepted','Rejected','Withdrawn','Closed')
	AND trim(coalesce(jobs.applied_date,'')) = ''
	AND NOT EXISTS (SELECT 1 FROM applications a WHERE a.job_id = jobs.id AND trim(coalesce(a.applied_date,'')) <> '')
	AND NOT EXISTS (SELECT 1 FROM job_dismissals d WHERE d.job_id = jobs.id)`

const nextUpOrder = `(score_total IS NULL OR trim(score_total) = '') ASC, CAST(score_total AS INTEGER) DESC,
	(CASE priority WHEN 'High' THEN 1 WHEN 'Medium' THEN 2 WHEN 'Low' THEN 3 ELSE 4 END) ASC, company COLLATE NOCASE ASC, id ASC`

// Every job in the queue is at some stage short of applying. The radio group
// on Next up narrows the queue to one stage, so the owner can work through,
// say, everything they are preparing before turning to new leads.
type NextStage struct {
	Key, Label string
	Count      int
	On         bool
}

var nextStages = []NextStage{{Key: "", Label: "All not applied"}, {Key: "new", Label: "New leads"}, {Key: "interested", Label: "Interested"}, {Key: "preparing", Label: "Preparing"}}

// nextStageOf is a job's stage: its application's, else its own status.
const nextStageOf = `coalesce((SELECT a.status FROM applications a WHERE a.job_id = jobs.id), jobs.status, '')`

// validStage keeps a known stage key and turns anything else into "" (all).
func validStage(s string) string {
	for _, st := range nextStages {
		if st.Key == s {
			return s
		}
	}
	return ""
}

func stageWhere(stage string) string {
	switch stage {
	case "new":
		return " AND " + nextStageOf + " NOT IN ('Interested','Preparing')"
	case "interested":
		return " AND " + nextStageOf + " = 'Interested'"
	case "preparing":
		return " AND " + nextStageOf + " = 'Preparing'"
	}
	return ""
}

// stageCounts counts the queue by stage, with stage marking the chosen one.
func stageCounts(db *sql.DB, stage string) ([]NextStage, error) {
	out := append([]NextStage(nil), nextStages...)
	var interested, preparing, total int
	err := db.QueryRow(`SELECT count(*), coalesce(sum(s = 'Interested'), 0), coalesce(sum(s = 'Preparing'), 0)
		FROM (SELECT `+nextStageOf+` AS s FROM jobs WHERE `+nextUpWhere+`)`).Scan(&total, &interested, &preparing)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].On = out[i].Key == stage
		switch out[i].Key {
		case "":
			out[i].Count = total
		case "new":
			out[i].Count = total - interested - preparing
		case "interested":
			out[i].Count = interested
		case "preparing":
			out[i].Count = preparing
		}
	}
	return out, nil
}

// nextUpIDs returns the queue, or one stage of it, in order.
func nextUpIDs(db *sql.DB, stage string) ([]int64, error) {
	rows, err := db.Query("SELECT id FROM jobs WHERE " + nextUpWhere + stageWhere(stage) + " ORDER BY " + nextUpOrder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// NextNotice confirms a dismissal (with Undo) or a job put back.
type NextNotice struct {
	Text   string
	UndoID int64 // set after a dismissal: the job Undo puts back
}

// NextPage is one job from the queue with its place in it.
type NextPage struct {
	Nav                    Nav
	Job                    Job
	N, Total               int
	Self, PrevURL, NextURL string
	FirstURL               string
	After                  *Job // the job that comes next, previewed under the pager
	Notice                 *NextNotice
	Fragment               bool
	Stage                  string // "" for the whole queue
	StageLabel             string
	Stages                 []NextStage
	// FocusTitle moves focus to the job title after a swap. A change of
	// stage leaves focus on the radio button, so arrow keys keep cycling.
	FocusTitle bool
}

func nextURL(stage string, n int) string {
	v := url.Values{}
	if n > 1 {
		v.Set("n", strconv.Itoa(n))
	}
	if stage != "" {
		v.Set("stage", stage)
	}
	if len(v) == 0 {
		return "/next"
	}
	return "/next?" + v.Encode()
}

func loadNextPage(db *sql.DB, stage string, n int) (NextPage, error) {
	stage = validStage(stage)
	ids, err := nextUpIDs(db, stage)
	if err != nil {
		return NextPage{}, err
	}
	p := NextPage{Total: len(ids), FirstURL: nextURL(stage, 1), Stage: stage}
	if p.Stages, err = stageCounts(db, stage); err != nil {
		return p, err
	}
	for _, st := range p.Stages {
		if st.On {
			p.StageLabel = st.Label
		}
	}
	if p.Total == 0 {
		p.Self = nextURL(stage, 1)
		return p, nil
	}
	p.N = min(max(n, 1), p.Total)
	p.Self = nextURL(stage, p.N)
	if p.N > 1 {
		p.PrevURL = nextURL(stage, p.N-1)
	}
	if p.N < p.Total {
		p.NextURL = nextURL(stage, p.N+1)
		after, err := loadJob(db, ids[p.N])
		if err != nil {
			return p, err
		}
		p.After = &after
	}
	p.Job, err = loadJob(db, ids[p.N-1])
	return p, err
}

// queuePosition is the 1-based place of a job in the queue (or the stage
// of it), or 0.
func queuePosition(db *sql.DB, stage string, id int64) (int, error) {
	ids, err := nextUpIDs(db, stage)
	for i, v := range ids {
		if v == id {
			return i + 1, err
		}
	}
	return 0, err
}

func jobTitle(db *sql.DB, id int64) (string, bool) {
	var title string
	err := db.QueryRow("SELECT title FROM jobs WHERE id = ?", id).Scan(&title)
	return title, err == nil
}

func isDismissed(db *sql.DB, id int64) bool {
	var one int
	return db.QueryRow("SELECT 1 FROM job_dismissals WHERE job_id = ?", id).Scan(&one) == nil
}

func dismissedNotice(title string, id int64) *NextNotice {
	return &NextNotice{Text: "Removed “" + title + "” from Next up.", UndoID: id}
}

func restoredNotice(title string) *NextNotice {
	return &NextNotice{Text: "Put “" + title + "” back in Next up."}
}

func registerNextUp(db *sql.DB) {
	show := func(w http.ResponseWriter, r *http.Request, p NextPage, urlHeader string) {
		title := "Next up"
		if p.Total > 0 {
			title = fmt.Sprintf("Next up, %d of %d: %s", p.N, p.Total, p.Job.Title)
		}
		if p.Stage != "" {
			title += " (" + p.StageLabel + ")"
		}
		p.Nav = loadNav(db, "next", title)
		if isFragmentRequest(r) {
			p.Fragment = true
			// The stage radio group sends keep_focus: it is left in place
			// so focus stays on it. Any other swap redraws it too, for the
			// new counts or the stage after Back in the browser.
			keep := r.FormValue("keep_focus") != ""
			p.FocusTitle = !keep && p.Notice == nil
			frags := []Fragment{
				{Target: "#next-view(innerHTML)", Name: "nextview", Data: p},
				{Target: "title(innerHTML)", HTML: template.HTML(template.HTMLEscapeString(title + " · Job search"))},
			}
			if !keep {
				frags = append(frags, Fragment{Target: "#next-stages(innerHTML)", Name: "nextstages", Data: p})
			}
			writeFragments(w, urlHeader, p.Self, frags...)
			return
		}
		render(w, "nextup", p)
	}

	http.HandleFunc("/next", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		q := r.URL.Query()
		n, _ := strconv.Atoi(q.Get("n"))
		p, err := loadNextPage(db, q.Get("stage"), n)
		if err != nil {
			log.Printf("next up: %v", err)
			http.Error(w, "Could not read the jobs database", 500)
			return
		}
		// After a save without JavaScript, the redirect names what changed.
		if id := positive(q.Get("removed")); id != 0 && isDismissed(db, id) {
			if t, ok := jobTitle(db, id); ok {
				p.Notice = dismissedNotice(t, id)
			}
		} else if id := positive(q.Get("restored")); id != 0 && !isDismissed(db, id) {
			if t, ok := jobTitle(db, id); ok {
				p.Notice = restoredNotice(t)
			}
		}
		show(w, r, p, "Push-Url")
	})

	// /next/save takes a job out of Next up ("dismiss") or puts it back
	// ("restore"). Dismissing shows the job that moved into the same place;
	// restoring shows the job that was put back.
	http.HandleFunc("/next/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		if !localPost(r) {
			http.Error(w, "Local browser origin required", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
		if parseForm(r) != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		id := positive(r.FormValue("job_id"))
		title, ok := jobTitle(db, id)
		if id == 0 || !ok {
			http.Error(w, "Invalid job", 400)
			return
		}
		n, _ := strconv.Atoi(r.FormValue("n"))
		stage := validStage(r.FormValue("stage"))
		var notice *NextNotice
		var redirect string
		switch r.FormValue("action") {
		case "dismiss":
			if _, err := db.Exec("INSERT OR IGNORE INTO job_dismissals(job_id) VALUES(?)", id); err != nil {
				logTracker(err)
				http.Error(w, "Could not save", 500)
				return
			}
			notice = dismissedNotice(title, id)
			redirect = "removed=" + strconv.FormatInt(id, 10)
		case "restore":
			if _, err := db.Exec("DELETE FROM job_dismissals WHERE job_id = ?", id); err != nil {
				logTracker(err)
				http.Error(w, "Could not save", 500)
				return
			}
			pos, err := queuePosition(db, stage, id)
			if err != nil {
				logTracker(err)
				http.Error(w, "Could not read the jobs database", 500)
				return
			}
			if pos > 0 { // an applied or closed job stays out of the queue even when put back
				n = pos
			}
			notice = restoredNotice(title)
			redirect = "restored=" + strconv.FormatInt(id, 10)
		default:
			http.Error(w, "Unknown action", 400)
			return
		}
		if !isFragmentRequest(r) {
			to := nextURL(stage, n)
			if strings.Contains(to, "?") {
				to += "&" + redirect
			} else {
				to += "?" + redirect
			}
			http.Redirect(w, r, to, http.StatusSeeOther)
			return
		}
		p, err := loadNextPage(db, stage, n)
		if err != nil {
			log.Printf("next up: %v", err)
			http.Error(w, "Could not read the jobs database", 500)
			return
		}
		p.Notice = notice
		show(w, r, p, "Replace-Url")
	})
}
