package main

const nextupHTML = `
{{define "nextup"}}{{template "top" .Nav}}
<div class="page-head has-tile"><span class="tile" aria-hidden="true">{{icon "next"}}</span><h1>Next up</h1><p class="lede">Jobs you have not applied to yet, one at a time, best match first. A job leaves this list once you apply to it or mark it not interested.</p></div>
<div id="next-view">{{template "nextview" .}}</div>
{{template "bottom"}}{{end}}

{{define "nextgroup"}}<section class="group{{if eq .Title "Check before applying"}} check{{else if eq .Title "Notes and record"}} record{{end}}"><h3>{{if eq .Title "Check before applying"}}{{icon "alert"}}{{else if eq .Title "Why it may fit"}}{{icon "check"}}{{else if eq .Title "The role"}}{{icon "jobs"}}{{else}}{{icon "note"}}{{end}}{{.Title}}</h3><dl>{{range .Items}}<div><dt>{{.Label}}</dt><dd>{{.Value}}</dd></div>{{end}}</dl></section>{{end}}

{{define "nextpager"}}<nav class="nextbar{{if .Top}} top{{end}}" aria-label="{{.Label}}">
{{with .P}}{{if .PrevURL}}<a class="btn secondary" href="{{.PrevURL}}" get="{{.PrevURL}}" rel="prev">{{icon "left"}}Previous</a>{{else}}<span class="btn secondary is-off" role="link" aria-disabled="true">{{icon "left"}}Previous</span>{{end}}
<p class="next-pos"><strong>{{.N}}</strong> of {{.Total}}</p>
{{if .NextURL}}<a class="btn primary" href="{{.NextURL}}" get="{{.NextURL}}" rel="next">Next{{icon "right"}}</a>{{else}}<a class="btn secondary" href="{{.FirstURL}}" get="{{.FirstURL}}">Back to the first</a>{{end}}{{end}}
</nav>{{end}}

{{define "nextview"}}{{with .Notice}}<div class="saved next-notice" id="saved" role="status" tabindex="-1"{{if $.Fragment}} autofocus{{end}}><p>{{.Text}}</p>{{if .UndoID}}<form method="post" action="/next/save" post="/next/save"><input type="hidden" name="action" value="restore"><input type="hidden" name="job_id" value="{{.UndoID}}"><button class="btn secondary">Undo<span class="sr">: put it back in Next up</span></button></form>{{end}}</div>{{end}}
{{if not .Total}}<div class="empty"><span class="empty-glyph" aria-hidden="true">{{icon "check"}}</span><h2{{if and .Fragment (not .Notice)}} tabindex="-1" autofocus{{end}}>Nothing left to apply to</h2><p>Every saved job has been applied to, closed or marked not interested. Add new leads, or check Applications for follow-ups.</p><a class="btn primary" href="/tracker">{{icon "apps"}}Open applications</a></div>
{{else}}{{$p := .}}{{with .Job}}
{{template "nextpager" (dict2 "Label" "Previous and next job" "P" $p "Top" true)}}
<article class="panel nextcard" aria-labelledby="next-title">
<div class="next-head">
<div class="job-score{{if .HasScore}} tone-{{scoreTone .ScoreN}}{{end}}">{{if .HasScore}}<strong class="score-num" aria-hidden="true">{{.ScoreN}}</strong><span class="score-label" aria-hidden="true">{{scoreWord .ScoreN}}<span class="lw"> match</span></span>{{template "scorebar" .}}{{if .Weak}}<span class="weak">Weakest: {{.Weak}}</span>{{end}}{{else}}<span class="score-none">Not scored</span>{{end}}</div>
<div class="next-main">
<p class="job-company">{{.Company}}{{if ne .Industry "Unknown"}} <span class="market">{{.Industry}}</span>{{end}}</p>
<h2 id="next-title"{{if and $p.Fragment (not $p.Notice)}} tabindex="-1" autofocus{{end}}><a href="/track?id={{.ID}}&back={{$p.Self}}">{{.Title}}</a></h2>
<ul class="flags">{{if .Priority}}<li class="flag dot tone-{{priorityTone .Priority}}">{{.Priority}} priority</li>{{end}}{{if .AppStatus}}<li class="flag app tone-{{statusTone .AppStatus}}">{{icon "apps"}}Tracking: {{.AppStatus}}</li>{{else}}<li class="flag">{{icon "bookmark"}}Not tracked yet</li>{{end}}{{if .NeedsCheck}}<li class="flag tone-wait">{{icon "alert"}}Posting not confirmed open</li>{{end}}</ul>
<ul class="facts">
<li><span class="k">{{icon "pin"}}Location</span> {{if .Location}}{{.Location}}{{else}}Not listed{{end}}</li>
<li><span class="k">{{icon "pay"}}Pay</span> {{.Salary}}</li>
{{if .WorkType}}<li><span class="k">{{icon "clock"}}Type</span> {{.WorkType}}</li>{{end}}
<li><span class="k">{{icon "calendar"}}Listing posted</span> {{if .Posted}}{{.Posted}}{{else}}Not known{{end}}</li>
{{if .Added}}<li><span class="k">{{icon "plus"}}Added</span> {{.Added}}</li>{{end}}
</ul>
<div class="next-actions">
<a class="btn primary" href="/track?id={{.ID}}&back={{$p.Self}}">{{icon "apps"}}{{if .AppStatus}}Open application{{else}}Review and track{{end}}<span class="sr">: {{.Title}} at {{.Company}}</span></a>
{{with safeURL .URL}}<a class="btn secondary" href="{{.}}" target="_blank" rel="noopener noreferrer">View posting{{icon "external"}}<span class="sr"> (opens in a new tab)</span></a>{{end}}
<form method="post" action="/next/save" post="/next/save" class="dismiss"><input type="hidden" name="action" value="dismiss"><input type="hidden" name="job_id" value="{{.ID}}"><input type="hidden" name="n" value="{{$p.N}}"><button class="btn quiet">{{icon "dismiss"}}Not interested<span class="sr">: remove {{.Title}} from Next up</span></button></form>
</div>
</div>
</div>
<div class="next-body">
{{range .Groups}}{{if (or (eq .Title "Check before applying") (eq .Title "Why it may fit"))}}{{template "nextgroup" .}}{{end}}{{end}}
{{if .HasScore}}<section class="group next-score"><h3>{{icon "target"}}Why it scored {{.ScoreN}}</h3>{{template "scorebreakdown" .}}</section>{{end}}
{{range .Groups}}{{if not (or (eq .Title "Check before applying") (eq .Title "Why it may fit"))}}{{template "nextgroup" .}}{{end}}{{end}}
{{template "researchAside" .}}
</div>
</article>
{{with $p.After}}<p class="next-after"><span class="meta">After this:</span> <a href="{{$p.NextURL}}" get="{{$p.NextURL}}">{{.Title}}</a> <span class="meta">at {{.Company}}{{if .HasScore}}, {{.ScoreN}} {{scoreWord .ScoreN}} match{{end}}</span></p>{{end}}
{{template "nextpager" (dict2 "Label" "Previous and next job, end of page" "P" $p)}}
{{end}}{{end}}{{end}}
`
