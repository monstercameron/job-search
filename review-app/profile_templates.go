package main

const profileHTML = `
{{define "profile"}}{{template "top" .Nav}}
<div class="page-head has-tile"><span class="tile" aria-hidden="true">{{icon "profile"}}</span><h1>Profile</h1><p class="lede">The facts your résumé is built from. Edit any part in place, then copy it into your base résumé so every job page checks against it.</p></div>
<nav class="jump profile-jump" aria-label="On this page"><a href="#experience">Experience</a><a href="#skill">Skills</a><a href="#project">Projects</a><a href="#education">Education</a><a href="#to-base">Use for job checks</a><a href="#search">Preferences</a></nav>
{{template "profileview" .}}
{{template "bottom"}}{{end}}

{{define "profileview"}}<div id="profile-view">
<div class="profile-grid">
<div class="profile-main">
<article class="panel cv" aria-labelledby="cv-name">

<section id="basics" class="cv-head">
{{if eq .Saved "basics"}}{{template "saved" .}}{{end}}
<h2 id="cv-name" class="cv-name">{{if .F.name}}{{.F.name}}{{else}}Your name{{end}}</h2>
{{if .F.headline}}<p class="cv-headline">{{.F.headline}}</p>{{end}}
<ul class="cv-contact">
{{if .F.location}}<li>{{icon "pin"}}{{.F.location}}</li>{{end}}
{{if .F.email}}<li>{{icon "mail"}}<a href="mailto:{{.F.email}}">{{.F.email}}</a></li>{{end}}
{{if .F.phone}}<li>{{icon "phone"}}{{.F.phone}}</li>{{end}}
{{range $k := (list "website" "github" "linkedin" "devpost" "youtube")}}{{with index $.F $k}}{{$l := linkFor .}}<li>{{contactIcon $k}}{{if $l}}<a href="{{$l}}" target="_blank" rel="noopener noreferrer">{{.}}<span class="sr"> (opens in a new tab)</span></a>{{else}}{{.}}{{end}}</li>{{end}}{{end}}
</ul>
<details class="edit"><summary>Edit name and contact details</summary>
<form method="post" action="/profile/save" post="/profile/save" class="editor"><input type="hidden" name="action" value="fields"><input type="hidden" name="group" value="basics">
<div class="grid-2">{{range .Basics}}{{template "pfield" .}}{{end}}</div>
<div class="form-actions"><button class="btn primary">Save contact details</button></div></form></details>
</section>

<section id="summary" class="cv-sec" aria-labelledby="summary-title">
<h2 id="summary-title">{{icon "summary"}}Summary</h2>
{{if eq .Saved "summary"}}{{template "saved" .}}{{end}}
{{if .F.summary}}<p class="cv-summary">{{.F.summary}}</p>{{else}}<p class="empty-inline">No summary yet.</p>{{end}}
<details class="edit"><summary>Edit summary</summary>
<form method="post" action="/profile/save" post="/profile/save" class="editor"><input type="hidden" name="action" value="fields"><input type="hidden" name="group" value="summary">
{{range .Summary}}{{template "pfield" .}}{{end}}
<div class="form-actions"><button class="btn primary">Save summary</button></div></form></details>
</section>

<section id="experience" class="cv-sec" aria-labelledby="experience-title">
<h2 id="experience-title">{{icon "jobs"}}Experience</h2>
{{if eq .Saved "experience"}}{{template "saved" .}}{{end}}
{{if .Experience}}<ol class="entries">{{range $e := .Experience}}<li class="entry{{if not .OnResume}} off{{end}}">
<div class="entry-head"><h3>{{.Title}}</h3>{{with dateRange .Start .End}}<p class="entry-dates">{{.}}{{with tenure $e.Start $e.End}}<span class="tenure">{{.}}</span>{{end}}</p>{{end}}</div>
{{if or .Org .Place}}<p class="entry-org">{{.Org}}{{if and .Org .Place}}, {{end}}{{.Place}}</p>{{end}}
{{if not .OnResume}}<p class="off-note">Kept here only. Left out of the résumé text.</p>{{end}}
{{with splitLines .Bullets}}<ul class="bullets">{{range .}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{template "entryEdit" (dict "experience" .)}}
</li>{{end}}</ol>{{else}}<p class="empty-inline">No roles yet.</p>{{end}}
<details class="adder"><summary>{{icon "plus"}}Add a role</summary>{{template "entryForm" (dict "experience" nil)}}</details>
</section>

<section id="skill" class="cv-sec" aria-labelledby="skill-title">
<h2 id="skill-title">{{icon "skills"}}Skills</h2>
{{if eq .Saved "skill"}}{{template "saved" .}}{{end}}
{{if .Skills}}<ul class="entries skillset">{{range .Skills}}<li class="entry skillrow{{if not .OnResume}} off{{end}}">
<h3>{{.Title}}</h3>
<div><ul class="chips skills">{{range splitItems .Bullets}}<li>{{.}}</li>{{end}}</ul>
{{if not .OnResume}}<p class="off-note">Left out of the résumé text.</p>{{end}}
{{template "entryEdit" (dict "skill" .)}}</div>
</li>{{end}}</ul>{{else}}<p class="empty-inline">No skills yet.</p>{{end}}
<details class="adder"><summary>{{icon "plus"}}Add a skill group</summary>{{template "entryForm" (dict "skill" nil)}}</details>
</section>

<section id="project" class="cv-sec" aria-labelledby="project-title">
<h2 id="project-title">{{icon "code"}}Projects</h2>
{{if eq .Saved "project"}}{{template "saved" .}}{{end}}
{{if .Projects}}<ol class="entries">{{range $e := .Projects}}<li class="entry{{if not .OnResume}} off{{end}}">
<h3>{{with safeURL .URL}}<a href="{{.}}" target="_blank" rel="noopener noreferrer">{{$e.Title}}{{icon "external"}}<span class="sr"> (opens in a new tab)</span></a>{{else}}{{.Title}}{{end}}</h3>
{{if not .OnResume}}<p class="off-note">Kept here only. Left out of the résumé text.</p>{{end}}
{{with splitLines .Bullets}}<ul class="bullets">{{range .}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{template "entryEdit" (dict "project" .)}}
</li>{{end}}</ol>{{else}}<p class="empty-inline">No projects yet.</p>{{end}}
<details class="adder"><summary>{{icon "plus"}}Add a project</summary>{{template "entryForm" (dict "project" nil)}}</details>
</section>

<section id="education" class="cv-sec" aria-labelledby="education-title">
<h2 id="education-title">{{icon "cap"}}Education and training</h2>
{{if eq .Saved "education"}}{{template "saved" .}}{{end}}
{{if .Education}}<ol class="entries">{{range .Education}}<li class="entry{{if not .OnResume}} off{{end}}">
<div class="entry-head"><h3>{{.Title}}</h3>{{with dateRange .Start .End}}<p class="entry-dates">{{.}}</p>{{end}}</div>
{{if .Org}}<p class="entry-org">{{.Org}}</p>{{end}}
{{if not .OnResume}}<p class="off-note">Kept here only. Left out of the résumé text.</p>{{end}}
{{with splitLines .Bullets}}<ul class="bullets">{{range .}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{template "entryEdit" (dict "education" .)}}
</li>{{end}}</ol>{{else}}<p class="empty-inline">Nothing listed.</p>{{end}}
<details class="adder"><summary>{{icon "plus"}}Add education or training</summary>{{template "entryForm" (dict "education" nil)}}</details>
</section>
</article>
</div>

<aside class="profile-side" aria-label="Using your profile">
<section class="panel" id="to-base" aria-labelledby="to-base-title">
<h2 id="to-base-title">{{icon "apps"}}Use it for job checks</h2>
<p class="meta">Each job page compares its required skills with your base résumé.{{if .HasBase}} Copying replaces the base résumé saved now.{{else}} You have not saved one yet.{{end}}</p>
<form method="post" action="/profile/save" class="side-action"><input type="hidden" name="action" value="to-base"><button class="btn primary block">{{icon "copy"}}{{if .HasBase}}Replace base résumé with profile{{else}}Copy profile into base résumé{{end}}</button></form>
<details class="more"><summary>Preview the résumé text</summary><pre class="plain" tabindex="0" role="region" aria-label="Résumé text built from this profile">{{.Text}}</pre></details>
{{with .Tips}}<h3>{{icon "checklist"}}Quick checks</h3><ul class="checks">{{range .}}<li>{{.}}</li>{{end}}</ul>{{end}}
</section>

<section class="panel" id="search" aria-labelledby="search-title">
<h2 id="search-title">{{icon "target"}}What you are looking for</h2>
{{if eq .Saved "search"}}{{template "saved" .}}{{end}}
<dl class="side-facts">{{range .Search}}{{if .Value}}<div><dt>{{.Label}}</dt><dd>{{.Value}}</dd></div>{{end}}{{end}}</dl>
<details class="edit"><summary>Edit preferences</summary>
<form method="post" action="/profile/save" post="/profile/save" class="editor"><input type="hidden" name="action" value="fields"><input type="hidden" name="group" value="search">
{{range .Search}}{{template "pfield" .}}{{end}}
<div class="form-actions"><button class="btn primary">Save preferences</button></div></form></details>
</section>

<section class="panel accuracy" id="accuracy" aria-labelledby="accuracy-title">
<h2 id="accuracy-title">{{icon "shield"}}Claims to keep accurate</h2>
{{if eq .Saved "accuracy"}}{{template "saved" .}}{{end}}
{{if .Notes}}<ul class="notes">{{range .Notes}}<li>{{.}}</li>{{end}}</ul>{{else}}<p class="empty-inline">Nothing noted.</p>{{end}}
<details class="edit"><summary>Edit these notes</summary>
<form method="post" action="/profile/save" post="/profile/save" class="editor"><input type="hidden" name="action" value="fields"><input type="hidden" name="group" value="accuracy">
{{range .Accuracy}}{{template "pfield" .}}{{end}}
<div class="form-actions"><button class="btn primary">Save notes</button></div></form></details>
</section>
</aside>
</div>
</div>{{end}}

{{define "pfield"}}<label{{if .Long}} class="wide"{{end}}>{{.Label}}{{if .Hint}} <span class="hint">{{.Hint}}</span>{{end}}{{if .Long}}<textarea name="{{.Key}}" rows="5">{{.Value}}</textarea>{{else}}<input name="{{.Key}}" value="{{.Value}}"{{if .Type}} type="{{.Type}}"{{end}}{{if .Auto}} autocomplete="{{.Auto}}"{{end}}>{{end}}</label>{{end}}

{{define "entryEdit"}}{{$s := index . "p"}}{{$e := index . "r"}}<details class="edit"><summary>Edit<span class="sr"> {{$e.Title}}</span></summary>
{{template "entryForm" .}}
<div class="entry-tools">
{{if not $e.First}}<form method="post" action="/profile/save" post="/profile/save"><input type="hidden" name="action" value="move"><input type="hidden" name="id" value="{{$e.ID}}"><input type="hidden" name="dir" value="up"><button class="btn quiet">{{icon "up"}}Move up<span class="sr"> {{$e.Title}}</span></button></form>{{end}}
{{if not $e.Last}}<form method="post" action="/profile/save" post="/profile/save"><input type="hidden" name="action" value="move"><input type="hidden" name="id" value="{{$e.ID}}"><input type="hidden" name="dir" value="down"><button class="btn quiet">{{icon "down"}}Move down<span class="sr"> {{$e.Title}}</span></button></form>{{end}}
<details class="confirm"><summary class="btn quiet danger">{{icon "trash"}}Remove<span class="sr"> {{$e.Title}}</span></summary>
<form method="post" action="/profile/save" post="/profile/save"><input type="hidden" name="action" value="delete"><input type="hidden" name="id" value="{{$e.ID}}"><p>Remove “{{$e.Title}}” from your profile? This cannot be undone.</p><button class="btn danger solid">{{icon "trash"}}Yes, remove<span class="sr"> {{$e.Title}}</span></button></form></details>
</div></details>{{end}}

{{define "entryForm"}}{{$s := index . "p"}}{{$e := index . "r"}}<form method="post" action="/profile/save" post="/profile/save" class="editor"><input type="hidden" name="action" value="entry"><input type="hidden" name="section" value="{{$s}}">{{if $e}}<input type="hidden" name="id" value="{{$e.ID}}">{{end}}
{{if eq $s "experience"}}<div class="grid-2">
<label>Job title (required)<input name="title" required pattern=".*\S.*" title="Enter a name; it cannot be blank" value="{{if $e}}{{$e.Title}}{{end}}"></label>
<label>Employer<input name="org" value="{{if $e}}{{$e.Org}}{{end}}" autocomplete="organization"></label>
<label>Team or product<input name="place" value="{{if $e}}{{$e.Place}}{{end}}"></label>
<div class="grid-2 tight"><label>Started<input name="start" pattern="\d{4}(-\d{2})?" title="A year, or a year and month, like 2025 or 2025-01" value="{{if $e}}{{$e.Start}}{{end}}" placeholder="2025-01" inputmode="numeric"></label>
<label>Ended <span class="hint">Blank if current</span><input name="end" pattern="\d{4}(-\d{2})?" title="A year, or a year and month, like 2025 or 2025-01" value="{{if $e}}{{$e.End}}{{end}}" placeholder="2025-06" inputmode="numeric"></label></div>
</div>
<label>What you did <span class="hint">One point per line.</span><textarea name="bullets" rows="6">{{if $e}}{{$e.Bullets}}{{end}}</textarea></label>
{{else if eq $s "skill"}}<label>Group name (required)<input name="title" required pattern=".*\S.*" title="Enter a name; it cannot be blank" value="{{if $e}}{{$e.Title}}{{end}}" placeholder="Languages"></label>
<label>Skills <span class="hint">Separate with commas.</span><textarea name="bullets" rows="3">{{if $e}}{{join (splitItems $e.Bullets) ", "}}{{end}}</textarea></label>
{{else if eq $s "project"}}<div class="grid-2">
<label>Project name (required)<input name="title" required pattern=".*\S.*" title="Enter a name; it cannot be blank" value="{{if $e}}{{$e.Title}}{{end}}"></label>
<label>Link<input name="url" type="url" value="{{if $e}}{{$e.URL}}{{end}}" placeholder="https://github.com/…"></label></div>
<label>Description <span class="hint">One point per line.</span><textarea name="bullets" rows="4">{{if $e}}{{$e.Bullets}}{{end}}</textarea></label>
{{else}}<div class="grid-2">
<label>Program or credential (required)<input name="title" required pattern=".*\S.*" title="Enter a name; it cannot be blank" value="{{if $e}}{{$e.Title}}{{end}}"></label>
<label>School or provider<input name="org" value="{{if $e}}{{$e.Org}}{{end}}"></label>
<label>Started<input name="start" pattern="\d{4}(-\d{2})?" title="A year, or a year and month, like 2025 or 2025-01" value="{{if $e}}{{$e.Start}}{{end}}" placeholder="2015 or 2015-08" inputmode="numeric"></label>
<label>Ended<input name="end" pattern="\d{4}(-\d{2})?" title="A year, or a year and month, like 2025 or 2025-01" value="{{if $e}}{{$e.End}}{{end}}" placeholder="2017" inputmode="numeric"></label></div>
<label>Notes <span class="hint">One per line.</span><textarea name="bullets" rows="3">{{if $e}}{{$e.Bullets}}{{end}}</textarea></label>
{{end}}
<label class="check"><input type="checkbox" name="on_resume" value="1" {{if or (not $e) $e.OnResume}}checked{{end}}> Include on the résumé</label>
<div class="form-actions"><button class="btn primary">{{if $e}}Save changes{{else}}Add {{sectionNoun $s}}{{end}}</button></div></form>{{end}}
`
