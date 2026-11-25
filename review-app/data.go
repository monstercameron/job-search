package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/serdes"
	"github.com/ncruces/go-sqlite3/vfs/memdb"
)

// The Data page downloads the whole tracker as one SQLite file and, in the
// browser build, replaces it with a file the user uploads. A file exported
// from either build opens in the other.

// webBuild is true in the browser build (main_web.go sets it). Pages use it
// to say where the data lives.
var webBuild bool

// storageProblem, when set, says the browser build could not save the last
// change to the browser's storage. damagedNotice says the saved copy was
// damaged when the app started; it stays for as long as the app runs. Every
// page shows them.
var storageProblem, damagedNotice string

func storageNotices() []string {
	var out []string
	for _, s := range []string{damagedNotice, storageProblem} {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// maxImportBytes is a generous ceiling for an uploaded database; a tracker
// with years of history is a few megabytes.
const maxImportBytes = 512 << 20

var sqliteHeader = []byte("SQLite format 3\x00")

type DataPage struct {
	Nav             Nav
	Web, CanImport  bool
	Jobs, Apps      int
	Notice, Problem string
}

// exportDatabase is the whole main database as the bytes of a SQLite file.
func exportDatabase(ctx context.Context, db *sql.DB) ([]byte, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var data []byte
	err = conn.Raw(func(dc any) error {
		var e error
		data, e = serdes.Serialize(dc.(driver.Conn).Raw(), "main")
		return e
	})
	return data, err
}

var importSeq atomic.Int64

// deleteSecrets removes the settings that must never leave this machine in
// a file: the token in the secret -mcp-http URL is a password.
const deleteSecrets = "DELETE FROM tracker_settings WHERE key IN ('mcp_http_token')"

// exportForDownload is the database as a file to hand to the user, with the
// secret settings removed from the copy.
func exportForDownload(ctx context.Context, db *sql.DB) ([]byte, error) {
	data, err := exportDatabase(ctx, db)
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("export-%d.db", importSeq.Add(1))
	memdb.Create(name, data)
	defer memdb.Delete(name)
	// secure_delete overwrites the removed rows, which would otherwise stay
	// readable in the file's free pages.
	copyDB, err := sql.Open("sqlite3", "file:/"+name+"?vfs=memdb&_pragma=secure_delete(1)")
	if err != nil {
		return nil, err
	}
	defer copyDB.Close()
	var has int
	copyDB.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='tracker_settings'").Scan(&has)
	if has == 0 {
		return data, nil
	}
	if _, err = copyDB.ExecContext(ctx, deleteSecrets); err != nil {
		return nil, err
	}
	return exportDatabase(ctx, copyDB)
}

// importDatabase replaces the main database with data, after checking that
// data is an intact SQLite file holding a jobs table. The copy goes through
// SQLite's backup API, so every pooled connection sees the new contents.
// It returns how many jobs and applications the new database holds.
func importDatabase(ctx context.Context, db *sql.DB, data []byte) (jobs, apps int, err error) {
	if !bytes.HasPrefix(data, sqliteHeader) {
		return 0, 0, errors.New("that file is not a SQLite database. Choose a .sqlite3 file exported from this app")
	}
	name := fmt.Sprintf("import-%d.db", importSeq.Add(1))
	memdb.Create(name, data)
	defer memdb.Delete(name)
	uri := "file:/" + name + "?vfs=memdb"
	src, err := sql.Open("sqlite3", uri)
	if err != nil {
		return 0, 0, err
	}
	defer src.Close()
	var check string
	if err = src.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&check); err != nil || check != "ok" {
		return 0, 0, errors.New("that database file is damaged, so nothing was imported")
	}
	var hasJobs int
	src.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='jobs'").Scan(&hasJobs)
	if hasJobs == 0 {
		return 0, 0, errors.New("that database has no jobs table, so it is not a job search tracker. Nothing was imported")
	}
	src.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer conn.Close()
	// SQLite's backup cannot change the page size of an in-memory database,
	// so a file with another page size is first copied to the right one.
	var pageSize int
	if err = conn.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return 0, 0, err
	}
	if filePageSize(data) != pageSize {
		staged, drop, err := copyToPageSize(ctx, uri, pageSize)
		defer drop()
		if err != nil {
			log.Printf("import: page size %d to %d: %v", filePageSize(data), pageSize, err)
			return 0, 0, errors.New("that database could not be converted for this app, so nothing was imported")
		}
		uri = staged
	}
	// Keep the current contents, to put back if the new file cannot be
	// brought up to date.
	before, err := exportDatabase(ctx, db)
	if err != nil {
		return 0, 0, err
	}
	if err = restoreBytes(conn, uri); err != nil {
		return 0, 0, err
	}
	if err = migrateAll(db); err != nil {
		log.Printf("import: update the imported database: %v", err)
		name := fmt.Sprintf("import-%d.db", importSeq.Add(1))
		memdb.Create(name, before)
		defer memdb.Delete(name)
		if back := restoreBytes(conn, "file:/"+name+"?vfs=memdb"); back != nil {
			log.Printf("import: put the previous data back: %v", back)
			return 0, 0, errors.New("that database could not be updated for this app, and the previous data could not be put back. Import a copy you exported earlier")
		}
		return 0, 0, errors.New("that database could not be updated for this app, so nothing was imported")
	}
	conn.Close()
	// A file exported by an older version may still hold a secret.
	if _, err = db.ExecContext(ctx, deleteSecrets); err != nil {
		return 0, 0, err
	}
	db.QueryRowContext(ctx, "SELECT count(*) FROM jobs").Scan(&jobs)
	db.QueryRowContext(ctx, "SELECT count(*) FROM applications").Scan(&apps)
	return jobs, apps, nil
}

