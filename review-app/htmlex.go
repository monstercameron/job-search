package main

import (
	"bytes"
	"errors"
	"html/template"
	"log"
	"net/http"
	"strings"
)

// HTMLeX partial updates.
//
// Every URL serves two kinds of response. A browser navigation (address bar,
// link, reload, a form posted with JavaScript off) gets the full page. A
// request made by the HTMLeX runtime gets only the parts that changed, as
// <fragment target="#id(strategy)"> blocks, plus a Push-Url or Replace-Url
// header naming the full-page URL that shows the same state. The fetch
// metadata headers tell the two apart: navigations send Sec-Fetch-Mode
// "navigate"; fetch() sends "cors" or "same-origin".

// isFragmentRequest reports whether r was made by the HTMLeX runtime.
func isFragmentRequest(r *http.Request) bool {
	mode := r.Header.Get("Sec-Fetch-Mode")
	return mode != "" && mode != "navigate" && r.Header.Get("Sec-Fetch-Dest") == "empty"
}

// Fragment is one swap: Target is an HTMLeX target such as "#results(innerHTML)".
type Fragment struct {
	Target string
	Name   string // template to render
	Data   any
	HTML   template.HTML // used instead of Name when set
}

// writeFragments renders every fragment before writing anything, so a
// template error produces a 500 instead of a half-applied page.
func writeFragments(w http.ResponseWriter, urlHeader, url string, frags ...Fragment) {
	var buf bytes.Buffer
	for _, f := range frags {
		buf.WriteString(`<fragment target="` + template.HTMLEscapeString(f.Target) + `">`)
		if f.Name != "" {
			if err := pages.ExecuteTemplate(&buf, f.Name, f.Data); err != nil {
				log.Printf("fragment %s: %v", f.Name, err)
				http.Error(w, "Could not render the update", http.StatusInternalServerError)
				return
			}
		} else {
			buf.WriteString(string(f.HTML))
		}
		buf.WriteString("</fragment>\n")
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	h.Add("Vary", "Sec-Fetch-Mode, Sec-Fetch-Dest")
	if url != "" {
		h.Set(urlHeader, url)
	}
	w.Write(buf.Bytes())
}

// writeFormError answers a rejected HTMLeX form with a 200 response whose
// fragment carries the error (HTMLeX discards the body of a non-2xx reply).
// The message lands in the page's #flash region and takes focus.
func writeFormError(w http.ResponseWriter, msg string) {
	writeFragments(w, "", "", Fragment{
		Target: "#flash(innerHTML)",
		HTML:   template.HTML(`<p class="form-error" role="alert" tabindex="-1" autofocus>` + template.HTMLEscapeString(msg) + `</p>`),
	})
}

// parseForm reads both encodings: browsers post urlencoded forms, and the
// HTMLeX runtime posts multipart FormData, which r.ParseForm ignores.
func parseForm(r *http.Request) error {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		err := r.ParseMultipartForm(1 << 20)
		if errors.Is(err, http.ErrNotMultipart) {
			return r.ParseForm()
		}
		return err
	}
	return r.ParseForm()
}
