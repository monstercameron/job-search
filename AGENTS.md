# Agents guide: Job Search

This repository is one person's job search: a SQLite tracker of job leads and applications, a local web app and MCP server that read and write it, and the skills an agent uses to apply to jobs. "The owner" below is the person whose search it is. Read this file first, then `.claude/skills/apply-to-job/owner.md`, then the skill for the task.

## The owner's details

The owner's name, contact details, standing answers and personal rules are in `.claude/skills/apply-to-job/owner.md`. It is git-ignored, so it exists only on the owner's machine; `owner.example.md` beside it is the template. **Its personal rules outrank everything in this file and in the skill.** If it is missing, ask the owner to create it before applying to anything.

## Skills

| Skill | Use it when | File |
|---|---|---|
| apply-to-job | The owner says "apply to <company/job>", "apply to the next job", "fill out this application", or names a job id | [.claude/skills/apply-to-job/SKILL.md](.claude/skills/apply-to-job/SKILL.md) |

The skill holds the full procedure, the generic form answers, the blind review, upload steps and platform notes. Claude Code loads skills from `.claude/skills/` when it runs in this folder. The owner may also install a copy for their user account at `%USERPROFILE%\.claude\skills\apply-to-job\`. If so, keep `SKILL.md` and `owner.md` identical in both places.

## Layout

| Path | What it is |
|---|---|
| `job_search.sqlite3` | The live tracker database (git-ignored). Never edit it with raw SQL; use the app's tools. |
| `job_search-before-*.sqlite3` | Database backups (git-ignored). Copy the database to one before any schema or data change. |
| `applications\<job id>-<Company>\` | Exported résumés per job (PDF, Word and text), written by `save_tailored_resume` (git-ignored). |
| `review-app\` | The Go web app and MCP server. Its [README](review-app/README.md) explains every screen and tool. |
| `README.md` | The project overview: features, quick start, MCP setup. |
| `SEARCH-NOTES.md` | The owner's search profile and research notes, if present (git-ignored). |
| `LICENSE` | MIT. The embedded HTMLeX runtime keeps its own ISC license. |

## Tools

- **Web app:** http://127.0.0.1:8080/ (job page: `/track?id=<job id>`; the queue of jobs not yet applied to and not marked "Not interested", best match first: `/next`; charts and the résumé A/B comparison: `/metrics`).
- **MCP tools:** use the `job-search` MCP server when it is connected. Otherwise call any tool from the command line, from the repository root:
  ```powershell
  & ".\review-app\job-review-mcp.exe" -db ".\job_search.sqlite3" -call get_application_kit -args '{"job_id":62}'
  ```
  `-call list` lists the tools. Pass long arguments, such as a résumé, with `-args-file <path>`.
- **Adding a job:** `create_job` with the posting's own URL. Pass `posting_date` (YYYY-MM-DD) whenever the posting shows when it was published; `date_found` (the day it was added) is set for you.
- **Applying:** `get_application_kit`, then `save_tailored_resume`, then `track_application`. When a résumé test is running, label each application with its version (`variant` / `resume_variant`); the skill's "Résumé A/B test" section says how to pick it. The other tools are `search_jobs`, `get_job`, `create_job`, `update_job`, `list_applications`, `update_application`, `save_interview`, `save_communication`, `save_document`, `set_base_resume` and `compare_resume`.

## Working on the app

- Code lives in `review-app\`. Run `go vet ./...` and `go test ./...` there; tests use a throwaway database, never the live one. Test fixtures use invented people and companies; never put the owner's real details in a test.
- After a change, rebuild and restart the running app so the owner always sees the latest version:
  1. Build to `job-review-mcp.new.exe`.
  2. Stop the running `job-review-mcp.exe`, along with whatever starts it automatically, if anything does.
  3. Rename the current exe to `job-review-mcp.previous.exe`, and the new one into place.
  4. Start it again and check that http://127.0.0.1:8080/ answers.
- Colours carry meaning: green is moving forward, yellow is waiting or worth a check, red is a problem or urgent, and cobalt marks the app's own controls. Every icon sits beside a text label.
- Never commit the owner's personal details. Names, contact details, addresses, pay and notes belong in `owner.md`, the database or `SEARCH-NOTES.md`, all git-ignored.

## Rules that always apply

- Follow the personal rules in `owner.md`.
- Never disclose the owner's current pay.
- Use only facts from the Profile and its "Claims to keep accurate" notes. Never invent employers, titles, dates, degrees, metrics or skills.
- Fill application forms completely, including acknowledgement initials and the signature when `owner.md` authorizes it, as the skill describes.
- Every application gets a blind review by a separate agent before it is submitted. Submit only when the owner asks for it in that run and the review passes.
- The owner handles CAPTCHA challenges, account sign-ins, and any SSN, ID-number or password fields.
- Mark an application Applied only after a confirmation page or email has been seen.
- The tools never send email or submit applications. Read the owner's email only; never reply on their behalf.
