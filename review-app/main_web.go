//go:build js && wasm

package main

// The browser build. sw.js (in web/) starts this program inside a service
// worker and hands every request for the site to it; the same handlers as the
// desktop app answer, against a SQLite database held in memory. After each
// change the database is saved to the browser's Cache Storage, because the
// browser stops an idle service worker and its memory goes with it; the next
// start loads that copy back.

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"syscall/js"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/vfs/memdb"
)

const (
	snapshotKey = "__data__/job-search.sqlite3"
	// 4096-byte pages match a database file from the desktop app, so an
	// import of one is a straight copy. Requests run on goroutines that
	// share the database, so a writer waits for a reader instead of failing.
	webDSN = "file:/jobs.db?vfs=memdb&_pragma=page_size(4096)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
)

// snapshotCache names the Cache Storage bucket. Every GitHub Pages project
// of one account shares an origin, and so shares Cache Storage; the base
// path keeps this app's bucket apart from the others'.
var snapshotCache = "job-search-data"

func main() {
	webBuild = true
	scope, err := url.Parse(js.Global().Get("registration").Get("scope").String())
	if err != nil {
		log.Fatal(err)
	}
	base := strings.TrimSuffix(scope.Path, "/")
	origin := scope.Scheme + "://" + scope.Host
	hostAllowed = func(h string) bool { return strings.EqualFold(h, scope.Host) }
	snapshotCache += ":" + base

	// If the saved copy cannot be read, stop rather than start empty: the
	// first save would overwrite the saved data. sw.js shows an error page
	// and starts the app again on the next request.
	db, err := openWebDB(origin + base + "/" + snapshotKey)
	if err != nil {
		log.Fatal(err)
	}
	registerAll(db, true)
	static, _ := fs.Sub(assets, "static")
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))

	w := &webServer{db: db, base: base, origin: origin, key: origin + base + "/" + snapshotKey, next: http.DefaultServeMux}
	js.Global().Call("goReady", js.FuncOf(w.handle))
	select {}
}

// openWebDB loads the saved copy, if there is one and it is intact, and
// makes sure every table exists.
func openWebDB(key string) (*sql.DB, error) {
	saved, err := loadSnapshot(key)
	if err != nil {
		return nil, fmt.Errorf("read the data saved in this browser: %w", err)
	}
	memdb.Create("jobs.db", saved)
	db, err := sql.Open("sqlite3", webDSN)
	if err != nil {
		return nil, err
	}
	var check string
	if err = db.QueryRow("PRAGMA quick_check").Scan(&check); err != nil || check != "ok" {
		// Keep the damaged copy under another name rather than lose it.
		log.Printf("saved data is damaged (%v %s); starting empty", err, check)
		damagedNotice = "The copy saved in this browser was damaged, so the app started empty. The damaged copy is kept in this site's storage; import a copy you exported to get your data back."
		db.Close()
		putCache(strings.Replace(key, ".sqlite3", "-damaged-"+time.Now().Format("20060102-150405")+".sqlite3", 1), saved)
		memdb.Delete("jobs.db")
		memdb.Create("jobs.db", nil)
		if db, err = sql.Open("sqlite3", webDSN); err != nil {
			return nil, err
		}
	}
	if err = initJobs(db); err != nil {
		return nil, err
	}
	return db, migrateAll(db)
}

type webServer struct {
	db                *sql.DB
	base, origin, key string
	next              http.Handler
	saveMu            sync.Mutex
}

// handle is called from sw.js with a Request and returns a Promise of a
// Response. The work runs on a goroutine so awaiting promises never blocks
// the JavaScript event loop.
func (s *webServer) handle(_ js.Value, args []js.Value) any {
	req := args[0]
	// The Promise constructor runs the executor at once, so it is released
	// as soon as the Promise exists.
	executor := js.FuncOf(func(_ js.Value, pa []js.Value) any {
		resolve, reject := pa[0], pa[1]
		go func() {
			defer func() {
				if p := recover(); p != nil {
					reject.Invoke(fmt.Sprint(p))
				}
			}()
			resp, err := s.serve(req)
			if err != nil {
				reject.Invoke(err.Error())
				return
			}
			resolve.Invoke(resp)
		}()
		return nil
	})
	defer executor.Release()
	return js.Global().Get("Promise").New(executor)
}

