package main

import (
	"database/sql"
	"net/url"
	"strings"
)

// Workplace and job-type categories. The saved fields are free text written by
// research ("Hybrid, 60-80% onsite", "Remote / Hybrid; Bay Area office 5 days",
// "Full-time individual contractor or B2B"), so the list filters, the row
// badges and the URL all use these fixed categories instead of the raw text.
const (
	notStated = "none"
)

// ChoiceGroup is one labelled set of toggles in the finder.
type ChoiceGroup struct {
	Legend, Name string
	Choices      []Choice
}

// Choice is one toggle in a filter group.
type Choice struct {
	Value, Label string
	On           bool
}

var workplaceChoices = []Choice{{Value: "remote", Label: "Remote"}, {Value: "hybrid", Label: "Hybrid"}, {Value: "office", Label: "In-office"}, {Value: notStated, Label: "Not stated"}}
var jobTypeChoices = []Choice{{Value: "full-time", Label: "Full-time"}, {Value: "part-time", Label: "Part-time"}, {Value: "contract", Label: "Contract"}, {Value: notStated, Label: "Not stated"}}

var workplaceWords = []struct{ word, value string }{
	{"remote", "remote"}, {"hybrid", "hybrid"},
	{"on-site", "office"}, {"onsite", "office"}, {"on site", "office"}, {"in-office", "office"}, {"in office", "office"}, {"office", "office"},
}

// uncertain marks text that says the arrangement is unknown, such as
// "work arrangement not specified" or "verify exact work arrangement".
var uncertain = []string{"not specified", "not stated", "not confirmed", "not explicitly", "not listed", "unclear", "unknown", "to confirm", "verify", "check exact", "tbd"}

// firstWorkplace returns the category of the earliest workplace word in s.
func firstWorkplace(s string) string {
	best, at := "", -1
	for _, w := range workplaceWords {
		if i := strings.Index(s, w.word); i >= 0 && (at < 0 || i < at) {
			best, at = w.value, i
		}
	}
	return best
}

func isUncertain(s string) bool {
	for _, u := range uncertain {
		if strings.Contains(s, u) {
			return true
		}
	}
	return false
}

// classifyWorkplace reads the arrangement's lead clause (before ';') and treats
// "A / B" as alternatives, so "Remote / Hybrid" is both and "Hybrid, 60-80%
// onsite" is hybrid. When the arrangement names none, each ';'-separated part of
// the location is read the same way, skipping parts that say it is unknown.
func classifyWorkplace(arrangement, location string) []string {
	add := func(out []string, v string) []string {
		if v == "" {
			return out
		}
		for _, x := range out {
			if x == v {
				return out
			}
		}
		return append(out, v)
	}
	var out []string
	a := strings.ToLower(arrangement)
	lead, _, _ := strings.Cut(a, ";")
	if !isUncertain(lead) {
		for _, alt := range strings.Split(lead, "/") {
			out = add(out, firstWorkplace(alt))
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, part := range strings.Split(strings.ToLower(location), ";") {
		if !isUncertain(part) {
			for _, alt := range strings.Split(part, "/") {
				out = add(out, firstWorkplace(alt))
			}
		}
	}
	return out
}

// classifyJobType reads the employment type: contract (including contractor and
// B2B), full-time (including "regular" and "permanent") and part-time. A role
// can be more than one, as in "Full-time fixed-term or contract". With no
// employment type saved, notes that mention a contract count as contract.
func classifyJobType(employment, notes string) []string {
	e := strings.ToLower(employment)
	var out []string
	if strings.Contains(e, "full-time") || strings.Contains(e, "full time") || strings.Contains(e, "regular") || strings.Contains(e, "permanent") {
		out = append(out, "full-time")
	}
	if strings.Contains(e, "part-time") || strings.Contains(e, "part time") {
		out = append(out, "part-time")
	}
	if strings.Contains(e, "contract") || strings.Contains(e, "b2b") || strings.Contains(e, "freelance") || (strings.TrimSpace(e) == "" && strings.Contains(strings.ToLower(notes), "contract")) {
		out = append(out, "contract")
	}
	return out
}

func choiceLabel(choices []Choice, value string) string {
	for _, c := range choices {
		if c.Value == value {
			return c.Label
		}
	}
	return ""
}

// pickChoices keeps the known values from a repeated query parameter, in the
// list's own order, with no duplicates.
func pickChoices(choices []Choice, got []string) []string {
	var out []string
	for _, c := range choices {
		for _, g := range got {
			if g == c.Value {
				out = append(out, c.Value)
				break
			}
		}
	}
	return out
}

// matchesAny is true when nothing is selected, or one of the job's categories
// (or "none" when it has no category) is selected.
func matchesAny(selected, has []string) bool {
	if len(selected) == 0 {
		return true
	}
	for _, s := range selected {
		if s == notStated && len(has) == 0 {
			return true
		}
		for _, h := range has {
			if s == h {
				return true
			}
		}
	}
	return false
}

// categoryFilter classifies every job once, returns the ids that pass the
// selected workplace and job-type toggles, and the toggles to show: every
// category that at least one saved job has, marked on when selected.
func categoryFilter(db *sql.DB, workplace, jobType []string) (ids []any, places, types []Choice, err error) {
	rows, err := db.Query("SELECT id, coalesce(work_arrangement,''), coalesce(location,''), coalesce(employment_type,''), coalesce(notes,'') FROM jobs")
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	seenPlace, seenType := map[string]bool{}, map[string]bool{}
	for rows.Next() {
		var id int64
		var arrangement, location, employment, notes string
		if err = rows.Scan(&id, &arrangement, &location, &employment, &notes); err != nil {
			return nil, nil, nil, err
		}
		wp, jt := classifyWorkplace(arrangement, location), classifyJobType(employment, notes)
		for _, v := range wp {
			seenPlace[v] = true
		}
		for _, v := range jt {
			seenType[v] = true
		}
		if len(wp) == 0 {
			seenPlace[notStated] = true
		}
		if len(jt) == 0 {
			seenType[notStated] = true
		}
		if matchesAny(workplace, wp) && matchesAny(jobType, jt) {
			ids = append(ids, id)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	mark := func(choices []Choice, seen map[string]bool, selected []string) []Choice {
		var out []Choice
		for _, c := range choices {
			on := false
			for _, s := range selected {
				on = on || s == c.Value
			}
			if seen[c.Value] || on {
				c.On = on
				out = append(out, c)
			}
		}
		return out
	}
	return ids, mark(workplaceChoices, seenPlace, workplace), mark(jobTypeChoices, seenType, jobType), nil
}

// setChoices writes a repeated parameter in list order.
func setChoices(values url.Values, key string, selected []string) {
	for _, s := range selected {
		values.Add(key, s)
	}
}
