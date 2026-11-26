package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// metricsSetup gives the shared test database the column Metrics reads that
// the harness's jobs table lacks, runs the Metrics migration (twice, to prove
// it is idempotent) and empties the tracker tables.
func metricsSetup(t *testing.T) {
	t.Helper()
	if ok, err := hasColumn(testDB, "jobs", "resume_version"); err != nil {
		t.Fatal(err)
	} else if !ok {
		if _, err = testDB.Exec("ALTER TABLE jobs ADD COLUMN resume_version TEXT"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := migrateMetrics(testDB); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := migrateInsights(testDB); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{"DELETE FROM interviews", "DELETE FROM communications", "DELETE FROM application_documents", "DELETE FROM job_locations", "DELETE FROM job_requirements", "DELETE FROM companies"} {
		if _, err := testDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	resetJobs(t)
}

// addApplied saves a job and an applied application with a résumé label.
func addApplied(t *testing.T, id int, variant, status string) {
	t.Helper()
	if _, err := testDB.Exec(`INSERT INTO jobs(id,company,title,url,status,date_found) VALUES(?,?,?,?,?, '2026-11-20')`, id, fmt.Sprintf("Co%d", id), "Engineer", fmt.Sprintf("https://x.test/%d", id), status); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec(`INSERT INTO applications(job_id,status,applied_date,resume_variant) VALUES(?,?,'2026-11-18',?)`, id, status, nullIfEmpty(variant)); err != nil {
		t.Fatal(err)
	}
}

func getMetrics(t *testing.T) string {
	t.Helper()
	w := httptest.NewRecorder()
	metricsHandler(testDB)(w, httptest.NewRequest("GET", "http://127.0.0.1:8080/metrics", nil))
	if w.Code != 200 {
		t.Fatalf("GET /metrics = %d: %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

func TestWilsonAndNewcombe(t *testing.T) {
	lo, hi := wilson(0, 1)
	if lo != 0 || math.Abs(hi-0.7935) > 0.002 {
		t.Errorf("wilson(0,1) = %.4f..%.4f, want 0..0.7935", lo, hi)
	}
	lo, hi = wilson(5, 10)
	if math.Abs(lo-0.2366) > 0.002 || math.Abs(hi-0.7634) > 0.002 {
		t.Errorf("wilson(5,10) = %.4f..%.4f, want 0.2366..0.7634", lo, hi)
	}
	if lo, hi = wilson(0, 0); lo != 0 || hi != 1 {
		t.Errorf("wilson(0,0) = %v..%v, want 0..1", lo, hi)
	}
	// Equal rates: the interval for the difference straddles zero.
	d, lo, hi := newcombe(5, 10, 5, 10)
	if d != 0 || lo >= 0 || hi <= 0 {
		t.Errorf("newcombe equal rates = %v %v %v", d, lo, hi)
	}
	// A large clear gap excludes zero.
	if _, lo, _ = newcombe(10, 100, 60, 100); lo <= 0 {
		t.Errorf("newcombe 10%% vs 60%% of 100: lower bound %v should be above 0", lo)
	}
}

func TestSmallArmsAreNeverCompared(t *testing.T) {
	apps := []appRec{}
	for i := 0; i < 9; i++ {
		apps = append(apps, appRec{Applied: true, Variant: "A", Status: "Applied", Reached: rankApplied})
	}
	for i := 0; i < 12; i++ {
		apps = append(apps, appRec{Applied: true, Variant: "b", Status: "Screening", Reached: rankScreening})
	}
	arms := buildArms(apps)
	if len(arms) != 2 || arms[0].Name != "b" || arms[0].N != 12 || arms[1].N != 9 {
		t.Fatalf("arms = %+v", arms)
	}
	if got := compareArms(arms); got != nil {
		t.Fatalf("9 vs 12 applied must not be compared, got %+v", got)
	}
	v := buildAB(apps)
	if v.Verdict.Head != "Too early to conclude anything" || !strings.Contains(v.Verdict.Detail, "A has 9") || len(v.Compares) != 0 {
		t.Errorf("verdict = %+v", v.Verdict)
	}
	// Case-insensitive grouping: "a" joins "A", which reaches 10 and is compared.
	apps = append(apps, appRec{Applied: true, Variant: "a", Status: "Applied", Reached: rankApplied})
	arms = buildArms(apps)
	if len(arms) != 2 || arms[1].N != 10 {
		t.Fatalf("a and A should be one arm of 10: %+v", arms)
	}
	cmp := compareArms(arms)
	if len(cmp) != 1 {
		t.Fatalf("10 vs 12 should be compared once: %+v", cmp)
	}
	// 0 of 10 against 12 of 12 is a clear gap, and the page says so in words.
	if cmp[0].Base != "b" || cmp[0].Other != "A" || cmp[0].Tone != "bad" {
		t.Errorf("expected A clearly lower than b: %+v", cmp[0])
	}
}

func TestMetricsEmptyDatabase(t *testing.T) {
	metricsSetup(t)
	body := getMetrics(t)
	for _, want := range []string{
		`aria-current="page"><svg class="i i-metrics"`, "Pipeline funnel", "Match-score distribution", "Listing age when added",
		"Start a résumé test", "No test has started", "No applications are tracked yet", "Leads saved",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("empty page missing %q", want)
		}
	}
	for _, bad := range []string{"NaN", "Inf", "%!"} {
		if strings.Contains(body, bad) {
			t.Errorf("empty page contains %q", bad)
		}
	}
}

func TestMetricsOneApplicationSaysTooSmall(t *testing.T) {
	metricsSetup(t)
	addApplied(t, 1, "", "Applied")
	body := getMetrics(t)
	for _, want := range []string{"1 applied application has no résumé version", "Start a résumé test", "Only 1 applied", "too few"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(body, "Is one version ahead?") || strings.Contains(body, "Enough to compare") {
		t.Error("one application must never produce a comparison")
	}
}

func TestMetricsLabelledSmallArmsShowCountsWithSampleSizes(t *testing.T) {
	metricsSetup(t)
	addApplied(t, 1, "Platform", "Screening")
	addApplied(t, 2, "AI focus", "Applied")
	addApplied(t, 3, "AI focus", "Rejected")
	body := getMetrics(t)
	for _, want := range []string{
		"Too early to conclude anything", "n = 1 applied", "n = 2 applied", "Too few to conclude",
		`<span class="of">of 2</span>`, "Response rate by résumé version", "<title>Platform: 1 of 1",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(body, "Is one version ahead?") || strings.Contains(body, "Start a résumé test") {
		t.Error("small arms must show counts only, without a comparison or the start steps")
	}
}

func TestMetricsComparisonWhenEnough(t *testing.T) {
	metricsSetup(t)
	for i := 0; i < 10; i++ {
		addApplied(t, i+1, "A", "Applied")
		addApplied(t, i+101, "B", "Screening")
	}
	body := getMetrics(t)
	for _, want := range []string{"Enough applications to compare", "Is one version ahead?", "B got a higher response rate than A"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

func TestFunnelCountsEventsAndInterviews(t *testing.T) {
	metricsSetup(t)
	addApplied(t, 1, "A", "Rejected") // reached an interview before the rejection
	addApplied(t, 2, "A", "Rejected") // rejected, which is a reply
	addApplied(t, 3, "A", "Applied")
	var aid int64
	if err := testDB.QueryRow("SELECT id FROM applications WHERE job_id=1").Scan(&aid); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec("INSERT INTO interviews(application_id,scheduled_at,kind) VALUES(?, '2026-11-20', 'Phone')", aid); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec("INSERT INTO application_events(application_id,occurred_at,kind,detail) VALUES(?, '2026-11-19T10:00', 'Status changed', 'Applied → Screening')", aid); err != nil {
		t.Fatal(err)
	}
	// An automatic confirmation email is not a reply.
	var aid3 int64
	testDB.QueryRow("SELECT id FROM applications WHERE job_id=3").Scan(&aid3)
	if _, err := testDB.Exec("INSERT INTO communications(application_id,occurred_at,channel,direction,contact,notes) VALUES(?, '2026-11-18', 'Email', 'Received', 'Co3', 'Thank you for applying')", aid3); err != nil {
		t.Fatal(err)
	}
	p, err := loadMetrics(testDB, time.Date(2026, 11, 24, 12, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range p.Funnel.Rows {
		got[r[0]] = r[1]
	}
	want := map[string]string{"Tracked": "3", "Applied": "3", "Heard back": "2", "Screening or later": "1", "Interview": "1", "Offer": "0"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("funnel %s = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	if p.Tiles[3].Value != "2 of 3" {
		t.Errorf("heard back tile = %q", p.Tiles[3].Value)
	}
}

func TestChartsHaveTitleLabelAndTable(t *testing.T) {
	metricsSetup(t)
	addApplied(t, 1, "A", "Applied")
	testDB.Exec("UPDATE jobs SET score_total=72, industry='Developer tools', work_arrangement='Remote', posting_date='2026-11-10' WHERE id=1")
	body := getMetrics(t)
	charts := strings.Count(body, `<svg class="chart"`)
	if charts < 6 {
		t.Fatalf("expected at least 6 charts, got %d", charts)
	}
	if strings.Count(body, `role="img" aria-label="`) != charts {
		t.Error("every chart needs role=img and an aria-label")
	}
	if strings.Count(body, `class="mx-table"`) != charts {
		t.Error("every chart needs a table of the same numbers")
	}
	if strings.Count(body, "<title>") < charts+1 {
		t.Error("every chart needs a <title>")
	}
	if !strings.Contains(body, "Developer tools") || !strings.Contains(body, "8 to 14 days") {
		t.Error("industry and listing-age charts should show the saved data")
	}
}

func TestVariantFormAndTools(t *testing.T) {
	metricsSetup(t)
	addApplied(t, 1, "", "Applied")
	post := func(fields url.Values, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8080/metrics/variant", strings.NewReader(fields.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		variantHandler(testDB)(w, r)
		return w
	}
	variant := func() string {
		var v *string
		testDB.QueryRow("SELECT resume_variant FROM applications WHERE job_id=1").Scan(&v)
		if v == nil {
			return "<null>"
		}
		return *v
	}
	if w := post(url.Values{"job_id": {"1"}, "variant": {"x"}}, ""); w.Code != http.StatusForbidden || variant() != "<null>" {
		t.Errorf("a post without a local origin must be refused, got %d and %q", w.Code, variant())
	}
	if w := post(url.Values{"job_id": {"1"}, "variant": {"  Platform   engineer "}}, "http://127.0.0.1:8080"); w.Code != http.StatusSeeOther || variant() != "Platform engineer" {
		t.Errorf("save: %d %q", w.Code, variant())
	}
	if w := post(url.Values{"job_id": {"999"}, "variant": {"x"}}, "http://127.0.0.1:8080"); w.Code != 400 {
		t.Errorf("unknown job: %d", w.Code)
	}
	post(url.Values{"job_id": {"1"}, "variant": {""}}, "http://127.0.0.1:8080")
	if variant() != "<null>" {
		t.Errorf("an empty label should clear it, got %q", variant())
	}

	// track_application records the label and keeps it when a later call omits it.
	if _, err := testDB.Exec(`INSERT INTO jobs(id,company,title,url) VALUES(50,'Acme','Dev','https://x.test/50')`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := trackApplication(context.Background(), testDB, mcpTrack{JobID: 50, Status: "Applied", ResumeVariant: "AI focus"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := trackApplication(context.Background(), testDB, mcpTrack{JobID: 50, Status: "Applied"}); err != nil {
		t.Fatal(err)
	}
	var v string
	testDB.QueryRow("SELECT resume_variant FROM applications WHERE job_id=50").Scan(&v)
	if v != "AI focus" {
		t.Errorf("track_application variant = %q", v)
	}
	var note string
	testDB.QueryRow("SELECT notes FROM communications WHERE application_id=(SELECT id FROM applications WHERE job_id=50) AND channel='Application'").Scan(&note)
	if !strings.Contains(note, "Variant: AI focus") {
		t.Errorf("submission note = %q", note)
	}
	if got := len([]rune(normalizeVariant(strings.Repeat("é", 90)))); got != maxVariantLen {
		t.Errorf("normalizeVariant cap = %d", got)
	}
}

func TestJobResumeVersionFallback(t *testing.T) {
	metricsSetup(t)
	addApplied(t, 1, "", "Applied")
	testDB.Exec("UPDATE jobs SET resume_version='Base v2' WHERE id=1")
	apps, err := loadApps(testDB)
	if err != nil || len(apps) != 1 || apps[0].Variant != "Base v2" {
		t.Fatalf("fallback to jobs.resume_version: %+v %v", apps, err)
	}
}

func TestClosedBeforeApplyingIsLeftOut(t *testing.T) {
	metricsSetup(t)
	addApplied(t, 1, "A", "Applied")
	for i, st := range []string{"Closed", "Closed", "Withdrawn"} {
		id := 10 + i
		if _, err := testDB.Exec(`INSERT INTO jobs(id,company,title,url,status) VALUES(?,?,?,?,?)`, id, fmt.Sprintf("Gone%d", id), "Engineer", fmt.Sprintf("https://x.test/%d", id), st); err != nil {
			t.Fatal(err)
		}
		if _, err := testDB.Exec(`INSERT INTO applications(job_id,status) VALUES(?,?)`, id, st); err != nil {
			t.Fatal(err)
		}
	}
	// Still in progress, so it stays; and a closed one that was applied to stays too.
	if _, err := testDB.Exec(`INSERT INTO jobs(id,company,title,url,status) VALUES(20,'Maybe','Engineer','https://x.test/20','Interested')`); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec(`INSERT INTO applications(job_id,status) VALUES(20,'Interested')`); err != nil {
		t.Fatal(err)
	}
	addApplied(t, 30, "A", "Closed") // closed after an applied date: it was pursued

	p, err := loadMetrics(testDB, time.Date(2026, 11, 24, 12, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	if p.Tiles[1].Value != "3" { // jobs 1, 20 and 30
		t.Errorf("tracked tile = %q, want 3", p.Tiles[1].Value)
	}
	if p.Tiles[2].Value != "2" {
		t.Errorf("applied tile = %q, want 2", p.Tiles[2].Value)
	}
	if p.Funnel.Rows[0][1] != "3" {
		t.Errorf("funnel tracked = %v", p.Funnel.Rows[0])
	}
	want := "3 postings closed before you applied are left out."
	if p.Closed != want || p.Funnel.Quiet != want {
		t.Errorf("closed note = %q / %q", p.Closed, p.Funnel.Quiet)
	}
	if n := strings.Count(getMetrics(t), want); n != 2 {
		t.Errorf("the note should show under the summary and under the funnel, found %d", n)
	}
	var names []string
	for _, l := range p.AB.Labels {
		names = append(names, l.Company)
	}
	if strings.Join(names, ",") != "Co1,Co30,Maybe" {
		t.Errorf("label list = %v, want applied first, then in progress, no closed-before-applying", names)
	}
	// With none closed, no line is shown.
	metricsSetup(t)
	addApplied(t, 1, "A", "Applied")
	if p, _ = loadMetrics(testDB, time.Now()); p.Closed != "" || strings.Contains(getMetrics(t), "left out.") {
		t.Error("no note expected when nothing was closed before applying")
	}
}
