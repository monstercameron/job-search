package main

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

// seedInsights saves four invented companies' jobs and their profiles.
func seedInsights(t *testing.T) {
	t.Helper()
	metricsSetup(t)
	if err := migrateProfile(testDB); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"DELETE FROM profile_fields",
		`INSERT INTO profile_fields(key,value) VALUES('location','Tampa, FL')`,
		`INSERT INTO jobs(id,company,title,url) VALUES
			(1,'Brightwater Analytics','Backend Engineer','https://x.test/1'),
			(2,'brightwater  analytics','Platform Engineer','https://x.test/2'),
			(3,'Pinecrest Health','Full Stack Engineer','https://x.test/3'),
			(4,'Maple Ledger','Software Engineer','https://x.test/4'),
			(5,'Copperleaf Energy','Staff Engineer','https://x.test/5')`,
	} {
		if _, err := testDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	str := func(s string) *string { return &s }
	for _, c := range []companyIn{
		{Company: "Brightwater Analytics", City: str("Austin"), Region: str("tx"), Country: str("us"), Size: str("51-200"), Owner: str("venture")},
		{Company: "Pinecrest Health", City: str("Denver"), Region: str("CO"), Country: str("US"), Owner: str("Private")},
		{Company: "Maple Ledger", City: str("Toronto"), Region: str("ON"), Country: str("CA"), Size: str("201-500")},
	} {
		if _, _, err := saveCompany(testDB, c); err != nil {
			t.Fatal(err)
		}
	}
	yes, no := true, false
	_ = yes
	n := func(i int) *int { return &i }
	profiles := map[int64]jobProfileIn{
		1: {Locations: &[]placeIn{{Kind: "remote", Country: "US"}}, YearsMin: n(5), Degree: str("none"), Seniority: str("senior"),
			Requirements: &[]requirementIn{{Kind: "language", Name: "Go", Required: true, OwnerHas: "yes"}, {Kind: "platform", Name: "Kubernetes", Required: true, OwnerHas: "no"}, {Kind: "platform", Name: "PostgreSQL", Required: !no}}},
		2: {Locations: &[]placeIn{{Kind: "remote", Country: "us", Region: "fl"}, {Kind: "remote", Country: "US", Region: "GA"}}, YearsMin: n(3), Degree: str("bachelor"),
			Requirements: &[]requirementIn{{Kind: "language", Name: "go", Required: false}, {Kind: "platform", Name: "kubernetes", Required: false, OwnerHas: "partly"}}},
		3: {Locations: &[]placeIn{{Kind: "office", Country: "US", Region: "CO", City: "Denver"}}, Degree: str("equivalent"),
			Requirements: &[]requirementIn{{Kind: "language", Name: "TypeScript", Required: true}, {Kind: "platform", Name: "Kubernetes", Required: true, OwnerHas: "no"}}},
		4: {Locations: &[]placeIn{{Kind: "remote", Country: "NORTH_AMERICA"}}},
	}
	for id, p := range profiles {
		if err := saveJobProfile(testDB, id, p); err != nil {
			t.Fatalf("job %d: %v", id, err)
		}
	}
}

func findCount(rs []CountRow, name string) int {
	for _, r := range rs {
		if r.Name == name || (r.Code != "" && r.Code == name) {
			return r.Count
		}
	}
	return -1
}

