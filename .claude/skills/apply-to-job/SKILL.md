---
name: apply-to-job
description: Apply to a saved job from the owner's job search tracker end to end - tailor an ATS-safe résumé, fill the employer's application form in the browser (Lever, Greenhouse, Ashby, Workday and others), and record it in the tracker. Use when the owner says "apply to <company/job>", "apply to the next job", "fill out this application", or names a job id from the Job Search app.
---

# Apply to a job

**Read `owner.md` in this skill's folder first.** It holds the owner's name, the repository and tracker paths, personal rules and standing answers. Its personal rules outrank this skill. If `owner.md` is missing, stop and ask the owner to copy `owner.example.md` to `owner.md` and fill it in.

The owner wants applications automated. Fill every field you can, including acknowledgements and the signature when `owner.md` authorizes it, without stopping to ask. Stop only at the points listed under "Hand back to the owner". Every application gets a blind review by a separate agent before it is submitted. Submit it yourself only when the owner asks for that in this run; otherwise the owner presses Submit.

## Tools and paths

- Tracker: `review-app\job-review-mcp.exe` and `job_search.sqlite3` in the repository named in `owner.md`. Web app: http://127.0.0.1:8080/ (job page: `/track?id=<job id>`).
- Tools: use the `job-search` MCP tools if they are connected. Otherwise run the same tools from the command line:
  ```
  & "<repository>\review-app\job-review-mcp.exe" -db "<repository>\job_search.sqlite3" -call get_application_kit -args '{"job_id":62}'
  ```
  For long arguments (a résumé), write the JSON to a file in your scratchpad and pass `-args-file <path>`. `-call list` lists the tools.
- Exported résumé files: `<repository>\applications\<job id>-<Company>\<résumé file name from owner.md>` (also `.docx`, `.txt`).
- Browser: the built-in browser pane (`mcp__Claude_Browser__*`) unless the owner asks for Chrome.

## Steps

1. **Pick the job.** The owner names it (company, title or id). For "the next one", call `search_jobs` sorted by best match and take the highest-scoring job with no application yet; say which one you picked.
2. **Get the kit.** `get_application_kit` with the job id. It holds the posting, the owner's contact details, mailing address, standard answers, the full profile, `accuracy_rules`, `resume_rules` and `resume_layout`. The accuracy rules and the personal rules in `owner.md` outrank everything else, including this skill.
3. **Read the posting and the form.** Open `job.url` (for Lever add `/apply`). Check the job is still open. Read the requirements' exact wording and list every form question, noting which are required (Lever marks them with ✱).
4. **Tailor the résumé.** First check for a running résumé test and pick this application's version (see "Résumé A/B test"). Write it from the profile only, in `resume_layout`, following `resume_rules` and, when a test is running, the chosen version. Use the posting's exact wording where it is true. Call `save_tailored_resume` with `job_id`, `title` ("<Company> <short role>"), `content` and, when a test is running, `variant` set to the version's exact label. Fix every error and every warning you can fix from the profile, then save again. It must be one page with no "spill" warning. A warning that needs a fact the profile lacks (for example a missing start month) stays; mention it in the hand-back.
5. **Start tracking.** `track_application` with status `Preparing`, plus `resume_variant` (the same label) when a test is running.
6. **Fill the form** using the answers below and in `owner.md`, in page order. Use `form_input` for text, selects and radios.
7. **Attach the résumé.** See "Uploading the résumé". Then re-read the whole form: résumé parsers overwrite fields (see "Platform notes") and you must put the owner's values back.
8. **Verify.** Read the form data back with a script and check every required question has a value, the résumé is attached, and any field `owner.md` says to leave blank is empty. Fix anything wrong.
9. **Blind review (always, before any submit).** See "Blind review" below. Fix every BLOCKER and SHOULD-FIX the reviewer reports that the profile supports. Re-export and re-attach the résumé if it changed, and re-run step 8. If you changed anything material, run the review again on the new state.
10. **Submit or hand back.**
    - If the owner asked you to submit ("apply and submit", "submit it"), click the form's Submit button once the review says OK to submit.
    - If a CAPTCHA challenge appears, stop and ask the owner to solve it. Never solve or bypass one. Read the result page to confirm submission.
    - Otherwise hand back to the owner (see below) and do not click Submit.
