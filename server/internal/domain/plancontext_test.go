package domain

import (
	"errors"
	"reflect"
	"testing"
)

// Regression: the real plan from manual testing ("To write one paragraph").
func TestSimilarStepsCatchesTheRepeatedPlan(t *testing.T) {
	done := []string{
		"Brainstorm a clear topic for the paragraph",
		"Create a brief outline with main point and supporting detail",
		"Gather any necessary facts or examples",
		"Draft the first sentence introducing the main idea",
		"Write the remaining sentences and conclude",
	}
	for _, repeated := range []string{"Brainstorm ideas for the paragraph", "Outline the main point and supporting sentence"} {
		if err := CheckNotRepeated([]RawStep{{Text: repeated}}, done); !errors.Is(err, ErrInvalidPlan) {
			t.Errorf("%q not caught as a repeat", repeated)
		}
	}
	for _, fresh := range []string{"Read the paragraph aloud and fix awkward sentences", "Ask a friend for feedback", "Write a rough first draft of the paragraph"} {
		if err := CheckNotRepeated([]RawStep{{Text: fresh}}, done); err != nil {
			t.Errorf("%q wrongly flagged: %v", fresh, err)
		}
	}
}

func TestSimilarStepsNumbersAndLanguages(t *testing.T) {
	if SimilarSteps("Read chapter 1 of the Python tutorial", "Read chapter 2 of the Python tutorial") {
		t.Error("different chapters are different steps")
	}
	if !SimilarSteps("Διάβασε το κεφάλαιο για τις μεταβλητές", "Διάβασε ξανά το κεφάλαιο για μεταβλητές") {
		t.Error("Greek near-duplicate not caught")
	}
	if SimilarSteps("", "anything") {
		t.Error("empty text is never similar")
	}
}

func TestPlanContextFor(t *testing.T) {
	s := &State{
		Milestones: []Milestone{{ID: "m1", Title: "Plan"}, {ID: "m2", Title: "Write"}, {ID: "m3", Title: "Polish"}},
		Steps: []Step{
			{MilestoneID: "m1", Text: "Pick a topic", Status: StatusDone},
			{MilestoneID: "m1", Text: "Outline", Status: StatusDone},
			{MilestoneID: "m2", Text: "Not done yet", Status: StatusPending},
		},
	}
	got := s.PlanContextFor(s.Milestones[1])
	want := PlanContext{Milestones: []string{"Plan", "Write", "Polish"}, Current: "Write", DoneSteps: []string{"Pick a topic", "Outline"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}
