package main

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// The Applications page can be narrowed and reordered: a Show choice picks
// one of the page's groups, a search matches company, title, contact or
// notes, a stage filter keeps one stage, and a sort orders each group. The
// address holds the view, so Back from an application returns to it.

type AppFilter struct {
	Q, Show, Stage, Sort string
}

var appShows = []NextStage{{Key: "", Label: "All"}, {Key: "due", Label: "Follow up now"}, {Key: "active", Label: "In progress"}, {Key: "later", Label: "Not applied yet"}, {Key: "closed", Label: "Closed"}}

type AppSort struct{ Key, Label string }

var appSorts = []AppSort{
	{"", "Needs attention"},
	{"activity", "Latest activity"},
	{"applied", "Applied, newest first"},
	{"score", "Best match"},
	{"stage", "Furthest stage"},
	{"company", "Company, A to Z"},
}

func parseAppFilter(q url.Values) AppFilter {
	f := AppFilter{Q: strings.TrimSpace(q.Get("q"))}
	if len(f.Q) > 200 {
		f.Q = f.Q[:200]
	}
	for _, s := range appShows {
		if s.Key == q.Get("show") {
			f.Show = s.Key
		}
	}
	for _, s := range statusOrder {
		if s == q.Get("stage") {
			f.Stage = s
		}
	}
	for _, s := range appSorts {
		if s.Key == q.Get("sort") {
			f.Sort = s.Key
		}
	}
	return f
}

// URL is the page address for the filter; defaults are left out.
func (f AppFilter) URL() string {
	v := url.Values{}
	for k, s := range map[string]string{"q": f.Q, "show": f.Show, "stage": f.Stage, "sort": f.Sort} {
		if s != "" {
			v.Set(k, s)
		}
	}
	if len(v) == 0 {
		return "/tracker"
	}
	return "/tracker?" + v.Encode()
}

// Narrowed says whether anything hides applications (sorting hides none).
func (f AppFilter) Narrowed() bool { return f.Q != "" || f.Show != "" || f.Stage != "" }

func (f AppFilter) matches(j TrackerJob) bool {
	if f.Stage != "" && j.Status != f.Stage {
		return false
	}
	if f.Q == "" {
		return true
	}
	q := strings.ToLower(f.Q)
	for _, s := range []string{j.Company, j.Title, j.Contact, j.Notes} {
		if strings.Contains(strings.ToLower(s), q) {
			return true
		}
	}
	return false
}

// sortApps orders one group. "Needs attention" keeps the database order:
// soonest follow-up first, then company.
func sortApps(jobs []TrackerJob, key string) {
	stageRank := map[string]int{}
	for i, s := range statusOrder {
		stageRank[s] = i
	}
	score := func(j TrackerJob) int {
		n, err := strconv.Atoi(strings.TrimSpace(j.Score))
		if err != nil {
			return -1
		}
		return n
	}
	// Blank dates go last in a newest-first sort.
	newest := func(a, b string) (bool, bool) {
		if (a == "") != (b == "") {
			return a != "", true
		}
		if a != b {
			return a > b, true
		}
		return false, false
	}
	sort.SliceStable(jobs, func(i, k int) bool {
		a, b := jobs[i], jobs[k]
		switch key {
		case "activity":
			if less, ok := newest(a.LastActivity, b.LastActivity); ok {
				return less
			}
		case "applied":
			if less, ok := newest(a.Applied, b.Applied); ok {
				return less
			}
		case "score":
			if score(a) != score(b) {
				return score(a) > score(b)
			}
		case "stage":
			if stageRank[a.Status] != stageRank[b.Status] {
				return stageRank[a.Status] > stageRank[b.Status]
			}
		case "company":
			if c := strings.Compare(strings.ToLower(a.Company), strings.ToLower(b.Company)); c != 0 {
				return c < 0
			}
			return strings.ToLower(a.Title) < strings.ToLower(b.Title)
		default:
			return false
		}
		return strings.ToLower(a.Company) < strings.ToLower(b.Company)
	})
}
