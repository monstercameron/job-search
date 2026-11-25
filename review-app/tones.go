package main

import "time"

// Tones tie colour to meaning, the same way on every screen:
//   good (green):  moving forward, a strong match, low urgency
//   wait (yellow): not applied yet, something to check, a fair match
//   bad (red):     ended badly, overdue, high urgency, a weak match
// Every toned element also carries the words, so colour is never the only cue.

// statusTone colours an application stage.
func statusTone(status string) string {
	switch status {
	case "Research", "Interested", "Preparing":
		return "wait"
	case "Applied", "Screening", "Interviewing", "Offer", "Accepted":
		return "good"
	case "Rejected", "Withdrawn":
		return "bad"
	}
	return "none"
}

// priorityTone colours a job's priority by urgency.
func priorityTone(priority string) string {
	switch priority {
	case "High":
		return "bad"
	case "Medium":
		return "wait"
	case "Low":
		return "good"
	}
	return "none"
}

// groupTone colours an Applications group.
func groupTone(title string) string {
	switch title {
	case "Follow up now":
		return "bad"
	case "In progress":
		return "good"
	case "Not applied yet":
		return "wait"
	}
	return "none"
}

// scoreTone and scoreWord grade a match score: 80 and up strong, 65 to 79
// fair, below 65 weak.
func scoreTone(n int) string {
	switch {
	case n >= 80:
		return "good"
	case n >= 65:
		return "wait"
	}
	return "bad"
}

func scoreWord(n int) string {
	switch {
	case n >= 80:
		return "strong"
	case n >= 65:
		return "fair"
	}
	return "weak"
}

// followTone colours a follow-up date: red once it has arrived, yellow while
// it is still ahead, none for closed applications.
func followTone(date, status string) string {
	if date == "" || statusStage[status] == "closed" {
		return "none"
	}
	if date[:min(len(date), 10)] <= time.Now().Format("2006-01-02") {
		return "bad"
	}
	return "wait"
}
