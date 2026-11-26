package main

const metricsHTML = `
{{define "metrics"}}{{template "top" .Nav}}
<div class="page-head has-tile"><span class="tile" aria-hidden="true">{{icon "metrics"}}</span><h1>Metrics</h1><p class="lede">How the search is going, drawn from your own tracker. With only a handful of applications so far, read the counts as a log and not as a trend.</p></div>

<nav class="mx-toc" aria-label="Sections of this page"><ul><li><a href="#pipeline">Pipeline and replies</a></li><li><a href="#ab">Résumé A/B</a></li><li><a href="#leads">Your leads</a></li><li><a href="#places">Places</a></li><li><a href="#asks">What jobs ask for</a></li></ul></nav>

<dl class="mx-tiles">{{range .Tiles}}<div><dt>{{.Label}}</dt><dd><strong>{{.Value}}</strong><span class="meta">{{.Note}}</span></dd></div>{{end}}</dl>
{{with .Closed}}<p class="meta mx-quiet">{{.}}</p>{{end}}

<section class="panel mx" id="pipeline" aria-labelledby="pipeline-h">
<h2 id="pipeline-h">{{icon "progress"}}Pipeline and replies</h2>
<div class="mx-grid">{{template "mxchart" .Funnel}}{{template "mxchart" .Response}}</div>
<div class="mx-grid mx-wide">{{template "mxchart" .Weekly}}</div>
</section>

<section class="panel mx" id="ab" aria-labelledby="ab-h">
<h2 id="ab-h">{{icon "resume"}}Résumé A/B</h2>
<p class="meta">Which résumé version gets more replies? Each application carries one version label, and only applications that were actually applied to are counted.</p>
{{with .Notice}}<div class="saved" role="status"><p>{{.}}</p></div>{{end}}
{{template "abview" .AB}}
</section>

<section class="panel mx" id="leads" aria-labelledby="leads-h">
<h2 id="leads-h">{{icon "jobs"}}Your leads</h2>
<p class="meta">{{.Leads}}</p>
<div class="mx-grid">{{template "mxchart" .Score}}{{template "mxchart" .Industry}}{{template "mxchart" .Where}}{{template "mxchart" .Age}}</div>
</section>

{{with .Places}}<section class="panel mx" id="places" aria-labelledby="places-h">
<h2 id="places-h">{{icon "pin"}}Places</h2>
<p class="meta">{{.Lede}}</p>
<div class="mx-grid mx-map-row">{{template "mxchart" .HQ}}<div class="mx-stack">{{template "mxchart" .Abroad}}{{template "mxchart" .HQCities}}</div></div>
<div class="mx-grid mx-map-row">{{template "mxchart" .Open}}<div class="mx-stack">{{template "mxchart" .Reach}}{{template "mxchart" .Offices}}</div></div>
<div class="mx-grid">{{template "mxchart" .Sizes}}{{template "mxchart" .Ownership}}</div>
</section>{{end}}

{{with .Asks}}<section class="panel mx" id="asks" aria-labelledby="asks-h">
<h2 id="asks-h">{{icon "skills"}}What jobs ask for</h2>
<p class="meta">{{.Lede}}</p>
<div class="mx-grid">{{template "mxchart" .Gaps}}{{template "mxchart" .Years}}{{template "mxchart" .Degree}}{{template "mxchart" .Level}}</div>
<h3 class="mx-sub">By kind</h3>
<div class="mx-grid">{{range .Kinds}}{{template "mxchart" .}}{{end}}</div>
</section>{{end}}
{{template "bottom"}}{{end}}

{{define "mxchart"}}<figure class="mx-chart" id="chart-{{.ID}}">
<figcaption><h3>{{.Title}}</h3><p class="meta">{{.Lede}}</p></figcaption>
{{if .Empty}}<p class="mx-empty">{{icon "alert"}}<span>{{.Empty}}</span></p>{{else}}{{.SVG}}
{{if .Legend}}<ul class="mx-legend">{{range .Legend}}<li><span class="sw c-{{.Class}}" aria-hidden="true"></span>{{.Label}}</li>{{end}}</ul>{{end}}
{{if .Note}}<p class="meta mx-note">{{.Note}}</p>{{end}}
{{if .Rows}}<details class="mx-table"><summary>Show as a table<span class="sr">: {{.Title}}</span></summary><table><caption class="sr">{{.Title}}</caption><thead><tr>{{range .Head}}<th scope="col">{{.}}</th>{{end}}</tr></thead><tbody>{{range .Rows}}<tr>{{range $i, $c := .}}{{if eq $i 0}}<th scope="row">{{$c}}</th>{{else}}<td>{{$c}}</td>{{end}}{{end}}</tr>{{end}}</tbody></table></details>{{end}}{{end}}
{{with .Quiet}}<p class="meta mx-quiet">{{.}}</p>{{end}}
</figure>{{end}}

{{define "abview"}}
<div class="mx-verdict tone-{{.Verdict.Tone}}" role="status"><h3>{{icon .Verdict.Icon}}{{.Verdict.Head}}</h3><p>{{.Verdict.Detail}}</p></div>

{{if .HasArms}}
<div class="mx-scroll"><table class="mx-ab">
<caption>Outcomes per résumé version, out of the applications that went out with it</caption>
<thead><tr><td></td>{{range .Heads}}<th scope="col" class="{{if .Unlabelled}}faint{{end}}"><span class="sw c-{{.Class}}" aria-hidden="true"></span>{{.Name}}<span class="armn">n = {{.N}} applied</span>{{if .Unlabelled}}<span class="armflag tone-none">Not compared</span>{{else if .Enough}}<span class="armflag tone-good">{{icon "check"}}Enough to compare</span>{{else}}<span class="armflag tone-wait">{{icon "alert"}}Too few to conclude</span>{{end}}</th>{{end}}</tr></thead>
<tbody>{{range .Rows}}<tr><th scope="row">{{.Label}}</th>{{range .Cells}}<td>{{if .Plain}}<strong>{{.Count}}</strong>{{else}}<strong>{{.Count}}</strong> <span class="of">of {{.Of}}</span>{{if .Of}} <span class="rate">{{.Pct}}</span>{{end}}{{end}}</td>{{end}}</tr>{{end}}</tbody>
</table></div>
<p class="meta mx-note">Every rate is out of that version's applied count, shown as n in its header. A response is a stage of Screening or later, a rejection, or a logged interview.</p>
<div class="mx-ab-chart">{{template "mxchart" .Chart}}</div>
{{if .Compares}}<div class="mx-compare"><h3>Is one version ahead?</h3><ul>{{range .Compares}}<li class="tone-{{.Tone}}">{{.Verdict}}</li>{{end}}</ul><p class="meta">Response rate only, one comparison per version, using a 95% interval for the difference. This does not account for the jobs being different.</p></div>{{end}}
{{else}}
<div class="empty mx-start"><span class="empty-glyph" aria-hidden="true">{{icon "resume"}}</span><h3>Start a résumé test</h3>
<ol>
<li>Pick two or more names for the versions you want to compare, for example “Platform engineer” and “AI focus”.</li>
<li>Put the name on each application as you tailor and send it. Ask the assistant to pass it as <code>variant</code> in <code>save_tailored_resume</code> or <code>resume_variant</code> in <code>track_application</code>, or type it in the list below.</li>
<li>Keep going until each version has {{.Min}} applied applications. Until then this page shows counts only and will not call a winner.</li>
</ol></div>
{{end}}

<details class="mx-label-box" id="label"{{if .LabelOpen}} open{{end}}><summary>Label your applications{{if .Labels}} ({{len .Labels}}){{end}}</summary>
<p class="meta">Type the name of the résumé version each application went out with. Use the same spelling for the same version; capitals do not matter. Clear the field to remove a label.</p>
{{if .Labels}}<datalist id="variants">{{range .Variants}}<option value="{{.}}">{{end}}</datalist>
<ul class="mx-labels">{{range .Labels}}<li><div class="mx-lab-who"><strong>{{.Company}}</strong> <span class="meta">{{.Title}}</span> <span class="flag tone-{{statusTone .Status}}">{{.Status}}</span>{{if not .Applied}} <span class="meta">not applied yet, not counted</span>{{end}}</div>
<form method="post" action="/metrics/variant" class="mx-lab-form"><input type="hidden" name="job_id" value="{{.JobID}}"><label><span class="sr">Résumé version for {{.Company}}, {{.Title}}</span><input type="text" name="variant" value="{{.Variant}}" list="variants" maxlength="40" placeholder="Version name"></label><button class="btn secondary">Save version<span class="sr"> for {{.Company}}</span></button></form></li>{{end}}</ul>
{{else}}<p class="empty-inline">No applications are tracked yet. Open a job and choose Review and track first.</p>{{end}}
</details>
{{end}}
`
