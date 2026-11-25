package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const goodResume = `Alex Rivera
Staff Engineer
Travis County, Texas | alex.rivera@example.com | (555) 010-2030 | github.com/alex-rivera-dev

SUMMARY
Senior software engineer with eight years at NWT building logistics software in TypeScript, Go and Kotlin.

EXPERIENCE
Staff Engineer, NWT (Routing Platform) | Mar 2023 – Present
- Build route-planning agents in TypeScript over ERP data.
Site Reliability Engineer, Contoso Systems (now NWT) | Jun 2016 – Feb 2020
- Led on-call rotations across engineering and support.

SKILLS
Languages: Go, Kotlin, TypeScript, Python
`

var resumeFilesOnce sync.Once

func setupApplyTest(t *testing.T) *mcp.ClientSession {
	t.Helper()
	resumeFilesOnce.Do(func() { registerResumeFiles(testDB) })
	if err := migrateProfile(testDB); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"DELETE FROM communications", "DELETE FROM application_documents", "DELETE FROM profile_entries", "DELETE FROM profile_fields"} {
		if _, err := testDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	resetJobs(t, `INSERT INTO jobs (id, company, title, url, status, skills_required, location) VALUES (7, 'Acme', 'Software Engineer, AI Transformation', 'https://jobs.lever.co/acme/x', 'Research', 'TypeScript and/or Python; LLM APIs; AWS or equivalent cloud; Salesforce', 'Remote')`)
	for _, q := range []string{
		`INSERT INTO profile_fields(key,value) VALUES ('name','Alex Rivera'),('email','alex.rivera@example.com'),('authorization','No sponsorship needed'),('salary_floor','$90,000 base'),('accuracy_notes','Never list a degree.'),('current_pay','Base $1 secret')`,
		`INSERT INTO profile_entries(section,position,title,org,start_date,end_date,bullets) VALUES ('experience',1,'Staff Engineer','NWT','2023-03','','Build route-planning agents over ERP data with LLM APIs.'),('experience',2,'Site Reliability Engineer','Contoso Systems (now NWT)','2016-06','2020-02','Led on-call rotations.'),('skill',1,'Languages','','','','Go, Kotlin, TypeScript, Python')`,
	} {
		if _, err := testDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	dataDir = t.TempDir()
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := newMCPServer(testDB).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		b, _ := json.Marshal(res.Content)
		t.Fatalf("%s failed: %s", name, b)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestApplicationKitHasRulesAndNoPrivatePay(t *testing.T) {
	cs := setupApplyTest(t)
	kit := callTool(t, cs, "get_application_kit", map[string]any{"job_id": 7})
	raw, _ := json.Marshal(kit)
	s := string(raw)
	for _, want := range []string{"Acme", "alex.rivera@example.com", "resume_rules", "Never list a degree.", "Leave blank. Never enter the user's X account.", "missing_from_profile", "phone"} {
		if !strings.Contains(s, want) {
			t.Errorf("kit missing %q", want)
		}
	}
	if strings.Contains(s, "$1 secret") {
		t.Error("kit leaks current pay")
	}
}

func TestSaveTailoredResumeBlocksFalseClaimsThenExports(t *testing.T) {
	cs := setupApplyTest(t)
	bad := strings.Replace(goodResume, "SKILLS", "EDUCATION\nB.S. Information Technology, State University\n\nSKILLS", 1) + "x.com/alexrivera\n"
	out := callTool(t, cs, "save_tailored_resume", map[string]any{"job_id": 7, "content": bad})
	if out["saved"] != false {
		t.Fatal("résumé claiming a degree was saved")
	}
	errs, _ := json.Marshal(out["check"].(map[string]any)["errors"])
	for _, want := range []string{"EDUCATION", "B.S.", "x.com"} {
		if !strings.Contains(string(errs), want) {
			t.Errorf("errors do not mention %q: %s", want, errs)
		}
	}

	out = callTool(t, cs, "save_tailored_resume", map[string]any{"job_id": 7, "content": goodResume + "- Cut costs by 40%.\n"})
	if out["saved"] != true {
		t.Fatalf("good résumé not saved: %v", out["check"])
	}
	check, _ := json.Marshal(out["check"])
	for _, want := range []string{`"TypeScript and/or Python"`, `"LLM APIs"`, `40%`, `Salesforce`} {
		if !strings.Contains(string(check), want) {
			t.Errorf("check missing %s: %s", want, check)
		}
	}
	files := out["files"].(map[string]any)
	pdf, err := os.ReadFile(files["pdf"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-1.4")) || !bytes.Contains(pdf, []byte("(Alex Rivera)")) || !bytes.Contains(pdf, []byte("%%EOF")) {
		t.Error("PDF is not a text-based PDF with the name as text")
	}
	if filepath.Base(files["pdf"].(string)) != "Alex-Rivera-Resume.pdf" || !strings.Contains(files["pdf"].(string), "7-Acme") {
		t.Errorf("unexpected file path %s", files["pdf"])
	}
	z, err := zip.OpenReader(files["docx"].(string))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range z.File {
		if f.Name == "word/document.xml" {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			found = strings.Contains(string(b), "Led on-call rotations across engineering and support.")
		}
	}
	z.Close()
	if !found {
		t.Error("DOCX does not contain the bullet text")
	}
	for format, want := range map[string]string{"pdf": "%PDF-1.4", "docx": "PK"} {
		w := httptest.NewRecorder()
		http.DefaultServeMux.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/resume-file?id=%v&format=%s", out["document_id"], format), nil))
		if w.Code != 200 || !strings.HasPrefix(w.Body.String(), want) || !strings.Contains(w.Header().Get("Content-Disposition"), "Alex-Rivera-Resume."+format) {
			t.Errorf("/resume-file %s: %d %q", format, w.Code, w.Header().Get("Content-Disposition"))
		}
	}
	// Saving again under the same name replaces the version instead of adding one.
	callTool(t, cs, "save_tailored_resume", map[string]any{"job_id": 7, "content": goodResume})
	var n int
	testDB.QueryRow("SELECT COUNT(*) FROM application_documents WHERE kind='Resume'").Scan(&n)
	if n != 1 {
		t.Errorf("%d résumé versions, want 1", n)
	}
}

func TestTrackApplicationStartsThenRecordsSubmission(t *testing.T) {
	cs := setupApplyTest(t)
	out := callTool(t, cs, "track_application", map[string]any{"job_id": 7, "status": "Preparing"})
	if out["status"] != "Preparing" {
		t.Fatalf("status %v", out["status"])
	}
	out = callTool(t, cs, "track_application", map[string]any{"job_id": 7, "status": "Applied", "applied_date": "2026-11-24", "via": "Lever application form", "resume_title": "Acme AI"})
	if out["applied_date"] != "2026-11-24" || out["follow_up_date"] != "2026-12-01" {
		t.Errorf("dates %v %v", out["applied_date"], out["follow_up_date"])
	}
	var status, notes string
	testDB.QueryRow("SELECT status FROM jobs WHERE id=7").Scan(&status)
	testDB.QueryRow("SELECT notes FROM communications").Scan(&notes)
	if status != "Applied" || !strings.Contains(notes, "via Lever application form") || !strings.Contains(notes, "Acme AI") {
		t.Errorf("job status %q, log %q", status, notes)
	}
	var events int
	testDB.QueryRow("SELECT COUNT(*) FROM application_events WHERE kind='Status changed'").Scan(&events)
	if events != 2 {
		t.Errorf("%d status events, want 2", events)
	}
	// Preparing again must not move an applied job backwards.
	out = callTool(t, cs, "track_application", map[string]any{"job_id": 7, "status": "Preparing"})
	if out["status"] != "Applied" {
		t.Errorf("status went back to %v", out["status"])
	}
}

func TestCheckResumeFlagsRegionYearOnlyAndBareAcronyms(t *testing.T) {
	text := strings.Replace(goodResume, "Travis County, Texas", "Miami Metropolitan Area", 1) +
		"\nPROJECTS\nWidget, Fabrikam | 2020 – Present\n- Built LLM tooling on GCP for enterprise resource planning (ERP) data.\n"
	c := checkResume(text, ProfilePage{F: map[string]string{"name": "Alex Rivera"}}, "", "")
	all := strings.Join(c.Warnings, "\n")
	for _, want := range []string{"gives a region", "year without a month", "LLM, GCP"} {
		if !strings.Contains(all, want) {
			t.Errorf("warnings miss %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "ERP,") || strings.Contains(all, ", ERP") {
		t.Errorf("ERP is spelled out but still flagged:\n%s", all)
	}
	cleanText := strings.NewReplacer("Travis County, Texas", "Austin, TX", "over ERP data", "over enterprise resource planning (ERP) data").Replace(goodResume)
	clean := checkResume(cleanText, ProfilePage{F: map[string]string{"name": "Alex Rivera"}}, "", "")
	for _, w := range clean.Warnings {
		if strings.Contains(w, "region") || strings.Contains(w, "without a month") || strings.Contains(w, "Spell out") {
			t.Errorf("clean résumé warned: %s", w)
		}
	}
}

func TestCallMCPToolFromCommandLine(t *testing.T) {
	setupApplyTest(t)
	out, err := callMCPTool(testDB, "list", nil)
	if err != nil || !strings.Contains(out, "get_application_kit:") || !strings.Contains(out, "track_application:") {
		t.Fatalf("list: %v\n%s", err, out)
	}
	out, err = callMCPTool(testDB, "track_application", []byte(`{"job_id":7,"status":"Preparing"}`))
	if err != nil || !strings.Contains(out, `"status": "Preparing"`) {
		t.Fatalf("call: %v\n%s", err, out)
	}
	if _, err = callMCPTool(testDB, "track_application", []byte(`not json`)); err == nil {
		t.Error("bad JSON accepted")
	}
}
