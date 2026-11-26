package main

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP tools for company facts, a job's locations and requirements, and the
// market picture the Metrics page draws from them. The assistant decides
// what a posting means; these tools check formats and store the answer.

type mcpCompanyFacts struct {
	Company     string  `json:"company" jsonschema:"Company name as saved on its jobs; case and extra spaces are ignored"`
	HQCity      *string `json:"hq_city,omitempty" jsonschema:"City of the company's headquarters; empty clears"`
	HQRegion    *string `json:"hq_region,omitempty" jsonschema:"State or province of the headquarters as a code: a US state is two letters such as CA, NY or DC; empty clears"`
	HQCountry   *string `json:"hq_country,omitempty" jsonschema:"Country of the headquarters as an ISO 3166 two-letter code such as US, CA, GB or DE; empty clears"`
	SizeBand    *string `json:"size_band,omitempty" jsonschema:"Headcount band: 1-10, 11-50, 51-200, 201-500, 501-1000, 1001-5000, 5001-10000 or 10001+; empty clears"`
	Ownership   *string `json:"ownership,omitempty" jsonschema:"public, venture (a venture-backed startup), private, nonprofit or government; empty clears"`
	FoundedYear *int    `json:"founded_year,omitempty" jsonschema:"Year the company was founded; 0 clears"`
	Website     *string `json:"website,omitempty" jsonschema:"The company's own website, HTTP(S); empty clears"`
	Notes       *string `json:"notes,omitempty" jsonschema:"Short notes about the company, such as where the facts came from; empty clears"`
}

type mcpPlace struct {
	Kind    string `json:"kind" jsonschema:"remote: an area a remote worker may live and work in; office: a place to work from, for on-site and hybrid roles"`
	Country string `json:"country" jsonschema:"ISO 3166 two-letter country code such as US or CA; for remote work open beyond one country, one of WORLD, AMERICAS, NORTH_AMERICA, LATAM, EUROPE, EMEA or APAC"`
	Region  string `json:"region,omitempty" jsonschema:"State or province code, such as FL or ON. For remote work limited to some US states, add one remote entry per state. Leave empty for the whole country"`
	City    string `json:"city,omitempty" jsonschema:"City, for an office"`
}

type mcpRequirement struct {
	Kind     string `json:"kind" jsonschema:"language (programming language), framework (framework or library), platform (cloud, database, runtime or infrastructure), tool (an application or piece of software used day to day, such as Figma, Jira or Docker), practice (a way of working or skill, such as CI/CD, system design or code review), domain (industry or subject knowledge, such as healthcare or payments) or certification"`
	Name     string `json:"name" jsonschema:"One thing per entry, with its usual spelling, so the same thing groups across jobs: TypeScript, React, PostgreSQL, AWS, Kubernetes, CI/CD. When a posting accepts either of two, add both"`
	Required *bool  `json:"required,omitempty" jsonschema:"true (the default) when the posting requires it; false when it is preferred, a plus or nice to have"`
	Years    int    `json:"years,omitempty" jsonschema:"Years of experience with this the posting asks for, if it says"`
	OwnerHas string `json:"owner_has,omitempty" jsonschema:"Whether the owner's Profile shows this: yes, partly or no. Leave empty unless you have read the Profile (get_application_kit). Judge from the Profile only, never assume"`
}

type mcpJobProfile struct {
	JobID        int64             `json:"job_id"`
	Locations    *[]mcpPlace       `json:"locations,omitempty" jsonschema:"Where the role can be done from. Replaces the job's locations; omit to keep them, or pass [] to clear"`
	Requirements *[]mcpRequirement `json:"requirements,omitempty" jsonschema:"Skills, languages, tools, platforms and other things the posting asks for. Replaces the job's requirements; omit to keep them, or pass [] to clear"`
	YearsMin     *int              `json:"years_min,omitempty" jsonschema:"Fewest years of overall professional experience the posting asks for; -1 clears"`
	Degree       *string           `json:"degree,omitempty" jsonschema:"none (no degree mentioned or none needed), preferred, equivalent (a degree or equivalent experience), bachelor, master or phd (required); empty clears"`
	Seniority    *string           `json:"seniority,omitempty" jsonschema:"intern, junior, mid, senior, staff, principal, lead, manager or director; empty clears"`
}

type mcpInsightsQuery struct {
	Requirement string `json:"requirement,omitempty" jsonschema:"A requirement name, such as Kubernetes: also list the jobs that ask for it"`
	Limit       int    `json:"limit,omitempty" jsonschema:"Most entries per list; default 25"`
}

