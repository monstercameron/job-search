package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Metrics: how the search is going, drawn from the tracker's own tables, and a
// résumé A/B view that compares outcomes per résumé version. Every chart is an
// inline SVG written here (no script, no network), with a text title, labelled
// values, an aria-label and a table of the same numbers.
//
// A résumé version is a label on the application (applications.resume_variant),
// set by save_tailored_resume, track_application, update_application or the
// form on this page. When an application has no label, the job's
// resume_version field is used if it has one.

// abMinPerArm is the fewest applied applications a résumé version needs before
// the page will compare it with another. Below it the page shows counts only.
const abMinPerArm = 10

// maxVariantLen caps a variant label so it fits a table header.
const maxVariantLen = 40

func hasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, nn, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&cid, &name, &typ, &nn, &def, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// migrateMetrics adds the one column the A/B view needs: the label of the
// résumé version an application went out with. It is nullable and additive.
func migrateMetrics(db *sql.DB) error {
	ok, err := hasColumn(db, "applications", "resume_variant")
	if err != nil || ok {
		return err
	}
	_, err = db.Exec("ALTER TABLE applications ADD COLUMN resume_variant TEXT")
	return err
}

// normalizeVariant trims a label, collapses runs of spaces and caps its length.
func normalizeVariant(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxVariantLen {
		s = string([]rune(s)[:maxVariantLen])
	}
	return strings.TrimSpace(s)
}

// setVariant records the résumé version on a job's application, creating the
// application row first when the job has none. An empty label clears it.
func setVariant(db *sql.DB, jobID int64, label string) error {
	label = normalizeVariant(label)
	if _, err := db.Exec(`INSERT INTO applications(job_id,status) SELECT id,COALESCE(NULLIF(status,''),'Research') FROM jobs WHERE id=? ON CONFLICT(job_id) DO NOTHING`, jobID); err != nil {
		return err
	}
	_, err := db.Exec("UPDATE applications SET resume_variant=?, updated_at=CURRENT_TIMESTAMP WHERE job_id=?", nullIfEmpty(label), jobID)
	return err
}

// ---------- statistics ----------

const z95 = 1.959964

// wilson is the 95% Wilson score interval for k successes in n trials. It stays
// inside 0..1 and is honest at small n, unlike the normal approximation.
func wilson(k, n int) (lo, hi float64) {
	if n <= 0 {
		return 0, 1
	}
	p := float64(k) / float64(n)
	nf := float64(n)
	den := 1 + z95*z95/nf
	centre := (p + z95*z95/(2*nf)) / den
	half := z95 * math.Sqrt(p*(1-p)/nf+z95*z95/(4*nf*nf)) / den
	return math.Max(0, centre-half), math.Min(1, centre+half)
}

// newcombe is the 95% interval for the difference p2 - p1 of two proportions
// (Newcombe's hybrid score method, built from the two Wilson intervals).
func newcombe(k1, n1, k2, n2 int) (diff, lo, hi float64) {
	p1, p2 := float64(k1)/float64(n1), float64(k2)/float64(n2)
	l1, u1 := wilson(k1, n1)
	l2, u2 := wilson(k2, n2)
	diff = p2 - p1
	lo = diff - math.Sqrt((p2-l2)*(p2-l2)+(u1-p1)*(u1-p1))
	hi = diff + math.Sqrt((u2-p2)*(u2-p2)+(p1-l1)*(p1-l1))
	return
}

func pctStr(k, n int) string {
	if n <= 0 {
		return "n/a"
	}
	return strconv.Itoa(int(math.Round(100*float64(k)/float64(n)))) + "%"
}

func pctF(f float64) string { return strconv.Itoa(int(math.Round(100 * f))) }

func signedPoints(f float64) string {
	v := int(math.Round(100 * f))
	if v > 0 {
		return "+" + strconv.Itoa(v)
	}
	return strconv.Itoa(v)
}

// ---------- application records ----------

// appRec is one application with what the page needs to place it in the funnel.
type appRec struct {
	JobID      int64
	Company    string
	Title      string
	Status     string
	AppliedOn  string
	Variant    string
	Interviews int
	Reached    int // highest pipeline stage from Applied (3) to Accepted (7) it has been in; 0 if none
	Applied    bool
	// Skipped marks a posting that closed (stage Closed or Withdrawn) before an
	// application was sent: never pursued, so not counted as tracked.
	Skipped bool
}

