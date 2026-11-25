package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"math"
	"net/url"
	"strings"
	"time"
)

// Nav is the shared top bar: which tab is current and the counts shown on it.
type Nav struct {
	Active, Title string
	Jobs, Apps    int
}

func loadNav(db *sql.DB, active, title string) Nav {
	n := Nav{Active: active, Title: title}
	db.QueryRow("SELECT COUNT(*) FROM jobs").Scan(&n.Jobs)
	db.QueryRow("SELECT COUNT(*) FROM applications").Scan(&n.Apps)
	return n
}

// safeBack accepts only same-site list and tracker paths for "Back" links, so
// a crafted link cannot send the browser to another site after a save.
func safeBack(s string) string {
	if s == "/" || s == "/tracker" || s == "/resume" || s == "/profile" || s == "/next" ||
		strings.HasPrefix(s, "/?") || strings.HasPrefix(s, "/track?id=") || strings.HasPrefix(s, "/next?n=") {
		if !strings.ContainsAny(s, "\\\r\n") {
			return s
		}
	}
	return ""
}

func parseWhen(s string) (time.Time, bool, bool) {
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, strings.TrimSpace(s), time.Local); err == nil {
			return t, layout != "2006-01-02", true
		}
	}
	return time.Time{}, false, false
}

// fmtDate renders "2026-11-24" or "2026-11-24T18:30" as "Tue 24 Nov 2026" or
// "Tue 24 Nov 2026, 6:30 pm". Unparseable text is shown as stored.
func fmtDate(s string) string {
	t, hasTime, ok := parseWhen(s)
	if !ok {
		return s
	}
	out := t.Format("Mon 2 Jan 2006")
	if hasTime {
		out += ", " + strings.ToLower(t.Format("3:04 PM"))
	}
	return out
}

// dateAge is a date with how long ago it was, for the dates that say how
// fresh a listing is: "Mon 10 Nov 2026 (14 days ago)". Empty stays empty.
func dateAge(s string) string {
	t, _, ok := parseWhen(s)
	if !ok {
		return s
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
	out := t.Format("Mon 2 Jan 2006")
	switch days := int(today.Sub(day).Hours() / 24); {
	case days == 0:
		return out + " (today)"
	case days == 1:
		return out + " (yesterday)"
	case days > 1:
		return fmt.Sprintf("%s (%d days ago)", out, days)
	}
	return out
}

// fmtShort is the compact form for lists: "24 Nov" or "24 Nov, 6:30 pm",
// adding the year only when it is not this year.
func fmtShort(s string) string {
	t, hasTime, ok := parseWhen(s)
	if !ok {
		return s
	}
	layout := "2 Jan"
	if t.Year() != time.Now().Year() {
		layout = "2 Jan 2006"
	}
	out := t.Format(layout)
	if hasTime {
		out += ", " + strings.ToLower(t.Format("3:04 PM"))
	}
	return out
}

// relDay describes a date relative to today: "today", "tomorrow", "in 3 days",
// "yesterday", "2 days ago".
func relDay(s string) string {
	t, _, ok := parseWhen(s)
	if !ok {
		return ""
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
	d := int(math.Round(day.Sub(today).Hours() / 24))
	switch {
	case d == 0:
		return "today"
	case d == 1:
		return "tomorrow"
	case d == -1:
		return "yesterday"
	case d > 1:
		return fmt.Sprintf("in %d days", d)
	default:
		return fmt.Sprintf("%d days ago", -d)
	}
}

var pageFuncs = template.FuncMap{
	"officialWebsite": officialWebsite,
	"researchURL": func(company, destination string) string {
		query := company
		switch destination {
		case "website":
			query += " official website"
		case "wikipedia":
			query += " site:en.wikipedia.org"
		case "glassdoor":
			query += " site:glassdoor.com company reviews"
		case "linkedin":
			query += " site:linkedin.com/company"
		case "careers":
			query += " company careers"
		}
		return "https://www.google.com/search?q=" + url.QueryEscape(query)
	},
	"safeURL": func(s string) string {
		u, e := url.Parse(s)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return ""
		}
		return s
	},
	// dict passes the page and one record to a shared form template.
	"dict": func(p, r any) map[string]any { return map[string]any{"p": p, "r": r} },
	// dict2 builds a map from name/value pairs for templates that take several values.
	"dict2": func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			if k, ok := kv[i].(string); ok {
				m[k] = kv[i+1]
			}
		}
		return m
	},
	"fmtDate":      fmtDate,
	"list":         func(s ...string) []string { return s },
	"group":        func(legend, name string, c []Choice) ChoiceGroup { return ChoiceGroup{legend, name, c} },
	"join":         strings.Join,
	"linkFor":      linkFor,
	"splitLines":   splitLines,
	"splitItems":   splitItems,
	"dateRange":    dateRange,
	"tenure":       tenure,
	"sectionNoun":  sectionNoun,
	"fmtShort":     fmtShort,
	"icon":         icon,
	"statusTone":   statusTone,
	"priorityTone": priorityTone,
	"groupTone":    groupTone,
	"scoreTone":    scoreTone,
	"scoreWord":    scoreWord,
	"followTone":   followTone,
	"groupIcon":    groupIcon,
	"contactIcon":  contactIcon,
	"iconSprite":   func() template.HTML { return iconSprite },
	"relDay":       relDay,
	"plural": func(n int, one, many string) string {
		if n == 1 {
			return one
		}
		return many
	},
}

