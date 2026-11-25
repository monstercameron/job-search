package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// seedNextUp makes a queue where only jobs 1, 4 and 7 have not been applied to.
func seedNextUp(t *testing.T) {
	t.Helper()
	resetJobs(t,
		`INSERT INTO jobs(id,company,title,url,status,score_total,priority) VALUES
			(1,'Acme','Queue First','https://example.com/1','Research',90,'High'),
			(2,'Beta','Already Applied','https://example.com/2','Applied',95,'High'),
			(3,'Gamma','Has Applied Date','https://example.com/3','Research',99,'High'),
			(4,'Delta','Queue Second','https://example.com/4','Interested',80,'Medium'),
			(5,'Epsilon','In Screening','https://example.com/5','Research',85,'High'),
			(6,'Zeta','Was Rejected','https://example.com/6','Rejected',70,'Low'),
			(7,'Eta','Queue Third','https://example.com/7','Research',NULL,'Low'),
			(8,'Theta','Tie Low Priority','https://example.com/8','Research',80,'Low')`,
		`UPDATE jobs SET applied_date='2026-11-20' WHERE id=3`,
		`INSERT INTO applications(job_id,status) VALUES(4,'Preparing'),(5,'Screening')`,
		`INSERT INTO applications(job_id,status,applied_date) VALUES(8,'Interested','2026-11-21')`,
	)
}

func TestNextUpQueueSkipsAppliedAndClosedJobs(t *testing.T) {
	seedNextUp(t)
	ids, err := nextUpIDs(testDB)
	if err != nil {
		t.Fatal(err)
	}
	// 2 is Applied, 3 has an applied date, 5's application is in Screening,
	// 6 was rejected, and 8's application has an applied date.
	if want := []int64{1, 4, 7}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("queue %v, want %v", ids, want)
	}
}

func TestNextUpOrdersTiesByPriority(t *testing.T) {
	resetJobs(t, `INSERT INTO jobs(id,company,title,url,score_total,priority) VALUES
		(1,'A','Low','https://example.com/a',80,'Low'),(2,'B','High','https://example.com/b',80,'High'),(3,'C','Medium','https://example.com/c',80,'Medium')`)
	ids, err := nextUpIDs(testDB)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{2, 3, 1}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("queue %v, want %v", ids, want)
	}
}

func nextRequest(path string, fragment bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "http://127.0.0.1:8080"+path, nil)
	if fragment {
		r.Header.Set("Sec-Fetch-Mode", "cors")
		r.Header.Set("Sec-Fetch-Dest", "empty")
	}
	w := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(w, r)
	return w
}

func TestNextUpPageShowsOneJobWithPreviousAndNext(t *testing.T) {
	seedNextUp(t)
	w := nextRequest("/next?n=2", false)
	body := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	for _, want := range []string{
		"<!doctype html>", `<div id="next-view">`, ">Queue Second</a>",
		`<strong>2</strong> of 3`, `href="/next" get="/next" rel="prev"`, `href="/next?n=3" get="/next?n=3" rel="next"`,
		`/track?id=4&back=%2fnext%3fn%3d2`, "Tracking: Preparing", "After this:", ">Queue Third</a>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	for _, never := range []string{"Already Applied", "Has Applied Date", "In Screening", "Was Rejected", "Tie Low Priority"} {
		if strings.Contains(body, never) {
			t.Errorf("page shows %q, which is not in the queue", never)
		}
	}
}

func TestNextUpFragmentAndBounds(t *testing.T) {
	seedNextUp(t)
	w := nextRequest("/next?n=3", true)
	body := w.Body.String()
	if w.Header().Get("Push-Url") != "/next?n=3" || !strings.Contains(body, `<fragment target="#next-view(innerHTML)">`) || strings.Contains(body, "<!doctype") {
		t.Fatalf("fragment response wrong: Push-Url %q\n%s", w.Header().Get("Push-Url"), body)
	}
	if !strings.Contains(body, `id="next-title" tabindex="-1" autofocus`) || !strings.Contains(body, "Back to the first") {
		t.Error("last job should take focus and offer a way back to the first")
	}
	// Out-of-range positions land on the nearest real job.
	if b := nextRequest("/next?n=99", false).Body.String(); !strings.Contains(b, ">Queue Third</a>") {
		t.Error("n=99 should show the last job")
	}
	if b := nextRequest("/next?n=-4", false).Body.String(); !strings.Contains(b, ">Queue First</a>") || !strings.Contains(b, `aria-disabled="true"`) {
		t.Error("n=-4 should show the first job with Previous disabled")
	}
}

