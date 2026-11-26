# Job Search

A local job-search tracker: a Go web app for reviewing leads and tracking applications, and an MCP server that lets an AI assistant tailor a résumé and fill an application form while the tracker keeps the record. Everything runs on your own machine against one SQLite file. The same app also runs entirely inside a web browser, with no server, so it can be hosted on GitHub Pages.

The app never submits an application or sends a message. The assistant fills the employer's form; you check it and press Submit.

## What it does

- **Jobs:** every saved lead, best match first, with a 0–100 score split into five parts (role direction, qualifications, location, pay, still open). Search, filter by workplace and job type, and sort by match, pay, priority, stage, newest listing or date added. The address bar holds the exact view.
- **Next up:** one job at a time from the leads you have not applied to, with Previous and Next. A job leaves the queue once you apply or mark it **Not interested**, which you can undo.
- **Applications:** tracked jobs grouped as Follow up now, In progress, Not applied yet and Closed. Each job has a page for its stage, dates, contact, notes, and one timeline of messages, interviews and status changes.
- **Profile:** the facts your résumé is built from (contact details, summary, experience, skills, projects, education), plus a list of claims to keep accurate.
- **Résumé:** your base résumé, used to check each job's required skills against what you have.
- **Metrics:** a pipeline funnel, response rate with its 95% range, applications per week, the match-score spread, leads by market and arrangement, and listing age, plus maps of where companies are headquartered and which states each lead is open to, and what jobs ask for (languages, frameworks, platforms, tools, practices, domains, experience, degree, seniority, and the asks missing from your Profile). Charts are inline SVG written in Go, each with a table of the same numbers.
- **Your data:** export the whole tracker as one SQLite file from the link at the foot of every page. The browser version also imports one, so you can move between the desktop app and the browser, or between browsers.
- **Résumé A/B test:** label each application with the résumé version it went out with, and the Metrics page compares replies, interviews and offers per version. It shows counts only until each version has 10 applications, then compares response rates with a 95% interval for the difference.

