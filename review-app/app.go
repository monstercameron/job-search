package main

import (
	"database/sql"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// The app is built two ways from the same handlers: main_native.go serves a
// database file over local HTTP (and MCP), and main_web.go runs in a browser
// service worker against an in-memory database. Both call these.

// migrateAll adds every table and column the app needs. Each step only adds
// what is missing, so it is safe on every start.
func migrateAll(db *sql.DB) error {
	for _, step := range []func(*sql.DB) error{migrateTracker, migrateIndustry, migrateProfile, migrateNextUp, migrateMetrics} {
		if err := step(db); err != nil {
			return err
		}
	}
	return nil
}

// registerAll puts every page's handlers on http.DefaultServeMux.
func registerAll(db *sql.DB, canImport bool) {
	registerTracker(db)
	registerProfile(db)
	registerResumeFiles(db)
	registerNextUp(db)
	registerMetrics(db)
	registerData(db, canImport)
	http.HandleFunc("/", jobsHandler(db))
}

// fileDSN is the driver address for a database file. On Windows the path
// keeps its drive letter with no leading slash ("file:C:/..."), which the
// driver's file layer needs; elsewhere it is an ordinary absolute path.
func fileDSN(path string, create bool) string {
	q := url.Values{}
	q.Set("mode", "rw")
	if create {
		q.Set("mode", "rwc")
	}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	return "file:" + (&url.URL{Path: filepath.ToSlash(path)}).EscapedPath() + "?" + q.Encode()
}

// The browser build is served under a base path such as /job-search/ on
// GitHub Pages, while the handlers write root-relative links. These put the
// base path in front of them on the way out.

var rootAttr = regexp.MustCompile(`(\s(?:href|src|action|formaction|get|post)=")/([^/"]|")`)

// rebaseHTML prefixes base to root-relative URLs in link, form and HTMLeX
// request attributes. Other attributes (a hidden "back" value, for one) keep
// the path the server reads.
func rebaseHTML(b []byte, base string) []byte {
	if base == "" {
		return b
	}
	return rootAttr.ReplaceAll(b, []byte("${1}"+base+"/${2}"))
}

// rebaseURL prefixes base to a root-relative URL in a redirect or history
// header; a full or protocol-relative URL is left alone.
func rebaseURL(v, base string) string {
	if base != "" && strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "//") {
		return base + v
	}
	return v
}
