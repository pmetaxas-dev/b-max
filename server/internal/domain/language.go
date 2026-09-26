package domain

import "unicode"

// The app speaks the language the user wrote the goal in: the planner writes
// the plan in it and the extension shows its messages in it.
const (
	LangGreek   = "el"
	LangEnglish = "en"
)

// GoalLanguage is Greek when at least a quarter of the goal's letters are
// Greek: Greek goals often carry Latin-script names ("Να μάθω Python και
// TypeScript"), while an English goal rarely has more than a stray symbol
// ("the μ-law"). Everything else is English.
func GoalLanguage(goal string) string {
	greek, latin := 0, 0
	for _, r := range goal {
		switch {
		case unicode.Is(unicode.Greek, r):
			greek++
		case unicode.Is(unicode.Latin, r):
			latin++
		}
	}
	if greek > 0 && greek*3 >= latin {
		return LangGreek
	}
	return LangEnglish
}

// Language is the language of the active goal (English before onboarding).
func (s *State) Language() string {
	if s.AppLang == LangGreek || s.AppLang == LangEnglish {
		return s.AppLang
	}
	if s.Goal == nil {
		if s.LifeGoal != "" {
			return GoalLanguage(s.LifeGoal)
		}
		return LangEnglish
	}
	return GoalLanguage(s.Goal.Statement)
}