Colours carry meaning on every screen: green is moving forward, yellow is waiting or worth a check, red is a problem or urgent. Every coloured item also says its meaning in words, and every icon sits next to a text label. Pages work without JavaScript; with it, [HTMLeX](https://github.com/monstercameron/HTMLeX) swaps only the part of the page that changed.

## Quick start

You need [Go](https://go.dev/dl/) 1.26 or later. The examples use Windows paths and PowerShell; the Go code itself has no Windows-only parts.

```powershell
git clone https://github.com/monstercameron/job-search.git
cd job-search/review-app
go build -o job-review-mcp.exe .
./job-review-mcp.exe -init -db ../job_search.sqlite3
```

Open http://127.0.0.1:8080/. `-init` is only for the first run: it creates the database file and its tables. After that, start the app without it, or on Windows double-click `review-app/Start Job Review.cmd`. Without `-init` the app refuses a database file that does not exist, so a mistyped path fails instead of starting an empty tracker.

Add leads with the `create_job` tool (see below), or from the command line:

```powershell
./job-review-mcp.exe -db ../job_search.sqlite3 -call create_job -args '{"company":"Acme","title":"Software Engineer","url":"https://example.com/jobs/1","posting_date":"2026-11-20"}'
```

| Flag | Default | Purpose |
|---|---|---|
| `-db` | `../job_search.sqlite3` | Path to the SQLite database |
| `-init` | off | Create the database and its tables if they do not exist (first run) |
| `-addr` | `127.0.0.1:8080` | Address for the web app |
| `-mcp` | off | Serve MCP over standard input and output instead of the web app |
| `-mcp-http` | none | Serve MCP over Streamable HTTP on this address, for ChatGPT |
| `-call` | none | Run one MCP tool, print its JSON result and exit (`list` lists the tools) |
| `-args`, `-args-file` | `{}` | JSON arguments for `-call` |

## Run it in the browser

The app also builds for WebAssembly and runs inside the browser: a service worker starts the Go program, and the same pages answer from a SQLite database held in memory ([ncruces/go-sqlite3](https://github.com/ncruces/go-sqlite3), SQLite compiled to pure Go). Nothing is sent to a server. The database is saved to the browser's storage after every change, because browsers stop an idle service worker and its memory with it. The next start loads the saved copy.

On a first visit the tracker is empty. Import a database exported from the desktop app or another browser, or load six sample jobs (invented employers) to try it. The browser version has no MCP server, so an assistant cannot add jobs to it; add them in the desktop app and import the file. Clearing the site's data in the browser deletes the tracker, so export a copy now and then.

To build and try it locally:

```powershell
cd review-app
go run ./tools/buildweb -serve 127.0.0.1:8111
```

Then open http://127.0.0.1:8111/job-search/. `buildweb` compiles the app for `js/wasm` (about 32 MB, downloaded once and then cached), copies Go's `wasm_exec.js` and the files in `review-app/web/`, and writes the site to `review-app/_site/`. `-base` sets the path the site is served under (default `/job-search/`; use `/` for a custom domain). With `-serve` it then serves the folder the way GitHub Pages does.

The workflow in [.github/workflows/pages.yml](.github/workflows/pages.yml) runs the tests, builds the site and publishes it to GitHub Pages on every push to `main`. To turn it on for a copy of this repository, set **Settings > Pages > Build and deployment > Source** to **GitHub Actions**. The site is then at `https://<user>.github.io/<repository>/`.

## Using it with an AI assistant

The same executable is an MCP server. For Claude Code:

```powershell
claude mcp add --scope user job-search -- "<path>\review-app\job-review-mcp.exe" -mcp -db "<path>\job_search.sqlite3"
```

Use absolute paths, because clients start the server from another folder. Claude Desktop and ChatGPT setup is in [review-app/README.md](review-app/README.md#mcp-server).

Applying to a job takes three tools in order:

1. `get_application_kit` returns the posting, your contact details, standard answers, the full profile, the accuracy rules and the ATS résumé rules. It never includes your current pay.
2. `save_tailored_resume` checks the tailored résumé against the profile's accuracy rules and common applicant-tracking-system problems. It refuses a résumé that breaks a hard rule, such as a missing EXPERIENCE heading or characters a parser cannot read. It warns about softer problems, such as figures that are not in the profile or dates without a month. It then exports a text-based PDF, a Word file and plain text.
3. `track_application` records the stage, and the submission once you have seen the employer's confirmation.

The other tools are `search_jobs`, `get_job`, `create_job`, `update_job`, `list_applications`, `update_application`, `save_interview`, `save_communication`, `save_document`, `set_base_resume`, `compare_resume`, `set_company`, `set_job_profile`, `get_market_insights` and `list_profile_gaps`.

The [apply-to-job skill](.claude/skills/apply-to-job/SKILL.md) runs a whole application with these tools: tailoring, filling the form, a blind review by a second agent, and recording the result. It drives the employer's form in a browser through Claude Code's browser tools, the built-in browser pane or Claude in Chrome. Before using it, copy [`owner.example.md`](.claude/skills/apply-to-job/owner.example.md) to `owner.md` in the same folder and fill in your name, contact details, standing answers and personal rules. `owner.md` is git-ignored. [AGENTS.md](AGENTS.md) is the guide for any agent working in this repository.

## Privacy and safety

- Your data stays in the SQLite file. `.gitignore` keeps every database file, the exported résumés in `applications/`, your `owner.md`, built binaries and logs out of the repository.
- The web app answers only requests addressed to localhost and accepts writes only from the same local browser origin.
- The browser version keeps the tracker in that browser's storage for the site and never uploads it. It accepts form posts only from its own pages.
- Any connected assistant can read and change the tracker, so connect only clients you trust. The `-mcp-http` mode is reachable by anyone who has its secret URL; stop the tunnel when you are done.

## Repository layout

| Path | What it is |
|---|---|
| `review-app/` | The Go web app and MCP server, with its tests and the detailed [README](review-app/README.md) |
| `review-app/static/` | The stylesheet and the embedded HTMLeX runtime |
| `review-app/web/` | The browser version's service worker and first-visit page |
| `review-app/tools/buildweb/` | Builds the browser version into a static site |
| `.github/workflows/pages.yml` | Publishes the browser version to GitHub Pages |
| `.claude/skills/apply-to-job/` | The Claude Code skill for applying to a job |
| `AGENTS.md` | Rules and workflow for agents working here |

## Development

```powershell
cd review-app
go vet ./...
go test ./...
$env:GOOS = "js"; $env:GOARCH = "wasm"; go vet .; Remove-Item Env:GOOS, Env:GOARCH
```

The tests use a throwaway database and never touch your real one.

## License

[MIT](LICENSE). The embedded HTMLeX runtime in `review-app/static/htmlex/` is under its own [ISC license](review-app/static/htmlex/LICENSE).