var pages = template.Must(template.New("pages").Funcs(pageFuncs).Parse(layoutHTML + jobsHTML + trackerHTML + profileHTML + nextupHTML + metricsHTML))

const layoutHTML = `
{{define "top"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} · Job search</title><link rel="icon" href="data:,"><link rel="stylesheet" href="/static/style.css"><script type="module" src="/static/htmlex/main.js"></script></head>
<body>{{iconSprite}}<a class="skip" href="#main">Skip to content</a>
<header class="topbar"><div class="topbar-in">
<a class="wordmark" href="/"><span class="mark" aria-hidden="true">{{icon "search"}}</span>Job search</a>
<nav class="tabs" id="tabs" aria-label="Sections">{{template "tabs" .}}</nav>
</div></header>
<main id="main" class="page"><div id="flash" class="flash"></div>{{end}}

{{define "tabs"}}<a href="/" {{if eq .Active "jobs"}}aria-current="page"{{end}}>{{icon "jobs"}}<span class="tab-label">Jobs</span>{{if eq .Active "jobs"}}<span class="tab-mark" aria-hidden="true"></span>{{end}}</a>
<a href="/next" {{if eq .Active "next"}}aria-current="page"{{end}}>{{icon "next"}}<span class="tab-label">Next up</span>{{if eq .Active "next"}}<span class="tab-mark" aria-hidden="true"></span>{{end}}</a>
<a href="/tracker" {{if eq .Active "apps"}}aria-current="page"{{end}}>{{icon "apps"}}<span class="tab-label">Applications <span class="count" aria-hidden="true">{{.Apps}}</span></span><span class="sr"> ({{.Apps}} tracked)</span>{{if eq .Active "apps"}}<span class="tab-mark" aria-hidden="true"></span>{{end}}</a>
<a href="/profile" {{if eq .Active "profile"}}aria-current="page"{{end}}>{{icon "profile"}}<span class="tab-label">Profile</span>{{if eq .Active "profile"}}<span class="tab-mark" aria-hidden="true"></span>{{end}}</a>
<a href="/resume" {{if eq .Active "resume"}}aria-current="page"{{end}}>{{icon "resume"}}<span class="tab-label">Résumé</span>{{if eq .Active "resume"}}<span class="tab-mark" aria-hidden="true"></span>{{end}}</a>
<a href="/metrics" {{if eq .Active "metrics"}}aria-current="page"{{end}}>{{icon "metrics"}}<span class="tab-label">Metrics</span>{{if eq .Active "metrics"}}<span class="tab-mark" aria-hidden="true"></span>{{end}}</a>{{end}}

{{define "bottom"}}</main></body></html>{{end}}

{{define "problem"}}{{template "top" .Nav}}
<div class="page-head has-tile"><span class="tile tone-bad" aria-hidden="true">{{icon "alert"}}</span><h1>Could not save</h1><p class="lede">{{.Message}}</p></div>
<div class="panel"><p>Nothing was saved. Your browser's Back button usually keeps what you typed.</p>
<div class="form-actions side-action"><a class="btn primary" href="{{.FixURL}}">{{.FixLabel}}</a></div></div>
{{template "bottom"}}{{end}}

{{define "scorebar"}}<div class="scorebar" role="img" aria-label="Match {{.ScoreN}} out of 100, {{scoreWord .ScoreN}} match: {{range $i, $p := .Parts}}{{if $i}}, {{end}}{{$p.Label}} {{$p.Got}} of {{$p.Max}}{{end}}">{{range .Parts}}<span class="seg" style="flex-grow:{{.Max}}"><span class="fill" style="width:{{.Pct}}%"></span></span>{{end}}</div>{{end}}

{{define "scorebreakdown"}}<div class="breakdown">
<table class="parts"><caption class="sr">Score by component</caption><tbody>
{{range .Parts}}<tr><th scope="row">{{.Label}}</th><td class="partbar" aria-hidden="true"><span class="seg" style="width:100%"><span class="fill" style="width:{{.Pct}}%"></span></span></td><td class="num">{{.Got}} <span class="of">of {{.Max}}</span></td></tr>{{end}}
</tbody></table>
{{if .ScoreNote}}<p class="score-note">{{.ScoreNote}}</p>{{end}}
{{if .ScoreMeta}}<p class="meta">{{.ScoreMeta}}</p>{{end}}
</div>{{end}}
`

