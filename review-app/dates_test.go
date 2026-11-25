package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Each job records when the employer published the listing (posting_date)
// and when it was added to the tracker (date_found).

func TestCreateJobRecordsPostingAndAddedDates(t *testing.T) {
	cs := setupApplyTest(t)
	out := callTool(t, cs, "create_job", map[string]any{"company": "Datum", "title": "Dated Role", "url": "https://example.com/dated", "posting_date": "2026-11-10"})
	id := int64(out["job_id"].(float64))
	var posted, found string
	if err := testDB.QueryRow("SELECT coalesce(posting_date,''), date_found FROM jobs WHERE id=?", id).Scan(&posted, &found); err != nil {
		t.Fatal(err)
	}
	if posted != "2026-11-10" {
		t.Errorf("posting_date = %q", posted)
	}
	if found != time.Now().Format("2006-01-02") {
		t.Errorf("date_found = %q, want today", found)
	}

	// No posting date stays NULL rather than an empty string.
	out = callTool(t, cs, "create_job", map[string]any{"company": "Datum", "title": "Undated Role", "url": "https://example.com/undated"})
	var isNull bool
	if err := testDB.QueryRow("SELECT posting_date IS NULL FROM jobs WHERE id=?", int64(out["job_id"].(float64))).Scan(&isNull); err != nil {
		t.Fatal(err)
	}
	if !isNull {
		t.Error("missing posting_date should be NULL")
	}
}

func TestJobDateToolsRejectBadDates(t *testing.T) {
	cs := setupApplyTest(t)
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"create_job", map[string]any{"company": "Datum", "title": "Bad", "url": "https://example.com/bad", "posting_date": "Nov 10"}},
		{"update_job", map[string]any{"job_id": 7, "fields": map[string]any{"posting_date": "last week"}}},
		{"update_job", map[string]any{"job_id": 7, "fields": map[string]any{"application_deadline": "2026-13-40"}}},
	} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: c.tool, Arguments: c.args})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			t.Errorf("%s accepted %v", c.tool, c.args)
		}
	}
	callTool(t, cs, "update_job", map[string]any{"job_id": 7, "fields": map[string]any{"posting_date": "2026-11-01"}})
	callTool(t, cs, "update_job", map[string]any{"job_id": 7, "fields": map[string]any{"posting_date": nil}})
}

func TestDateAge(t *testing.T) {
	today := time.Now()
	cases := map[string]string{
		"":                         "",
		today.Format("2006-01-02"): "(today)",
		today.AddDate(0, 0, -1).Format("2006-01-02"):  "(yesterday)",
		today.AddDate(0, 0, -14).Format("2006-01-02"): "(14 days ago)",
	}
	for in, want := range cases {
		if got := dateAge(in); !strings.HasSuffix(got, want) {
			t.Errorf("dateAge(%q) = %q, want suffix %q", in, got, want)
		}
	}
}

func TestNextUpCardShowsPostedAndAdded(t *testing.T) {
	seedNextUp(t)
	if _, err := testDB.Exec(`UPDATE jobs SET posting_date='2026-11-03', date_found='2026-11-20' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	body := nextRequest("/next", false).Body.String()
	for _, want := range []string{"Listing posted</span> Tue 3 Nov 2026", "Added</span> Fri 20 Nov 2026"} {
		if !strings.Contains(body, want) {
			t.Errorf("card missing %q", want)
		}
	}
	body = nextRequest("/next?n=2", false).Body.String()
	if !strings.Contains(body, "Listing posted</span> Not known") {
		t.Error("a job without a posting date should say Not known")
	}
}
