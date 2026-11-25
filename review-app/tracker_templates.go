package main

const trackerHTML = `
{{define "saved"}}{{if .SavedText}}<p class="saved" id="saved" tabindex="-1" role="status"{{if .Fragment}} autofocus{{end}}>{{.SavedText}}</p>{{end}}{{end}}

{{define "dashboard"}}{{template "top" .Nav}}
<div class="page-head has-tile"><span class="tile" aria-hidden="true">{{icon "apps"}}</span><h1>Applications</h1><p class="lede">{{if .Total}}{{.Total}} {{plural .Total "job" "jobs"}} you are tracking, with what needs attention first.{{else}}Jobs you decide to pursue land here, with their status, follow-ups and contacts.{{end}}</p></div>
{{if .Total}}<form class="finder app-finder" method="get" action="/tracker" get="/tracker" trigger="input" debounce="250" role="search">
<div class="finder-search"><label for="app-q">Search</label><input id="app-q" type="search" name="q" value="{{.Filter.Q}}" placeholder="Company, title, contact, notes" autocomplete="off"></div>
<div class="finder-filters">
<label>Stage<select name="stage"><option value="">Any stage</option>{{range .Statuses}}<option value="{{.}}"{{if eq . $.Filter.Stage}} selected{{end}}>{{.}}</option>{{end}}</select></label>
<label>Sort by<select name="sort">{{range .Sorts}}<option value="{{.Key}}"{{if eq .Key $.Filter.Sort}} selected{{end}}>{{.Label}}</option>{{end}}</select></label>
</div>
<div class="finder-actions"><button type="submit" class="btn primary">{{icon "search"}}Show applications</button></div>
<fieldset class="choices finder-chips"><legend>Show</legend><div class="choice-row">{{range .Shows}}<label class="choice"><input type="radio" name="show" value="{{.Key}}"{{if .On}} checked{{end}}><span>{{.Label}} <span class="count" id="apps-count-{{showID .Key}}">{{.Count}}</span></span></label>{{end}}</div></fieldset>
</form>{{end}}
<div id="apps-view">{{template "appsview" .}}</div>
{{template "bottom"}}{{end}}

{{define "appsview"}}{{if and .Total .Filter.Narrowed}}<p class="results-line" role="status">Showing {{.Shown}} of {{.Total}} {{plural .Total "application" "applications"}}. <a href="/tracker" get="/tracker">Clear search and filters</a></p>{{end}}
{{if .Groups}}{{range $gi, $g := .Groups}}<section class="appgroup tone-{{groupTone .Title}}" aria-labelledby="g-{{$gi}}"><div class="appgroup-head"><h2 id="g-{{$gi}}">{{groupIcon .Title}}{{.Title}} <span class="count">{{len .Jobs}}</span></h2><p class="meta">{{.Note}}</p></div>
<ol class="applist">{{range .Jobs}}<li class="appitem">
<div class="app-main"><h3 style="view-transition-name:job-{{.ID}}"><a href="/track?id={{.ID}}&back={{$.Self}}">{{.Title}}</a></h3><p class="job-company">{{.Company}}</p></div>
<dl class="app-facts">
<div><dt>Stage</dt><dd><span class="flag dot stage tone-{{statusTone .Status}}">{{.Status}}</span></dd></div>
<div><dt>Follow-up</dt><dd>{{if .Followup}}<span class="flag tone-{{followTone .Followup .Status}}">{{icon "bell"}}{{fmtShort .Followup}}</span> <span class="rel">{{relDay .Followup}}</span>{{else}}None set{{end}}</dd></div>
<div><dt>Applied</dt><dd>{{if .Applied}}{{fmtShort .Applied}}{{else}}Not yet{{end}}</dd></div>
<div><dt>Last activity</dt><dd>{{if .LastActivity}}{{fmtShort .LastActivity}}{{else}}None logged{{end}}</dd></div>
{{if .Contact}}<div class="wide"><dt>Contact</dt><dd>{{.Contact}}</dd></div>{{end}}
</dl>
<a class="btn secondary" href="/track?id={{.ID}}&back={{$.Self}}">{{icon "apps"}}Open application<span class="sr">: {{.Title}} at {{.Company}}</span></a>
</li>{{end}}</ol></section>{{end}}
{{else if .Total}}<div class="empty"><span class="empty-glyph" aria-hidden="true">{{icon "search"}}</span><h2>No applications match</h2><p>Try a shorter search, another stage, or show all {{.Total}} applications.</p><a class="btn primary" href="/tracker" get="/tracker">{{icon "close"}}Clear search and filters</a></div>
{{else}}<div class="empty"><span class="empty-glyph" aria-hidden="true">{{icon "apps"}}</span><h2>No applications yet</h2><p>Find a job worth pursuing, choose <strong>Review and track</strong>, and save its status. It will appear here.</p><a class="btn primary" href="/">{{icon "jobs"}}Browse jobs</a></div>{{end}}{{end}}

{{define "resume"}}{{template "top" .Nav}}
{{if .Back}}<p class="crumb"><a href="{{.Back}}">{{.BackLabel}}</a></p>{{end}}
<div id="resume-view">{{template "resumeview" .}}</div>
{{template "bottom"}}{{end}}

{{define "resumeview"}}<div class="page-head has-tile"><span class="tile" aria-hidden="true">{{icon "resume"}}</span><h1>Base résumé</h1><p class="lede">Paste the text of your current résumé. Each job's résumé match compares against it, and new tailored versions start from it. It stays in the local database.</p></div>
{{template "saved" .}}
<div class="two-col">
<form method="post" action="/save" post="/save" class="panel editor"><input type="hidden" name="kind" value="base"><input type="hidden" name="back" value="{{.Back}}">
<label for="base">Résumé text</label><textarea id="base" name="content" rows="18" spellcheck="true" placeholder="Paste the full text: contact details, summary, experience, skills.">{{.Base}}</textarea>
<div class="form-actions"><button class="btn primary">Save base résumé</button></div></form>
<aside class="panel side"><h2>{{icon "profile"}}Fill it from your profile</h2><p class="meta">Your <a href="/profile">profile</a> holds your experience, skills and projects. Copy it here in one step from the profile page instead of pasting.</p><h2 class="side-next">{{icon "checklist"}}Quick checks</h2><ul class="checks">{{range .ResumeTips}}<li>{{.}}</li>{{end}}</ul><p class="meta">These look only for basic headings, contact details and dates. Read the layout yourself before sending.</p></aside>
</div>{{end}}

{{define "track"}}{{template "top" .Nav}}
<p class="crumb"><a href="{{.Back}}">{{.BackLabel}}</a></p>
<div id="track-view">{{template "trackview" .}}</div>
{{template "bottom"}}{{end}}

{{define "trackview"}}<div class="track-head">
<p class="job-company">{{.Info.Company}}</p>
<h1 style="view-transition-name:job-{{.Info.ID}}">{{.Info.Title}}</h1>
{{if .HasApp}}{{if .Closed}}<p class="closed-note"><span class="flag dot tone-{{statusTone .Job.Status}}">Closed: {{.Job.Status}}</span></p>{{else}}<ol class="stepper tone-{{statusTone .Job.Status}}" aria-label="Application stage">{{range .Steps}}<li class="{{if .Done}}done{{end}}{{if .Current}} current{{end}}" {{if .Current}}aria-current="step"{{end}}><span>{{.Label}}</span>{{if and .Done (not .Current)}}<span class="sr"> (completed)</span>{{end}}</li>{{end}}</ol>{{end}}{{end}}
<ul class="flags">{{if not .HasApp}}<li class="flag">{{icon "bookmark"}}Not tracked yet</li>{{end}}{{if .Job.Followup}}<li class="flag {{if .FollowDue}}tone-bad{{else}}tone-wait{{end}}">{{icon "bell"}}Follow up {{fmtDate .Job.Followup}} ({{relDay .Job.Followup}})</li>{{end}}{{if .Info.NeedsCheck}}<li class="flag tone-wait">{{icon "alert"}}Posting not confirmed open</li>{{end}}{{if .Info.Dismissed}}<li class="flag">{{icon "dismiss"}}Not interested</li>{{end}}</ul>
{{if .Info.Dismissed}}<form method="post" action="/next/save" class="restore"><p class="meta">You marked this job not interested, so it is hidden from Next up. Its status is unchanged.</p><input type="hidden" name="action" value="restore"><input type="hidden" name="job_id" value="{{.Info.ID}}"><button class="btn secondary">{{icon "next"}}Put back in Next up</button></form>{{end}}
<nav class="jump" aria-label="On this page"><a href="#status">Status</a><a href="#activity">Activity</a><a href="#documents">Résumé and cover letter</a><a href="#about">About the job</a></nav>
</div>
<div class="track-grid{{if .HasApp}} tracked{{end}}">
<div class="track-main">

<section class="panel" id="status" aria-labelledby="status-title"><h2 id="status-title">{{icon "flag"}}Where it stands</h2>
{{if eq .Saved "application"}}{{template "saved" .}}{{end}}
<p class="meta">{{if .HasApp}}Update this as things move. Saving never submits anything to the employer.{{else}}Save a status to start tracking this job. Saving never submits anything to the employer.{{end}}</p>
<form method="post" action="/save" post="/save" class="editor"><input type="hidden" name="job_id" value="{{.Job.ID}}"><input type="hidden" name="kind" value="application"><input type="hidden" name="back" value="{{.Back}}">
<div class="grid-2">
<label>Stage<select name="status">{{range .Statuses}}<option {{if eq . $.Job.Status}}selected{{end}}>{{.}}</option>{{end}}</select></label>
<label>Follow up on<input type="date" name="followup" value="{{.Job.Followup}}"></label>
<label>Date applied<input type="date" name="applied" value="{{.Job.Applied}}"></label>
</div>
{{if not .HasApp}}<details class="more-fields"><summary>Add a contact, notes, or how you feel</summary><div class="editor">{{end}}
<label>Contact<input name="contact" value="{{.Job.Contact}}" placeholder="Recruiter or hiring manager"></label>
<label>Notes<textarea name="notes" rows="3">{{.Job.Notes}}</textarea></label>
<fieldset class="feeling"><legend>How do you feel about your chances? <span class="hint">Your own view, separate from the match score.</span></legend>
<div class="grid-2"><label>Feeling<select name="feeling"><option value="" {{if eq .Job.Feeling ""}}selected{{end}}>Not set</option><option {{if eq .Job.Feeling "Low"}}selected{{end}}>Low</option><option {{if eq .Job.Feeling "Medium"}}selected{{end}}>Medium</option><option {{if eq .Job.Feeling "High"}}selected{{end}}>High</option><option {{if eq .Job.Feeling "Unsure"}}selected{{end}}>Unsure</option></select></label>
<label>Why (optional)<input name="feeling_notes" value="{{.Job.FeelingNotes}}"></label></div></fieldset>
{{if not .HasApp}}</div></details>{{end}}
<div class="form-actions"><button class="btn primary">{{if .HasApp}}Save status{{else}}Start tracking{{end}}</button></div>
</form></section>

<section class="panel" id="activity" aria-labelledby="activity-title"><h2 id="activity-title">{{icon "activity"}}Activity</h2>
{{if or (eq .Saved "message") (eq .Saved "interview")}}{{template "saved" .}}{{end}}
<p class="meta">Messages, interviews and status changes in date order. Logging a message here does not send it.</p>
{{if .Timeline}}<ol class="timeline">{{range .Timeline}}<li class="{{if .Upcoming}}upcoming{{end}}"><span class="node" aria-hidden="true">{{if eq .Kind "message"}}{{icon "mail"}}{{else if eq .Kind "interview"}}{{icon "calendar"}}{{else}}{{icon "flag"}}{{end}}</span>
<p class="when">{{.Display}}{{if .Upcoming}} <span class="tag">Upcoming</span>{{end}}{{if .Gap}} <span class="gap">{{.Gap}}</span>{{end}}</p>
<p class="what">{{.Title}}</p>{{if .Detail}}<p class="detail">{{.Detail}}</p>{{end}}
{{if eq .Kind "message"}}<details class="edit"><summary>Edit this message<span class="sr"> from {{.Display}}</span></summary>{{template "messageForm" (dict $ .Rec)}}</details>{{end}}
{{if eq .Kind "interview"}}<details class="edit"><summary>Edit this interview<span class="sr"> on {{.Display}}</span></summary>{{template "interviewForm" (dict $ .Rec)}}</details>{{end}}
</li>{{end}}</ol>{{else}}<p class="empty-inline">Nothing logged yet. Record the first message or interview below.</p>{{end}}
<div class="adders">
<details class="adder"><summary>{{icon "plus"}}Log a message</summary>{{template "messageForm" (dict $ nil)}}</details>
<details class="adder"><summary>{{icon "plus"}}Add an interview</summary>{{template "interviewForm" (dict $ nil)}}</details>
</div></section>

<section class="panel" id="documents" aria-labelledby="docs-title"><h2 id="docs-title">{{icon "resume"}}Résumé and cover letter</h2>
{{if eq .Saved "document"}}{{template "saved" .}}{{end}}
<h3>Résumé match</h3>
{{if not .HasBase}}<p class="meta">No base résumé saved yet. <a href="/resume?back={{.Self}}">Add your base résumé</a> to see which of this job's required skills it covers.</p>
{{else if .MatchNote}}<p class="meta">{{.MatchNote}}</p>
{{else}}<p class="meta">Required skills from this posting, checked as whole words against {{.ResumeTitle}}. Synonyms are missed, so treat gaps as prompts, not verdicts.</p>
<div class="match"><div><h4>In your résumé <span class="count">{{len .Matched}}</span></h4><ul class="chips ok">{{range .Matched}}<li>{{.}}</li>{{else}}<li class="none">None</li>{{end}}</ul></div>
<div><h4>Not found <span class="count">{{len .Missing}}</span></h4><ul class="chips miss">{{range .Missing}}<li>{{.}}</li>{{else}}<li class="none">None</li>{{end}}</ul></div></div>{{end}}
{{if .HasBase}}<p class="meta"><a href="/resume?back={{.Self}}">Edit base résumé</a></p>{{end}}

<h3>Versions for this job</h3>
{{if or .Resumes .Letters}}<ul class="doclist">{{range .Resumes}}<li><details class="edit"><summary><span class="dockind">Résumé</span> {{.B}} <span class="meta">updated {{fmtDate .D}}</span></summary>
<p class="linkrow-inline"><a href="{{$.Self}}&doc={{.ID}}&back={{$.Back}}#documents">Check the match against this version</a> · <a class="dl" href="/resume-file?id={{.ID}}&format=pdf" target="_blank" rel="noopener">{{icon "download"}}PDF for upload<span class="sr"> (opens in a new tab)</span></a> · <a class="dl" href="/resume-file?id={{.ID}}&format=docx" download>{{icon "download"}}Word file</a></p>{{template "docForm" (dict $ .)}}</details></li>{{end}}
{{range .Letters}}<li><details class="edit"><summary><span class="dockind">Cover letter</span> {{.B}} <span class="meta">updated {{fmtDate .D}}</span></summary>{{template "docForm" (dict $ .)}}</details></li>{{end}}</ul>
{{else}}<p class="empty-inline">No tailored versions yet.</p>{{end}}
<div class="adders">
<details class="adder"><summary>{{icon "plus"}}New tailored résumé</summary>
<form method="post" action="/save" post="/save" class="editor"><input type="hidden" name="job_id" value="{{.Job.ID}}"><input type="hidden" name="kind" value="document"><input type="hidden" name="doc_type" value="Resume"><input type="hidden" name="back" value="{{.Back}}">
<p class="meta">Starts from {{.ResumeTitle}}. Change only what you can back up.</p>
<label>Name this version (required)<input name="title" required pattern=".*\S.*" title="Enter a name; it cannot be blank" placeholder="For example: Backend focus, November"></label>
<label>Résumé text<textarea name="content" rows="14">{{.Resume}}</textarea></label>
<div class="form-actions"><button class="btn primary">Save résumé version</button></div></form></details>
<details class="adder"><summary>{{icon "plus"}}New cover letter</summary>
<form method="post" action="/save" post="/save" class="editor"><input type="hidden" name="job_id" value="{{.Job.ID}}"><input type="hidden" name="kind" value="document"><input type="hidden" name="doc_type" value="Cover letter"><input type="hidden" name="back" value="{{.Back}}">
<label>Name this letter (required)<input name="title" required pattern=".*\S.*" title="Enter a name; it cannot be blank" placeholder="For example: First draft"></label>
<label>Letter<textarea name="content" rows="12"></textarea></label>
<div class="form-actions"><button class="btn primary">Save cover letter</button></div></form></details>
</div></section>
</div>

<aside class="track-side" id="about" aria-label="About this job">
<section class="panel"><h2 class="sr">About this job</h2>
{{if .Info.HasScore}}<div class="side-score tone-{{scoreTone .Info.ScoreN}}"><strong class="score-num" aria-hidden="true" style="view-transition-name:score-{{.Info.ID}}">{{.Info.ScoreN}}</strong><span class="score-label" aria-hidden="true">{{scoreWord .Info.ScoreN}}<span class="lw"> match</span></span>{{template "scorebar" .Info}}</div>{{end}}
<dl class="side-facts">
<div><dt>{{icon "pay"}}Pay</dt><dd>{{.Info.Salary}}</dd></div>
<div><dt>{{icon "pin"}}Location</dt><dd>{{if .Info.Location}}{{.Info.Location}}{{else}}Not listed{{end}}</dd></div>
{{if .Info.WorkType}}<div><dt>{{icon "clock"}}Type</dt><dd>{{.Info.WorkType}}</dd></div>{{end}}
{{if .Info.Posting}}<div><dt>{{icon "link"}}Posting</dt><dd>{{.Info.Posting}}</dd></div>{{end}}
</dl>
{{with safeURL .Info.URL}}<a class="btn secondary block" href="{{.}}" target="_blank" rel="noopener noreferrer">View posting{{icon "external"}}<span class="sr"> for {{$.Info.Title}} (opens in a new tab)</span></a>{{end}}
{{if .Info.Questions}}<h3>{{icon "alert"}}Check before applying</h3><p>{{.Info.Questions}}</p>{{end}}
{{if .Info.FitNotes}}<h3>{{icon "target"}}Why it may fit</h3><p>{{.Info.FitNotes}}</p>{{end}}
<details class="more"><summary>Score breakdown and full details</summary><div class="more-body">{{if .Info.HasScore}}{{template "scorebreakdown" .Info}}{{end}}{{range .Info.Groups}}<section class="group"><h3>{{.Title}}</h3><dl>{{range .Items}}<div><dt>{{.Label}}</dt><dd>{{.Value}}</dd></div>{{end}}</dl></section>{{end}}{{template "researchAside" .Info}}</div></details>
</section>
</aside>
</div>{{end}}

{{define "messageForm"}}{{$p := index . "p"}}{{$r := index . "r"}}<form method="post" action="/save" post="/save" class="editor"><input type="hidden" name="job_id" value="{{$p.Job.ID}}"><input type="hidden" name="kind" value="message"><input type="hidden" name="back" value="{{$p.Back}}">{{if $r}}<input type="hidden" name="record_id" value="{{$r.ID}}">{{end}}
<div class="grid-2">
<label>Date and time<input type="datetime-local" name="when" value="{{if $r}}{{$r.A}}{{end}}"></label>
<label>Direction<select name="direction">{{$d := ""}}{{if $r}}{{$d = $r.C}}{{end}}<option {{if eq $d "Received"}}selected{{end}}>Received</option><option {{if eq $d "Sent"}}selected{{end}}>Sent</option><option {{if eq $d "Other"}}selected{{end}}>Other</option></select></label>
<label>Channel<input name="channel" value="{{if $r}}{{$r.B}}{{end}}" placeholder="Email, call, LinkedIn"></label>
<label>With<input name="person" value="{{if $r}}{{$r.D}}{{else}}{{$p.Job.Contact}}{{end}}"></label>
</div>
<label>What was said<textarea name="notes" rows="3">{{if $r}}{{$r.E}}{{end}}</textarea></label>
<div class="form-actions"><button class="btn primary">{{if $r}}Save changes{{else}}Log message{{end}}</button></div></form>{{end}}

{{define "interviewForm"}}{{$p := index . "p"}}{{$r := index . "r"}}<form method="post" action="/save" post="/save" class="editor"><input type="hidden" name="job_id" value="{{$p.Job.ID}}"><input type="hidden" name="kind" value="interview"><input type="hidden" name="back" value="{{$p.Back}}">{{if $r}}<input type="hidden" name="record_id" value="{{$r.ID}}">{{end}}
<div class="grid-2">
<label>Date and time<input type="datetime-local" name="when" value="{{if $r}}{{$r.A}}{{end}}"></label>
<label>Kind of interview<input name="type" value="{{if $r}}{{$r.B}}{{end}}" placeholder="Recruiter screen, technical, onsite"></label>
<label>With<input name="person" value="{{if $r}}{{$r.C}}{{end}}"></label>
<label>Outcome<input name="outcome" value="{{if $r}}{{$r.D}}{{end}}" placeholder="Leave blank until you hear"></label>
</div>
<label>Notes<textarea name="notes" rows="3">{{if $r}}{{$r.E}}{{end}}</textarea></label>
<div class="form-actions"><button class="btn primary">{{if $r}}Save changes{{else}}Add interview{{end}}</button></div></form>{{end}}

{{define "docForm"}}{{$p := index . "p"}}{{$r := index . "r"}}<form method="post" action="/save" post="/save" class="editor"><input type="hidden" name="job_id" value="{{$p.Job.ID}}"><input type="hidden" name="kind" value="document"><input type="hidden" name="record_id" value="{{$r.ID}}"><input type="hidden" name="doc_type" value="{{$r.A}}"><input type="hidden" name="back" value="{{$p.Back}}">
<label>Name (required)<input name="title" required pattern=".*\S.*" title="Enter a name; it cannot be blank" value="{{$r.B}}"></label>
<label>Text<textarea name="content" rows="12">{{$r.C}}</textarea></label>
<div class="form-actions"><button class="btn primary">Save changes</button></div></form>{{end}}
`