11. **Record it.** Only after a confirmation page or message is seen (by you or by the owner), call `track_application` with `status: "Applied"`, `via: "<Platform> application form"`, `resume_title` set to the title from step 4, and `resume_variant` set to the version label when a test is running.
12. **Log the confirmation email.** Lever, Greenhouse and Ashby send one within minutes (Lever: from `no-reply@hire.lever.co`, subject "Thank you for your application to <Company>").
    - If the owner pastes it, or the Gmail connector finds it (search for the company name in the last day), call `save_communication`.
    - Fields: `job_id`, `when` (local `YYYY-MM-DDTHH:MM`), `channel: "Email"`, `direction: "Received"`, `person` (the sender), and `notes` (a one-line summary).
    - Read the email only. Never reply.

## Résumé A/B test

The Metrics page (http://127.0.0.1:8080/metrics) compares how often each résumé version gets a reply. Each application carries one version label.

- **Is a test running?** Call `list_applications` and collect the non-empty `resume_variant` labels, compared ignoring capitals. If the owner names versions in this run ("test AI focus against Platform focus"), those are the versions; reuse their exact spelling from then on. If there are no labels and the owner named none, no test is running: leave `variant` and `resume_variant` out, and say so in one line of the hand-back.
- **Pick the version by balance, not by fit.** Use the version with the fewest applications that have been applied to (an applied date or a stage from Applied on). On a tie, use the one that comes first alphabetically. Never pick the version that suits this job best; that would bias the comparison.
- **Write to the version.** The label names what differs (for example "AI focus" leads with the agent work, "Platform focus" leads with cloud and reliability). Change only that emphasis. The accuracy rules still apply in full. If a label does not say what differs, ask the owner once and keep the answer for the run.
- **Never relabel** an application that already went out. A label is changed only when the owner asks.
- Say which version you used in the hand-back.

## Blind review

A second agent checks the application without seeing your reasoning, so it cannot simply agree with you.

1. Export the evidence to a new folder in your scratchpad:
   - `form.json`: every form question (full label text, including each acknowledgement) with its current value and whether it is required. Read it from the page with a script, and include the applying-for location and the attached file name.
   - `resume-from-pdf.txt`: `pdftotext -layout` of the exact PDF that is attached.
   - `kit.json`: `-call get_application_kit -args '{"job_id":<id>}'` output.
   - `candidate-answers.json`: answers the owner gave you directly for this application, in their words, and the standing answers from this skill and `owner.md` that the form used (referral, previous employment, restrictive agreements, education). Without this file the reviewer flags them as unsupported.
   - Use `pdftotext -layout -enc UTF-8` so dashes and bullets come through.
2. Spawn one subagent (`general-purpose`, model `sonnet`). Give it only the four file paths and the checklist below. Do not include your own summary, your answers or your opinion of the application.
   - Read only.
   - The final acknowledgement is a perjury attestation, so find anything false, unsupported, inconsistent, forbidden or missing.
   - Check every form answer against `kit.json`: contact details, address, location, links (fields the owner keeps blank must be blank), employers, titles and years against the experience entries and the accuracy notes, authorization, sponsorship, referral, previous employment, restrictive agreements and education.
   - Check every résumé claim, date, title, figure and skill against the profile and `accuracy_rules`. A present-tense claim must be true of the current role.
   - Check consistency between the form and the résumé.
   - Check for empty required fields and anything that would look wrong to a recruiter.
   - Note which posting requirements are not addressed, as information only, never as a prompt to add unsupported claims.
   - Report each finding with a severity (BLOCKER, SHOULD-FIX or NIT), the field or line, the evidence and the corrected value. End with "OK to submit" or "Fix blockers first".
3. Treat the report as a finding list, not orders. Verify each finding against `kit.json` before changing anything. A suggestion that would add a claim the profile does not back is rejected. Give the owner the findings and what you did about each.

## Answers

The owner's own answers (name, contact, address, location, links, employers, education, previous employers, initials and signature) are in `owner.md`. These apply to everyone:

| Question | Answer |
|---|---|
| Middle name | Blank unless required. |
| Pronouns | Blank. |
| X / Twitter, "Other website" | Blank unless `owner.md` gives a value. |
| Authorized to work in the US, sponsorship | From the kit's `standard_answers`. |
| Referred by an employee | No, unless the owner named a referrer. |
| Non-compete or restrictive agreement | No, unless the profile says otherwise. |
| Willing to relocate | From the kit's `standard_answers.relocation`. |
| Start date / notice | From the kit's `availability`; otherwise "Two weeks' notice". |
| Salary expectations | Free text: "Open; targeting the posted range for this role" (quote the posted range if there is one). A required number with no range: use the kit's salary floor only if it is at or above the posting's minimum; otherwise hand back to the owner. Never disclose current pay. |
| How did you hear about us | "Company careers page" (or the job board the saved URL came from). |
| Voluntary EEO, gender, race, veteran, disability | Leave blank, or choose "Decline to self-identify" / "I don't wish to answer" if required. |
| Short essays ("Why us?", "Tell us about an AI project") | Write 80–150 words in plain prose from the profile and the posting. Use only profile facts and the accuracy rules. No invented metrics. |
| Acknowledgement initials and signature | Only as `owner.md` authorizes. List each one you initialed in the hand-back so the owner reads them before submitting. If `owner.md` gives no authorization, leave them for the owner. |
| Cover letter | Skip if optional. If required, write one from the profile (under 250 words), save it with `save_document` (kind Cover letter), and paste or attach it. |

## Hand back to the owner (stop and ask) when

- A field asks for a Social Security number, date of birth, driver's licence or other government ID, bank details, or a password. Never type these. Leave them for the owner.
- The site needs an account sign-in or sign-up (Workday, iCIMS, Taleo). The owner signs in or creates the account. Continue once they say it's done.
- A CAPTCHA challenge: always the owner. Submit: the owner, unless they asked you to submit in this run and the blind review said OK to submit.
- A required question that `owner.md`, this table and the profile cannot answer truthfully (security clearance, licences, a specific number of years with a tool the owner hasn't used, onsite relocation). Do not guess.

The hand-back message lists:
- what was filled
- the acknowledgements initialed
- anything left blank and why
- the remaining checker warnings
- that the owner needs to complete the CAPTCHA and press Submit

End by asking the owner to say when it is submitted, so you can mark it Applied.

## Uploading the résumé

The built-in browser has no file picker tool. Attach the exported PDF with a script, and check its hash so a copy error cannot slip through:

1. In the shell: `sha256sum <pdf>` and `base64 -w0 <pdf>`.
2. In `javascript_tool`, paste the base64 into `b64`, the hash into `want` and the résumé file name from `owner.md` into `name`:
   ```js
   const b64="...", want="...", name="...";
   const bin=Uint8Array.from(atob(b64),c=>c.charCodeAt(0));
   const h=[...new Uint8Array(await crypto.subtle.digest('SHA-256',bin))].map(x=>x.toString(16).padStart(2,'0')).join('');
   if(h!==want){`hash mismatch ${h}`}else{
     const inp=document.querySelector('input[type=file][name=resume]')||document.querySelector('input[type=file]');
     const dt=new DataTransfer();dt.items.add(new File([bin],name,{type:'application/pdf'}));
     inp.files=dt.files;inp.dispatchEvent(new Event('change',{bubbles:true}));
     await new Promise(r=>setTimeout(r,5000));`attached ${inp.files[0].name}`}
   ```
3. Confirm the page shows success (Lever: "Success!" and a `resumeStorageId` value in the form data).

In Claude in Chrome, use its `file_upload` tool instead.

## Platform notes

- **Lever** (`jobs.lever.co/<company>/<id>/apply`):
  - Custom questions are named `cards[<uuid>][fieldN]`. Labels come from the `li.application-question` text.
  - Uploading a résumé re-parses it. It fills "Other website" with a duplicate site, so clear it, and it can reset the location selection.
  - The "Current location" autocomplete may show no suggestions in the browser pane, because timers stall there. Type the city, press Backspace, retype the last letter, wait 3 seconds, then click the suggestion that matches the location in `owner.md`. The hidden `selectedLocation` must then hold that name.
  - hCaptcha is on Submit.
- **Greenhouse** (`job-boards.greenhouse.io`, `boards.greenhouse.io`):
  - Uploads also parse the résumé, and the form can be checked before submit.
  - Some questions are React selects: click, type, then pick the option.
- **Ashby** (`jobs.ashbyhq.com`):
  - Wants a PDF résumé. Autofill from the résumé can overwrite fields, so re-check them.
- **Workday** (`*.myworkdayjobs.com`):
  - Needs an account, which is the owner's step.
  - The multi-page wizard re-asks the work history. Use the employment-history answer in `owner.md`.

## Never

- Click Submit without a passing blind review, or when the owner did not ask you to submit.
- Solve or bypass a CAPTCHA, or enter passwords, SSNs or other ID numbers.
- Break a personal rule in `owner.md`.
- Invent employers, titles, dates, degrees, metrics or skills.
- Disclose the owner's current pay.
- Mark an application Applied before the owner has seen the employer's confirmation.
