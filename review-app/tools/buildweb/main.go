// Command buildweb builds the browser version of the app into a folder that
// any static host can serve, GitHub Pages included:
//
//	go run ./tools/buildweb -base /job-search/ -out _site
//
// With -serve it then serves the folder the way GitHub Pages does (404.html
// for any unknown path), to try the build locally:
//
//	go run ./tools/buildweb -serve 127.0.0.1:8111
//
// It compiles the app for js/wasm, copies Go's wasm_exec.js and the web/
// shell, and stamps the build's version and base path into them.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	base := flag.String("base", "/job-search/", "the path the site is served under, with slashes at both ends (\"/\" for a user or custom-domain site)")
	out := flag.String("out", "_site", "the folder to write the site to; it is replaced")
	serve := flag.String("serve", "", "after building, serve the site on this address, for example 127.0.0.1:8111")
	flag.Parse()
	if !strings.HasPrefix(*base, "/") || !strings.HasSuffix(*base, "/") {
		log.Fatalf("-base must start and end with a slash, for example /job-search/ (got %q)", *base)
	}
	if err := build(*base, *out); err != nil {
		log.Fatal(err)
	}
	if *serve != "" {
		log.Printf("serving http://%s%s", *serve, *base)
		log.Fatal(http.ListenAndServe(*serve, pagesHandler(*base, *out)))
	}
}

// safeToReplace refuses an output folder that holds anything other than an
// earlier build, so a mistyped -out cannot delete the source or a home folder.
func safeToReplace(out string) error {
	abs, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(abs, cwd); err == nil && !strings.HasPrefix(rel, "..") {
		return fmt.Errorf("-out %s holds the source folder; choose a new folder such as _site", out)
	}
	entries, err := os.ReadDir(abs)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	ours := map[string]bool{"app.wasm": true, "wasm_exec.js": true, "sw.js": true, "index.html": true, "404.html": true, ".nojekyll": true}
	for _, e := range entries {
		if !ours[e.Name()] {
			return fmt.Errorf("-out %s holds %s, which buildweb did not write; choose an empty or new folder", out, e.Name())
		}
	}
	return nil
}

// pagesHandler serves dir under base like GitHub Pages: files as they are,
// and 404.html with status 404 for a path that has no file.
func pagesHandler(base, dir string) http.Handler {
	files := http.StripPrefix(strings.TrimSuffix(base, "/"), http.FileServer(http.Dir(dir)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, base) {
			http.Redirect(w, r, base, http.StatusFound)
			return
		}
		rel := strings.TrimPrefix(r.URL.Path, base)
		info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
		if rel == "" || (err == nil && !info.IsDir()) {
			files.ServeHTTP(w, r)
			return
		}
		page, err := os.ReadFile(filepath.Join(dir, "404.html"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write(page)
	})
}

func build(base, out string) error {
	if _, err := os.Stat("web/sw.js"); err != nil {
		return fmt.Errorf("run buildweb from the review-app folder: %w", err)
	}
	if err := safeToReplace(out); err != nil {
		return err
	}
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	wasm := filepath.Join(out, "app.wasm")
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", wasm, ".")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build for js/wasm: %w", err)
	}
	data, err := os.ReadFile(wasm)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	version := hex.EncodeToString(sum[:6])

	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return fmt.Errorf("go env GOROOT: %w", err)
	}
	root := strings.TrimSpace(string(goroot))
	execJS, err := os.ReadFile(filepath.Join(root, "lib", "wasm", "wasm_exec.js"))
	if err != nil { // Go before 1.24 kept it under misc/
		if execJS, err = os.ReadFile(filepath.Join(root, "misc", "wasm", "wasm_exec.js")); err != nil {
			return fmt.Errorf("wasm_exec.js not found in GOROOT %s", root)
		}
	}

	stamp := strings.NewReplacer("__VERSION__", version, "__BASE__", base, "__WASM_BYTES__", fmt.Sprint(len(data)))
	sw, err := os.ReadFile("web/sw.js")
	if err != nil {
		return err
	}
	boot, err := os.ReadFile("web/bootstrap.html")
	if err != nil {
		return err
	}
	files := map[string][]byte{
		"wasm_exec.js": execJS,
		"sw.js":        []byte(stamp.Replace(string(sw))),
		// GitHub Pages serves 404.html for any unknown path, so a deep link
		// on a first visit still installs the app and then opens that page.
		"index.html": []byte(stamp.Replace(string(boot))),
		"404.html":   []byte(stamp.Replace(string(boot))),
		// Serve files as they are, without Jekyll.
		".nojekyll": nil,
	}
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(out, name), b, 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("built %s: app.wasm %.1f MB, version %s, base %s\n", out, float64(len(data))/1048576, version, base)
	return nil
}
