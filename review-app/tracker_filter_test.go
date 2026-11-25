package main

import (
	"regexp"
	"strings"
	"testing"
)

// seedApps tracks five jobs across the Applications page's groups.
func seedApps(t *testing.T) {
	t.Helper()
	resetJobs(t,
		`INSERT INTO jobs(id,company,title,url,score_total) VALUES
			(1,'Acme','Platform Engineer','https://example.com/1',70),
			(2,'Bravo','Backend Engineer','https://example.com/2',92),
			(3,'Cobalt','Data Engineer','https://example.com/3',85),
			(4,'Delta','Site Reliability','https://example.com/4',60),
			(5,'Echo','Frontend Engineer','https://example.com/5',NULL)`,
		`INSERT INTO applications(job_id,status,applied_date,follow_up_date,contact,notes) VALUES
			(1,'Applied','2026-11-01','2020-01-01','Pat Recruiter',''),
			(2,'Interviewing','2026-11-10',NULL,'','panel next week'),
			(3,'Preparing',NULL,NULL,'',''),
			(4,'Rejected','2026-10-01',NULL,'',''),
			(5,'Applied','2026-11-20',NULL,'','')`,
	)
}

func appTitles(body string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`<h3 style="view-transition-name:job-\d+"><a [^>]*>([^<]+)</a>`).FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

func TestApplicationsFilterAndSort(t *testing.T) {
	seedApps(t)
	all := nextRequest("/tracker", false).Body.String()
	for _, want := range []string{`<span class="count" id="apps-count-all">5</span>`, `id="apps-count-due">1</span>`, `id="apps-count-active">2</span>`, `id="apps-count-later">1</span>`, `id="apps-count-closed">1</span>`, `value="" checked`} {
		if !strings.Contains(all, want) {
			t.Errorf("tracker missing %q", want)
		}
	}
	cases := []struct {
		path string
		want []string
	}{
		{"/tracker", []string{"Platform Engineer", "Backend Engineer", "Frontend Engineer", "Data Engineer", "Site Reliability"}},
		{"/tracker?show=active", []string{"Backend Engineer", "Frontend Engineer"}},
		{"/tracker?q=pat", []string{"Platform Engineer"}},  // contact
		{"/tracker?q=PANEL", []string{"Backend Engineer"}}, // notes, any case
		{"/tracker?q=cobalt", []string{"Data Engineer"}},   // company
		{"/tracker?stage=Applied", []string{"Platform Engineer", "Frontend Engineer"}},
		{"/tracker?sort=score&show=active", []string{"Backend Engineer", "Frontend Engineer"}},
		{"/tracker?sort=applied", []string{"Platform Engineer", "Frontend Engineer", "Backend Engineer", "Data Engineer", "Site Reliability"}},
		{"/tracker?sort=company&stage=nonsense&show=bogus", []string{"Platform Engineer", "Backend Engineer", "Frontend Engineer", "Data Engineer", "Site Reliability"}},
	}
	for _, c := range cases {
		got := appTitles(nextRequest(c.path, false).Body.String())
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: %v, want %v", c.path, got, c.want)
		}
	}
	// Search narrows the Show counts too, and says how many it shows.
	b := nextRequest("/tracker?q=engineer", false).Body.String()
	if !strings.Contains(b, `id="apps-count-all">4</span>`) || !strings.Contains(b, "Showing 4 of 5 applications.") || !strings.Contains(b, "<title>Applications, 4 of 5") {
		t.Error("search should narrow the counts and say how many show")
	}
	// Links to an application come back to the same view.
	if !strings.Contains(b, `back=%2ftracker%3fq%3dengineer`) && !strings.Contains(b, `back=/tracker?q=engineer`) && !strings.Contains(b, "back=%2Ftracker%3Fq%3Dengineer") {
		t.Errorf("open links should carry the filtered view as back: %s", regexp.MustCompile(`href="/track\?id=1[^"]*"`).FindString(b))
	}
	if safeBack("/tracker?q=engineer&sort=score") == "" || backLabel("/tracker?q=x") != "Back to applications" {
		t.Error("a filtered Applications view should be a valid Back link")
	}
	// Nothing matches: say so and offer a way back.
	if b := nextRequest("/tracker?q=zzz", false).Body.String(); !strings.Contains(b, "No applications match") {
		t.Error("no matches should say so")
	}
	// HTMLeX: the list and the counts swap; the form stays, and the address
	// is replaced, not pushed, so typing does not fill the history.
	w := nextRequest("/tracker?q=engineer&show=active", true)
	fb := w.Body.String()
	if w.Header().Get("Replace-Url") != "/tracker?q=engineer&show=active" || w.Header().Get("Push-Url") != "" {
		t.Errorf("fragment URL headers: %v", w.Header())
	}
	for _, want := range []string{`<fragment target="#apps-view(innerHTML)">`, `<fragment target="#apps-count-active(innerHTML)">2</fragment>`, `<fragment target="#apps-count-all(innerHTML)">4</fragment>`} {
		if !strings.Contains(fb, want) {
			t.Errorf("fragment missing %q", want)
		}
	}
	if strings.Contains(fb, `id="app-q"`) {
		t.Error("the search box must not be redrawn while typing in it")
	}
	if got := (AppFilter{Q: "a b", Sort: "score"}).URL(); got != "/tracker?q=a+b&sort=score" {
		t.Errorf("URL %q", got)
	}
}
