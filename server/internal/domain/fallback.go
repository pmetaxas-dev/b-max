package domain

import "time"

// FallbackPlan is a simple plan made without the AI, for when Groq is
// unreachable and the user chooses to start anyway (Phase 5). It is generic
// on purpose: four stages every big goal goes through, in the goal's
// language, with search phrases built from the goal itself. Its steps are
// scheduled one per day from today. The user can ask the AI for better steps
// later (⚙️ "New steps for the current milestone").
func FallbackPlan(goal string, now time.Time) RawPlan {
	lang := GoalLanguage(goal)
	stages := fallbackStages[lang]
	plan := RawPlan{TargetHorizon: fallbackHorizon[lang], EstimatedHours: 60}
	day := 0
	for i, st := range stages {
		m := RawMilestone{Title: st.title, Weight: 1}
		if i == 0 {
			for _, text := range st.steps {
				m.Steps = append(m.Steps, RawStep{
					Text: text, EstimatedMinutes: 25,
					ScheduledFor:  DayKey(now.AddDate(0, 0, day)),
					SearchQueries: []string{goal, goal + fallbackSearchSuffix[lang]},
				})
				day++
			}
		}
		plan.Milestones = append(plan.Milestones, m)
	}
	return plan
}

// FallbackSteps are the steps of a later fallback milestone.
func FallbackSteps(goal, milestone string, now time.Time) []RawStep {
	lang := GoalLanguage(goal)
	for _, st := range fallbackStages[lang] {
		if st.title != milestone {
			continue
		}
		var out []RawStep
		for i, text := range st.steps {
			out = append(out, RawStep{
				Text: text, EstimatedMinutes: 25, ScheduledFor: DayKey(now.AddDate(0, 0, i)),
				SearchQueries: []string{goal, goal + fallbackSearchSuffix[lang]},
			})
		}
		return out
	}
	return nil
}

type fallbackStage struct {
	title string
	steps []string
}

var fallbackHorizon = map[string]string{LangEnglish: "about 2 months", LangGreek: "περίπου 2 μήνες"}

var fallbackSearchSuffix = map[string]string{LangEnglish: " for beginners", LangGreek: " για αρχάριους"}

var fallbackStages = map[string][]fallbackStage{
	LangEnglish: {
		{"Understand where to start", []string{
			"Write down in three sentences what reaching this goal looks like",
			"Find one beginner guide about your goal and read its introduction",
			"List the three skills or things you need most",
		}},
		{"Make a first small version", []string{
			"Pick the smallest thing you can make or do toward the goal",
			"Work on it for 25 minutes",
			"Finish a rough first version",
		}},
		{"Practise and get feedback", []string{
			"Show your first version to one person",
			"Write down one thing to improve",
			"Improve that one thing",
		}},
		{"Finish and share", []string{
			"Decide what “done” means for this goal",
			"Complete the last missing part",
			"Share the result",
		}},
	},
	LangGreek: {
		{"Κατάλαβε από πού ξεκινάς", []string{
			"Γράψε σε τρεις προτάσεις πώς μοιάζει ο στόχος όταν τον πετύχεις",
			"Βρες έναν οδηγό για αρχάριους για τον στόχο σου και διάβασε την εισαγωγή",
			"Γράψε τις τρεις δεξιότητες ή τα τρία πράγματα που χρειάζεσαι περισσότερο",
		}},
		{"Φτιάξε μια πρώτη μικρή εκδοχή", []string{
			"Διάλεξε το μικρότερο πράγμα που μπορείς να κάνεις για τον στόχο",
			"Δούλεψέ το για 25 λεπτά",
			"Ολοκλήρωσε μια πρόχειρη πρώτη εκδοχή",
		}},
		{"Εξάσκηση και σχόλια", []string{
			"Δείξε την πρώτη εκδοχή σε έναν άνθρωπο",
			"Γράψε ένα πράγμα που θα βελτιώσεις",
			"Βελτίωσε αυτό το ένα πράγμα",
		}},
		{"Ολοκλήρωσε και μοιράσου το", []string{
			"Αποφάσισε τι σημαίνει «τελείωσε» για αυτόν τον στόχο",
			"Ολοκλήρωσε το τελευταίο κομμάτι που λείπει",
			"Μοιράσου το αποτέλεσμα",
		}},
	},
}
