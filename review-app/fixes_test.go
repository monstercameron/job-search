package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

var testDB *sql.DB

// TestMain builds one throwaway database and registers the tracker routes
// once, because registerTracker installs handlers on http.DefaultServeMux.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "job-review-test")
	if err != nil {
		panic(err)
	}
	testDB, err = sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dir, "test.sqlite3")))
	if err != nil {
		panic(err)
	}
	_, err = testDB.Exec(`CREATE TABLE jobs (id INTEGER PRIMARY KEY, company TEXT NOT NULL, title TEXT NOT NULL, url TEXT NOT NULL UNIQUE,
		location TEXT, notes TEXT, skills_required TEXT, fit_notes TEXT, status TEXT NOT NULL DEFAULT 'Research', priority TEXT,
		work_arrangement TEXT, employment_type TEXT, industry TEXT, score_total INTEGER, salary_min INTEGER, salary_period TEXT,
		date_found TEXT NOT NULL DEFAULT '2026-11-24', last_checked TEXT, applied_date TEXT, follow_up_date TEXT,
		category TEXT, source TEXT, posting_status TEXT, gaps_questions TEXT, posting_date TEXT, application_deadline TEXT)`)
	if err != nil {
		panic(err)
	}
	if err = migrateTracker(testDB); err != nil {
		panic(err)
	}
	if err = migrateNextUp(testDB); err != nil {
		panic(err)
	}
	registerTracker(testDB)
	registerNextUp(testDB)
	code := m.Run()
	testDB.Close()
	os.RemoveAll(dir)
	os.Exit(code)
}