const (
	rankApplied   = 3
	rankScreening = 4
	rankInterview = 5
	rankOffer     = 6
	rankAccepted  = 7
)

func (a appRec) heard() bool {
	return a.Applied && (a.Reached >= rankScreening || a.Status == "Rejected")
}

func loadApps(db *sql.DB) ([]appRec, error) {
	reached := map[int64]int{}
	ev, err := db.Query("SELECT application_id, detail FROM application_events")
	if err != nil {
		return nil, err
	}
	for ev.Next() {
		var id int64
		var detail string
		if err = ev.Scan(&id, &detail); err != nil {
			ev.Close()
			return nil, err
		}
		if _, to, ok := strings.Cut(detail, "→"); ok {
			if r := statusRank(strings.TrimSpace(to)); r >= rankApplied && r <= rankAccepted && r > reached[id] {
				reached[id] = r
			}
		}
	}
	ev.Close()
	if err = ev.Err(); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT a.id, a.job_id, j.company, j.title, COALESCE(a.status,''), COALESCE(a.applied_date,''),
		COALESCE(NULLIF(trim(a.resume_variant),''), NULLIF(trim(j.resume_version),''), ''),
		(SELECT COUNT(*) FROM interviews i WHERE i.application_id = a.id)
		FROM applications a JOIN jobs j ON j.id = a.job_id ORDER BY a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appRec
	for rows.Next() {
		var a appRec
		var id int64
		if err = rows.Scan(&id, &a.JobID, &a.Company, &a.Title, &a.Status, &a.AppliedOn, &a.Variant, &a.Interviews); err != nil {
			return nil, err
		}
		a.Variant = normalizeVariant(a.Variant)
		a.Reached = reached[id]
		if r := statusRank(a.Status); r >= rankApplied && r <= rankAccepted && r > a.Reached {
			a.Reached = r
		}
		if a.Interviews > 0 && a.Reached < rankInterview {
			a.Reached = rankInterview
		}
		a.Applied = strings.TrimSpace(a.AppliedOn) != "" || a.Reached >= rankApplied
		a.Skipped = !a.Applied && (a.Status == "Closed" || a.Status == "Withdrawn")
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---------- A/B arms ----------

// Arm is the applied applications that went out with one résumé version.
type Arm struct {
	Name       string
	Unlabelled bool
	N          int
	Heard      int
	Screening  int
	Interview  int
	Offer      int
	Rejected   int
}

func (a Arm) enough() bool { return !a.Unlabelled && a.N >= abMinPerArm }

// buildArms groups applied applications by label, ignoring case. Labelled arms
// come first, largest first; applications with no label share one last arm.
func buildArms(apps []appRec) []Arm {
	idx := map[string]*Arm{}
	var order []string
	var none *Arm
	for _, a := range apps {
		if !a.Applied {
			continue
		}
		var arm *Arm
		if a.Variant == "" {
			if none == nil {
				none = &Arm{Name: "Not labelled", Unlabelled: true}
			}
			arm = none
		} else {
			key := strings.ToLower(a.Variant)
			if idx[key] == nil {
				idx[key] = &Arm{Name: a.Variant}
				order = append(order, key)
			}
			arm = idx[key]
		}
		arm.N++
		if a.heard() {
			arm.Heard++
		}
		if a.Reached >= rankScreening {
			arm.Screening++
		}
		if a.Reached >= rankInterview {
			arm.Interview++
		}
		if a.Reached >= rankOffer {
			arm.Offer++
		}
		if a.Status == "Rejected" {
			arm.Rejected++
		}
	}
	arms := make([]Arm, 0, len(order)+1)
	for _, k := range order {
		arms = append(arms, *idx[k])
	}
	sort.SliceStable(arms, func(i, j int) bool { return arms[i].N > arms[j].N })
	if none != nil {
		arms = append(arms, *none)
	}
	return arms
}

// Compare is the response-rate difference between one version and the largest.
type Compare struct {
	Base, Other   string
	Diff, Lo, Hi  float64
	Tone, Verdict string
}

// compareArms compares the response rate of every other labelled version that
// has enough applications with the largest labelled version, when that one has
// enough too. Nothing is computed below abMinPerArm.
func compareArms(arms []Arm) []Compare {
	var labelled []Arm
	for _, a := range arms {
		if a.enough() {
			labelled = append(labelled, a)
		}
	}
	if len(labelled) < 2 {
		return nil
	}
	base := labelled[0]
	var out []Compare
	for _, o := range labelled[1:] {
		d, lo, hi := newcombe(base.Heard, base.N, o.Heard, o.N)
		c := Compare{Base: base.Name, Other: o.Name, Diff: d, Lo: lo, Hi: hi}
		rng := fmt.Sprintf("%s to %s points", signedPoints(lo), signedPoints(hi))
		switch {
		case lo > 0:
			c.Tone, c.Verdict = "good", fmt.Sprintf("%s got a higher response rate than %s: %s points, 95%% interval %s.", o.Name, base.Name, signedPoints(d), rng)
		case hi < 0:
			c.Tone, c.Verdict = "bad", fmt.Sprintf("%s got a lower response rate than %s: %s points, 95%% interval %s.", o.Name, base.Name, signedPoints(d), rng)
		default:
			c.Tone, c.Verdict = "wait", fmt.Sprintf("No clear difference between %s and %s: %s points, 95%% interval %s, which includes 0.", o.Name, base.Name, signedPoints(d), rng)
		}
		out = append(out, c)
	}
	return out
}

// ---------- page model ----------

type MetricTile struct{ Label, Value, Note string }

type LegendItem struct{ Label, Class string }

// ChartData is one chart: its SVG, a legend, and the same numbers as a table.
type ChartData struct {
	ID, Title, Lede, Empty, Note, Quiet string
	SVG                                 template.HTML
	Legend                              []LegendItem
	Head                                []string
	Rows                                [][]string
}

type ABCell struct {
	Count, Of int
	Pct       string
	Plain     bool // count only, as in the "Applied" row
}

type ABRow struct {
	Label string
	Cells []ABCell
}

type ABHead struct {
	Name       string
	N          int
	Class      string
	Unlabelled bool
	Enough     bool
}

type ABVerdict struct {
	Tone, Icon, Head, Detail string
}

type ABLabelRow struct {
	JobID                  int64
	Company, Title, Status string
	Variant                string
	Applied                bool
}

type ABView struct {
	Verdict   ABVerdict
	Heads     []ABHead
	Rows      []ABRow
	Chart     ChartData
	Compares  []Compare
	Labels    []ABLabelRow
	Variants  []string
	HasArms   bool
	LabelOpen bool
	Min       int
	Unlabeled int
}

type MetricsPage struct {
	Nav      Nav
	Tiles    []MetricTile
	Funnel   ChartData
	Response ChartData
	Weekly   ChartData
	AB       ABView
	Score    ChartData
	Industry ChartData
	Where    ChartData
	Age      ChartData
	Leads    string
	Closed   string // quiet line about closed postings left out, empty when none
	Notice   string
}

func (p MetricsPage) Title() string { return "Metrics" }

type jobRec struct {
	Industry, Arrangement, Location string
	Score                           int
	Scored                          bool
	Posted, Found                   string
}

func loadMetrics(db *sql.DB, now time.Time) (MetricsPage, error) {
	var p MetricsPage
	all, err := loadApps(db)
	if err != nil {
		return p, err
	}
	var apps []appRec
	skipped := 0
	for _, a := range all {
		if a.Skipped {
			skipped++
		} else {
			apps = append(apps, a)
		}
	}
	if skipped > 0 {
		p.Closed = fmt.Sprintf("%d %s closed before you applied %s left out.", skipped, plural2(skipped, "posting", "postings"), plural2(skipped, "is", "are"))
	}
	rows, err := db.Query(`SELECT COALESCE(industry,''), COALESCE(work_arrangement,''), COALESCE(location,''),
		COALESCE(CAST(score_total AS TEXT),''), COALESCE(posting_date,''), COALESCE(date_found,'') FROM jobs`)
	if err != nil {
		return p, err
	}
	var jobs []jobRec
	for rows.Next() {
		var j jobRec
		var sc string
		if err = rows.Scan(&j.Industry, &j.Arrangement, &j.Location, &sc, &j.Posted, &j.Found); err != nil {
			rows.Close()
			return p, err
		}
		if n, e := strconv.Atoi(strings.TrimSpace(sc)); e == nil {
			j.Score, j.Scored = n, true
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return p, err
	}
	var open, dismissed, inbound int
	db.QueryRow("SELECT COUNT(*) FROM jobs WHERE " + nextUpWhere).Scan(&open)
	db.QueryRow("SELECT COUNT(*) FROM job_dismissals WHERE job_id IN (SELECT id FROM jobs)").Scan(&dismissed)
	db.QueryRow("SELECT COUNT(*) FROM communications c JOIN applications a ON a.id=c.application_id WHERE c.direction='Received' AND a.applied_date IS NOT NULL AND trim(a.applied_date)<>''").Scan(&inbound)

	p.Nav = loadNav(db, "metrics", "Metrics")
	var applied, heard, screening, interview, offer, rejected int
	for _, a := range apps {
		if !a.Applied {
			continue
		}
		applied++
		if a.heard() {
			heard++
		}
		if a.Reached >= rankScreening {
			screening++
		}
		if a.Reached >= rankInterview {
			interview++
		}
		if a.Reached >= rankOffer {
			offer++
		}
		if a.Status == "Rejected" {
			rejected++
		}
	}
	p.Tiles = []MetricTile{
		{"Leads saved", strconv.Itoa(len(jobs)), fmt.Sprintf("%d still in Next up", open)},
		{"Applications tracked", strconv.Itoa(len(apps)), "in progress or applied"},
		{"Applied", strconv.Itoa(applied), "an applied date or a later stage"},
		{"Heard back", fmt.Sprintf("%d of %d", heard, applied), "a reply beyond the confirmation"},
		{"Interviews", strconv.Itoa(interview), "reached or logged"},
		{"Offers", strconv.Itoa(offer), fmt.Sprintf("%d rejected", rejected)},
	}
	p.Leads = fmt.Sprintf("%d %s, %d marked not interested.", len(jobs), plural2(len(jobs), "lead", "leads"), dismissed)

	p.Funnel = funnelChart(len(apps), applied, heard, screening, interview, offer)
	p.Funnel.Quiet = p.Closed
	p.Response = responseChart(applied, heard, inbound)
	p.Weekly = weeklyChart(apps, now)
	p.AB = buildAB(apps)
	p.Score = scoreChart(jobs)
	p.Industry = industryChart(jobs)
	p.Where = workplaceChart(jobs)
	p.Age = ageChart(jobs)
	return p, nil
}

func plural2(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// ---------- charts ----------

func funnelChart(tracked, applied, heard, screening, interview, offer int) ChartData {
	c := ChartData{ID: "funnel", Title: "Pipeline funnel", Lede: "How far tracked applications have got. Each stage counts applications that reached it or went past it; the percent is the share of the row above.",
		Head: []string{"Stage", "Applications", "Share of the row above"}}
	if tracked == 0 {
		c.Empty = "No applications are tracked yet. Open a job and choose Review and track to start."
		return c
	}
	stages := []struct {
		label string
		n     int
		class string
	}{{"Tracked", tracked, "slate"}, {"Applied", applied, "good"}, {"Heard back", heard, "good"}, {"Screening or later", screening, "good"}, {"Interview", interview, "good"}, {"Offer", offer, "good"}}
	var bars []bar
	prev := 0
	for i, s := range stages {
		v := strconv.Itoa(s.n)
		share := "n/a"
		if i > 0 && prev > 0 {
			share = pctStr(s.n, prev)
			v += " · " + share
		}
		c.Rows = append(c.Rows, []string{s.label, strconv.Itoa(s.n), share})
		bars = append(bars, bar{Label: s.label, Value: v, N: float64(s.n), Class: s.class})
		prev = s.n
	}
	c.SVG = hbarsSVG("Pipeline funnel", bars, float64(tracked))
	c.Note = "Heard back counts a stage of Screening or later, a rejection, or a logged interview. An automatic confirmation email does not count."
	return c
}

func responseChart(applied, heard, inbound int) ChartData {
	c := ChartData{ID: "response", Title: "Response rate", Lede: "Share of applied applications that heard back. The line is the 95% range the true rate could be in, given how few applications there are.",
		Head: []string{"Group", "Heard back", "Applied", "Rate", "95% range"}}
	rows := []intervalRow{{Label: "All applied", K: heard, N: applied, Class: "slate"}}
	c.SVG = intervalSVG("Response rate", rows)
	if applied == 0 {
		c.Empty = "Nothing has been applied to yet, so there is no rate to show."
		c.SVG = ""
		return c
	}
	lo, hi := wilson(heard, applied)
	c.Rows = [][]string{{"All applied", strconv.Itoa(heard), strconv.Itoa(applied), pctStr(heard, applied), pctF(lo) + "% to " + pctF(hi) + "%"}}
	if applied < abMinPerArm {
		c.Note = fmt.Sprintf("Only %d applied: the range is wide because this is too few to read a rate from. Treat the number as a log, not a result.", applied)
	}
	if inbound > 0 {
		c.Note += fmt.Sprintf(" %d inbound %s logged on applied jobs, not counted as a reply unless the stage moved.", inbound, plural2(inbound, "message is", "messages are"))
	}
	c.Note = strings.TrimSpace(c.Note)
	return c
}

func weekStart(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
	off := (int(d.Weekday()) + 6) % 7
	return d.AddDate(0, 0, -off)
}

func weeklyChart(apps []appRec, now time.Time) ChartData {
	const weeks = 12
	c := ChartData{ID: "weekly", Title: "Applications over time", Lede: "Applied applications per week for the last 12 weeks, by applied date.",
		Head: []string{"Week starting", "Applied"}}
	thisWeek := weekStart(now)
	first := thisWeek.AddDate(0, 0, -7*(weeks-1))
	counts := make([]int, weeks)
	earlier, total := 0, 0
	for _, a := range apps {
		if !a.Applied {
			continue
		}
		t, _, ok := parseWhen(a.AppliedOn)
		if !ok {
			continue
		}
		total++
		w := weekStart(t)
		if w.Before(first) {
			earlier++
			continue
		}
		i := int(math.Round(w.Sub(first).Hours() / (24 * 7)))
		if i >= 0 && i < weeks {
			counts[i]++
		}
	}
	if total == 0 {
		c.Empty = "No application has an applied date yet."
		return c
	}
	cols := make([]col, weeks)
	for i := range cols {
		d := first.AddDate(0, 0, 7*i)
		cols[i] = col{Label: d.Format("2 Jan"), Short: d.Format("2 Jan"), N: counts[i], Class: "slate", ShowLabel: (weeks-1-i)%3 == 0}
		c.Rows = append(c.Rows, []string{d.Format("2 Jan 2006"), strconv.Itoa(counts[i])})
	}
	c.SVG = columnsSVG("Applications per week", cols)
	if earlier > 0 {
		c.Note = fmt.Sprintf("%d earlier %s not shown.", earlier, plural2(earlier, "application is", "applications are"))
	}
	return c
}

func scoreChart(jobs []jobRec) ChartData {
	c := ChartData{ID: "score", Title: "Match-score distribution", Lede: "How many leads fall in each 5-point band of the match score out of 100. A column is labelled with the lowest score in its band.",
		Head:   []string{"Score band", "Leads", "Meaning"},
		Legend: []LegendItem{{"Under 65: weak match", "bad"}, {"65 to 79: fair match", "wait"}, {"80 and up: strong match", "good"}}}
	minS, maxS, scored := 100, 0, 0
	for _, j := range jobs {
		if j.Scored {
			scored++
			minS, maxS = min(minS, j.Score), max(maxS, j.Score)
		}
	}
	if scored == 0 {
		c.Empty = "No lead has a match score yet."
		c.Legend = nil
		return c
	}
	lo := min(minS/5*5, 60)
	hi := max(maxS/5*5, 80)
	hi = min(hi, 100)
	n := (hi-lo)/5 + 1
	counts := make([]int, n)
	for _, j := range jobs {
		if j.Scored {
			counts[min(max((j.Score-lo)/5, 0), n-1)]++
		}
	}
	cols := make([]col, n)
	for i := range cols {
		s := lo + 5*i
		class := "bad"
		word := "weak"
		switch {
		case s >= 80:
			class, word = "good", "strong"
		case s >= 65:
			class, word = "wait", "fair"
		}
		end := s + 4
		if s == 100 {
			end = 100
		}
		cols[i] = col{Label: fmt.Sprintf("%d to %d", s, end), Short: strconv.Itoa(s), N: counts[i], Class: class, ShowLabel: i%2 == 0}
		c.Rows = append(c.Rows, []string{fmt.Sprintf("%d to %d", s, end), strconv.Itoa(counts[i]), word})
	}
	c.SVG = columnsSVG("Leads by match score", cols)
	if un := len(jobs) - scored; un > 0 {
		c.Note = fmt.Sprintf("%d %s not scored yet.", un, plural2(un, "lead is", "leads are"))
	}
	return c
}

func topBars(counts map[string]int, keep int, unknown string) ([]bar, [][]string) {
	type kv struct {
		k string
		v int
	}
	var list []kv
	for k, v := range counts {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].v != list[j].v {
			return list[i].v > list[j].v
		}
		return list[i].k < list[j].k
	})
	var bars []bar
	var rows [][]string
	other, unk := 0, 0
	shown := 0
	for _, e := range list {
		if e.k == unknown {
			unk = e.v
			continue
		}
		if shown >= keep {
			other += e.v
			continue
		}
		shown++
		bars = append(bars, bar{Label: e.k, Value: strconv.Itoa(e.v), N: float64(e.v), Class: "slate"})
		rows = append(rows, []string{e.k, strconv.Itoa(e.v)})
	}
	if other > 0 {
		bars = append(bars, bar{Label: "All other markets", Value: strconv.Itoa(other), N: float64(other), Class: "slate"})
		rows = append(rows, []string{"All other markets", strconv.Itoa(other)})
	}
	if unk > 0 {
		bars = append(bars, bar{Label: unknown, Value: strconv.Itoa(unk), N: float64(unk), Class: "faint"})
		rows = append(rows, []string{unknown, strconv.Itoa(unk)})
	}
	return bars, rows
}

func maxBar(bars []bar) float64 {
	m := 0.0
	for _, b := range bars {
		m = math.Max(m, b.N)
	}
	return m
}

func industryChart(jobs []jobRec) ChartData {
	c := ChartData{ID: "industry", Title: "Leads by company market", Lede: "The seven largest markets, the rest together, and leads whose market is not yet researched.", Head: []string{"Company market", "Leads"}}
	counts := map[string]int{}
	for _, j := range jobs {
		k := strings.TrimSpace(j.Industry)
		if k == "" || k == "Unknown" {
			k = "Unknown"
		}
		counts[k]++
	}
	if len(jobs) == 0 {
		c.Empty = "No leads saved yet."
		return c
	}
	bars, rows := topBars(counts, 7, "Unknown")
	c.Rows = rows
	c.SVG = hbarsSVG("Leads by company market", bars, maxBar(bars))
	return c
}

func workplaceChart(jobs []jobRec) ChartData {
	c := ChartData{ID: "where", Title: "Leads by work arrangement", Lede: "Read from the saved arrangement, falling back to the location, with the same rules as the Jobs filters.", Head: []string{"Arrangement", "Leads"}}
	counts := map[string]int{}
	for _, j := range jobs {
		cats := classifyWorkplace(j.Arrangement, j.Location)
		if len(cats) == 0 {
			counts[choiceLabel(workplaceChoices, notStated)]++
		}
		for _, cat := range cats {
			counts[choiceLabel(workplaceChoices, cat)]++
		}
	}
	if len(jobs) == 0 {
		c.Empty = "No leads saved yet."
		return c
	}
	var bars []bar
	for _, ch := range workplaceChoices {
		cls := "slate"
		if ch.Value == notStated {
			cls = "faint"
		}
		n := counts[ch.Label]
		bars = append(bars, bar{Label: ch.Label, Value: strconv.Itoa(n), N: float64(n), Class: cls})
		c.Rows = append(c.Rows, []string{ch.Label, strconv.Itoa(n)})
	}
	c.SVG = hbarsSVG("Leads by work arrangement", bars, maxBar(bars))
	c.Note = "A role offered as Remote / Hybrid counts under both, so the bars can add up to more than the number of leads."
	return c
}

func ageChart(jobs []jobRec) ChartData {
	c := ChartData{ID: "age", Title: "Listing age when added", Lede: "Days between the listing going up (posting date) and the lead being added to the tracker. Older listings are more likely to be filled.", Head: []string{"Age when added", "Leads"}}
	buckets := []struct {
		label string
		max   int
	}{{"0 to 2 days", 2}, {"3 to 7 days", 7}, {"8 to 14 days", 14}, {"15 to 30 days", 30}, {"31 to 60 days", 60}, {"Over 60 days", math.MaxInt32}}
	counts := make([]int, len(buckets))
	dated := 0
	for _, j := range jobs {
		pt, _, ok1 := parseWhen(j.Posted)
		ft, _, ok2 := parseWhen(j.Found)
		if !ok1 || !ok2 {
			continue
		}
		dated++
		days := int(math.Round(ft.Sub(pt).Hours() / 24))
		for i, b := range buckets {
			if days <= b.max {
				counts[i]++
				break
			}
		}
	}
	if dated == 0 {
		c.Empty = fmt.Sprintf("None of the %d leads has a posting date yet, so listing age cannot be drawn. Add the posting date when you save a lead.", len(jobs))
		return c
	}
	var bars []bar
	for i, b := range buckets {
		bars = append(bars, bar{Label: b.label, Value: strconv.Itoa(counts[i]), N: float64(counts[i]), Class: "slate"})
		c.Rows = append(c.Rows, []string{b.label, strconv.Itoa(counts[i])})
	}
	c.SVG = hbarsSVG("Listing age when added", bars, maxBar(bars))
	c.Note = fmt.Sprintf("%d of %d leads have a posting date; the rest are left out.", dated, len(jobs))
	return c
}

// ---------- A/B view ----------

func buildAB(apps []appRec) ABView {
	v := ABView{Min: abMinPerArm}
	arms := buildArms(apps)
	armClass := func(i int) string { return "arm" + strconv.Itoa(i%4) }
	var labelled []Arm
	for _, a := range arms {
		if a.Unlabelled {
			v.Unlabeled = a.N
		} else {
			labelled = append(labelled, a)
		}
	}
	v.HasArms = len(labelled) > 0
	v.LabelOpen = len(apps) <= 8
	seen := map[string]bool{}
	for _, a := range apps {
		v.Labels = append(v.Labels, ABLabelRow{JobID: a.JobID, Company: a.Company, Title: a.Title, Status: a.Status, Variant: a.Variant, Applied: a.Applied})
		if a.Variant != "" && !seen[strings.ToLower(a.Variant)] {
			seen[strings.ToLower(a.Variant)] = true
			v.Variants = append(v.Variants, a.Variant)
		}
	}
	sort.SliceStable(v.Labels, func(i, j int) bool { return v.Labels[i].Applied && !v.Labels[j].Applied })

	if v.HasArms {
		for i, a := range arms {
			cls := armClass(i)
			if a.Unlabelled {
				cls = "faint"
			}
			v.Heads = append(v.Heads, ABHead{Name: a.Name, N: a.N, Class: cls, Unlabelled: a.Unlabelled, Enough: a.enough()})
		}
		stage := []struct {
			label string
			get   func(Arm) int
		}{
			{"Applied", func(a Arm) int { return a.N }},
			{"Got a response", func(a Arm) int { return a.Heard }},
			{"Screening or later", func(a Arm) int { return a.Screening }},
			{"Interview", func(a Arm) int { return a.Interview }},
			{"Offer", func(a Arm) int { return a.Offer }},
			{"Rejected", func(a Arm) int { return a.Rejected }},
		}
		for si, s := range stage {
			r := ABRow{Label: s.label}
			for _, a := range arms {
				k := s.get(a)
				r.Cells = append(r.Cells, ABCell{Count: k, Of: a.N, Pct: pctStr(k, a.N), Plain: si == 0})
			}
			v.Rows = append(v.Rows, r)
		}
		var ir []intervalRow
		ch := ChartData{ID: "abchart", Title: "Response rate by résumé version", Lede: "Each dot is the response rate; the line is the 95% range it could really be in. Overlapping lines mean the data cannot tell the versions apart.",
			Head: []string{"Version", "Heard back", "Applied", "Rate", "95% range"}}
		for i, a := range arms {
			cls := armClass(i)
			if a.Unlabelled {
				continue
			}
			ir = append(ir, intervalRow{Label: a.Name, K: a.Heard, N: a.N, Class: cls})
			lo, hi := wilson(a.Heard, a.N)
			ch.Rows = append(ch.Rows, []string{a.Name, strconv.Itoa(a.Heard), strconv.Itoa(a.N), pctStr(a.Heard, a.N), pctF(lo) + "% to " + pctF(hi) + "%"})
		}
		ch.SVG = intervalSVG("Response rate by résumé version", ir)
		v.Chart = ch
		v.Compares = compareArms(arms)
	}

	switch {
	case !v.HasArms && v.Unlabeled == 0:
		v.Verdict = ABVerdict{"none", "resume", "No test has started", "Nothing has been applied to yet, and no application has a résumé version."}
	case !v.HasArms:
		v.Verdict = ABVerdict{"wait", "alert", "No test has started", fmt.Sprintf("%d applied %s no résumé version, so there is nothing to compare. Label them below, then label each new application as you go.", v.Unlabeled, plural2(v.Unlabeled, "application has", "applications have"))}
	case len(labelled) < 2:
		v.Verdict = ABVerdict{"wait", "alert", "Only one version so far", fmt.Sprintf("A test needs at least two versions. %s is the only one with applications. Use a second label on the next applications.", labelled[0].Name)}
	default:
		var small []string
		for _, a := range labelled {
			if !a.enough() {
				small = append(small, fmt.Sprintf("%s has %d", a.Name, a.N))
			}
		}
		switch {
		case len(small) > 0:
			v.Verdict = ABVerdict{"wait", "alert", "Too early to conclude anything", fmt.Sprintf("Each version needs at least %d applied applications before the page compares them (%s). The counts below are a log of what happened, not evidence that one version works better.", abMinPerArm, strings.Join(small, ", "))}
		case len(v.Compares) > 0:
			v.Verdict = ABVerdict{"good", "check", "Enough applications to compare", fmt.Sprintf("Every version has at least %d applied. One comparison is made, on response rate only. Other things differ between applications (company, role, timing), so treat even a clear gap as a lead, not proof.", abMinPerArm)}
		}
	}
	return v
}

// ---------- handlers ----------

func registerMetrics(db *sql.DB) {
	http.HandleFunc("/metrics", metricsHandler(db))
	http.HandleFunc("/metrics/variant", variantHandler(db))
}

func metricsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		p, err := loadMetrics(db, time.Now())
		if err != nil {
			log.Printf("metrics: %v", err)
			http.Error(w, "Could not read the jobs database", 500)
			return
		}
		if id := positive(r.URL.Query().Get("labelled")); id != 0 {
			if t, ok := jobTitle(db, id); ok {
				p.Notice = "Saved the résumé version for “" + t + "”."
			}
		}
		render(w, "metrics", p)
	}
}

func variantHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		if !localPost(r) {
			http.Error(w, "Local browser origin required", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
		if parseForm(r) != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		id := positive(r.FormValue("job_id"))
		if _, ok := jobTitle(db, id); id == 0 || !ok {
			http.Error(w, "Invalid job", 400)
			return
		}
		if err := setVariant(db, id, r.FormValue("variant")); err != nil {
			logTracker(err)
			http.Error(w, "Could not save", 500)
			return
		}
		http.Redirect(w, r, "/metrics?labelled="+strconv.FormatInt(id, 10)+"#ab", http.StatusSeeOther)
	}
}
