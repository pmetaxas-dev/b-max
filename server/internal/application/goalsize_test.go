package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"focuscompanion/internal/application"
	"focuscompanion/internal/domain"
)

// A goal the planner estimates as a task ("write one paragraph") would take
// the planet to its last era in an afternoon: the user is asked first.
func TestASmallGoalIsRefusedUntilTheUserConfirms(t *testing.T) {
	r := newRig(t)
	r.pl.hours = 1
	in := application.OnboardInput{Goal: "write one paragraph"}
	err := r.app.Onboard(context.Background(), in)
	if !errors.Is(err, application.ErrGoalTooSmall) || !strings.Contains(err.Error(), "about 1 hours") {
		t.Fatalf("got %v", err)
	}
	if r.app.Summary().Onboarded {
		t.Fatal("a refused goal must not be stored")
	}
	calls := r.pl.calls
	in.AllowSmall = true // "continue anyway"
	if err := r.app.Onboard(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if r.pl.calls != calls {
		t.Fatal("continue anyway must reuse the plan already made, not ask the planner again")
	}
	st, _ := r.repo.Load()
	if st.Goal == nil || st.Goal.EstimatedHours != 1 {
		t.Fatalf("goal not stored with its estimate: %+v", st.Goal)
	}
}

func TestABigGoalOrAnUnknownSizeIsNotAsked(t *testing.T) {
	for _, hours := range []float64{0, 8, 400} {
		r := newRig(t)
		r.pl.hours = hours
		if err := r.app.Onboard(context.Background(), application.OnboardInput{Goal: "become a developer"}); err != nil {
			t.Fatalf("%v hours: %v", hours, err)
		}
	}
}

func TestContinueAnywayForADifferentGoalAsksThePlannerAgain(t *testing.T) {
	r := newRig(t)
	r.pl.hours = 2
	_ = r.app.Onboard(context.Background(), application.OnboardInput{Goal: "write one paragraph"})
	calls := r.pl.calls
	if err := r.app.Onboard(context.Background(), application.OnboardInput{Goal: "write two paragraphs", AllowSmall: true}); err != nil {
		t.Fatal(err)
	}
	if r.pl.calls != calls+1 {
		t.Fatal("a different goal must get its own plan")
	}
}

// The next milestone's steps are planned with every milestone and the steps
// already done (the fix for repeated steps).
func TestNextMilestoneIsPlannedWithWhatWasDone(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.ev(0, domain.EvTabActivated, work, true)
	for range 2 {
		var d application.Decision
		for i := 0; i < 20 && d.Command != domain.CmdAskCompletion; i++ {
			d = r.ev(time.Minute, domain.EvHeartbeat, work, true)
		}
		if _, err := r.app.RespondStep(context.Background(), application.StepInput{StepID: d.Payload.Step.ID, Answer: "yes"}); err != nil {
			t.Fatal(err)
		}
	}
	p := r.pl.lastPlan
	if p.Current != "Polish" || strings.Join(p.Milestones, ",") != "Draft,Polish" || strings.Join(p.DoneSteps, ",") != "write the intro,write the outline" {
		t.Fatalf("plan context %+v", p)
	}
}