func resetJobs(t *testing.T, rows ...string) {
	t.Helper()
	for _, q := range []string{"DELETE FROM application_events", "DELETE FROM applications", "DELETE FROM job_dismissals", "DELETE FROM jobs"} {
		if _, err := testDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range rows {
		if _, err := testDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestContainsTermWholeWords(t *testing.T) {
	resume := strings.ToLower("Languages: Go, C#, TypeScript/JavaScript, Python. Backend: ASP.NET, Node.js, C++ tooling. Go→WASM.")
	cases := map[string]bool{
		"java":       false, // only inside "javascript"
		"javascript": true,
		"typescript": true,
		"go":         true,
		"c#":         true,
		"c":          false, // only inside "c#" and "c++"
		"c++":        true,
		".net":       true, // leading punctuation is not boundary-checked
		"node.js":    true,
		"python":     true,
		"rust":       false,
		"script":     false, // only inside longer words
	}
	for term, want := range cases {
		if got := containsTerm(resume, term); got != want {
			t.Errorf("containsTerm(%q) = %v, want %v", term, got, want)
		}
	}
	if containsTerm("good google", "go") {
		t.Error(`"go" must not match inside "good" or "google"`)
	}
}

func TestCompareSkillsNoSubstringFalsePositive(t *testing.T) {
	matched, missing := compareSkills("Java; Go; React", "TypeScript/JavaScript, Go, React")
	if matched != "Go, React" || missing != "Java" {
		t.Fatalf("matched=%q missing=%q", matched, missing)
	}
}

func TestLocalHostOnlyBlocksRebindingHosts(t *testing.T) {
	h := localHostOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for host, want := range map[string]int{
		"127.0.0.1:8080":    200,
		"localhost:8080":    200,
		"[::1]:8080":        200,
		"evil.example:8080": 403,
		"evil.example":      403,
		"192.168.1.5:8080":  403,
	} {
		r := httptest.NewRequest("GET", "http://"+host+"/tracker", nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("Host %s: got %d, want %d", host, w.Code, want)
		}
	}
}

func TestSaveStoresBlankDatesAsNull(t *testing.T) {
	resetJobs(t, `INSERT INTO jobs(id,company,title,url) VALUES(1,'A','Engineer','https://a.example/1')`)
	post := func(applied, followup string) int {
		v := url.Values{"job_id": {"1"}, "kind": {"application"}, "status": {"Applied"}, "applied": {applied}, "followup": {followup}}
		r := httptest.NewRequest("POST", "http://127.0.0.1:8080/save", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		w := httptest.NewRecorder()
		http.DefaultServeMux.ServeHTTP(w, r)
		return w.Code
	}
	if code := post("2026-11-24", ""); code != http.StatusSeeOther {
		t.Fatalf("save: got %d", code)
	}
	var jobFollow, appFollow sql.NullString
	var applied string
	if err := testDB.QueryRow("SELECT applied_date, follow_up_date FROM jobs WHERE id=1").Scan(&applied, &jobFollow); err != nil {
		t.Fatal(err)
	}
	if err := testDB.QueryRow("SELECT follow_up_date FROM applications WHERE job_id=1").Scan(&appFollow); err != nil {
		t.Fatal(err)
	}
	if applied != "2026-11-24" || jobFollow.Valid || appFollow.Valid {
		t.Fatalf("applied=%q jobs.follow_up=%v applications.follow_up=%v; blank dates must be NULL", applied, jobFollow, appFollow)
	}
	if code := post("11/24/2026", ""); code != http.StatusBadRequest {
		t.Fatalf("malformed date: got %d, want 400", code)
	}
}

func sortedCompanies(t *testing.T, sortKey, direction string) []string {
	t.Helper()
	r := httptest.NewRequest("GET", "/?sort="+sortKey+"&direction="+direction, nil)
	p, err := query(testDB, r)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, j := range p.Jobs {
		out = append(out, j.Company)
	}
	return out
}

func TestSortPriorityByRankNotAlphabet(t *testing.T) {
	resetJobs(t,
		`INSERT INTO jobs(company,title,url,priority) VALUES('Low co','t','https://x/1','Low')`,
		`INSERT INTO jobs(company,title,url,priority) VALUES('Medium co','t','https://x/2','Medium')`,
		`INSERT INTO jobs(company,title,url,priority) VALUES('High co','t','https://x/3','High')`,
		`INSERT INTO jobs(company,title,url,priority) VALUES('None co','t','https://x/4',NULL)`)
	if got := strings.Join(sortedCompanies(t, "priority", "asc"), ","); got != "High co,Medium co,Low co,None co" {
		t.Fatalf("asc: %s", got)
	}
	if got := strings.Join(sortedCompanies(t, "priority", "desc"), ","); got != "Low co,Medium co,High co,None co" {
		t.Fatalf("desc (blank stays last): %s", got)
	}
}

func TestSortStatusByPipeline(t *testing.T) {
	resetJobs(t,
		`INSERT INTO jobs(company,title,url,status) VALUES('Offer co','t','https://x/1','Offer')`,
		`INSERT INTO jobs(company,title,url,status) VALUES('Applied co','t','https://x/2','Applied')`,
		`INSERT INTO jobs(company,title,url,status) VALUES('Research co','t','https://x/3','Research')`,
		`INSERT INTO jobs(company,title,url,status) VALUES('Interviewing co','t','https://x/4','Interviewing')`)
	if got := strings.Join(sortedCompanies(t, "status", "asc"), ","); got != "Research co,Applied co,Interviewing co,Offer co" {
		t.Fatalf("asc: %s", got)
	}
}

func TestSortSalaryAnnualizesHourly(t *testing.T) {
	resetJobs(t,
		`INSERT INTO jobs(company,title,url,salary_min,salary_period) VALUES('Hourly 80','t','https://x/1',80,'hour')`,
		`INSERT INTO jobs(company,title,url,salary_min,salary_period) VALUES('Yearly 115k','t','https://x/2',115000,'year')`,
		`INSERT INTO jobs(company,title,url,salary_min,salary_period) VALUES('No period 100k','t','https://x/3',100000,NULL)`,
		`INSERT INTO jobs(company,title,url,salary_min,salary_period) VALUES('Unlisted','t','https://x/4',NULL,NULL)`)
	// 80/hour * 2080 = 166,400 a year, above the 115k salary.
	if got := strings.Join(sortedCompanies(t, "salary", "desc"), ","); got != "Hourly 80,Yearly 115k,No period 100k,Unlisted" {
		t.Fatalf("desc: %s", got)
	}
}

// TestPagesRender requests every screen, including one after a save, and
// checks it renders fully: template errors otherwise show as a cut-off page.
func TestPagesRender(t *testing.T) {
	resetJobs(t,
		`INSERT INTO jobs(id,company,title,url,score_total,skills_required) VALUES(7,'Acme','Platform Engineer','https://acme.example/7',80,'Go; Java')`)
	if _, err := testDB.Exec(`INSERT INTO applications(id,job_id,status,follow_up_date) VALUES(70,7,'Applied','2020-01-01')`); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec(`INSERT INTO communications(application_id,occurred_at,channel,direction,contact,notes) VALUES(70,'2026-11-20T09:00','Email','Sent','Pat','Followed up')`); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p, err := query(testDB, r)
		if err != nil {
			t.Fatal(err)
		}
		p.Nav = loadNav(testDB, "jobs", "Jobs")
		render(w, "page", p)
	})
	cases := map[string][]string{
		"/":                         {"Platform Engineer", "Best match", "</html>"},
		"/?q=nothing-matches":       {"Nothing matches", "</html>"},
		"/tracker":                  {"Follow up now", "Platform Engineer", "</html>"},
		"/resume?back=/track?id=7":  {"Base résumé", "Back to the application", "</html>"},
		"/track?id=7&back=/tracker": {"Back to applications", "Where it stands", "Email sent to Pat", "No base résumé saved yet", "</html>"},
		"/track?id=7&saved=message": {"Message logged.", "</html>"},
	}
	for path, wants := range cases {
		r := httptest.NewRequest("GET", "http://127.0.0.1:8080"+path, nil)
		w := httptest.NewRecorder()
		if path == "/" || strings.HasPrefix(path, "/?") {
			mux.ServeHTTP(w, r)
		} else {
			http.DefaultServeMux.ServeHTTP(w, r)
		}
		body := w.Body.String()
		if w.Code != 200 {
			t.Errorf("%s: status %d", path, w.Code)
		}
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", path, want)
			}
		}
	}
}

