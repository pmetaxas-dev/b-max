package application_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"focuscompanion/internal/application"
	"focuscompanion/internal/domain"
)

func TestStartCueLeavesStateAndCompletionUnchanged(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.pl.down = true
	r.ev(0, domain.EvTabActivated, work, true)
	id := r.app.Summary().CurrentStep.ID
	check := func() {
		t.Helper()
		before := r.app.Full()
		stored, err := r.repo.Load()
		if err != nil {
			t.Fatal(err)
		}
		calls := r.pl.calls
		for i := 0; i < 2; i++ {
			cue, err := r.app.StartCue(application.StartCueInput{StepID: id})
			if err != nil || cue.StepID != id || cue.Text != "Read just the current step once." || cue.ProgressWeight != 0 {
				t.Fatalf("cue = %+v, error = %v", cue, err)
			}
		}
		if r.pl.calls != calls {
			t.Fatal("cue called the AI planner")
		}
		if !reflect.DeepEqual(before, r.app.Full()) {
			t.Fatal("cue changed parent, progress, milestone, era/world or completion view")
		}
		after, err := r.repo.Load()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(stored, after) {
			t.Fatal("cue changed persisted state")
		}
	}
	check()
	for minute := 1; minute <= 8; minute++ {
		d := r.ev(time.Minute, domain.EvHeartbeat, work, true)
		if (d.Command == domain.CmdAskCompletion) != (minute == 8) {
			t.Fatalf("completion threshold changed at minute %d: %s", minute, d.Command)
		}
		check()
	}
	if _, err := r.app.RespondStep(context.Background(), application.StepInput{StepID: id, Answer: "not_yet"}); err != nil {
		t.Fatal(err)
	}
	check()
	for minute := 1; minute < 20; minute++ { // "not yet": back after 20 more minutes of work
		if d := r.ev(time.Minute, domain.EvHeartbeat, work, true); d.Command == domain.CmdAskCompletion {
			t.Fatal("cue changed when the question comes back")
		}
	}
	expect(t, r.ev(time.Minute, domain.EvHeartbeat, work, true), domain.CmdAskCompletion, "")
	if _, err := r.app.RespondStep(context.Background(), application.StepInput{StepID: id, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}
	if cue, err := r.app.StartCue(application.StartCueInput{StepID: id}); !errors.Is(err, application.ErrNoPending) || cue.Text != "" {
		t.Fatalf("completed parent accepted: %+v, %v", cue, err)
	}
	id = r.app.Summary().CurrentStep.ID
	check() // also preserve nonzero confirmed progress and the resulting world view
}

func TestStartCueRejectsMissingAndNonCurrentStep(t *testing.T) {
	r := newRig(t)
	if _, err := r.app.StartCue(application.StartCueInput{}); !errors.Is(err, application.ErrNoPending) {
		t.Fatal(err)
	}
	r.onboard()
	st, err := r.repo.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "stale", st.Steps[1].ID} {
		if cue, err := r.app.StartCue(application.StartCueInput{StepID: id}); !errors.Is(err, application.ErrNoPending) || cue.Text != "" {
			t.Fatalf("accepted %q: %+v, %v", id, cue, err)
		}
	}
}
