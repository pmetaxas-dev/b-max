package application

import (
	"fmt"
	"math"
	"strings"

	"focuscompanion/internal/domain"
)

// The spoken status: where the user is, in words, for anyone who cannot look
// at the screen. It needs no AI (it must always work and be instant): it is
// built from the tasks, the clock and the planet.

type StatusView struct {
	Text string `json:"text"`
	Lang string `json:"lang"`
}

var eraNames = map[string][5]string{
	domain.LangGreek:   {"Προϊστορική εποχή", "Εποχή του Χαλκού", "Μεσαιωνική εποχή", "Βιομηχανική εποχή", "Διαστημική εποχή"},
	domain.LangEnglish: {"Prehistoric era", "Copper era", "Medieval era", "Industrial era", "Space era"},
}

var eraScenes = map[string][5]string{
	domain.LangGreek: {
		"άγρια φύση και οι πρώτες φωτιές",
		"μικρά χωριά και τα πρώτα εργαλεία",
		"κάστρα και πόλεις",
		"εργοστάσια και σιδηρόδρομοι",
		"μια πόλη που φτάνει ως τα άστρα",
	},
	domain.LangEnglish: {
		"untamed wilderness and the first fires",
		"small villages and the first tools",
		"castles and towns",
		"factories and railways",
		"a city that reaches the stars",
	},
}

// EraDescription describes the planet's era in a few words, in lang.
func EraDescription(percent float64, lang string) string {
	return eraScenes[langKey(lang)][domain.EraIndexForPercent(percent)]
}

func eraName(percent float64, lang string) string {
	return eraNames[langKey(lang)][domain.EraIndexForPercent(percent)]
}

func langKey(lang string) string {
	if lang == domain.LangGreek {
		return lang
	}
	return domain.LangEnglish
}

// SpokenStatus is GET /status.
func (a *App) SpokenStatus() StatusView {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, now := a.st, a.now()
	lang := langKey(st.Language())
	el := lang == domain.LangGreek
	say := func(greek, english string) string {
		if el {
			return greek
		}
		return english
	}
	if !st.Profile.Onboarded {
		return StatusView{Lang: lang, Text: say(
			"Δεν έχουμε γνωριστεί ακόμα. Άνοιξε το B-MAX και πες μου ποιο είναι το όνειρό σου.",
			"We have not met yet. Open B-MAX and tell me your dream.")}
	}

	var parts []string
	open := st.OpenTasksByPriority(now)
	done := 0
	for _, i := range st.Ideas {
		if i.Type == "task" && i.DoneAt != nil && domain.DayKey(*i.DoneAt) == domain.DayKey(now) {
			done++
		}
	}
	if len(open) == 0 {
		parts = append(parts, say("Δεν έχεις ανοιχτά tasks αυτή τη στιγμή.", "You have no open tasks right now."))
	} else {
		cur := open[0]
		line := say("Τώρα: ", "Right now: ") + cur.Text + "."
		if cur.EstimateMin > 0 {
			line += say(fmt.Sprintf(" Περίπου %d λεπτά.", cur.EstimateMin), fmt.Sprintf(" About %d minutes.", cur.EstimateMin))
		}
		if cur.Deadline != "" {
			line += say(" Προθεσμία: ", " Deadline: ") + cur.Deadline + "."
		}
		switch {
		case cur.Status == domain.TaskDoing && cur.StartedAt != nil:
			m := int(now.Sub(*cur.StartedAt).Minutes())
			line += say(fmt.Sprintf(" Το ξεκίνησες πριν %d λεπτά.", m), fmt.Sprintf(" You started it %d minutes ago.", m))
		case cur.Status == domain.TaskPaused && cur.Note != "":
			line += say(" Το είχες σταματήσει. Είχες μείνει ", " You had set it aside. You left off ") + cur.Note + "."
		}
		parts = append(parts, line)
		if len(open) > 1 {
			var next []string
			for _, o := range open[1:min(len(open), 3)] {
				next = append(next, o.Text)
			}
			parts = append(parts, say(
				fmt.Sprintf("Μετά ακολουθούν %d: %s.", len(open)-1, strings.Join(next, ", ")),
				fmt.Sprintf("Next up, %d: %s.", len(open)-1, strings.Join(next, ", "))))
		}
		for _, o := range open {
			if o.DeadlineUrgent(now) && o.ID != cur.ID {
				parts = append(parts, say("Προσοχή: πλησιάζει η προθεσμία του «"+o.Text+"».", "Careful: the deadline of “"+o.Text+"” is close."))
				break
			}
		}
	}
	if done > 0 {
		parts = append(parts, say(fmt.Sprintf("Σήμερα έχεις ολοκληρώσει %d.", done), fmt.Sprintf("Today you have finished %d.", done)))
	}
	pct := st.Progress.Percentage
	planet := say(
		fmt.Sprintf("Ο πλανήτης σου: %s, στο %d%% του στόχου.", eraName(pct, lang), int(math.Round(pct))),
		fmt.Sprintf("Your planet: %s, %d%% of the way to your goal.", eraName(pct, lang), int(math.Round(pct))))
	if st.LifeGoal != "" {
		planet += say(" Ο στόχος σου: ", " Your goal: ") + st.LifeGoal + "."
	}
	planet += say(" Βλέπεις ", " It shows ") + EraDescription(pct, lang) + "."
	if st.BadDay(now) {
		planet += say(" Σήμερα έχει συννεφιά και κεραυνούς στον πλανήτη σου. Ένα μικρό βήμα, όσο μικρό κι αν είναι, καθαρίζει τον ουρανό.", " Today there are clouds and lightning over your planet. One small step, however small, clears the sky.")
	}
	parts = append(parts, planet)
	return StatusView{Lang: lang, Text: strings.Join(parts, " ")}
}