func TestNextUpEmptyQueue(t *testing.T) {
	resetJobs(t, `INSERT INTO jobs(id,company,title,url,status) VALUES(1,'Acme','Done','https://example.com/1','Applied')`)
	if b := nextRequest("/next", false).Body.String(); !strings.Contains(b, "Nothing left to apply to") {
		t.Error("empty queue should say so")
	}
}

func TestSafeBackAcceptsNextUp(t *testing.T) {
	if safeBack("/next?n=4") != "/next?n=4" || safeBack("/next") != "/next" || backLabel("/next?n=4") != "Back to next up" {
		t.Error("the application page should link back to Next up")
	}
}

func nextPost(fields url.Values, fragment bool, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080/next/save", strings.NewReader(fields.Encode()))
	r.Host = "127.0.0.1:8080"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if fragment {
		r.Header.Set("Sec-Fetch-Mode", "cors")
		r.Header.Set("Sec-Fetch-Dest", "empty")
	}
	w := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(w, r)
	return w
}

const localOrigin = "http://127.0.0.1:8080"

func TestNotInterestedRemovesFromQueueAndUndoPutsItBack(t *testing.T) {
	seedNextUp(t)
	w := nextPost(url.Values{"action": {"dismiss"}, "job_id": {"1"}, "n": {"1"}}, true, localOrigin)
	body := w.Body.String()
	if w.Code != 200 || w.Header().Get("Replace-Url") != "/next" {
		t.Fatalf("dismiss: status %d, Replace-Url %q", w.Code, w.Header().Get("Replace-Url"))
	}
	for _, want := range []string{"Removed “Queue First” from Next up.", `name="action" value="restore"`, `name="job_id" value="1"`, ">Queue Second</a>", `<strong>1</strong> of 2`, `id="saved" role="status" tabindex="-1" autofocus`} {
		if !strings.Contains(body, want) {
			t.Errorf("dismiss response missing %q", want)
		}
	}
	if strings.Contains(body, `id="next-title" tabindex="-1" autofocus`) {
		t.Error("only the notice should take focus")
	}
	if ids, _ := nextUpIDs(testDB); !reflect.DeepEqual(ids, []int64{4, 7}) {
		t.Fatalf("queue after dismiss %v", ids)
	}
	// The job page and the Jobs list say so, and offer the way back.
	if b := nextRequest("/track?id=1", false).Body.String(); !strings.Contains(b, "Not interested") || !strings.Contains(b, "Put back in Next up") {
		t.Error("job page should show Not interested and a Put back button")
	}

	w = nextPost(url.Values{"action": {"restore"}, "job_id": {"1"}}, true, localOrigin)
	body = w.Body.String()
	if !strings.Contains(body, "Put “Queue First” back in Next up.") || !strings.Contains(body, ">Queue First</a>") || strings.Contains(body, `value="restore"`) {
		t.Errorf("restore should show the job that came back, without another Undo:\n%s", body)
	}
	if ids, _ := nextUpIDs(testDB); !reflect.DeepEqual(ids, []int64{1, 4, 7}) {
		t.Fatalf("queue after restore %v", ids)
	}
}

func TestNotInterestedWithoutJavaScriptRedirects(t *testing.T) {
	seedNextUp(t)
	w := nextPost(url.Values{"action": {"dismiss"}, "job_id": {"4"}, "n": {"2"}}, false, localOrigin)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/next?n=2&removed=4" {
		t.Fatalf("status %d, Location %q", w.Code, w.Header().Get("Location"))
	}
	b := nextRequest("/next?n=2&removed=4", false).Body.String()
	if !strings.Contains(b, "Removed “Queue Second” from Next up.") || !strings.Contains(b, ">Queue Third</a>") {
		t.Error("the redirect target should confirm the removal and show the next job")
	}
	// A stale removed= link for a job that is back in the queue shows no notice.
	nextPost(url.Values{"action": {"restore"}, "job_id": {"4"}}, false, localOrigin)
	if b := nextRequest("/next?removed=4", false).Body.String(); strings.Contains(b, "Removed “") {
		t.Error("no removal notice for a job that is back")
	}
}

func TestNotInterestedNeedsLocalOrigin(t *testing.T) {
	seedNextUp(t)
	for _, origin := range []string{"", "http://evil.example"} {
		if w := nextPost(url.Values{"action": {"dismiss"}, "job_id": {"1"}}, true, origin); w.Code != http.StatusForbidden {
			t.Errorf("origin %q: status %d, want 403", origin, w.Code)
		}
	}
	if w := nextPost(url.Values{"action": {"explode"}, "job_id": {"1"}}, true, localOrigin); w.Code != http.StatusBadRequest {
		t.Errorf("unknown action: status %d", w.Code)
	}
	if ids, _ := nextUpIDs(testDB); len(ids) != 3 {
		t.Errorf("nothing should have been dismissed: %v", ids)
	}
}