func TestInsightsCountPlacesAndRequirements(t *testing.T) {
	seedInsights(t)
	in, err := computeInsights(testDB)
	if err != nil {
		t.Fatal(err)
	}
	// Brightwater's two jobs differ only in case and spacing: one company.
	if in.Companies != 4 || in.CompaniesWithHQ != 3 {
		t.Errorf("companies %d with HQ %d, want 4 and 3", in.Companies, in.CompaniesWithHQ)
	}
	if findCount(in.HQByState, "TX") != 1 || findCount(in.HQByState, "CO") != 1 || len(in.HQByState) != 2 {
		t.Errorf("HQ by state: %+v", in.HQByState)
	}
	if findCount(in.HQByCountry, "CA") != 1 || findCount(in.HQByCountry, "US") != 2 {
		t.Errorf("HQ by country: %+v", in.HQByCountry)
	}
	if in.Home != "FL" {
		t.Errorf("home state %q, want FL", in.Home)
	}
	// FL: job 1 (all US), job 2 (FL), job 4 (North America). CO: 1, 3, 4. TX: 1, 4.
	for st, want := range map[string]int{"FL": 3, "CO": 3, "TX": 2, "GA": 3} {
		if got := findCount(in.OpenByState, st); got != want {
			t.Errorf("open to %s = %d, want %d", st, got, want)
		}
	}
	for name, want := range map[string]int{"Remote, anywhere in the US": 1, "Remote, some US states only": 1, "Office or hybrid only": 1, "Remote, several countries": 1, "Not recorded": 1} {
		if got := findCount(in.Reach, name); got != want {
			t.Errorf("reach %q = %d, want %d", name, got, want)
		}
	}
	if in.OpenNationwide != 2 {
		t.Errorf("open nationwide = %d, want 2 (jobs 1 and 4)", in.OpenNationwide)
	}
	if findCount(in.OfficeCities, "Denver, CO") != 1 {
		t.Errorf("office cities: %+v", in.OfficeCities)
	}
	// Kubernetes: three jobs, two required and one preferred, grouped
	// ignoring case under the most common spelling.
	var k8s *ReqCount
	for i, r := range in.Requirements["platform"] {
		if strings.EqualFold(r.Name, "kubernetes") {
			k8s = &in.Requirements["platform"][i]
		}
	}
	if k8s == nil || k8s.Name != "Kubernetes" || k8s.Jobs != 3 || k8s.Required != 2 || k8s.Preferred != 1 || k8s.OwnerNo != 2 || k8s.OwnerPartly != 1 {
		t.Errorf("kubernetes: %+v", k8s)
	}
	if len(in.Gaps) == 0 || in.Gaps[0].Name != "Kubernetes" {
		t.Errorf("gaps should lead with Kubernetes: %+v", in.Gaps)
	}
	if in.Requirements["platform"][0].Name != "Kubernetes" {
		t.Errorf("most asked platform first: %+v", in.Requirements["platform"])
	}
	if findCount(in.Degree, "No degree asked for") != 1 || findCount(in.Degree, "Bachelor's degree required") != 1 || findCount(in.Degree, "Not recorded") != 1 {
		t.Errorf("degree: %+v", in.Degree)
	}
	if findCount(in.YearsMin, "4 to 5 years") != 1 || findCount(in.YearsMin, "2 to 3 years") != 1 || findCount(in.YearsMin, "Not stated") != 2 {
		t.Errorf("years: %+v", in.YearsMin)
	}
	if findCount(in.Sizes, "Not recorded") != 2 || findCount(in.Ownership, "Venture-backed startup") != 1 {
		t.Errorf("sizes %+v ownership %+v", in.Sizes, in.Ownership)
	}
}

func TestJobProfileValidatesAndReplaces(t *testing.T) {
	seedInsights(t)
	str := func(s string) *string { return &s }
	bad := []jobProfileIn{
		{Locations: &[]placeIn{{Kind: "anywhere", Country: "US"}}},
		{Locations: &[]placeIn{{Kind: "remote", Country: "USA"}}},
		{Locations: &[]placeIn{{Kind: "remote", Country: "US", Region: "Florida"}}},
		{Locations: &[]placeIn{{Kind: "office", Country: "WORLD"}}},
		{Requirements: &[]requirementIn{{Kind: "skill", Name: "Go"}}},
		{Requirements: &[]requirementIn{{Kind: "language", Name: "  "}}},
		{Requirements: &[]requirementIn{{Kind: "language", Name: "Go", OwnerHas: "maybe"}}},
		{Degree: str("associate")},
		{Seniority: str("wizard")},
		{},
	}
	for i, p := range bad {
		if err := saveJobProfile(testDB, 1, p); err == nil {
			t.Errorf("case %d: want an error", i)
		}
	}
	if err := saveJobProfile(testDB, 999, jobProfileIn{Degree: str("none")}); err == nil {
		t.Error("an unknown job should be refused")
	}
	// A failed call changes nothing.
	var n int
	testDB.QueryRow("SELECT COUNT(*) FROM job_requirements WHERE job_id=1").Scan(&n)
	if n != 3 {
		t.Errorf("job 1 has %d requirements after refused calls, want 3", n)
	}
	// Duplicates merge; required wins.
	if err := saveJobProfile(testDB, 1, jobProfileIn{Requirements: &[]requirementIn{{Kind: "language", Name: "Rust", Required: false}, {Kind: "language", Name: "rust", Required: true, Years: 2}}}); err != nil {
		t.Fatal(err)
	}
	var req, years int
	testDB.QueryRow("SELECT COUNT(*), MAX(required), MAX(years) FROM job_requirements WHERE job_id=1").Scan(&n, &req, &years)
	if n != 1 || req != 1 || years != 2 {
		t.Errorf("after replace: %d rows, required %d, years %d", n, req, years)
	}
	// Locations were not given, so they are kept.
	testDB.QueryRow("SELECT COUNT(*) FROM job_locations WHERE job_id=1").Scan(&n)
	if n != 1 {
		t.Errorf("locations should be kept, have %d", n)
	}
	// An empty list clears.
	if err := saveJobProfile(testDB, 1, jobProfileIn{Locations: &[]placeIn{}}); err != nil {
		t.Fatal(err)
	}
	testDB.QueryRow("SELECT COUNT(*) FROM job_locations WHERE job_id=1").Scan(&n)
	if n != 0 {
		t.Errorf("locations should be cleared, have %d", n)
	}
}

