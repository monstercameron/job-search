package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func trackPost(t *testing.T, fields map[string]string, fragment bool) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	ct := "application/x-www-form-urlencoded"
	if fragment {
		mw := multipart.NewWriter(&body)
		for k, v := range fields {
			mw.WriteField(k, v)
		}
		mw.Close()
		ct = mw.FormDataContentType()
	} else {
		var parts []string
		for k, v := range fields {
			parts = append(parts, k+"="+strings.ReplaceAll(v, " ", "+"))
		}
		body.WriteString(strings.Join(parts, "&"))
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080/save", &body)
	r.Host = "127.0.0.1:8080"
	r.Header.Set("Content-Type", ct)
	r.Header.Set("Origin", "http://127.0.0.1:8080")
	if fragment {
		r.Header.Set("Sec-Fetch-Mode", "cors")
		r.Header.Set("Sec-Fetch-Dest", "empty")
	} else {
		r.Header.Set("Sec-Fetch-Mode", "navigate")
		r.Header.Set("Sec-Fetch-Dest", "document")
	}
	w := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(w, r)
	return w
}

func seedTrackJob(t *testing.T) {
	t.Helper()
	resetJobs(t, `INSERT INTO jobs(id,company,title,url) VALUES(7,'Acme','Backend Engineer','https://example.com/7')`)
}

func TestSaveFragmentSwapsTrackViewAndTabs(t *testing.T) {
	seedTrackJob(t)
	w := trackPost(t, map[string]string{"kind": "application", "job_id": "7", "status": "Applied", "back": "/tracker"}, true)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		`<fragment target="#track-view(innerHTML)">`,
		`<fragment target="#tabs(innerHTML)">`,
		`id="saved" tabindex="-1" role="status" autofocus>Status saved.`,
		`post="/save"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response lacks %q", want)
		}
	}
	if got := w.Header().Get("Replace-Url"); got != "/track?id=7&back=%2Ftracker" {
		t.Errorf("Replace-Url = %q", got)
	}
	if strings.Contains(w.Header().Get("Replace-Url"), "saved=") {
		t.Error("Replace-Url must not carry saved=")
	}
}

func TestSavePlainPostStillRedirects(t *testing.T) {
	seedTrackJob(t)
	w := trackPost(t, map[string]string{"kind": "application", "job_id": "7", "status": "Applied", "back": "/tracker"}, false)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d", w.Code)
	}
	if got := w.Header().Get("Location"); got != "/track?id=7&saved=application&back=%2Ftracker#saved" {
		t.Errorf("Location = %q", got)
	}
}

func TestSaveFragmentValidationErrorsUseFlash(t *testing.T) {
	seedTrackJob(t)
	w := trackPost(t, map[string]string{"kind": "document", "job_id": "7", "doc_type": "Resume", "title": "   "}, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `<fragment target="#flash(innerHTML)">`) || !strings.Contains(w.Body.String(), `role="alert"`) {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	w = trackPost(t, map[string]string{"kind": "document", "job_id": "7", "doc_type": "Resume", "title": "   "}, false)
	if w.Code == http.StatusSeeOther || strings.Contains(w.Body.String(), "<fragment") {
		t.Errorf("navigation got a fragment or redirect: %d", w.Code)
	}
}

func TestSaveBaseFragmentSwapsResumeView(t *testing.T) {
	seedTrackJob(t)
	w := trackPost(t, map[string]string{"kind": "base", "content": "Go developer", "back": "/profile"}, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `<fragment target="#resume-view(innerHTML)">`) || !strings.Contains(w.Body.String(), "Base résumé saved.") {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Replace-Url"); got != "/resume?back=%2Fprofile" {
		t.Errorf("Replace-Url = %q", got)
	}
	w = trackPost(t, map[string]string{"kind": "base", "content": "Go developer"}, false)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/resume?saved=base#saved" {
		t.Errorf("plain base post: %d %q", w.Code, w.Header().Get("Location"))
	}
}
