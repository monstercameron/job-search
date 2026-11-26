package main

import (
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Where the companies are and what the jobs ask for. These facts are written
// by the assistant through set_company and set_job_profile: an LLM reads the
// posting and the company's own pages and records structured values (an ISO
// country code, a US state code, a requirement's canonical name and kind).
// The app never decides meaning from words; it only validates formats,
// groups equal values ignoring case, and counts.

func migrateInsights(db *sql.DB) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS companies (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE COLLATE NOCASE,
			hq_city TEXT NOT NULL DEFAULT '',
			hq_region TEXT NOT NULL DEFAULT '',
			hq_country TEXT NOT NULL DEFAULT '',
			size_band TEXT NOT NULL DEFAULT '',
			ownership TEXT NOT NULL DEFAULT '',
			founded_year INTEGER,
			website TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS job_locations (
			id INTEGER PRIMARY KEY,
			job_id INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
			kind TEXT NOT NULL CHECK (kind IN ('remote', 'office')),
			country TEXT NOT NULL,
			region TEXT NOT NULL DEFAULT '',
			city TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS job_locations_job ON job_locations(job_id)`,
		`CREATE TABLE IF NOT EXISTS job_requirements (
			id INTEGER PRIMARY KEY,
			job_id INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
			kind TEXT NOT NULL,
			name TEXT NOT NULL,
			required INTEGER NOT NULL DEFAULT 1,
			years INTEGER,
			owner_has TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS job_requirements_job ON job_requirements(job_id)`,
	} {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	for _, c := range []struct{ name, def string }{
		{"years_min", "INTEGER"}, {"degree_level", "TEXT"}, {"seniority_level", "TEXT"}, {"profiled_at", "TEXT"},
	} {
		ok, err := hasColumn(db, "jobs", c.name)
		if err != nil {
			return err
		}
		if !ok {
			if _, err = db.Exec("ALTER TABLE jobs ADD COLUMN " + c.name + " " + c.def); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------- closed vocabularies ----------

var reqKinds = []Choice{
	{Value: "language", Label: "Programming languages"},
	{Value: "framework", Label: "Frameworks and libraries"},
	{Value: "platform", Label: "Platforms, cloud and data"},
	{Value: "tool", Label: "Tools and software"},
	{Value: "practice", Label: "Practices and skills"},
	{Value: "domain", Label: "Domain knowledge"},
	{Value: "certification", Label: "Certifications"},
}

var sizeBands = []Choice{
	{Value: "1-10", Label: "1 to 10 people"}, {Value: "11-50", Label: "11 to 50"}, {Value: "51-200", Label: "51 to 200"},
	{Value: "201-500", Label: "201 to 500"}, {Value: "501-1000", Label: "501 to 1,000"}, {Value: "1001-5000", Label: "1,001 to 5,000"},
	{Value: "5001-10000", Label: "5,001 to 10,000"}, {Value: "10001+", Label: "Over 10,000"},
}

var ownerships = []Choice{
	{Value: "public", Label: "Public company"}, {Value: "venture", Label: "Venture-backed startup"},
	{Value: "private", Label: "Private company"}, {Value: "nonprofit", Label: "Nonprofit"}, {Value: "government", Label: "Government"},
}

var degreeLevels = []Choice{
	{Value: "none", Label: "No degree asked for"}, {Value: "preferred", Label: "Degree preferred, not required"},
	{Value: "equivalent", Label: "Degree or equivalent experience"}, {Value: "bachelor", Label: "Bachelor's degree required"},
	{Value: "master", Label: "Master's degree required"}, {Value: "phd", Label: "PhD required"},
}

var seniorityLevels = []Choice{
	{Value: "intern", Label: "Intern"}, {Value: "junior", Label: "Junior"}, {Value: "mid", Label: "Mid-level"},
	{Value: "senior", Label: "Senior"}, {Value: "staff", Label: "Staff"}, {Value: "principal", Label: "Principal"},
	{Value: "lead", Label: "Lead"}, {Value: "manager", Label: "Manager"}, {Value: "director", Label: "Director or above"},
}

var ownerHasValues = []string{"yes", "partly", "no"}

func inChoices(cs []Choice, v string) bool {
	for _, c := range cs {
		if c.Value == v {
			return true
		}
	}
	return false
}

// ---------- geography ----------

type usState struct {
	Code, Name string
	Col, Row   int
}

// usStates places each state and DC on a 12 by 8 tile grid that keeps
// neighbours roughly where they are on a map.
var usStates = []usState{
	{"AK", "Alaska", 0, 0}, {"ME", "Maine", 11, 0},
	{"WI", "Wisconsin", 6, 1}, {"VT", "Vermont", 10, 1}, {"NH", "New Hampshire", 11, 1},
	{"WA", "Washington", 1, 2}, {"ID", "Idaho", 2, 2}, {"MT", "Montana", 3, 2}, {"ND", "North Dakota", 4, 2}, {"MN", "Minnesota", 5, 2},
	{"IL", "Illinois", 6, 2}, {"MI", "Michigan", 7, 2}, {"NY", "New York", 9, 2}, {"MA", "Massachusetts", 10, 2},
	{"OR", "Oregon", 1, 3}, {"NV", "Nevada", 2, 3}, {"WY", "Wyoming", 3, 3}, {"SD", "South Dakota", 4, 3}, {"IA", "Iowa", 5, 3},
	{"IN", "Indiana", 6, 3}, {"OH", "Ohio", 7, 3}, {"PA", "Pennsylvania", 8, 3}, {"NJ", "New Jersey", 9, 3}, {"CT", "Connecticut", 10, 3}, {"RI", "Rhode Island", 11, 3},
	{"CA", "California", 1, 4}, {"UT", "Utah", 2, 4}, {"CO", "Colorado", 3, 4}, {"NE", "Nebraska", 4, 4}, {"MO", "Missouri", 5, 4},
	{"KY", "Kentucky", 6, 4}, {"WV", "West Virginia", 7, 4}, {"VA", "Virginia", 8, 4}, {"MD", "Maryland", 9, 4}, {"DE", "Delaware", 10, 4},
	{"AZ", "Arizona", 2, 5}, {"NM", "New Mexico", 3, 5}, {"KS", "Kansas", 4, 5}, {"AR", "Arkansas", 5, 5}, {"TN", "Tennessee", 6, 5},
	{"NC", "North Carolina", 7, 5}, {"SC", "South Carolina", 8, 5}, {"DC", "District of Columbia", 9, 5},
	{"OK", "Oklahoma", 4, 6}, {"LA", "Louisiana", 5, 6}, {"MS", "Mississippi", 6, 6}, {"AL", "Alabama", 7, 6}, {"GA", "Georgia", 8, 6},
	{"HI", "Hawaii", 0, 7}, {"TX", "Texas", 4, 7}, {"FL", "Florida", 9, 7},
}

func stateName(code string) (string, bool) {
	for _, s := range usStates {
		if s.Code == code {
			return s.Name, true
		}
	}
	return "", false
}

// Remote areas wider than one country. coversUS says whether someone living
// in the United States is inside the area.
var remoteAreas = []struct {
	Code, Label string
	CoversUS    bool
}{
	{"WORLD", "Anywhere in the world", true},
	{"AMERICAS", "The Americas", true},
	{"NORTH_AMERICA", "North America", true},
	{"LATAM", "Latin America", false},
	{"EUROPE", "Europe", false},
	{"EMEA", "Europe, the Middle East and Africa", false},
	{"APAC", "Asia-Pacific", false},
}

func remoteArea(code string) (label string, coversUS, ok bool) {
	for _, a := range remoteAreas {
		if a.Code == code {
			return a.Label, a.CoversUS, true
		}
	}
	return "", false, false
}

var countryNames = map[string]string{
	"US": "United States", "CA": "Canada", "MX": "Mexico", "BR": "Brazil", "AR": "Argentina", "CL": "Chile", "CO": "Colombia", "PE": "Peru", "UY": "Uruguay", "CR": "Costa Rica",
	"GB": "United Kingdom", "IE": "Ireland", "FR": "France", "DE": "Germany", "NL": "Netherlands", "BE": "Belgium", "LU": "Luxembourg", "CH": "Switzerland", "AT": "Austria",
	"ES": "Spain", "PT": "Portugal", "IT": "Italy", "SE": "Sweden", "NO": "Norway", "DK": "Denmark", "FI": "Finland", "IS": "Iceland", "PL": "Poland", "CZ": "Czechia",
	"EE": "Estonia", "LV": "Latvia", "LT": "Lithuania", "RO": "Romania", "BG": "Bulgaria", "GR": "Greece", "HU": "Hungary", "UA": "Ukraine", "RS": "Serbia", "HR": "Croatia",
	"IL": "Israel", "AE": "United Arab Emirates", "SA": "Saudi Arabia", "TR": "Türkiye", "EG": "Egypt", "ZA": "South Africa", "NG": "Nigeria", "KE": "Kenya",
	"IN": "India", "PK": "Pakistan", "SG": "Singapore", "MY": "Malaysia", "ID": "Indonesia", "PH": "Philippines", "VN": "Vietnam", "TH": "Thailand",
	"JP": "Japan", "KR": "South Korea", "CN": "China", "HK": "Hong Kong", "TW": "Taiwan", "AU": "Australia", "NZ": "New Zealand",
}

func countryName(code string) string {
	if n, ok := countryNames[code]; ok {
		return n
	}
	if l, _, ok := remoteArea(code); ok {
		return l
	}
	return code
}

var countryCode = regexp.MustCompile(`^[A-Z]{2}$`)

// cleanText trims a value, collapses runs of spaces and caps its length.
func cleanText(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > max {
		s = strings.TrimSpace(string([]rune(s)[:max]))
	}
	return s
}

// placeLabel is "Miami, FL", "Toronto, ON, Canada" or "Canada".
func placeLabel(city, region, country string) string {
	var parts []string
	if city != "" {
		parts = append(parts, city)
	}
	if region != "" {
		parts = append(parts, region)
	}
	if country != "US" || len(parts) == 0 {
		parts = append(parts, countryName(country))
	}
	return strings.Join(parts, ", ")
}

// homeState reads a US state code from the Profile's location when it is
// written as "City, ST". It returns "" for anything else.
func homeState(db *sql.DB) string {
	var loc string
	db.QueryRow("SELECT COALESCE(value,'') FROM profile_fields WHERE key='location'").Scan(&loc)
	parts := strings.Split(loc, ",")
	for i := len(parts) - 1; i >= 1; i-- {
		code := strings.ToUpper(strings.TrimSpace(parts[i]))
		if _, ok := stateName(code); ok {
			return code
		}
	}
	return ""
}

// ---------- loading ----------

type companyRec struct {
	Name, City, Region, Country, Size, Ownership string
}

type placeRec struct {
	Kind, Country, Region, City string
}

type reqRec struct {
	JobID          int64
	Kind, Name     string
	Required       bool
	Years          int // 0 when not stated
	OwnerHas       string
	CompanyKey     string
	Company, Title string
}

type insightJob struct {
	ID                  int64
	Company, Title, Key string
	YearsMin            int // -1 when not recorded
	Degree, Level       string
	Profiled            bool
}

type CountRow struct {
	Name  string   `json:"name"`
	Code  string   `json:"code,omitempty"`
	Count int      `json:"count"`
	Names []string `json:"companies,omitempty"`
}

type ReqCount struct {
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Jobs        int     `json:"jobs"`
	Required    int     `json:"required"`
	Preferred   int     `json:"preferred"`
	OwnerYes    int     `json:"owner_has_yes,omitempty"`
	OwnerPartly int     `json:"owner_has_partly,omitempty"`
	OwnerNo     int     `json:"owner_has_no,omitempty"`
	JobIDs      []int64 `json:"job_ids,omitempty"`
}

// Insights is the location and requirement picture across all saved leads.
// The Metrics page draws it and get_market_insights returns it.
type Insights struct {
	Leads             int                   `json:"leads"`
	LeadsProfiled     int                   `json:"leads_with_job_profile"`
	LeadsWithPlaces   int                   `json:"leads_with_locations"`
	Companies         int                   `json:"companies"`
	CompaniesWithHQ   int                   `json:"companies_with_headquarters"`
	HQByState         []CountRow            `json:"headquarters_by_us_state"`
	HQByCountry       []CountRow            `json:"headquarters_by_country"`
	HQCities          []CountRow            `json:"headquarters_cities"`
	Home              string                `json:"home_state,omitempty"`
	OpenByState       []CountRow            `json:"leads_open_by_us_state"`
	OpenNationwide    int                   `json:"leads_open_anywhere_in_us"`
	Reach             []CountRow            `json:"remote_reach"`
	OfficeCities      []CountRow            `json:"office_cities"`
	Sizes             []CountRow            `json:"company_sizes"`
	Ownership         []CountRow            `json:"company_ownership"`
	Requirements      map[string][]ReqCount `json:"requirements_by_kind"`
	Gaps              []ReqCount            `json:"profile_gaps"`
	YearsMin          []CountRow            `json:"years_of_experience"`
	Degree            []CountRow            `json:"degree_requirement"`
	Seniority         []CountRow            `json:"seniority"`
	RequirementsTotal int                   `json:"requirements_recorded"`
}

func companyKey(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

func loadInsightData(db *sql.DB) (jobs []insightJob, companies map[string]companyRec, places map[int64][]placeRec, reqs []reqRec, err error) {
	rows, err := db.Query(`SELECT id, company, title, COALESCE(years_min, -1), COALESCE(degree_level,''), COALESCE(seniority_level,''), profiled_at IS NOT NULL FROM jobs ORDER BY id`)
	if err != nil {
		return
	}
	byID := map[int64]insightJob{}
	for rows.Next() {
		var j insightJob
		if err = rows.Scan(&j.ID, &j.Company, &j.Title, &j.YearsMin, &j.Degree, &j.Level, &j.Profiled); err != nil {
			rows.Close()
			return
		}
		j.Key = companyKey(j.Company)
		jobs = append(jobs, j)
		byID[j.ID] = j
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return
	}
	companies = map[string]companyRec{}
	rows, err = db.Query(`SELECT name, hq_city, hq_region, hq_country, size_band, ownership FROM companies`)
	if err != nil {
		return
	}
	for rows.Next() {
		var c companyRec
		if err = rows.Scan(&c.Name, &c.City, &c.Region, &c.Country, &c.Size, &c.Ownership); err != nil {
			rows.Close()
			return
		}
		companies[companyKey(c.Name)] = c
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return
	}
	places = map[int64][]placeRec{}
	rows, err = db.Query(`SELECT job_id, kind, country, region, city FROM job_locations ORDER BY id`)
	if err != nil {
		return
	}
	for rows.Next() {
		var id int64
		var p placeRec
		if err = rows.Scan(&id, &p.Kind, &p.Country, &p.Region, &p.City); err != nil {
			rows.Close()
			return
		}
		places[id] = append(places[id], p)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return
	}
	rows, err = db.Query(`SELECT job_id, kind, name, required, COALESCE(years,0), owner_has FROM job_requirements ORDER BY id`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var r reqRec
		var req int
		if err = rows.Scan(&r.JobID, &r.Kind, &r.Name, &req, &r.Years, &r.OwnerHas); err != nil {
			return
		}
		r.Required = req != 0
		j := byID[r.JobID]
		r.CompanyKey, r.Company, r.Title = j.Key, j.Company, j.Title
		reqs = append(reqs, r)
	}
	err = rows.Err()
	return
}

// sortCounts orders rows largest first, then by name.
func sortCounts(rs []CountRow) {
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Count != rs[j].Count {
			return rs[i].Count > rs[j].Count
		}
		return rs[i].Name < rs[j].Name
	})
}

func countMap(m map[string]*CountRow) []CountRow {
	out := make([]CountRow, 0, len(m))
	for _, r := range m {
		sort.Strings(r.Names)
		out = append(out, *r)
	}
	sortCounts(out)
	return out
}

// choiceCounts counts values in the order of their vocabulary, with
// "Not recorded" last when any are missing.
func choiceCounts(cs []Choice, values []string) []CountRow {
	n := map[string]int{}
	for _, v := range values {
		n[v]++
	}
	var out []CountRow
	for _, c := range cs {
		out = append(out, CountRow{Name: c.Label, Code: c.Value, Count: n[c.Value]})
	}
	if n[""] > 0 {
		out = append(out, CountRow{Name: "Not recorded", Count: n[""]})
	}
	return out
}

// reachOf sums up one job's recorded locations as a single category.
func reachOf(ps []placeRec) string {
	if len(ps) == 0 {
		return ""
	}
	var remote []placeRec
	for _, p := range ps {
		if p.Kind == "remote" {
			remote = append(remote, p)
		}
	}
	if len(remote) == 0 {
		return "office"
	}
	countries := map[string]bool{}
	for _, p := range remote {
		if p.Country == "WORLD" {
			return "world"
		}
		if _, _, area := remoteArea(p.Country); area {
			return "region"
		}
		countries[p.Country] = true
	}
	if len(countries) > 1 {
		return "region"
	}
	if !countries["US"] {
		return "abroad"
	}
	for _, p := range remote {
		if p.Region == "" {
			return "us"
		}
	}
	return "states"
}

var reachChoices = []Choice{
	{Value: "world", Label: "Remote, anywhere in the world"},
	{Value: "region", Label: "Remote, several countries"},
	{Value: "us", Label: "Remote, anywhere in the US"},
	{Value: "states", Label: "Remote, some US states only"},
	{Value: "abroad", Label: "Remote, one other country"},
	{Value: "office", Label: "Office or hybrid only"},
}

// openTo says whether someone living in US state st could take a job with
// these locations: a remote area that covers the state, or an office in it.
func openTo(ps []placeRec, st string) bool {
	for _, p := range ps {
		switch p.Kind {
		case "remote":
			if p.Country == "US" && (p.Region == "" || p.Region == st) {
				return true
			}
			if _, covers, ok := remoteArea(p.Country); ok && covers {
				return true
			}
		case "office":
			if p.Country == "US" && p.Region == st {
				return true
			}
		}
	}
	return false
}

func computeInsights(db *sql.DB) (Insights, error) {
	in := Insights{Requirements: map[string][]ReqCount{}}
	jobs, companies, places, reqs, err := loadInsightData(db)
	if err != nil {
		return in, err
	}
	in.Leads = len(jobs)
	in.Home = homeState(db)
	in.RequirementsTotal = len(reqs)

	// Companies: each distinct name on a saved job, once.
	seen := map[string]bool{}
	hqState, hqCountry, hqCity := map[string]*CountRow{}, map[string]*CountRow{}, map[string]*CountRow{}
	var sizes, owns []string
	for _, j := range jobs {
		if j.Profiled {
			in.LeadsProfiled++
		}
		if len(places[j.ID]) > 0 {
			in.LeadsWithPlaces++
		}
		if seen[j.Key] {
			continue
		}
		seen[j.Key] = true
		in.Companies++
		c, ok := companies[j.Key]
		name := strings.TrimSpace(j.Company)
		if !ok {
			sizes, owns = append(sizes, ""), append(owns, "")
			continue
		}
		sizes, owns = append(sizes, c.Size), append(owns, c.Ownership)
		if c.Country == "" {
			continue
		}
		in.CompaniesWithHQ++
		add := func(m map[string]*CountRow, key, label, code string) {
			if m[key] == nil {
				m[key] = &CountRow{Name: label, Code: code}
			}
			m[key].Count++
			m[key].Names = append(m[key].Names, name)
		}
		if c.Country == "US" {
			if sn, ok := stateName(c.Region); ok {
				add(hqState, c.Region, sn, c.Region)
			}
		}
		add(hqCountry, c.Country, countryName(c.Country), c.Country)
		if c.City != "" {
			add(hqCity, strings.ToLower(placeLabel(c.City, c.Region, c.Country)), placeLabel(c.City, c.Region, c.Country), "")
		}
	}
	in.HQByState, in.HQByCountry, in.HQCities = countMap(hqState), countMap(hqCountry), countMap(hqCity)
	in.Sizes, in.Ownership = choiceCounts(sizeBands, sizes), choiceCounts(ownerships, owns)

	// Where the roles can be done from.
	reach := make([]string, 0, len(jobs))
	offices := map[string]*CountRow{}
	for _, j := range jobs {
		ps := places[j.ID]
		reach = append(reach, reachOf(ps))
		done := map[string]bool{}
		for _, p := range ps {
			if p.Kind != "office" || p.City == "" {
				continue
			}
			l := placeLabel(p.City, p.Region, p.Country)
			k := strings.ToLower(l)
			if done[k] {
				continue
			}
			done[k] = true
			if offices[k] == nil {
				offices[k] = &CountRow{Name: l}
			}
			offices[k].Count++
		}
	}
	in.Reach = choiceCounts(reachChoices, reach)
	in.OfficeCities = countMap(offices)
	everywhere := map[int64]bool{}
	for _, j := range jobs {
		everywhere[j.ID] = len(places[j.ID]) > 0
	}
	for _, s := range usStates {
		n := 0
		for _, j := range jobs {
			if openTo(places[j.ID], s.Code) {
				n++
			} else {
				everywhere[j.ID] = false
			}
		}
		in.OpenByState = append(in.OpenByState, CountRow{Name: s.Name, Code: s.Code, Count: n})
	}
	for _, ok := range everywhere {
		if ok {
			in.OpenNationwide++
		}
	}

	// Requirements, grouped by kind and by name ignoring case. A job that
	// lists a name twice counts once, as required if either entry is.
	type agg struct {
		ReqCount
		spell map[string]int
		jobs  map[int64]*reqRec
	}
	groups := map[string]*agg{}
	var order []string
	for i := range reqs {
		r := &reqs[i]
		k := r.Kind + "\x00" + strings.ToLower(r.Name)
		g := groups[k]
		if g == nil {
			g = &agg{ReqCount: ReqCount{Kind: r.Kind}, spell: map[string]int{}, jobs: map[int64]*reqRec{}}
			groups[k] = g
			order = append(order, k)
		}
		g.spell[r.Name]++
		if prev := g.jobs[r.JobID]; prev == nil || (!prev.Required && r.Required) {
			g.jobs[r.JobID] = r
		}
	}
	for _, k := range order {
		g := groups[k]
		best := 0
		for s, n := range g.spell {
			if n > best || (n == best && s < g.Name) {
				g.Name, best = s, n
			}
		}
		for id, r := range g.jobs {
			g.Jobs++
			g.JobIDs = append(g.JobIDs, id)
			if r.Required {
				g.Required++
			} else {
				g.Preferred++
			}
			switch r.OwnerHas {
			case "yes":
				g.OwnerYes++
			case "partly":
				g.OwnerPartly++
			case "no":
				g.OwnerNo++
			}
		}
		sort.Slice(g.JobIDs, func(a, b int) bool { return g.JobIDs[a] < g.JobIDs[b] })
		in.Requirements[g.Kind] = append(in.Requirements[g.Kind], g.ReqCount)
		if g.OwnerNo+g.OwnerPartly > 0 {
			in.Gaps = append(in.Gaps, g.ReqCount)
		}
	}
	byJobs := func(rs []ReqCount) {
		sort.SliceStable(rs, func(i, j int) bool {
			if rs[i].Jobs != rs[j].Jobs {
				return rs[i].Jobs > rs[j].Jobs
			}
			if rs[i].Required != rs[j].Required {
				return rs[i].Required > rs[j].Required
			}
			return strings.ToLower(rs[i].Name) < strings.ToLower(rs[j].Name)
		})
	}
	for k := range in.Requirements {
		byJobs(in.Requirements[k])
	}
	sort.SliceStable(in.Gaps, func(i, j int) bool {
		a, b := in.Gaps[i].OwnerNo+in.Gaps[i].OwnerPartly, in.Gaps[j].OwnerNo+in.Gaps[j].OwnerPartly
		if a != b {
			return a > b
		}
		if in.Gaps[i].OwnerNo != in.Gaps[j].OwnerNo {
			return in.Gaps[i].OwnerNo > in.Gaps[j].OwnerNo
		}
		return strings.ToLower(in.Gaps[i].Name) < strings.ToLower(in.Gaps[j].Name)
	})

	// Experience, degree and level, from profiled jobs only.
	years := []struct {
		label  string
		lo, hi int
	}{{"0 to 1 years", 0, 1}, {"2 to 3 years", 2, 3}, {"4 to 5 years", 4, 5}, {"6 to 7 years", 6, 7}, {"8 to 9 years", 8, 9}, {"10 or more years", 10, 99}}
	yc := make([]int, len(years))
	unstated := 0
	var degrees, levels []string
	for _, j := range jobs {
		if !j.Profiled {
			continue
		}
		degrees, levels = append(degrees, j.Degree), append(levels, j.Level)
		if j.YearsMin < 0 {
			unstated++
			continue
		}
		for i, y := range years {
			if j.YearsMin >= y.lo && j.YearsMin <= y.hi {
				yc[i]++
				break
			}
		}
	}
	for i, y := range years {
		in.YearsMin = append(in.YearsMin, CountRow{Name: y.label, Count: yc[i]})
	}
	if unstated > 0 {
		in.YearsMin = append(in.YearsMin, CountRow{Name: "Not stated", Count: unstated})
	}
	in.Degree = choiceCounts(degreeLevels, degrees)
	in.Seniority = choiceCounts(seniorityLevels, levels)
	return in, nil
}

// ---------- charts ----------

// Places and Asks are the Metrics page's two new sections.
type PlacesView struct {
	HQ, Abroad, HQCities, Open, Reach, Offices, Sizes, Ownership ChartData
	Lede                                                         string
}

type AsksView struct {
	Kinds                      []ChartData
	Gaps, Years, Degree, Level ChartData
	Lede                       string
}

func pluralN(n int, one, many string) string { return strconv.Itoa(n) + " " + plural2(n, one, many) }

// hqShade buckets a count of companies.
func hqShade(n int) string {
	switch {
	case n <= 0:
		return "m0"
	case n == 1:
		return "m1"
	case n <= 4:
		return "m2"
	case n <= 9:
		return "m3"
	}
	return "m4"
}

func listNames(names []string, keep int) string {
	if len(names) > keep {
		return strings.Join(names[:keep], ", ") + fmt.Sprintf(" and %d more", len(names)-keep)
	}
	return strings.Join(names, ", ")
}

func countBars(rows []CountRow, keep int, other string) ([]bar, [][]string) {
	var bars []bar
	var table [][]string
	rest := 0
	for i, r := range rows {
		if i >= keep {
			rest += r.Count
			continue
		}
		cls := "slate"
		if r.Name == "Not recorded" || r.Name == "Not stated" {
			cls = "faint"
		}
		bars = append(bars, bar{Label: r.Name, Value: strconv.Itoa(r.Count), N: float64(r.Count), Class: cls})
		table = append(table, []string{r.Name, strconv.Itoa(r.Count)})
	}
	if rest > 0 {
		bars = append(bars, bar{Label: other, Value: strconv.Itoa(rest), N: float64(rest), Class: "faint"})
		table = append(table, []string{other, strconv.Itoa(rest)})
	}
	return bars, table
}

func buildPlaces(in Insights) PlacesView {
	var v PlacesView
	v.Lede = fmt.Sprintf("%d of %d companies have a recorded headquarters, and %d of %d leads have recorded locations. The assistant fills these in with set_company and set_job_profile.",
		in.CompaniesWithHQ, in.Companies, in.LeadsWithPlaces, in.Leads)

	// Headquarters map.
	v.HQ = ChartData{ID: "hq", Title: "Where the companies are headquartered", Lede: "US companies by the state of their headquarters. Each company counts once, however many of its jobs you saved.",
		Head:   []string{"State", "Companies", "Which"},
		Legend: []LegendItem{{"1 company", "m1"}, {"2 to 4", "m2"}, {"5 to 9", "m3"}, {"10 or more", "m4"}}}
	if in.CompaniesWithHQ == 0 {
		v.HQ.Empty = "No company has a recorded headquarters yet. Ask the assistant to record them with set_company."
		v.HQ.Legend = nil
	} else {
		tiles := map[string]tileVal{}
		usN := 0
		for _, r := range in.HQByState {
			usN += r.Count
			tiles[r.Code] = tileVal{N: r.Count, Class: hqShade(r.Count), Title: fmt.Sprintf("%s: %s, %s", r.Name, pluralN(r.Count, "company", "companies"), listNames(r.Names, 6))}
			v.HQ.Rows = append(v.HQ.Rows, []string{r.Name, strconv.Itoa(r.Count), listNames(r.Names, 12)})
		}
		var desc []string
		for _, r := range in.HQByState {
			desc = append(desc, fmt.Sprintf("%s %d", r.Name, r.Count))
		}
		if usN == 0 {
			v.HQ.Empty = "None of the recorded headquarters is in the United States. The list beside this shows where they are."
			v.HQ.Legend = nil
		} else {
			v.HQ.SVG = tileMapSVG("Company headquarters by US state", strings.Join(desc, "; "), tiles, "")
			v.HQ.Note = fmt.Sprintf("%s in the US. Point at a state to see which.", pluralN(usN, "company is", "companies are"))
		}
	}
	v.Abroad = ChartData{ID: "hq-country", Title: "Headquarters by country", Lede: "Every recorded headquarters, by country.", Head: []string{"Country", "Companies"}}
	if len(in.HQByCountry) == 0 {
		v.Abroad.Empty = "No headquarters recorded yet."
	} else {
		bars, rows := countBars(in.HQByCountry, 8, "All other countries")
		v.Abroad.SVG, v.Abroad.Rows = hbarsSVG("Headquarters by country", bars, maxBar(bars)), rows
		if un := in.Companies - in.CompaniesWithHQ; un > 0 {
			v.Abroad.Note = fmt.Sprintf("%s not recorded yet.", pluralN(un, "company's headquarters is", "companies' headquarters are"))
		}
	}
	v.HQCities = ChartData{ID: "hq-city", Title: "Headquarters cities", Lede: "The cities with the most headquarters.", Head: []string{"City", "Companies"}}
	if len(in.HQCities) == 0 {
		v.HQCities.Empty = "No headquarters city recorded yet."
	} else {
		bars, rows := countBars(in.HQCities, 8, "All other cities")
		v.HQCities.SVG, v.HQCities.Rows = hbarsSVG("Headquarters cities", bars, maxBar(bars)), rows
	}

	// Who could take the job, by state.
	v.Open = ChartData{ID: "open", Title: "Where you could work from", Lede: "Each tile is how many leads someone living in that state could take. The shade is what the state adds to the roles open anywhere in the US: offices in it and remote work limited to some states.",
		Head:   []string{"State", "Leads open to it", "More than nationwide"},
		Legend: []LegendItem{{"Nationwide roles only", "m0"}, {"1 or 2 more", "m1"}, {"3 to 5 more", "m2"}, {"6 to 9 more", "m3"}, {"10 or more", "m4"}}}
	if in.LeadsWithPlaces == 0 {
		v.Open.Empty = "No lead has recorded locations yet. Ask the assistant to record them with set_job_profile."
		v.Open.Legend = nil
	} else {
		tiles := map[string]tileVal{}
		var desc []string
		homeN := 0
		for _, r := range in.OpenByState {
			extra := r.Count - in.OpenNationwide
			cls := "m0"
			switch {
			case extra <= 0:
			case extra <= 2:
				cls = "m1"
			case extra <= 5:
				cls = "m2"
			case extra <= 9:
				cls = "m3"
			default:
				cls = "m4"
			}
			t := fmt.Sprintf("%s: %d of %d leads, %d more than nationwide", r.Name, r.Count, in.LeadsWithPlaces, extra)
			if r.Code == in.Home {
				t += " (your Profile location)"
				homeN = r.Count
			}
			tiles[r.Code] = tileVal{N: r.Count, Class: cls, Title: t}
			desc = append(desc, fmt.Sprintf("%s %d", r.Name, r.Count))
			v.Open.Rows = append(v.Open.Rows, []string{r.Name, strconv.Itoa(r.Count), strconv.Itoa(extra)})
		}
		v.Open.SVG = tileMapSVG("Leads open to each US state", strings.Join(desc, "; "), tiles, in.Home)
		v.Open.Note = fmt.Sprintf("%d of the %d leads with recorded locations are open anywhere in the US.", in.OpenNationwide, in.LeadsWithPlaces)
		if in.Home != "" {
			hn, _ := stateName(in.Home)
			v.Open.Note += fmt.Sprintf(" The outlined state is %s, from your Profile location: %d are open to it.", hn, homeN)
			v.Open.Legend = append(v.Open.Legend, LegendItem{"Your Profile location", "home"})
		}
		if gap := in.Leads - in.LeadsWithPlaces; gap > 0 {
			v.Open.Note = strings.TrimSpace(v.Open.Note + fmt.Sprintf(" %s no recorded locations yet and %s left out.", pluralN(gap, "lead has", "leads have"), plural2(gap, "is", "are")))
		}
	}
	v.Reach = ChartData{ID: "reach", Title: "How far the remote roles reach", Lede: "Each lead once, by the widest area its remote work is open to.", Head: []string{"Reach", "Leads"}}
	if in.LeadsWithPlaces == 0 {
		v.Reach.Empty = "No lead has recorded locations yet."
	} else {
		bars, rows := countBars(in.Reach, 99, "")
		v.Reach.SVG, v.Reach.Rows = hbarsSVG("How far the remote roles reach", bars, maxBar(bars)), rows
	}
	v.Offices = ChartData{ID: "offices", Title: "Office cities", Lede: "Cities with an office you would work from, for on-site and hybrid roles.", Head: []string{"City", "Leads"}}
	if len(in.OfficeCities) == 0 {
		v.Offices.Empty = "No lead has a recorded office city yet."
	} else {
		bars, rows := countBars(in.OfficeCities, 8, "All other cities")
		v.Offices.SVG, v.Offices.Rows = hbarsSVG("Office cities", bars, maxBar(bars)), rows
	}

	// Company size and ownership.
	v.Sizes = ChartData{ID: "size", Title: "Company size", Lede: "Companies by headcount, each counted once.", Head: []string{"People", "Companies"}}
	v.Ownership = ChartData{ID: "ownership", Title: "Company ownership", Lede: "Companies by who owns them, each counted once.", Head: []string{"Ownership", "Companies"}}
	for _, p := range []struct {
		c    *ChartData
		rows []CountRow
	}{{&v.Sizes, in.Sizes}, {&v.Ownership, in.Ownership}} {
		recorded := 0
		for _, r := range p.rows {
			if r.Code != "" {
				recorded += r.Count
			}
		}
		if recorded == 0 {
			p.c.Empty = "Not recorded for any company yet. The assistant fills it in with set_company."
			continue
		}
		bars, table := countBars(p.rows, 99, "")
		p.c.SVG, p.c.Rows = hbarsSVG(p.c.Title, bars, maxBar(bars)), table
	}
	return v
}

func buildAsks(in Insights) AsksView {
	var v AsksView
	v.Lede = fmt.Sprintf("%d of %d leads have their requirements recorded (%s in all). The assistant reads each posting and records them with set_job_profile.",
		in.LeadsProfiled, in.Leads, pluralN(in.RequirementsTotal, "requirement", "requirements"))
	legend := []LegendItem{{"Required", "slate"}, {"Preferred or a plus", "soft"}}
	for _, k := range reqKinds {
		rs := in.Requirements[k.Value]
		if len(rs) == 0 && k.Value == "certification" {
			continue
		}
		c := ChartData{ID: "req-" + k.Value, Title: k.Label, Lede: "The most asked for, by how many leads list each.", Head: []string{"Name", "Leads", "Required", "Preferred"}, Legend: legend}
		if len(rs) == 0 {
			c.Empty = "None recorded yet."
			c.Legend = nil
			v.Kinds = append(v.Kinds, c)
			continue
		}
		var bars []bar
		for i, r := range rs {
			if i < 10 {
				val := fmt.Sprintf("%d required", r.Required)
				switch {
				case r.Required == 0:
					val = fmt.Sprintf("%d preferred", r.Preferred)
				case r.Preferred > 0:
					val = fmt.Sprintf("%d required, %d preferred", r.Required, r.Preferred)
				}
				bars = append(bars, bar{Label: r.Name, Value: val, N: float64(r.Required), Class: "slate", N2: float64(r.Preferred), Class2: "soft"})
			}
			c.Rows = append(c.Rows, []string{r.Name, strconv.Itoa(r.Jobs), strconv.Itoa(r.Required), strconv.Itoa(r.Preferred)})
		}
		c.SVG = hbarsSVG(k.Label, bars, maxBar(bars))
		if len(rs) > 10 {
			c.Note = fmt.Sprintf("%d more in the table.", len(rs)-10)
		}
		v.Kinds = append(v.Kinds, c)
	}

	v.Gaps = ChartData{ID: "gaps", Title: "Asked for, not in your profile", Lede: "When the assistant records a requirement it checks it against your Profile. These are the ones it marked missing or partly covered, by how many leads ask for them.",
		Head: []string{"Name", "Kind", "Missing", "Partly", "Leads asking"}, Legend: []LegendItem{{"Not in your profile", "wait"}, {"Partly covered", "waitsoft"}}}
	if len(in.Gaps) == 0 {
		v.Gaps.Empty = "No gaps recorded yet. They appear once the assistant has checked requirements against your Profile."
		v.Gaps.Legend = nil
	} else {
		var bars []bar
		for i, g := range in.Gaps {
			if i < 12 {
				val := fmt.Sprintf("%d missing", g.OwnerNo)
				switch {
				case g.OwnerNo == 0:
					val = fmt.Sprintf("%d partly", g.OwnerPartly)
				case g.OwnerPartly > 0:
					val = fmt.Sprintf("%d missing, %d partly", g.OwnerNo, g.OwnerPartly)
				}
				bars = append(bars, bar{Label: g.Name, Value: val, N: float64(g.OwnerNo), Class: "wait", N2: float64(g.OwnerPartly), Class2: "waitsoft"})
			}
			v.Gaps.Rows = append(v.Gaps.Rows, []string{g.Name, choiceLabel(reqKinds, g.Kind), strconv.Itoa(g.OwnerNo), strconv.Itoa(g.OwnerPartly), strconv.Itoa(g.Jobs)})
		}
		v.Gaps.SVG = hbarsSVG("Requirements not in your profile", bars, maxBar(bars))
		v.Gaps.Note = "Worth a look before tailoring: a skill you have but left off the Profile, or one to learn. Never claim one you do not have."
	}

	v.Years = ChartData{ID: "years", Title: "Years of experience asked for", Lede: "The fewest years of overall experience each posting asks for.", Head: []string{"Years", "Leads"}}
	v.Degree = ChartData{ID: "degree", Title: "Degree requirement", Lede: "What each posting says about a degree.", Head: []string{"Requirement", "Leads"}}
	v.Level = ChartData{ID: "level", Title: "Seniority", Lede: "The level each posting is pitched at.", Head: []string{"Level", "Leads"}}
	for _, p := range []struct {
		c    *ChartData
		rows []CountRow
	}{{&v.Years, in.YearsMin}, {&v.Degree, in.Degree}, {&v.Level, in.Seniority}} {
		if in.LeadsProfiled == 0 {
			p.c.Empty = "No lead has its requirements recorded yet."
			continue
		}
		bars, table := countBars(p.rows, 99, "")
		p.c.SVG, p.c.Rows = hbarsSVG(p.c.Title, bars, maxBar(bars)), table
	}
	return v
}

// ---------- writing ----------

type placeIn struct{ Kind, Country, Region, City string }

func validatePlace(p placeIn) (placeIn, error) {
	p.Kind = strings.ToLower(strings.TrimSpace(p.Kind))
	p.Country = strings.ToUpper(strings.TrimSpace(p.Country))
	p.Region = strings.ToUpper(cleanText(p.Region, 40))
	p.City = cleanText(p.City, 80)
	if p.Kind != "remote" && p.Kind != "office" {
		return p, fmt.Errorf("location kind must be remote or office, not %q", p.Kind)
	}
	if _, _, area := remoteArea(p.Country); area {
		if p.Kind != "remote" {
			return p, fmt.Errorf("%s is an area for remote work; an office needs a country code", p.Country)
		}
		if p.Region != "" || p.City != "" {
			return p, fmt.Errorf("an area such as %s takes no region or city", p.Country)
		}
		return p, nil
	}
	if !countryCode.MatchString(p.Country) {
		return p, fmt.Errorf("country must be a two-letter ISO code such as US or CA, or one of WORLD, AMERICAS, NORTH_AMERICA, LATAM, EUROPE, EMEA, APAC; got %q", p.Country)
	}
	if p.Country == "US" && p.Region != "" {
		if _, ok := stateName(p.Region); !ok {
			return p, fmt.Errorf("region %q is not a US state code; use two letters such as FL or DC", p.Region)
		}
	}
	return p, nil
}

type requirementIn struct {
	Kind, Name, OwnerHas string
	Required             bool
	Years                int
}

func validateRequirement(r requirementIn) (requirementIn, error) {
	r.Kind = strings.ToLower(strings.TrimSpace(r.Kind))
	r.Name = cleanText(r.Name, 80)
	r.OwnerHas = strings.ToLower(strings.TrimSpace(r.OwnerHas))
	if !inChoices(reqKinds, r.Kind) {
		return r, fmt.Errorf("requirement kind must be one of language, framework, platform, tool, practice, domain, certification; got %q", r.Kind)
	}
	if r.Name == "" {
		return r, fmt.Errorf("a requirement needs a name")
	}
	if r.Years < 0 || r.Years > 40 {
		return r, fmt.Errorf("years for %s must be 0 to 40", r.Name)
	}
	if r.OwnerHas != "" {
		ok := false
		for _, v := range ownerHasValues {
			ok = ok || v == r.OwnerHas
		}
		if !ok {
			return r, fmt.Errorf("owner_has must be yes, partly or no, or left empty")
		}
	}
	return r, nil
}

type jobProfileIn struct {
	Locations    *[]placeIn
	Requirements *[]requirementIn
	YearsMin     *int
	Degree       *string
	Seniority    *string
}

// saveJobProfile replaces the parts of a job's profile that are given and
// marks the job as profiled.
func saveJobProfile(db *sql.DB, jobID int64, p jobProfileIn) error {
	if p.Locations == nil && p.Requirements == nil && p.YearsMin == nil && p.Degree == nil && p.Seniority == nil {
		return fmt.Errorf("give at least one of locations, requirements, years_min, degree or seniority")
	}
	var locs []placeIn
	if p.Locations != nil {
		seen := map[placeIn]bool{}
		for _, l := range *p.Locations {
			l, err := validatePlace(l)
			if err != nil {
				return err
			}
			if !seen[l] {
				seen[l] = true
				locs = append(locs, l)
			}
		}
	}
	var reqs []requirementIn
	if p.Requirements != nil {
		at := map[string]int{}
		for _, r := range *p.Requirements {
			r, err := validateRequirement(r)
			if err != nil {
				return err
			}
			k := r.Kind + "\x00" + strings.ToLower(r.Name)
			if i, dup := at[k]; dup {
				reqs[i].Required = reqs[i].Required || r.Required
				reqs[i].Years = max(reqs[i].Years, r.Years)
				continue
			}
			at[k] = len(reqs)
			reqs = append(reqs, r)
		}
	}
	if p.YearsMin != nil && (*p.YearsMin < -1 || *p.YearsMin > 40) {
		return fmt.Errorf("years_min must be 0 to 40, or -1 to clear")
	}
	if p.Degree != nil && *p.Degree != "" && !inChoices(degreeLevels, *p.Degree) {
		return fmt.Errorf("degree must be none, preferred, equivalent, bachelor, master or phd")
	}
	if p.Seniority != nil && *p.Seniority != "" && !inChoices(seniorityLevels, *p.Seniority) {
		return fmt.Errorf("seniority must be intern, junior, mid, senior, staff, principal, lead, manager or director")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var one int
	if err = tx.QueryRow("SELECT 1 FROM jobs WHERE id=?", jobID).Scan(&one); err != nil {
		return fmt.Errorf("job %d not found", jobID)
	}
	if p.Locations != nil {
		if _, err = tx.Exec("DELETE FROM job_locations WHERE job_id=?", jobID); err != nil {
			return err
		}
		for _, l := range locs {
			if _, err = tx.Exec("INSERT INTO job_locations(job_id,kind,country,region,city) VALUES(?,?,?,?,?)", jobID, l.Kind, l.Country, l.Region, l.City); err != nil {
				return err
			}
		}
	}
	if p.Requirements != nil {
		if _, err = tx.Exec("DELETE FROM job_requirements WHERE job_id=?", jobID); err != nil {
			return err
		}
		for _, r := range reqs {
			var years any
			if r.Years > 0 {
				years = r.Years
			}
			if _, err = tx.Exec("INSERT INTO job_requirements(job_id,kind,name,required,years,owner_has) VALUES(?,?,?,?,?,?)", jobID, r.Kind, r.Name, r.Required, years, r.OwnerHas); err != nil {
				return err
			}
		}
	}
	sets, args := []string{"profiled_at=?"}, []any{time.Now().Format("2006-01-02")}
	if p.YearsMin != nil {
		sets = append(sets, "years_min=?")
		if *p.YearsMin < 0 {
			args = append(args, nil)
		} else {
			args = append(args, *p.YearsMin)
		}
	}
	if p.Degree != nil {
		sets, args = append(sets, "degree_level=?"), append(args, nullIfEmpty(*p.Degree))
	}
	if p.Seniority != nil {
		sets, args = append(sets, "seniority_level=?"), append(args, nullIfEmpty(*p.Seniority))
	}
	args = append(args, jobID)
	if _, err = tx.Exec("UPDATE jobs SET "+strings.Join(sets, ",")+" WHERE id=?", args...); err != nil {
		return err
	}
	return tx.Commit()
}

type companyIn struct {
	Company                                     string
	City, Region, Country, Size, Owner, Website *string
	Notes                                       *string
	Founded                                     *int
}

// saveCompany records facts about a company that has saved jobs. Omitted
// fields keep their value; an empty string clears one.
func saveCompany(db *sql.DB, c companyIn) (name string, jobs int, err error) {
	key := companyKey(c.Company)
	if key == "" {
		return "", 0, fmt.Errorf("company is required")
	}
	// Match ignoring case and runs of spaces, the way the page groups them.
	rows, err := db.Query("SELECT company FROM jobs ORDER BY id")
	if err != nil {
		return "", 0, err
	}
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			rows.Close()
			return "", 0, err
		}
		if companyKey(s) == key {
			jobs++
			if name == "" {
				name = s
			}
		}
	}
	rows.Close()
	if jobs == 0 {
		return "", 0, fmt.Errorf("no saved job has the company %q; use the name exactly as it appears on the job", c.Company)
	}
	name = strings.Join(strings.Fields(name), " ")
	sets, args := []string{}, []any{}
	str := func(col string, p *string, max int, check func(string) (string, error)) error {
		if p == nil {
			return nil
		}
		v := cleanText(*p, max)
		if check != nil && v != "" {
			var e error
			if v, e = check(v); e != nil {
				return e
			}
		}
		sets, args = append(sets, col+"=?"), append(args, v)
		return nil
	}
	country := ""
	if c.Country != nil {
		country = strings.ToUpper(strings.TrimSpace(*c.Country))
	}
	checks := []error{
		str("hq_country", c.Country, 20, func(v string) (string, error) {
			v = strings.ToUpper(v)
			if !countryCode.MatchString(v) {
				return v, fmt.Errorf("hq_country must be a two-letter ISO code such as US, CA or GB")
			}
			return v, nil
		}),
		str("hq_region", c.Region, 40, func(v string) (string, error) {
			v = strings.ToUpper(v)
			if country == "" {
				db.QueryRow("SELECT hq_country FROM companies WHERE name=?", name).Scan(&country)
			}
			if country == "US" {
				if _, ok := stateName(v); !ok {
					return v, fmt.Errorf("hq_region %q is not a US state code; use two letters such as CA or NY", v)
				}
			}
			return v, nil
		}),
		str("hq_city", c.City, 80, nil),
		str("size_band", c.Size, 20, func(v string) (string, error) {
			if !inChoices(sizeBands, v) {
				return v, fmt.Errorf("size_band must be one of 1-10, 11-50, 51-200, 201-500, 501-1000, 1001-5000, 5001-10000, 10001+")
			}
			return v, nil
		}),
		str("ownership", c.Owner, 20, func(v string) (string, error) {
			v = strings.ToLower(v)
			if !inChoices(ownerships, v) {
				return v, fmt.Errorf("ownership must be public, venture, private, nonprofit or government")
			}
			return v, nil
		}),
		str("website", c.Website, 300, func(v string) (string, error) {
			if !validWebURL(v) {
				return v, fmt.Errorf("website must be an HTTP(S) URL")
			}
			return v, nil
		}),
		str("notes", c.Notes, 2000, nil),
	}
	for _, e := range checks {
		if e != nil {
			return "", 0, e
		}
	}
	if c.Founded != nil {
		y := *c.Founded
		if y != 0 && (y < 1600 || y > time.Now().Year()) {
			return "", 0, fmt.Errorf("founded_year must be a year, or 0 to clear")
		}
		sets = append(sets, "founded_year=?")
		if y == 0 {
			args = append(args, nil)
		} else {
			args = append(args, y)
		}
	}
	if len(sets) == 0 {
		return "", 0, fmt.Errorf("give at least one field to record")
	}
	tx, err := db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO companies(name) VALUES(?) ON CONFLICT(name) DO NOTHING", name); err != nil {
		return
	}
	args = append(args, name)
	if _, err = tx.Exec("UPDATE companies SET "+strings.Join(sets, ",")+", updated_at=CURRENT_TIMESTAMP WHERE name=?", args...); err != nil {
		return
	}
	return name, jobs, tx.Commit()
}