// restoreBytes replaces the main database on conn with the one at uri.
func restoreBytes(conn *sql.Conn, uri string) error {
	return conn.Raw(func(dc any) error { return dc.(driver.Conn).Raw().Restore("main", uri) })
}

// filePageSize reads the page size from a SQLite file header.
func filePageSize(data []byte) int {
	n := int(data[16])<<8 | int(data[17])
	if n == 1 {
		return 65536
	}
	return n
}

// copyToPageSize copies the database at srcURI, table by table, into a new
// in-memory database with the given page size, and returns its address and
// a function that frees it.
func copyToPageSize(ctx context.Context, srcURI string, pageSize int) (string, func(), error) {
	name := fmt.Sprintf("import-%d.db", importSeq.Add(1))
	memdb.Create(name, nil)
	drop := func() { memdb.Delete(name) }
	uri := "file:/" + name + "?vfs=memdb"
	st, err := sql.Open("sqlite3", uri+"&_pragma=page_size("+strconv.Itoa(pageSize)+")&_pragma=foreign_keys(0)")
	if err != nil {
		return "", drop, err
	}
	defer st.Close()
	st.SetMaxOpenConns(1)
	exec := func(q string, args ...any) error { _, err := st.ExecContext(ctx, q, args...); return err }
	if err = exec("ATTACH DATABASE ? AS src", srcURI); err != nil {
		return "", drop, err
	}
	type object struct{ kind, name, sql string }
	var objects []object
	rows, err := st.QueryContext(ctx, `SELECT type, name, sql FROM src.sqlite_master
		WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%'
		ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 WHEN 'view' THEN 2 ELSE 3 END`)
	if err != nil {
		return "", drop, err
	}
	for rows.Next() {
		var o object
		if err = rows.Scan(&o.kind, &o.name, &o.sql); err != nil {
			rows.Close()
			return "", drop, err
		}
		objects = append(objects, o)
	}
	rows.Close()
	quote := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	for _, o := range objects {
		if err = exec(o.sql); err != nil {
			return "", drop, fmt.Errorf("%s %s: %w", o.kind, o.name, err)
		}
		if o.kind != "table" {
			continue
		}
		// Generated columns are rebuilt from their expressions, not copied.
		var cols []string
		crows, err := st.QueryContext(ctx, "SELECT name FROM pragma_table_xinfo(?, 'src') WHERE hidden = 0", o.name)
		if err != nil {
			return "", drop, err
		}
		for crows.Next() {
			var c string
			crows.Scan(&c)
			cols = append(cols, quote(c))
		}
		crows.Close()
		list := strings.Join(cols, ", ")
		if err = exec("INSERT INTO main." + quote(o.name) + " (" + list + ") SELECT " + list + " FROM src." + quote(o.name)); err != nil {
			return "", drop, fmt.Errorf("copy %s: %w", o.name, err)
		}
	}
	var hasSeq int
	st.QueryRowContext(ctx, "SELECT count(*) FROM src.sqlite_master WHERE name = 'sqlite_sequence'").Scan(&hasSeq)
	if hasSeq > 0 {
		if err = exec("DELETE FROM main.sqlite_sequence"); err == nil {
			err = exec("INSERT INTO main.sqlite_sequence SELECT * FROM src.sqlite_sequence")
		}
		if err != nil {
			return "", drop, err
		}
	}
	var version int
	st.QueryRowContext(ctx, "PRAGMA src.user_version").Scan(&version)
	if err = exec("PRAGMA main.user_version = " + strconv.Itoa(version)); err != nil {
		return "", drop, err
	}
	return uri, drop, exec("DETACH DATABASE src")
}

