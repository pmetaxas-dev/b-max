package application_test

import (
	"context"
	"testing"
	"time"

	"focuscompanion/internal/application"
	"focuscompanion/internal/domain"
)

// After "not yet", "did you finish?" returns after more WORK on the step, not
// after clock time (someone with ADHD may finish and forget to say so), gently,
// and at most three times a day per step.

func askedOnce(t *testing.T, r *rig) string {
	t.Helper()
	r.ev(0, domain.EvTabActivated, work, true)
	var d application.Decision
	for i := 0; i < 20 && d.Command != domain.CmdAskCompletion; i++ {
		d = r.ev(time.Minute, domain.EvHeartbeat, work, true)
	}
	expect(t, d, domain.CmdAskCompletion, "")
	if d.Payload.Gentle {
		t.Fatal("the first question is the normal card")
	}
	return d.Payload.Step.ID
}

func notYet(t *testing.T, r *rig, id string) {
	t.Helper()
	if _, err := r.app.RespondStep(context.Background(), application.StepInput{StepID: id, Answer: "not_yet"}); err != nil {
		t.Fatal(err)
	}
}

func TestTimeWithoutWorkNeverBringsTheQuestionBack(t *testing.T) {
	r := newRig(t)
	r.onboard()
	id := askedOnce(t, r)
	notYet(t, r, id)
	// Hours pass: away, locked, another app, a distraction site. No work.
	for range 60 {
		r.clk.add(time.Minute)
		_, _ = r.app.HandleEvent(application.Event{Type: domain.EvActivityChanged, TabID: 7, URL: work, Focused: true, Activity: domain.ActivityIdle})
	}
	for range 60 {
		r.ev(time.Minute, domain.EvHeartbeat, work, false)
	}
	for range 60 {
		if d := r.ev(time.Minute, domain.EvHeartbeat, rd, true); d.Command == domain.CmdAskCompletion {
			t.Fatal("asked again without any work on the step")
		}
	}
	// Back to work: it returns after 20 minutes of it.
	r.ev(0, domain.EvTabActivated, work, true)
	var d application.Decision
	minutes := 0
	for d.Command != domain.CmdAskCompletion && minutes < 30 {
		d = r.ev(time.Minute, domain.EvHeartbeat, work, true)
		minutes++
	}
	// 19 or 20: the minute right after "not yet" was still active work on the
	// step (before going idle) and counts.
	if d.Command != domain.CmdAskCompletion || minutes < 19 || minutes > 20 || !d.Payload.Gentle {
		t.Fatalf("returned after %d minutes of work (gentle %v)", minutes, d.Payload.Gentle)
	}
}

func TestAskedAtMostThreeTimesAStep(t *testing.T) {
	r := newRig(t)
	r.onboard()
	id := askedOnce(t, r)
	asks := 1
	for range 5 {
		notYet(t, r, id)
		for range 25 {
			if d := r.ev(time.Minute, domain.EvHeartbeat, work, true); d.Command == domain.CmdAskCompletion && d.Reason != "card_open" {
				asks++
				break
			}
		}
	}
	if asks != domain.MaxCompletionAsks {
		t.Fatalf("asked %d times, want %d", asks, domain.MaxCompletionAsks)
	}
	// The popup still has the question, so "yes" is always possible.
	if s := r.app.Summary(); s.Pending == nil {
		t.Fatal("the question must stay answerable from the popup")
	}
}

func TestAnIgnoredQuestionIsSetAsideLikeNotYet(t *testing.T) {
	r := newRig(t)
	r.onboard()
	askedOnce(t, r)
	var closed bool
	for range 5 { // ignored for 5 active minutes: the card closes
		if d := r.ev(time.Minute, domain.EvHeartbeat, work, true); d.Command == domain.CmdCloseCard {
			closed = true
		}
	}
	if !closed {
		t.Fatal("the ignored question did not close")
	}
	for range 14 { // 20 minutes of work counted from when it was set aside
		if d := r.ev(time.Minute, domain.EvHeartbeat, work, true); d.Command == domain.CmdAskCompletion {
			t.Fatal("an ignored question came straight back")
		}
	}
}