func (s *webServer) serve(req js.Value) (js.Value, error) {
	method := req.Get("method").String()
	var body []byte
	if method != "GET" && method != "HEAD" {
		ab, err := await(req.Call("arrayBuffer"))
		if err != nil {
			return js.Undefined(), err
		}
		body = bytesOf(ab)
	}
	r := httptest.NewRequest(method, req.Get("url").String(), bytes.NewReader(body))
	it := req.Get("headers").Call("entries")
	for {
		n := it.Call("next")
		if n.Get("done").Bool() {
			break
		}
		kv := n.Get("value")
		r.Header.Add(kv.Index(0).String(), kv.Index(1).String())
	}
	// The browser hides Origin from a service worker on fetches the page
	// makes; those can only come from this site's own pages, which the
	// worker controls. A navigation (a form post) keeps the Origin the
	// browser sent, so a form on another site is still refused.
	mode := req.Get("mode").String()
	if mode != "navigate" && r.Header.Get("Origin") == "" {
		r.Header.Set("Origin", s.origin)
	}
	// Other GitHub Pages projects of the same account share this origin, so
	// a form they post here carries a matching Origin. Accept a posted form
	// only from this app's own pages, by the page it came from.
	if mode == "navigate" && method != "GET" && method != "HEAD" && !strings.HasPrefix(req.Get("referrer").String(), s.origin+s.base+"/") {
		r.Header.Del("Origin")
	}
	// Sec-Fetch-* headers are hidden from the worker too; the Request says
	// the same thing in mode and destination, and isFragmentRequest reads
	// these headers to tell an HTMLeX call from a page load.
	if r.Header.Get("Sec-Fetch-Mode") == "" {
		r.Header.Set("Sec-Fetch-Mode", mode)
		dest := req.Get("destination").String()
		if dest == "" {
			dest = "empty"
		}
		r.Header.Set("Sec-Fetch-Dest", dest)
	}

	rec := httptest.NewRecorder()
	http.StripPrefix(s.base, s.next).ServeHTTP(rec, r)
	if method != "GET" && method != "HEAD" && rec.Code < 400 {
		s.save()
	}
	return s.response(rec), nil
}

// save writes the whole database to Cache Storage. Saves run one at a time,
// each copying the database as it is when its turn comes, so a later save
// never loses to an earlier one.
func (s *webServer) save() {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	data, err := exportDatabase(context.Background(), s.db)
	if err == nil {
		err = putCache(s.key, data)
	}
	if err != nil {
		log.Printf("save to browser storage: %v", err)
		storageProblem = "The last change could not be saved in this browser's storage (" + err.Error() + "). The browser may drop it when it stops the app, so export a copy now."
		return
	}
	storageProblem = ""
}

var noBody = map[int]bool{101: true, 103: true, 204: true, 205: true, 304: true}

// response turns the recorded answer into a JavaScript Response, putting the
// site's base path in front of root-relative links and redirects.
func (s *webServer) response(rec *httptest.ResponseRecorder) js.Value {
	h := js.Global().Get("Headers").New()
	for k, vs := range rec.Header() {
		for _, v := range vs {
			switch k {
			case "Location", "Push-Url", "Replace-Url":
				v = s.rebase(v)
			case "Content-Length":
				continue // the body may change length below
			}
			h.Call("append", k, v)
		}
	}
	h.Call("set", "Content-Security-Policy", "frame-ancestors 'none'")
	h.Call("set", "X-Frame-Options", "DENY")
	init := js.Global().Get("Object").New()
	init.Set("status", rec.Code)
	init.Set("headers", h)
	if noBody[rec.Code] {
		return js.Global().Get("Response").New(js.Null(), init)
	}
	out := rec.Body.Bytes()
	if s.base != "" && strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		out = rebaseHTML(out, s.base)
	}
	u8 := js.Global().Get("Uint8Array").New(len(out))
	js.CopyBytesToJS(u8, out)
	return js.Global().Get("Response").New(u8, init)
}

func (s *webServer) rebase(v string) string {
	return rebaseURL(v, s.base)
}

// loadSnapshot reads the saved database, or returns nil when there is none.
func loadSnapshot(key string) ([]byte, error) {
	cache, err := await(js.Global().Get("caches").Call("open", snapshotCache))
	if err != nil {
		return nil, err
	}
	resp, err := await(cache.Call("match", key))
	if err != nil || resp.IsUndefined() {
		return nil, err
	}
	ab, err := await(resp.Call("arrayBuffer"))
	if err != nil {
		return nil, err
	}
	return bytesOf(ab), nil
}

func putCache(key string, data []byte) error {
	cache, err := await(js.Global().Get("caches").Call("open", snapshotCache))
	if err != nil {
		return err
	}
	u8 := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(u8, data)
	init := js.Global().Get("Object").New()
	hdr := js.Global().Get("Object").New()
	hdr.Set("Content-Type", "application/vnd.sqlite3")
	init.Set("headers", hdr)
	_, err = await(cache.Call("put", key, js.Global().Get("Response").New(u8, init)))
	return err
}

// await blocks the calling goroutine, never the JavaScript event loop, until
// the promise settles.
func await(p js.Value) (js.Value, error) {
	ok, fail := make(chan js.Value, 1), make(chan js.Value, 1)
	then := js.FuncOf(func(_ js.Value, a []js.Value) any { ok <- a[0]; return nil })
	catch := js.FuncOf(func(_ js.Value, a []js.Value) any { fail <- a[0]; return nil })
	defer then.Release()
	defer catch.Release()
	p.Call("then", then).Call("catch", catch)
	select {
	case v := <-ok:
		return v, nil
	case e := <-fail:
		return js.Undefined(), fmt.Errorf("%s", e.Call("toString").String())
	}
}

func bytesOf(v js.Value) []byte {
	u := js.Global().Get("Uint8Array").New(v)
	b := make([]byte, u.Get("length").Int())
	js.CopyBytesToGo(b, u)
	return b
}