// sentence turns an error message into a sentence for the page: a capital
// first letter and a full stop.
func sentence(s string) string {
	if s == "" {
		return s
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

func registerData(db *sql.DB, canImport bool) {
	page := func(w http.ResponseWriter, r *http.Request, status int, notice, problem string) {
		p := DataPage{Nav: loadNav(db, "data", "Your data"), Web: webBuild, CanImport: canImport, Notice: notice, Problem: problem}
		p.Jobs, p.Apps = p.Nav.Jobs, p.Nav.Apps
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		if err := pages.ExecuteTemplate(w, "data", p); err != nil {
			log.Printf("render data: %v", err)
		}
	}

	http.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		notice := ""
		if r.URL.Query().Get("imported") == "1" {
			j, _ := strconv.Atoi(r.URL.Query().Get("jobs"))
			a, _ := strconv.Atoi(r.URL.Query().Get("apps"))
			notice = fmt.Sprintf("Imported %d %s and %d %s.", j, plural2(j, "job", "jobs"), a, plural2(a, "application", "applications"))
		}
		page(w, r, 200, notice, "")
	})

	http.HandleFunc("/data/export", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		data, err := exportForDownload(r.Context(), db)
		if err != nil {
			log.Printf("export: %v", err)
			http.Error(w, "Could not export the database", 500)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.sqlite3")
		w.Header().Set("Content-Disposition", `attachment; filename="job-search-`+time.Now().Format("2006-01-02")+`.sqlite3"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Cache-Control", "no-store")
		w.Write(data)
	})

	if !canImport {
		return
	}
	http.HandleFunc("/data/import", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		if !localPost(r) {
			http.Error(w, "Same-origin form required", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
		// Keep the whole upload in memory: the browser build has no disk
		// for the temporary file a large upload would otherwise go to.
		var tooBig *http.MaxBytesError
		if err := r.ParseMultipartForm(maxImportBytes); errors.As(err, &tooBig) {
			page(w, r, 413, "", fmt.Sprintf("That file is larger than %d MB, so nothing was imported.", maxImportBytes>>20))
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			page(w, r, 400, "", "Choose a database file to import.")
			return
		}
		defer f.Close()
		data, err := io.ReadAll(f)
		if err != nil {
			page(w, r, 400, "", "That file could not be read, so nothing was imported.")
			return
		}
		jobs, apps, err := importDatabase(r.Context(), db, data)
		if err != nil {
			page(w, r, 400, "", sentence(err.Error()))
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/data?imported=1&jobs=%d&apps=%d", jobs, apps), http.StatusSeeOther)
	})
	http.HandleFunc("/data/sample", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		if !localPost(r) {
			http.Error(w, "Same-origin form required", 403)
			return
		}
		if _, err := loadSampleJobs(db); err != nil {
			page(w, r, 409, "", sentence(err.Error()))
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
}

const dataHTML = `
{{define "data"}}{{template "top" .Nav}}
<div class="page-head has-tile"><span class="tile" aria-hidden="true">{{icon "archive"}}</span><h1>Your data</h1><p class="lede">{{if .Web}}Everything you enter is kept in this browser and never sent anywhere. Export a copy to keep it safe or to move it to another device; import a copy to pick up where you left off.{{else}}Everything is kept in the database file on this computer. Export a copy to back it up, or to open it in the browser version of the app.{{end}}</p></div>
<div class="data-page">
{{with .Notice}}<p class="saved data-wide" role="status" tabindex="-1" autofocus>{{.}}</p>{{end}}
{{with .Problem}}<p class="form-error data-wide" role="alert" tabindex="-1" autofocus>{{.}}</p>{{end}}
<section class="panel data-panel" aria-labelledby="export-h">
<h2 id="export-h">{{icon "download"}}Export</h2>
<p>Download the whole tracker as one SQLite file: {{.Jobs}} {{if eq .Jobs 1}}job{{else}}jobs{{end}} and {{.Apps}} {{if eq .Apps 1}}application{{else}}applications{{end}}, with your profile, résumés, notes and history. Any SQLite tool can open it{{if .Web}}, and you can import it here later{{else}}, and the browser version can import it{{end}}.</p>
<p><a class="btn primary" href="/data/export">{{icon "download"}}Export the database</a></p>
</section>
{{if .CanImport}}<section class="panel data-panel" aria-labelledby="import-h">
<h2 id="import-h">{{icon "up"}}Import</h2>
<p>Load a database file you exported before, from this browser or from the desktop app. <strong>Importing replaces everything currently in this browser.</strong> Export first if you want to keep it.</p>
<form method="post" action="/data/import" enctype="multipart/form-data" class="data-import">
<label for="db-file">Database file (.sqlite3)</label>
<input id="db-file" name="file" type="file" accept=".sqlite3,.sqlite,.db" required>
<button class="btn secondary">{{icon "up"}}Import and replace</button>
</form>
</section>{{end}}
{{if .Web}}<section class="panel data-panel data-wide" aria-labelledby="where-h">
<h2 id="where-h">{{icon "shield"}}Where your data lives</h2>
<p>The app and its database run inside this browser tab, and the database is saved in this browser's storage after every change. Nothing is uploaded. Clearing this site's data, or browsing in a private window, removes it, so export a copy now and then.</p>
</section>{{end}}
</div>
{{template "bottom"}}{{end}}
`