func TestSafeBackRejectsOffsite(t *testing.T) {
	for in, want := range map[string]string{
		"/?q=go":               "/?q=go",
		"/tracker":             "/tracker",
		"/track?id=3":          "/track?id=3",
		"//evil.example/":      "",
		"https://evil.example": "",
		`/\evil.example`:       "",
		"javascript:alert(1)":  "",
	} {
		if got := safeBack(in); got != want {
			t.Errorf("safeBack(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormProblemsRenderAnHTMLPageAndSavesLandOnTheBanner(t *testing.T) {
	resetJobs(t, `INSERT INTO jobs(id,company,title,url) VALUES(1,'A','Engineer','https://a.example/1')`)
	post := func(v url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8080/save", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		w := httptest.NewRecorder()
		http.DefaultServeMux.ServeHTTP(w, r)
		return w
	}
	w := post(url.Values{"job_id": {"1"}, "kind": {"document"}, "doc_type": {"Resume"}, "title": {"   "}, "back": {"/tracker"}})
	body := w.Body.String()
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("blank name: got %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	for _, want := range []string{"<title>Could not save", "Sections", "name field was blank", `#documents"`, "</html>"} {
		if !strings.Contains(body, want) {
			t.Errorf("error page missing %q", want)
		}
	}
	w = post(url.Values{"job_id": {"1"}, "kind": {"application"}, "status": {"Applied"}})
	if loc := w.Header().Get("Location"); w.Code != http.StatusSeeOther || !strings.HasSuffix(loc, "#saved") {
		t.Errorf("save redirect %d %q should end with #saved", w.Code, loc)
	}
}
