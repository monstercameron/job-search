package main

import (
	"os"
	"path/filepath"
	"testing"
)

// buildweb replaces its output folder, so it must refuse one that holds
// anything it did not write, or that holds the source.
func TestSafeToReplace(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	site := filepath.Join(dir, "_site")
	if err := safeToReplace(site); err != nil {
		t.Errorf("a new folder: %v", err)
	}
	os.MkdirAll(site, 0o755)
	for _, f := range []string{"app.wasm", "sw.js", "index.html"} {
		os.WriteFile(filepath.Join(site, f), nil, 0o644)
	}
	if err := safeToReplace(site); err != nil {
		t.Errorf("an earlier build: %v", err)
	}
	os.WriteFile(filepath.Join(site, "notes.txt"), nil, 0o644)
	if err := safeToReplace(site); err == nil {
		t.Error("a folder with other files should be refused")
	}
	for _, out := range []string{".", dir, filepath.Dir(dir)} {
		if err := safeToReplace(out); err == nil {
			t.Errorf("-out %s holds the source and should be refused", out)
		}
	}
}
