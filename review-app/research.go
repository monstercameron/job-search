package main

import "strings"

// Only URLs verified against the employer's own public site belong here.
// Other companies use clearly labelled search links until confirmed.
func officialWebsite(company string) string {
	websites := map[string]string{
		"supabase":       "https://supabase.com/",
		"unframe":        "https://www.unframe.ai/",
		"utility profit": "https://www.utilityprofit.com/",
		"iru":            "https://www.iru.com/",
		"coder":          "https://coder.com/",
		"alpaca":         "https://alpaca.markets/",
		"automox":        "https://www.automox.com/",
		"livekit":        "https://livekit.com/",
		"rescale":        "https://rescale.com/",
		"ascertain":      "https://www.ascertain.com/",
	}
	return websites[strings.ToLower(strings.TrimSpace(company))]
}