const jobsHTML = `
{{define "jobsview"}}<form action="/" method="get" get="/" class="finder" role="search">
<div class="finder-search"><label for="q">Search</label><input id="q" type="search" name="q" value="{{.Q}}" placeholder="Title, company or skill"></div>
<details class="refine" {{if .RefineCount}}open{{end}}><summary>Filters and sort{{if .RefineCount}} <span class="count">{{.RefineCount}} on</span>{{end}}</summary>
<div class="finder-filters">
<label>Status<select name="status"><option value="">Any status</option>{{range .Statuses}}<option value="{{.}}" {{if eq . $.Status}}selected{{end}}>{{.}}</option>{{end}}</select></label>
<label>Priority<select name="priority"><option value="">Any priority</option>{{range .Priorities}}<option value="{{.}}" {{if eq . $.Priority}}selected{{end}}>{{.}}</option>{{end}}</select></label>
<label>Company market<select name="industry"><option value="">Any market</option>{{range .Industries}}<option value="{{.}}" {{if eq . $.Industry}}selected{{end}}>{{.}}</option>{{end}}</select></label>
<label>Sort by<select name="sort">{{range .SortOptions}}<option value="{{.Value}}" {{if eq .Value $.Sort}}selected{{end}}>{{.Menu}}</option>{{end}}</select></label>
</div>
</details>
<div class="finder-chips">{{template "choices" (group "Workplace" "workplace" .Workplaces)}}{{template "choices" (group "Job type" "type" .JobTypes)}}</div>
<div class="finder-actions"><button type="submit" class="btn primary">{{icon "search"}}Show jobs</button></div>
</form>
<section id="results" aria-labelledby="results-title">{{template "results" .}}</section>{{end}}

{{define "choices"}}{{$name := .Name}}{{with .Choices}}<fieldset class="choices"><legend>{{$.Legend}}</legend><div class="choice-row">{{range .}}<label class="choice"><input type="checkbox" name="{{$name}}" value="{{.Value}}" {{if .On}}checked{{end}}><span>{{.Label}}</span></label>{{end}}</div></fieldset>{{end}}{{end}}

{{define "page"}}{{template "top" .Nav}}
<div class="page-head has-tile"><span class="tile" aria-hidden="true">{{icon "jobs"}}</span><h1>Jobs</h1><p class="lede">{{.Nav.Jobs}} saved {{plural .Nav.Jobs "lead" "leads"}}. Open one to see why it scored that way and to track it.</p></div>
<div id="jobs-view">{{template "jobsview" .}}</div>
{{template "bottom"}}{{end}}

{{define "results"}}<div class="results-head"><h2 id="results-title"{{if .Fragment}} tabindex="-1" autofocus{{end}}>{{if .Count}}{{.Start}}–{{.End}} of {{.Count}} {{plural .Count "job" "jobs"}}{{else}}No jobs found{{end}}</h2><p class="meta">{{if .Count}}Sorted by {{.SortLabel}}.{{end}}{{if .FilterCount}} <a href="/" get="/">Clear {{.FilterCount}} {{plural .FilterCount "filter" "filters"}}</a>{{end}}</p></div>
{{if .Jobs}}<ol class="joblist">{{range .Jobs}}{{$t := .Title}}<li class="job">
<div class="job-score{{if .HasScore}} tone-{{scoreTone .ScoreN}}{{end}}">{{if .HasScore}}<strong class="score-num" aria-hidden="true" style="view-transition-name:score-{{.ID}}">{{.ScoreN}}</strong><span class="score-label" aria-hidden="true">{{scoreWord .ScoreN}}<span class="lw"> match</span></span>{{template "scorebar" .}}{{if .Weak}}<span class="weak">Weakest: {{.Weak}}</span>{{end}}{{else}}<span class="score-none">Not scored</span>{{end}}</div>
<div class="job-main">
<h3 style="view-transition-name:job-{{.ID}}"><a href="/track?id={{.ID}}&back={{$.ListURL}}">{{.Title}}</a></h3>
<p class="job-company">{{.Company}}{{if ne .Industry "Unknown"}} <span class="market">{{.Industry}}</span>{{end}}</p>
<ul class="facts">
<li><span class="k">{{icon "pin"}}Location</span> {{if .Location}}{{.Location}}{{else}}Not listed{{end}}</li>
<li><span class="k">{{icon "pay"}}Pay</span> {{.Salary}}</li>
{{if .WorkType}}<li><span class="k">{{icon "clock"}}Type</span> {{.WorkType}}</li>{{end}}
</ul>
<ul class="flags">{{if .NeedsCheck}}<li class="flag tone-wait">{{icon "alert"}}Posting not confirmed open</li>{{end}}{{if .Priority}}<li class="flag dot tone-{{priorityTone .Priority}}">{{.Priority}} priority</li>{{end}}{{if .AppStatus}}<li class="flag app tone-{{statusTone .AppStatus}}">{{icon "apps"}}Tracking: {{.AppStatus}}</li>{{end}}{{if .Dismissed}}<li class="flag">{{icon "dismiss"}}Not interested</li>{{end}}</ul>
<details class="more"><summary>Score breakdown and details<span class="sr"> for {{.Title}}</span></summary>
<div class="more-body">{{if .HasScore}}{{template "scorebreakdown" .}}{{end}}
{{range .Groups}}<section class="group{{if eq .Title "Check before applying"}} check{{else if eq .Title "Notes and record"}} record{{end}}"><h4>{{.Title}}</h4><dl>{{range .Items}}<div><dt>{{.Label}}</dt><dd>{{.Value}}</dd></div>{{end}}</dl></section>{{end}}
{{template "research" .}}</div></details>
</div>
<div class="job-actions">
<a class="btn secondary" href="/track?id={{.ID}}&back={{$.ListURL}}">{{icon "apps"}}{{if .AppStatus}}Open application{{else}}Review and track{{end}}<span class="sr">: {{.Title}} at {{.Company}}</span></a>
{{with safeURL .URL}}<a class="btn quiet" href="{{.}}" target="_blank" rel="noopener noreferrer">View posting{{icon "external"}}<span class="sr"> for {{$t}} (opens in a new tab)</span></a>{{end}}
</div>
</li>{{end}}</ol>
{{template "pagination" .}}
{{else}}<div class="empty"><span class="empty-glyph" aria-hidden="true">{{icon "search"}}</span><h3>Nothing matches{{if .Q}} “{{.Q}}”{{end}}.</h3><p>Try a shorter search, or clear the filters to see all {{.Nav.Jobs}} jobs.</p><a class="btn primary" href="/" get="/">{{icon "close"}}Clear search and filters</a></div>{{end}}{{end}}

{{define "research"}}<section class="group research"><h4>{{icon "globe"}}Research the company</h4><ul class="linkrow">
{{with officialWebsite .Company}}<li><a href="{{.}}" target="_blank" rel="noopener noreferrer">Company website<span class="sr"> (opens in a new tab)</span></a></li>{{else}}<li><a href="{{researchURL .Company "website"}}" target="_blank" rel="noopener noreferrer">Find the website<span class="sr"> (opens in a new tab)</span></a></li>{{end}}
<li><a href="{{researchURL .Company "glassdoor"}}" target="_blank" rel="noopener noreferrer">Glassdoor reviews<span class="sr"> (opens in a new tab)</span></a></li>
<li><a href="{{researchURL .Company "linkedin"}}" target="_blank" rel="noopener noreferrer">LinkedIn page<span class="sr"> (opens in a new tab)</span></a></li>
<li><a href="{{researchURL .Company "wikipedia"}}" target="_blank" rel="noopener noreferrer">Wikipedia<span class="sr"> (opens in a new tab)</span></a></li>
<li><a href="{{researchURL .Company "careers"}}" target="_blank" rel="noopener noreferrer">Careers page<span class="sr"> (opens in a new tab)</span></a></li>
</ul><p class="meta">All open in a new tab. Apart from a verified company website, each is a web search, so check the result is the right company.</p></section>{{end}}
{{define "researchAside"}}<section class="group research"><h3>{{icon "globe"}}Research the company</h3><ul class="linkrow">
{{with officialWebsite .Company}}<li><a href="{{.}}" target="_blank" rel="noopener noreferrer">Company website<span class="sr"> (opens in a new tab)</span></a></li>{{else}}<li><a href="{{researchURL .Company "website"}}" target="_blank" rel="noopener noreferrer">Find the website<span class="sr"> (opens in a new tab)</span></a></li>{{end}}
<li><a href="{{researchURL .Company "glassdoor"}}" target="_blank" rel="noopener noreferrer">Glassdoor reviews<span class="sr"> (opens in a new tab)</span></a></li>
<li><a href="{{researchURL .Company "linkedin"}}" target="_blank" rel="noopener noreferrer">LinkedIn page<span class="sr"> (opens in a new tab)</span></a></li>
<li><a href="{{researchURL .Company "wikipedia"}}" target="_blank" rel="noopener noreferrer">Wikipedia<span class="sr"> (opens in a new tab)</span></a></li>
<li><a href="{{researchURL .Company "careers"}}" target="_blank" rel="noopener noreferrer">Careers page<span class="sr"> (opens in a new tab)</span></a></li>
</ul><p class="meta">All open in a new tab. Apart from a verified company website, each is a web search, so check the result is the right company.</p></section>{{end}}

{{define "pagination"}}{{if gt .TotalPages 1}}<nav class="pager" aria-label="Pages of jobs">
{{if .PreviousURL}}<a class="btn secondary" href="{{.PreviousURL}}" get="{{.PreviousURL}}">{{icon "left"}}Previous page</a>{{end}}
<ol>{{range .PageLinks}}<li>{{if .Current}}<span aria-current="page">{{.Number}}</span>{{else}}<a href="{{.URL}}" get="{{.URL}}" aria-label="Page {{.Number}}">{{.Number}}</a>{{end}}</li>{{end}}</ol>
{{if .NextURL}}<a class="btn secondary" href="{{.NextURL}}" get="{{.NextURL}}">Next page{{icon "right"}}</a>{{end}}
</nav>{{end}}{{end}}
`
