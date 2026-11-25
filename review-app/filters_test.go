package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestClassifyWorkplaceReadsTheSavedText(t *testing.T) {
	for _, c := range []struct {
		arrangement, location string
		want                  []string
	}{
		{"Remote", "Remote - US", []string{"remote"}},
		{"Hybrid, 60-80% onsite", "", []string{"hybrid"}},
		{"Hybrid / onsite", "", []string{"hybrid", "office"}},
		{"Remote / Hybrid; Bay Area office 5 days", "", []string{"remote", "hybrid"}},
		{"Remote; Bay Area hybrid requested", "", []string{"remote"}},
		{"On-site / location-based; verify exact work arrangement", "", []string{"office"}},
		{"Onsite", "", []string{"office"}},
		{"Bellevue work location; onsite/remote arrangement not explicitly stated", "Bellevue, WA", nil},
		{"", "Hybrid - Miami", []string{"hybrid"}},
		{"", "Redmond, WA; onsite", []string{"office"}},
		{"", "Miami, FL - work arrangement not specified", nil},
		{"Check exact arrangement for Florida; employer careers search lists FL", "Remote - Florida", []string{"remote"}},
		{"", "", nil},
	} {
		if got := classifyWorkplace(c.arrangement, c.location); !reflect.DeepEqual(got, c.want) {
			t.Errorf("classifyWorkplace(%q, %q) = %v, want %v", c.arrangement, c.location, got, c.want)
		}
	}
}

func TestClassifyJobType(t *testing.T) {
	for _, c := range []struct {
		employment, notes string
		want              []string
	}{
		{"Full-time", "", []string{"full-time"}},
		{"Regular", "", []string{"full-time"}},
		{"Contract", "", []string{"contract"}},
		{"Independent contractor", "", []string{"contract"}},
		{"Full-time individual contractor or B2B; PTO included", "", []string{"full-time", "contract"}},
		{"Full-time fixed-term (12-14 months) or contract", "", []string{"full-time", "contract"}},
		{"", "Six-month contract to start", []string{"contract"}},
		{"Full-time", "Not a contract role", []string{"full-time"}},
		{"", "", nil},
	} {
		if got := classifyJobType(c.employment, c.notes); !reflect.DeepEqual(got, c.want) {
			t.Errorf("classifyJobType(%q, %q) = %v, want %v", c.employment, c.notes, got, c.want)
		}
	}
}

func TestWorkplaceAndJobTypeFilters(t *testing.T) {
	resetJobs(t,
		`INSERT INTO jobs (id, company, title, url, status, work_arrangement, location, employment_type) VALUES (1, 'A', 'Remote salaried', 'https://example.com/1', 'Research', 'Remote', 'Remote - US', 'Full-time')`,
		`INSERT INTO jobs (id, company, title, url, status, work_arrangement, location, employment_type) VALUES (2, 'B', 'Remote contract', 'https://example.com/2', 'Research', 'Remote', 'Remote - US', 'Contract')`,
		`INSERT INTO jobs (id, company, title, url, status, work_arrangement, location, employment_type) VALUES (3, 'C', 'Hybrid salaried', 'https://example.com/3', 'Research', '', 'Hybrid - Miami', 'Full-time')`,
		`INSERT INTO jobs (id, company, title, url, status, work_arrangement, location, employment_type) VALUES (4, 'D', 'Office salaried', 'https://example.com/4', 'Research', 'Onsite', 'Fort Lauderdale', 'Regular')`,
		`INSERT INTO jobs (id, company, title, url, status, work_arrangement, location, employment_type) VALUES (5, 'E', 'Unknown place', 'https://example.com/5', 'Research', '', 'Miami, FL - work arrangement not specified', '')`,
	)
	h := jobsHandler(testDB)
	for _, c := range []struct {
		query   string
		want    []string
		pushURL string
	}{
		{"/?workplace=remote", []string{"Remote salaried", "Remote contract"}, "/?workplace=remote"},
		{"/?workplace=remote&workplace=hybrid", []string{"Remote salaried", "Remote contract", "Hybrid salaried"}, "/?workplace=remote&workplace=hybrid"},
		{"/?type=contract", []string{"Remote contract"}, "/?type=contract"},
		{"/?workplace=hybrid&workplace=office&type=full-time", []string{"Hybrid salaried", "Office salaried"}, "/?type=full-time&workplace=hybrid&workplace=office"},
		{"/?workplace=none", []string{"Unknown place"}, "/?workplace=none"},
		{"/?type=none&type=bogus", []string{"Unknown place"}, "/?type=none"},
		{"/?workplace=office&type=contract", nil, "/?type=contract&workplace=office"},
	} {
		w := jobsRequest(h, c.query, true)
		body := w.Body.String()
		if w.Code != 200 {
			t.Fatalf("%s: status %d", c.query, w.Code)
		}
		if got := w.Header().Get("Push-Url"); got != c.pushURL {
			t.Errorf("%s: Push-Url = %q, want %q", c.query, got, c.pushURL)
		}
		for _, title := range []string{"Remote salaried", "Remote contract", "Hybrid salaried", "Office salaried", "Unknown place"} {
			want := false
			for _, x := range c.want {
				want = want || x == title
			}
			if got := strings.Contains(body, ">"+title+"</a>"); got != want {
				t.Errorf("%s: listed %q = %v, want %v", c.query, title, got, want)
			}
		}
		if len(c.want) == 0 && !strings.Contains(body, "No jobs found") {
			t.Errorf("%s: expected the empty state", c.query)
		}
	}
	body := jobsRequest(h, "/?workplace=remote&type=contract", false).Body.String()
	for _, want := range []string{
		`<legend>Workplace</legend>`, `<legend>Job type</legend>`,
		`name="workplace" value="remote" checked`, `name="workplace" value="hybrid" >`,
		`name="type" value="contract" checked`, `<span>Not stated</span>`,
		`Clear 2 filters`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("full page missing %q", want)
		}
	}
	if strings.Contains(body, `value="part-time"`) {
		t.Error("Part-time toggle shown although no saved job is part-time")
	}
}
