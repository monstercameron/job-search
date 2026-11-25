package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
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

// nextUpIDs returns the queue in order.
func nextUpIDs(db *sql.DB) ([]int64, error) {
	rows, err := db.Query("SELECT id FROM jobs WHERE " + nextUpWhere + " ORDER BY " + nextUpOrder)
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
}

func nextURL(n int) string {
	if n <= 1 {
		return "/next"
	}
	return "/next?n=" + strconv.Itoa(n)
}

func loadNextPage(db *sql.DB, n int) (NextPage, error) {
	ids, err := nextUpIDs(db)
	if err != nil {
		return NextPage{}, err
	}
	p := NextPage{Total: len(ids), FirstURL: nextURL(1)}
	if p.Total == 0 {
		p.Self = nextURL(1)
		return p, nil
	}
	p.N = min(max(n, 1), p.Total)
	p.Self = nextURL(p.N)
	if p.N > 1 {
		p.PrevURL = nextURL(p.N - 1)
	}
	if p.N < p.Total {
		p.NextURL = nextURL(p.N + 1)
		after, err := loadJob(db, ids[p.N])
		if err != nil {
			return p, err
		}
		p.After = &after
	}
	p.Job, err = loadJob(db, ids[p.N-1])
	return p, err
}

// queuePosition is the 1-based place of a job in the queue, or 0.
func queuePosition(db *sql.DB, id int64) (int, error) {
	ids, err := nextUpIDs(db)
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
		p.Nav = loadNav(db, "next", title)
		if isFragmentRequest(r) {
			p.Fragment = true
			writeFragments(w, urlHeader, p.Self,
				Fragment{Target: "#next-view(innerHTML)", Name: "nextview", Data: p},
				Fragment{Target: "title(innerHTML)", HTML: template.HTML(template.HTMLEscapeString(title + " · Job search"))})
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
		p, err := loadNextPage(db, n)
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
			pos, err := queuePosition(db, id)
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
			http.Redirect(w, r, "/next?n="+strconv.Itoa(max(n, 1))+"&"+redirect, http.StatusSeeOther)
			return
		}
		p, err := loadNextPage(db, n)
		if err != nil {
			log.Printf("next up: %v", err)
			http.Error(w, "Could not read the jobs database", 500)
			return
		}
		p.Notice = notice
		show(w, r, p, "Replace-Url")
	})
}