type mcpGapsQuery struct {
	Limit int `json:"limit,omitempty" jsonschema:"Most jobs and companies to list; default 500"`
}

func addInsightTools(s *mcp.Server, db *sql.DB) {
	mcp.AddTool(s, &mcp.Tool{Name: "set_company", Description: "Record facts about a company that has saved jobs: where it is headquartered, its size and ownership, its website. Facts belong to the company, so every job from it shares them. Use the company's own pages or a reliable source; leave a field out when unsure rather than guessing. Omitted fields keep their value."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpCompanyFacts) (*mcp.CallToolResult, map[string]any, error) {
		name, jobs, err := saveCompany(db, companyIn{Company: in.Company, City: in.HQCity, Region: in.HQRegion, Country: in.HQCountry, Size: in.SizeBand, Owner: in.Ownership, Website: in.Website, Notes: in.Notes, Founded: in.FoundedYear})
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"saved": true, "company": name, "jobs": jobs}, nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "set_job_profile", Description: "Record a job's structured profile from its posting: where it can be done from, what it requires (skills, languages, frameworks, platforms, tools, practices, domain knowledge, certifications), the years of experience, degree requirement and seniority. Read the posting (get_job) and decide each value yourself. Each list given replaces the old one; omitted parts are kept. Feeds the Metrics page and get_market_insights."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpJobProfile) (*mcp.CallToolResult, map[string]any, error) {
		if in.JobID < 1 {
			return nil, nil, fmt.Errorf("invalid job_id")
		}
		p := jobProfileIn{YearsMin: in.YearsMin, Degree: in.Degree, Seniority: in.Seniority}
		if in.Degree != nil {
			d := strings.ToLower(strings.TrimSpace(*in.Degree))
			p.Degree = &d
		}
		if in.Seniority != nil {
			l := strings.ToLower(strings.TrimSpace(*in.Seniority))
			p.Seniority = &l
		}
		if in.Locations != nil {
			ls := make([]placeIn, 0, len(*in.Locations))
			for _, l := range *in.Locations {
				ls = append(ls, placeIn{Kind: l.Kind, Country: l.Country, Region: l.Region, City: l.City})
			}
			p.Locations = &ls
		}
		if in.Requirements != nil {
			rs := make([]requirementIn, 0, len(*in.Requirements))
			for _, r := range *in.Requirements {
				req := r.Required == nil || *r.Required
				rs = append(rs, requirementIn{Kind: r.Kind, Name: r.Name, Required: req, Years: r.Years, OwnerHas: r.OwnerHas})
			}
			p.Requirements = &rs
		}
		if err := saveJobProfile(db, in.JobID, p); err != nil {
			return nil, nil, err
		}
		out := savedResult(in.JobID, "job_profile")
		jp, err := jobProfile(ctx, db, in.JobID)
		if err != nil {
			return nil, nil, err
		}
		out["profile"] = jp
		return nil, out, nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "get_market_insights", Description: "Summarize the saved leads: where companies are headquartered, which US states each lead is open to, how far remote roles reach, office cities, company size and ownership, the most asked-for requirements by kind, requirements missing from the owner's profile, and experience, degree and seniority. The same numbers as the Metrics page."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpInsightsQuery) (*mcp.CallToolResult, map[string]any, error) {
		ins, err := computeInsights(db)
		if err != nil {
			return nil, nil, err
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 25
		}
		cut := func(rs []CountRow) []CountRow {
			if len(rs) > limit {
				return rs[:limit]
			}
			return rs
		}
		var match []ReqCount
		for k, rs := range ins.Requirements {
			for i := range rs {
				if in.Requirement != "" && strings.EqualFold(strings.TrimSpace(in.Requirement), rs[i].Name) {
					match = append(match, rs[i])
				}
				rs[i].JobIDs = nil
			}
			if len(rs) > limit {
				ins.Requirements[k] = rs[:limit]
			}
		}
		for i := range ins.Gaps {
			ins.Gaps[i].JobIDs = nil
		}
		if len(ins.Gaps) > limit {
			ins.Gaps = ins.Gaps[:limit]
		}
		var open []CountRow
		for _, r := range ins.OpenByState {
			if r.Count > 0 {
				open = append(open, CountRow{Name: r.Name, Code: r.Code, Count: r.Count})
			}
		}
		sortCounts(open)
		ins.OpenByState = open
		ins.HQByState, ins.HQByCountry, ins.HQCities, ins.OfficeCities = cut(ins.HQByState), cut(ins.HQByCountry), cut(ins.HQCities), cut(ins.OfficeCities)
		out := map[string]any{"insights": ins}
		if in.Requirement != "" {
			var jobs []map[string]any
			for _, m := range match {
				for _, id := range m.JobIDs {
					var company, title string
					var required bool
					db.QueryRowContext(ctx, "SELECT j.company, j.title, MAX(r.required) FROM jobs j JOIN job_requirements r ON r.job_id=j.id WHERE j.id=? AND lower(r.name)=lower(?) AND r.kind=?", id, m.Name, m.Kind).Scan(&company, &title, &required)
					jobs = append(jobs, map[string]any{"job_id": id, "company": company, "title": title, "kind": m.Kind, "required": required})
				}
			}
			out["requirement"] = map[string]any{"name": in.Requirement, "jobs": jobs}
		}
		return nil, out, nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "list_profile_gaps", Description: "List the work left for the Metrics page: jobs with no recorded job profile (fill with set_job_profile) and companies with no recorded headquarters (fill with set_company)."}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpGapsQuery) (*mcp.CallToolResult, map[string]any, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 500
		}
		rows, err := db.QueryContext(ctx, "SELECT id AS job_id, company, title FROM jobs WHERE profiled_at IS NULL ORDER BY id LIMIT ?", limit)
		if err != nil {
			return nil, nil, err
		}
		jobs, err := rowsAsMaps(rows)
		if err != nil {
			return nil, nil, err
		}
		var unprofiled int
		db.QueryRowContext(ctx, "SELECT COUNT(*) FROM jobs WHERE profiled_at IS NULL").Scan(&unprofiled)
		all, known, _, _, err := loadInsightData(db)
		if err != nil {
			return nil, nil, err
		}
		counts, names, order := map[string]int{}, map[string]string{}, []string{}
		for _, j := range all {
			if c, ok := known[j.Key]; ok && c.Country != "" {
				continue
			}
			if counts[j.Key] == 0 {
				order = append(order, j.Key)
				names[j.Key] = strings.Join(strings.Fields(j.Company), " ")
			}
			counts[j.Key]++
		}
		sort.Strings(order)
		companies := []map[string]any{}
		for _, k := range order {
			if len(companies) >= limit {
				break
			}
			companies = append(companies, map[string]any{"company": names[k], "jobs": counts[k]})
		}
		return nil, map[string]any{"jobs_without_profile": jobs, "jobs_without_profile_total": unprofiled, "companies_without_headquarters": companies}, nil
	})
}

