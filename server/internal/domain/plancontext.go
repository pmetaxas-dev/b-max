package domain

import (
	"fmt"
	"strings"
	"unicode"
)

// PlanContext is what the planner needs to write the next milestone's steps
// without repeating itself: every milestone in order, which one is being
// planned, and the steps already done. (Without it, "Write the first draft"
// began again with "Brainstorm ideas" and "Outline the main point".)
type PlanContext struct {
	Milestones []string `json:"milestones"`
	Current    string   `json:"currentMilestone"`
	DoneSteps  []string `json:"completedSteps"`
}

// PlanContextFor builds the context for planning milestone m.
func (s *State) PlanContextFor(m Milestone) PlanContext {
	c := PlanContext{Current: m.Title}
	for _, ms := range s.Milestones {
		c.Milestones = append(c.Milestones, ms.Title)
	}
	for _, st := range s.Steps {
		if st.Status == StatusDone {
			c.DoneSteps = append(c.DoneSteps, st.Text)
		}
	}
	return c
}

// Words that carry no meaning when comparing two steps.
var stepFillers = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "of": true, "for": true, "to": true,
	"in": true, "on": true, "with": true, "your": true, "my": true, "it": true, "any": true, "some": true,
	"και": true, "το": true, "τα": true, "τη": true, "την": true, "του": true, "της": true, "για": true,
	"με": true, "σε": true, "στο": true, "στη": true, "στην": true, "ένα": true, "ενα": true, "μια": true,
}

func stepWords(text string) (words map[string]bool, numbers string) {
	words = map[string]bool{}
	var nums []string
	for _, w := range strings.Fields(normalizeText(text)) {
		if stepFillers[w] {
			continue
		}
		if strings.IndexFunc(w, func(r rune) bool { return !unicode.IsDigit(r) }) < 0 {
			nums = append(nums, w)
			continue
		}
		words[w] = true
	}
	return words, strings.Join(nums, ",")
}

// SimilarSteps reports whether two step texts say nearly the same thing:
// at least 40% of their meaningful words shared ("Brainstorm ideas for the
// paragraph" / "Brainstorm a clear topic for the paragraph"). Steps that
// differ only by a number ("Read chapter 1" / "Read chapter 2") are different.
func SimilarSteps(a, b string) bool {
	wa, na := stepWords(a)
	wb, nb := stepWords(b)
	if na != nb || len(wa) == 0 || len(wb) == 0 {
		return false
	}
	shared := 0
	for w := range wa {
		if wb[w] {
			shared++
		}
	}
	union := len(wa) + len(wb) - shared
	return float64(shared) >= 0.4*float64(union)
}

// CheckNotRepeated rejects new steps that repeat a completed one, so the
// planner's single repair question can fix them.
func CheckNotRepeated(steps []RawStep, done []string) error {
	for _, s := range steps {
		for _, d := range done {
			if SimilarSteps(s.Text, d) {
				return fmt.Errorf("%w: step %q repeats the completed step %q", ErrInvalidPlan, s.Text, d)
			}
		}
	}
	return nil
}
