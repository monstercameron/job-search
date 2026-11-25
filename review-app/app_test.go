package main

import "testing"

func TestRebaseForGitHubPages(t *testing.T) {
	in := `<a href="/">Jobs</a><a href="/track?id=7&back=/tracker">x</a><form action="/save" post="/save"><input name="back" value="/tracker"></form>` +
		`<a href="//cdn.example/x">c</a><a href="https://example.com/">e</a><use href="#i-up"></use><img src="/static/a.png"><button get="/next?x=1">n</button>`
	want := `<a href="/job-search/">Jobs</a><a href="/job-search/track?id=7&back=/tracker">x</a><form action="/job-search/save" post="/job-search/save"><input name="back" value="/tracker"></form>` +
		`<a href="//cdn.example/x">c</a><a href="https://example.com/">e</a><use href="#i-up"></use><img src="/job-search/static/a.png"><button get="/job-search/next?x=1">n</button>`
	if got := string(rebaseHTML([]byte(in), "/job-search")); got != want {
		t.Errorf("rebaseHTML:\n got %s\nwant %s", got, want)
	}
	if got := string(rebaseHTML([]byte(in), "")); got != in {
		t.Errorf("an empty base must change nothing")
	}
	for in, want := range map[string]string{"/data?imported=1": "/job-search/data?imported=1", "//evil.example/": "//evil.example/", "https://x.example/": "https://x.example/", "?page=2": "?page=2"} {
		if got := rebaseURL(in, "/job-search"); got != want {
			t.Errorf("rebaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}