// jobProfile is a job's company facts, locations and requirements, for
// get_job and set_job_profile.
func jobProfile(ctx context.Context, db *sql.DB, jobID int64) (map[string]any, error) {
	out := map[string]any{}
	var company string
	var years sql.NullInt64
	var degree, level, profiled sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT company, years_min, degree_level, seniority_level, profiled_at FROM jobs WHERE id=?", jobID).Scan(&company, &years, &degree, &level, &profiled); err != nil {
		return nil, err
	}
	out["profiled_at"], out["years_min"], out["degree"], out["seniority"] = nullable(profiled), nullableInt(years), nullable(degree), nullable(level)
	rows, err := db.QueryContext(ctx, "SELECT name, hq_city, hq_region, hq_country, size_band, ownership, founded_year, website, notes FROM companies WHERE name=?", strings.Join(strings.Fields(company), " "))
	if err != nil {
		return nil, err
	}
	cs, err := rowsAsMaps(rows)
	if err != nil {
		return nil, err
	}
	if len(cs) > 0 {
		out["company"] = cs[0]
	}
	rows, err = db.QueryContext(ctx, "SELECT kind, country, region, city FROM job_locations WHERE job_id=? ORDER BY id", jobID)
	if err != nil {
		return nil, err
	}
	if out["locations"], err = rowsAsMaps(rows); err != nil {
		return nil, err
	}
	rows, err = db.QueryContext(ctx, "SELECT kind, name, required = 1 AS required, years, owner_has FROM job_requirements WHERE job_id=? ORDER BY id", jobID)
	if err != nil {
		return nil, err
	}
	if out["requirements"], err = rowsAsMaps(rows); err != nil {
		return nil, err
	}
	return out, nil
}

func nullable(s sql.NullString) any {
	if s.Valid {
		return s.String
	}
	return nil
}

func nullableInt(n sql.NullInt64) any {
	if n.Valid {
		return n.Int64
	}
	return nil
}
