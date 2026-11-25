package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

var profileRoutesOnce sync.Once

// profileRoutes registers the profile handlers once; http.HandleFunc panics
// on a duplicate pattern, and TestMain does not install them.
func profileRoutes(t *testing.T) {
	t.Helper()
	profileRoutesOnce.Do(func() {
		if err := migrateProfile(testDB); err != nil {
			t.Fatal(err)
		}
		registerProfile(testDB)
	})
}

func profilePost(t *testing.T, fragment bool, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	profileRoutes(t)
	var body *bytes.Buffer
	var ctype string
	if fragment {
		body = &bytes.Buffer{}
		mw := multipart.NewWriter(body)
		for k, v := range fields {
			mw.WriteField(k, v)
		}
		mw.Close()
		ctype = mw.FormDataContentType()
	} else {
		form := url.Values{}
		for k, v := range fields {
			form.Set(k, v)
		}
		body = bytes.NewBufferString(form.Encode())
		ctype = "application/x-www-form-urlencoded"
	}
	req := httptest.NewRequest("POST", "http://127.0.0.1:8080/profile/save", body)
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	if fragment {
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Dest", "empty")
	} else {
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Dest", "document")
	}
	rec := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(rec, req)
	return rec
}

func TestProfileFragmentSave(t *testing.T) {
	rec := profilePost(t, true, map[string]string{"action": "fields", "group": "summary", "summary": "Builds fragment tests."})
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	if !strings.Contains(out, `<fragment target="#profile-view(innerHTML)">`) {
		t.Errorf("missing profile-view fragment: %.300s", out)
	}
	if got := rec.Header().Get("Replace-Url"); got != "/profile" {
		t.Errorf("Replace-Url = %q", got)
	}
	if !strings.Contains(out, `id="saved"`) || !strings.Contains(out, "autofocus") || !strings.Contains(out, "Summary saved.") {
		t.Errorf("saved banner with autofocus missing")
	}
	if !strings.Contains(out, "Builds fragment tests.") {
		t.Errorf("new summary missing")
	}
	if strings.Contains(out, "<html") || strings.Contains(out, `id="flash"`) {
		t.Errorf("fragment must not carry the page layout")
	}
}

func TestProfileFragmentError(t *testing.T) {
	rec := profilePost(t, true, map[string]string{"action": "entry", "section": "project", "title": " "})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `<fragment target="#flash(innerHTML)">`) || !strings.Contains(rec.Body.String(), "role=\"alert\"") {
		t.Errorf("want flash fragment, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestProfilePlainPostRedirects(t *testing.T) {
	rec := profilePost(t, false, map[string]string{"action": "fields", "group": "summary", "summary": "Plain."})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/profile?saved=summary#saved" {
		t.Errorf("got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = profilePost(t, false, map[string]string{"action": "entry", "section": "project", "title": ""})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("navigation error status %d", rec.Code)
	}
}

func TestProfileFormsCarryPost(t *testing.T) {
	var buf bytes.Buffer
	if err := pages.ExecuteTemplate(&buf, "profileview", ProfilePage{F: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	for _, f := range strings.Split(buf.String(), "<form")[1:] {
		tag := f[:strings.Index(f, ">")]
		isBase := strings.Contains(f[:strings.Index(f, "</form>")], `value="to-base"`)
		if has := strings.Contains(tag, `post="/profile/save"`); has == isBase {
			t.Errorf("form tag %q: post attribute wrong (to-base=%v)", tag, isBase)
		}
	}
}
