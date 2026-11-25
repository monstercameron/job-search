package main

import "database/sql"

// Seed a conservative set only during the first migration. Later imports stay
// unknown until researched; reruns never overwrite user-supplied categories.
func migrateIndustry(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(jobs)")
	if err != nil {
		return err
	}
	hasIndustry := false
	for rows.Next() {
		var id, notnull, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&id, &name, &typ, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "industry" {
			hasIndustry = true
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if hasIndustry {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("ALTER TABLE jobs ADD COLUMN industry TEXT"); err != nil {
		return err
	}
	known := map[string]string{
		"Alpaca": "Financial technology", "Clara": "Financial technology", "Clera": "Financial technology",
		"Ascertain": "Healthcare technology", "Evio": "Healthcare technology", "InsightRX": "Healthcare technology", "OneImaging": "Healthcare technology", "Verifiable": "Healthcare technology", "VivoSense": "Healthcare technology",
		"Automox": "Cybersecurity", "Hypori": "Cybersecurity", "Iru": "Cybersecurity",
		"Apryse": "Developer tools", "Coder": "Developer tools", "Docker": "Developer tools", "LiveKit": "Developer tools", "Pulumi": "Developer tools", "Supabase": "Developer tools",
		"Nous Research": "Artificial intelligence", "Unframe": "Artificial intelligence",
		"Smartsheet": "Enterprise software", "Sanity": "Content management software", "Rescale": "Engineering cloud software",
		"Sidley Austin LLP": "Legal services", "Utility Profit": "Real estate technology", "Whippy": "Business communications software",
		"Undisclosed quantitative trading firm (via Selby Jennings)": "Financial trading",
	}
	for company, industry := range known {
		if _, err = tx.Exec("UPDATE jobs SET industry=? WHERE company=? AND (industry IS NULL OR trim(industry)='')", industry, company); err != nil {
			return err
		}
	}
	return tx.Commit()
}