func TestSaveCompanyMatchesJobsAndChecksFormats(t *testing.T) {
	seedInsights(t)
	str := func(s string) *string { return &s }
	if _, _, err := saveCompany(testDB, companyIn{Company: "Nobody Inc", Country: str("US")}); err == nil {
		t.Error("a company with no saved jobs should be refused")
	}
	for i, c := range []companyIn{
		{Company: "Copperleaf Energy", Country: str("USA")},
		{Company: "Copperleaf Energy", Country: str("US"), Region: str("Texas")},
		{Company: "Copperleaf Energy", Size: str("huge")},
		{Company: "Copperleaf Energy", Owner: str("family")},
		{Company: "Copperleaf Energy", Website: str("javascript:alert(1)")},
		{Company: "Copperleaf Energy"},
	} {
		if _, _, err := saveCompany(testDB, c); err == nil {
			t.Errorf("case %d: want an error", i)
		}
	}
	name, jobs, err := saveCompany(testDB, companyIn{Company: "  BRIGHTWATER analytics ", Notes: str("From the company's about page.")})
	if err != nil || jobs != 2 || !strings.EqualFold(name, "Brightwater Analytics") {
		t.Fatalf("match ignoring case and spaces: %q %d %v", name, jobs, err)
	}
	var city, notes string
	testDB.QueryRow("SELECT hq_city, notes FROM companies WHERE name='Brightwater Analytics'").Scan(&city, &notes)
	if city != "Austin" || notes == "" {
		t.Errorf("omitted fields must keep their value: city %q notes %q", city, notes)
	}
	// Clearing the country lets the region be any code.
	if _, _, err = saveCompany(testDB, companyIn{Company: "Brightwater Analytics", Country: str(""), Region: str("")}); err != nil {
		t.Fatal(err)
	}
}

func TestMetricsShowsMapsAndRequirements(t *testing.T) {
	seedInsights(t)
	rec := httptest.NewRecorder()
	metricsHandler(testDB)(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /metrics = %d: %s", rec.Code, rec.Body.String())
	}
	body, _ := io.ReadAll(rec.Body)
	page := string(body)
	for _, want := range []string{
		`id="places"`, `id="asks"`, `id="chart-hq"`, `id="chart-open"`, `class="chart map"`,
		`<title>Texas: 1 company, Brightwater Analytics</title>`,
		`<title>Florida: 3 of 4 leads, 1 more than nationwide (your Profile location)</title>`,
		`class="c-home"`, "Kubernetes", "Asked for, not in your profile", "Canada",
		"Toronto, ON, Canada", "Denver, CO", `href="#places"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("metrics page is missing %q", want)
		}
	}
	// Every one of the 51 tiles is drawn, each with a title.
	if n := strings.Count(page, `<g class="tile `); n != 102 {
		t.Errorf("tiles drawn: %d, want 51 per map", n)
	}
}

func TestMetricsInsightsEmpty(t *testing.T) {
	metricsSetup(t)
	rec := httptest.NewRecorder()
	metricsHandler(testDB)(rec, httptest.NewRequest("GET", "/metrics", nil))
	page := rec.Body.String()
	for _, want := range []string{"No company has a recorded headquarters yet", "No lead has recorded locations yet", "No gaps recorded yet"} {
		if !strings.Contains(page, want) {
			t.Errorf("empty metrics page is missing %q", want)
		}
	}
	if strings.Contains(page, `class="chart map"`) {
		t.Error("an empty tracker should not draw a map")
	}
}

func TestInsightToolsOverMCP(t *testing.T) {
	cs := setupApplyTest(t)
	seedInsights(t)
	out := callTool(t, cs, "set_company", map[string]any{"company": "copperleaf energy", "hq_city": "Houston", "hq_region": "TX", "hq_country": "US", "ownership": "public"})
	if out["jobs"].(float64) != 1 {
		t.Errorf("set_company: %v", out)
	}
	out = callTool(t, cs, "set_job_profile", map[string]any{"job_id": 5,
		"locations":    []any{map[string]any{"kind": "office", "country": "US", "region": "TX", "city": "Houston"}},
		"requirements": []any{map[string]any{"kind": "language", "name": "Rust"}, map[string]any{"kind": "domain", "name": "Energy", "required": false}},
		"years_min":    8, "degree": "Bachelor", "seniority": "Staff"})
	b, _ := json.Marshal(out)
	for _, want := range []string{`"Houston"`, `"Rust"`, `"degree":"bachelor"`, `"seniority":"staff"`, `"years_min":8`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("set_job_profile result missing %s: %s", want, b)
		}
	}
	job := callTool(t, cs, "get_job", map[string]any{"job_id": 5})
	b, _ = json.Marshal(job["profile"])
	if !strings.Contains(string(b), `"hq_city":"Houston"`) || !strings.Contains(string(b), `"Energy"`) {
		t.Errorf("get_job profile: %s", b)
	}
	ins := callTool(t, cs, "get_market_insights", map[string]any{"requirement": "kubernetes"})
	b, _ = json.Marshal(ins)
	if !strings.Contains(string(b), `"headquarters_by_us_state"`) || !strings.Contains(string(b), `"job_id":3`) {
		t.Errorf("get_market_insights: %s", b)
	}
	gaps := callTool(t, cs, "list_profile_gaps", nil)
	b, _ = json.Marshal(gaps)
	if strings.Contains(string(b), "Copperleaf") || !strings.Contains(string(b), `"jobs_without_profile_total":0`) {
		t.Errorf("list_profile_gaps: %s", b)
	}
}
