package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// freshDB opens a database at dsn with every table the app uses.
func freshDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = initJobs(db); err != nil {
		t.Fatal(err)
	}
	if err = migrateAll(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A file database opens through fileDSN, including a Windows path with its
// drive letter and a folder name with a space.
func TestFileDSNOpensARealFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Job Search", "jobs.sqlite3")
	dsn := fileDSN(path, true)
	if runtime.GOOS == "windows" && !strings.HasPrefix(dsn, "file:"+filepath.VolumeName(path)) {
		t.Errorf("Windows DSN should start with the drive letter: %s", dsn)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db := freshDB(t, dsn)
	if _, err := db.Exec(`INSERT INTO jobs(company,title,url,date_found) VALUES('Acme','Engineer','https://example.com/1','2026-11-24')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	// mode=rw (no -init) must open the same file, not create another.
	again, err := sql.Open("sqlite3", fileDSN(path, false))
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if n := count(t, again, "jobs"); n != 1 {
		t.Errorf("reopened file has %d jobs, want 1", n)
	}
}

// Export from a file database, import into an in-memory one (the browser
// build), and every row comes across; the in-memory one exports again.
func TestExportThenImportRoundTrip(t *testing.T) {
	ctx := context.Background()
	src := freshDB(t, fileDSN(filepath.Join(t.TempDir(), "src.sqlite3"), true))
	for _, q := range []string{
		`INSERT INTO jobs(id,company,title,url,date_found) VALUES(1,'Acme','Engineer','https://example.com/1','2026-11-24')`,
		`INSERT INTO jobs(id,company,title,url,date_found) VALUES(2,'Globex','Developer','https://example.com/2','2026-11-24')`,
		`INSERT INTO applications(id,job_id,status) VALUES(10,1,'Applied')`,
	} {
		if _, err := src.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	data, err := exportDatabase(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, sqliteHeader) {
		t.Fatalf("export is not a SQLite file: % x", data[:16])
	}

	// The browser build's 4096-byte pages match a desktop file, so that import
	// is a straight backup; memdb's default 8192 takes the table copy.
	for _, dsn := range []string{"file:/roundtrip-4k.db?vfs=memdb&_pragma=page_size(4096)", "file:/roundtrip-8k.db?vfs=memdb"} {
		t.Run(dsn, func(t *testing.T) { importInto(t, freshDB(t, dsn), data) })
	}
}

func importInto(t *testing.T, web *sql.DB, data []byte) {
	ctx := context.Background()
	if _, err := web.Exec(`INSERT INTO jobs(id,company,title,url,date_found) VALUES(99,'Old','Gone','https://example.com/old','2026-11-24')`); err != nil {
		t.Fatal(err)
	}
	jobs, apps, err := importDatabase(ctx, web, data)
	if err != nil {
		t.Fatal(err)
	}
	if jobs != 2 || apps != 1 {
		t.Errorf("import reported %d jobs and %d applications, want 2 and 1", jobs, apps)
	}
	var company string
	if err = web.QueryRow(`SELECT company FROM jobs WHERE id=99`).Scan(&company); err != sql.ErrNoRows {
		t.Errorf("import should replace the old rows; job 99 is still there (%v)", err)
	}
	// Every pooled connection sees the import, not only the one that ran it.
	web.SetMaxOpenConns(4)
	for i := 0; i < 4; i++ {
		if n := count(t, web, "jobs"); n != 2 {
			t.Errorf("connection %d sees %d jobs, want 2", i, n)
		}
	}
	var status string
	if err = web.QueryRow(`SELECT a.status FROM applications a JOIN jobs j ON j.id = a.job_id WHERE j.company = 'Acme'`).Scan(&status); err != nil || status != "Applied" {
		t.Errorf("application after import: %q, %v", status, err)
	}
	if _, err = web.Exec(`INSERT INTO jobs(company,title,url,date_found) VALUES('New','Role','https://example.com/new','2026-11-24')`); err != nil {
		t.Errorf("the imported database should take new rows: %v", err)
	}
	again, err := exportDatabase(ctx, web)
	if err != nil || !bytes.HasPrefix(again, sqliteHeader) {
		t.Fatalf("export from memory: %v", err)
	}
}

// The secret -mcp-http token never leaves in an export, and an imported
// file that still holds one has it removed. Other settings travel.
func TestExportLeavesSecretsBehind(t *testing.T) {
	ctx := context.Background()
	src := freshDB(t, fileDSN(filepath.Join(t.TempDir(), "secret.sqlite3"), true))
	for _, q := range []string{
		`INSERT INTO tracker_settings(key,value) VALUES('mcp_http_token','s3cr3t-token-value')`,
		`INSERT INTO tracker_settings(key,value) VALUES('base_resume','My base resume')`,
	} {
		if _, err := src.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	data, err := exportForDownload(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("s3cr3t-token-value")) {
		t.Error("the export file contains the MCP token")
	}
	var token string
	if err = src.QueryRow(`SELECT value FROM tracker_settings WHERE key='mcp_http_token'`).Scan(&token); err != nil || token == "" {
		t.Errorf("exporting must not remove the token from the database itself: %q, %v", token, err)
	}
	web := freshDB(t, "file:/secrets.db?vfs=memdb&_pragma=page_size(4096)")
	if _, _, err = importDatabase(ctx, web, data); err != nil {
		t.Fatal(err)
	}
	var base string
	if err = web.QueryRow(`SELECT value FROM tracker_settings WHERE key='base_resume'`).Scan(&base); err != nil || base != "My base resume" {
		t.Errorf("other settings should travel: %q, %v", base, err)
	}
	raw, _ := exportDatabase(ctx, src) // an old-style export that kept the token
	if _, _, err = importDatabase(ctx, web, raw); err != nil {
		t.Fatal(err)
	}
	var n int
	web.QueryRow(`SELECT count(*) FROM tracker_settings WHERE key='mcp_http_token'`).Scan(&n)
	if n != 0 {
		t.Error("an imported token should be removed")
	}
}

// Files that are not a tracker are refused and leave the data alone.
func TestImportRefusesBadFiles(t *testing.T) {
	ctx := context.Background()
	web := freshDB(t, "file:/refuse.db?vfs=memdb")
	if _, err := web.Exec(`INSERT INTO jobs(company,title,url,date_found) VALUES('Acme','Engineer','https://example.com/1','2026-11-24')`); err != nil {
		t.Fatal(err)
	}
	other, err := sql.Open("sqlite3", "file:/other.db?vfs=memdb")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err = other.Exec(`CREATE TABLE notes(x)`); err != nil {
		t.Fatal(err)
	}
	notTracker, err := exportDatabase(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	good, err := exportDatabase(ctx, web)
	if err != nil {
		t.Fatal(err)
	}
	damaged := append([]byte(nil), good...)
	for i := 100; i < len(damaged); i++ {
		damaged[i] ^= 0x5a
	}
	cases := map[string]struct {
		data []byte
		want string
	}{
		"text":        {[]byte("id,company\n1,Acme\n"), "not a SQLite database"},
		"empty":       {nil, "not a SQLite database"},
		"not tracker": {notTracker, "no jobs table"},
		"damaged":     {damaged, "damaged"},
	}
	for name, c := range cases {
		_, _, err := importDatabase(ctx, web, c.data)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want one containing %q", name, err, c.want)
		}
		if n := count(t, web, "jobs"); n != 1 {
			t.Errorf("%s: a refused import changed the data (%d jobs)", name, n)
		}
	}
}

func TestDataPageRenders(t *testing.T) {
	for _, web := range []bool{false, true} {
		var b strings.Builder
		p := DataPage{Nav: Nav{Active: "data", Title: "Your data"}, Web: web, CanImport: web, Jobs: 3, Apps: 1, Problem: "That file is damaged."}
		if err := pages.ExecuteTemplate(&b, "data", p); err != nil {
			t.Fatal(err)
		}
		body := b.String()
		for _, want := range []string{"Export the database", "3 jobs and 1 application", "That file is damaged.", "</html>"} {
			if !strings.Contains(body, want) {
				t.Errorf("web=%v: missing %q", web, want)
			}
		}
		if got := strings.Contains(body, `action="/data/import"`); got != web {
			t.Errorf("web=%v: import form shown = %v", web, got)
		}
	}
}

// Sample jobs fill an empty tracker once, with scores and two applications,
// and Next up then has work to show.
func TestSampleJobsFillAnEmptyTracker(t *testing.T) {
	db := freshDB(t, "file:/sample.db?vfs=memdb")
	n, err := loadSampleJobs(db)
	if err != nil || n != len(sampleJobs) {
		t.Fatalf("loadSampleJobs: %d, %v", n, err)
	}
	if got := count(t, db, "applications"); got != 2 {
		t.Errorf("%d sample applications, want 2", got)
	}
	if _, err = loadSampleJobs(db); err == nil || !strings.Contains(err.Error(), "empty tracker") {
		t.Errorf("a second load should be refused, got %v", err)
	}
	p, err := loadNextPage(db, "", 1)
	if err != nil || p.Total == 0 || p.Job.Score == "" {
		t.Errorf("next up after the sample: total %d, score %q, %v", p.Total, p.Job.Score, err)
	}
}

// An empty tracker says so, instead of "Nothing matches", and the browser
// build offers import and the sample.
func TestEmptyTrackerExplainsWhatToDo(t *testing.T) {
	defer func(w bool) { webBuild = w }(webBuild)
	for _, web := range []bool{false, true} {
		webBuild = web
		var b strings.Builder
		if err := pages.ExecuteTemplate(&b, "nextview", NextPage{}); err != nil {
			t.Fatal(err)
		}
		body := b.String()
		if !strings.Contains(body, "No jobs saved yet") || strings.Contains(body, "Nothing left to apply to") {
			t.Errorf("web=%v: next up on an empty tracker: %s", web, body)
		}
		if got := strings.Contains(body, `action="/data/sample"`); got != web {
			t.Errorf("web=%v: sample button shown = %v", web, got)
		}
	}
}

// When an imported file cannot be brought up to date, the data that was
// there before is put back.
func TestFailedImportPutsTheOldDataBack(t *testing.T) {
	ctx := context.Background()
	web := freshDB(t, "file:/rollback.db?vfs=memdb&_pragma=page_size(4096)")
	if _, err := web.Exec(`INSERT INTO jobs(company,title,url,date_found) VALUES('Keep','Me','https://example.com/keep','2026-11-24')`); err != nil {
		t.Fatal(err)
	}
	// A tracker whose "applications" is a view: the migrations cannot add
	// columns to it.
	bad, err := sql.Open("sqlite3", "file:/bad.db?vfs=memdb&_pragma=page_size(4096)")
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Close()
	if err = initJobs(bad); err != nil {
		t.Fatal(err)
	}
	if _, err = bad.Exec(`CREATE VIEW applications AS SELECT 1 AS id`); err != nil {
		t.Fatal(err)
	}
	data, err := exportDatabase(ctx, bad)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = importDatabase(ctx, web, data); err == nil || !strings.Contains(err.Error(), "nothing was imported") {
		t.Fatalf("import of a tracker that cannot be updated: %v", err)
	}
	var company string
	if err = web.QueryRow(`SELECT company FROM jobs`).Scan(&company); err != nil || company != "Keep" {
		t.Errorf("the old data should be back: %q, %v", company, err)
	}
	if n := count(t, web, "applications"); n != 0 {
		t.Errorf("applications table after the rollback has %d rows", n)
	}
}

// A desktop database in WAL mode imports and keeps working in memory.
func TestImportOfAWALDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "wal.sqlite3")
	src := freshDB(t, fileDSN(path, true)+"&_pragma=journal_mode(wal)")
	var mode string
	if err := src.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal mode %q, %v", mode, err)
	}
	if _, err := src.Exec(`INSERT INTO jobs(company,title,url,date_found) VALUES('Acme','Engineer','https://example.com/1','2026-11-24')`); err != nil {
		t.Fatal(err)
	}
	data, err := exportDatabase(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	web := freshDB(t, "file:/walimport.db?vfs=memdb&_pragma=page_size(4096)")
	if _, _, err = importDatabase(ctx, web, data); err != nil {
		t.Fatal(err)
	}
	if _, err = web.Exec(`INSERT INTO jobs(company,title,url,date_found) VALUES('New','Role','https://example.com/2','2026-11-24')`); err != nil {
		t.Errorf("write after importing a WAL database: %v", err)
	}
	again, err := exportDatabase(ctx, web)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = importDatabase(ctx, freshDB(t, "file:/walimport2.db?vfs=memdb"), again); err != nil {
		t.Errorf("re-import of the exported copy: %v", err)
	}
}
