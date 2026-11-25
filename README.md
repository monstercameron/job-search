# Job Search

A local job-search tracker: a Go web app for reviewing leads and tracking applications, and an MCP server that lets an AI assistant tailor a résumé and fill an application form while the tracker keeps the record. Everything runs on your own machine against one SQLite file.

The app never submits an application or sends a message. The assistant fills the employer's form; you check it and press Submit.

## What it does

- **Jobs:** every saved lead, best match first, with a 0–100 score split into five parts (role direction, qualifications, location, pay, still open). Search, filter by workplace and job type, and sort by match, pay, priority, stage, newest listing or date added. The address bar holds the exact view.
- **Next up:** one job at a time from the leads you have not applied to, with Previous and Next. A job leaves the queue once you apply or mark it **Not interested**, which you can undo.
- **Applications:** tracked jobs grouped as Follow up now, In progress, Not applied yet and Closed. Each job has a page for its stage, dates, contact, notes, and one timeline of messages, interviews and status changes.
- **Profile:** the facts your résumé is built from (contact details, summary, experience, skills, projects, education), plus a list of claims to keep accurate.
- **Résumé:** your base résumé, used to check each job's required skills against what you have.
- **Metrics:** a pipeline funnel, response rate with its 95% range, applications per week, the match-score spread, leads by market and arrangement, and listing age. Charts are inline SVG written in Go, each with a table of the same numbers.
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

The other tools are `search_jobs`, `get_job`, `create_job`, `update_job`, `list_applications`, `update_application`, `save_interview`, `save_communication`, `save_document`, `set_base_resume` and `compare_resume`.

The [apply-to-job skill](.claude/skills/apply-to-job/SKILL.md) runs a whole application with these tools: tailoring, filling the form, a blind review by a second agent, and recording the result. It drives the employer's form in a browser through Claude Code's browser tools, the built-in browser pane or Claude in Chrome. Before using it, copy [`owner.example.md`](.claude/skills/apply-to-job/owner.example.md) to `owner.md` in the same folder and fill in your name, contact details, standing answers and personal rules. `owner.md` is git-ignored. [AGENTS.md](AGENTS.md) is the guide for any agent working in this repository.

## Privacy and safety

- Your data stays in the SQLite file. `.gitignore` keeps every database file, the exported résumés in `applications/`, your `owner.md`, built binaries and logs out of the repository.
- The web app answers only requests addressed to localhost and accepts writes only from the same local browser origin.
- Any connected assistant can read and change the tracker, so connect only clients you trust. The `-mcp-http` mode is reachable by anyone who has its secret URL; stop the tunnel when you are done.

## Repository layout

| Path | What it is |
|---|---|
| `review-app/` | The Go web app and MCP server, with its tests and the detailed [README](review-app/README.md) |
| `review-app/static/` | The stylesheet and the embedded HTMLeX runtime |
| `.claude/skills/apply-to-job/` | The Claude Code skill for applying to a job |
| `AGENTS.md` | Rules and workflow for agents working here |

## Development

```powershell
cd review-app
go vet ./...
go test ./...
```

The tests use a throwaway database and never touch your real one.

## License

[MIT](LICENSE). The embedded HTMLeX runtime in `review-app/static/htmlex/` is under its own [ISC license](review-app/static/htmlex/LICENSE).
