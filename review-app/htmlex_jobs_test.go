package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func seedManyJobs(t *testing.T, n int) {
	t.Helper()
	var rows []string
	for i := 1; i <= n; i++ {
		rows = append(rows, fmt.Sprintf(`INSERT INTO jobs (id, company, title, url, status) VALUES (%d, 'Acme %d', 'Analyst %d', 'https://example.com/%d', 'Research')`, i, i, i, i))
	}
	resetJobs(t, rows...)
}

func jobsRequest(h http.HandlerFunc, target string, fragment bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", target, nil)
	if fragment {
		r.Header.Set("Sec-Fetch-Mode", "cors")
		r.Header.Set("Sec-Fetch-Dest", "empty")
	} else {
		r.Header.Set("Sec-Fetch-Mode", "navigate")
		r.Header.Set("Sec-Fetch-Dest", "document")
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func TestJobsFragmentRequest(t *testing.T) {
	seedManyJobs(t, 30)
	h := jobsHandler(testDB)
	w := jobsRequest(h, "/?q=Analyst&page=2", true)
	body := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	if got := w.Header().Get("Push-Url"); got != "/?page=2&q=Analyst" {
		t.Errorf("Push-Url = %q", got)
	}
	for _, want := range []string{
		`<fragment target="#jobs-view(innerHTML)">`,
		`<fragment target="title(innerHTML)">`,
		`Jobs matching “Analyst”, page 2 of 2 · Job search`,
		`<h2 id="results-title" tabindex="-1" autofocus>`,
		`get="/"`,
		`class="finder"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment response missing %q", want)
		}
	}
	if strings.Contains(body, "<html") || strings.Contains(body, "<!doctype") || strings.Contains(body, `id="jobs-view"`) {
		t.Error("fragment response contains page chrome")
	}
	if strings.Contains(body, `<form target`) || strings.Contains(body, `class="btn secondary" target`) {
		t.Error("native target attribute on a form or pager link")
	}
	if !strings.Contains(w.Header().Get("Vary"), "Sec-Fetch-Mode") {
		t.Error("Vary missing Sec-Fetch-Mode")
	}
}

func TestJobsNavigationGetsFullPage(t *testing.T) {
	seedManyJobs(t, 30)
	h := jobsHandler(testDB)
	w := jobsRequest(h, "/?q=Analyst", false)
	body := w.Body.String()
	if w.Code != 200 || w.Header().Get("Push-Url") != "" {
		t.Fatalf("status %d, Push-Url %q", w.Code, w.Header().Get("Push-Url"))
	}
	for _, want := range []string{"<!doctype html>", `<div id="jobs-view">`, `<form action="/" method="get" get="/"`, `<h2 id="results-title">`, `get="/?page=2&amp;q=Analyst"`, `href="/?page=2&amp;q=Analyst"`} {
		if !strings.Contains(body, want) {
			t.Errorf("full page missing %q", want)
		}
	}
	if strings.Contains(body, "autofocus") || strings.Contains(body, "<fragment") {
		t.Error("full page contains fragment-only markup")
	}
}

func TestJobsResultsRouteRemoved(t *testing.T) {
	h := jobsHandler(testDB)
	if w := jobsRequest(h, "/results", false); w.Code != 404 {
		t.Errorf("/results status %d, want 404", w.Code)
	}
}
