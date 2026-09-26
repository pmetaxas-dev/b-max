package groq

import (
	"fmt"
	"unicode"

	"focuscompanion/internal/domain"
)

// The AI answers in the language the user wrote the goal in. It is detected
// here, deterministically, from the goal text itself, and every generated
// title and step is checked against it: a mixed answer is rejected and goes
// through the planner's single repair question like any other invalid plan.
const (
	langGreek   = domain.LangGreek
	langEnglish = domain.LangEnglish
)

var languageNames = map[string]string{langGreek: "Greek", langEnglish: "English"}

// goalLanguage: the detection lives in the domain, shared with the extension.
func goalLanguage(goal string) string { return domain.GoalLanguage(goal) }

// inLanguage reports whether generated text is written in lang. Greek text
// must contain Greek letters (names such as Python may stay in Latin script);
// English text must contain none.
func inLanguage(text, lang string) bool {
	hasGreek := false
	for _, r := range text {
		if unicode.Is(unicode.Greek, r) {
			hasGreek = true
			break
		}
	}
	if lang == langGreek {
		return hasGreek
	}
	return !hasGreek
}

func checkStepsLanguage(steps []domain.RawStep, lang string) error {
	for _, s := range steps {
		if !inLanguage(s.Text, lang) {
			return fmt.Errorf("%w: step %q is not written in %s", domain.ErrInvalidPlan, s.Text, languageNames[lang])
		}
	}
	return nil
}

func checkPlanLanguage(plan domain.RawPlan, lang string) error {
	for _, m := range plan.Milestones {
		if !inLanguage(m.Title, lang) {
			return fmt.Errorf("%w: milestone %q is not written in %s", domain.ErrInvalidPlan, m.Title, languageNames[lang])
		}
		if err := checkStepsLanguage(m.Steps, lang); err != nil {
			return err
		}
	}
	return nil
}
