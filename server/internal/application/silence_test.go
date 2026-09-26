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

func silenceRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	r.onboard()
	r.evUnacknowledged(0, domain.EvTabActivated, work, true)
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	expect(t, r.ev(6*time.Minute, domain.EvHeartbeat, yt, true), domain.CmdShowRamp, "")
	return r
}

func chooseSilence(t *testing.T, r *rig, choice string) application.RampResult {
	t.Helper()
	res, err := r.app.RespondRamp(application.RampInput{Site: "youtube.com", Choice: choice})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSilenceNotTodayAllCommandsAndExplicitActions(t *testing.T) {
	r := silenceRig(t)
	before, err := r.repo.Load()
	if err != nil {
		t.Fatal(err)
	}
	res := chooseSilence(t, r, domain.ChoiceNotToday)
	want := &application.DismissUnsolicited{Commands: []domain.Command{domain.CmdShowRamp, domain.CmdAskCompletion, domain.CmdAskDay}}
	if !reflect.DeepEqual(res.DismissUnsolicited, want) {
		t.Fatalf("dismissal: %+v", res)
	}
	expect(t, r.ev(0, domain.EvHeartbeat, yt, true), domain.CmdDoNothing, "not_today")
	expect(t, r.ev(0, domain.EvTabActivated, rd, true), domain.CmdDoNothing, "not_today")
	expect(t, r.ev(0, domain.EvTabActivated, work, true), domain.CmdDoNothing, "not_today")
	for i := 0; i < 9; i++ {
		expect(t, r.ev(time.Minute, domain.EvHeartbeat, work, true), domain.CmdDoNothing, "not_today")
	}
	stepID := before.CurrentStep().ID
	if cue, err := r.app.StartCue(application.StartCueInput{StepID: stepID}); err != nil || cue.StepID != stepID || cue.ProgressWeight != 0 {
		t.Fatalf("explicit cue blocked: %+v %v", cue, err)
	}
	for _, kind := range []string{"idea", "task"} {
		if _, err := r.app.Capture(application.CaptureInput{Type: kind, Text: "save for later"}); err != nil {
			t.Fatal(err)
		}
	}
	after, err := r.repo.Load()
	if err != nil {
		t.Fatal(err)
	}
	if after.Pending == nil || !after.Pending.ShownAt.IsZero() {
		t.Fatal("suppressed completion was lost or shown")
	}
	if after.OpenCard != nil {
		t.Fatalf("a card is still open after not today: %+v", after.OpenCard)
	}
	if !reflect.DeepEqual(before.Goal, after.Goal) || !reflect.DeepEqual(before.Steps, after.Steps) || !reflect.DeepEqual(before.Milestones, after.Milestones) || before.Progress != after.Progress || before.Day(r.clk.t).ConfirmedSteps != after.Day(r.clk.t).ConfirmedSteps {
		t.Fatal("silence or capture changed goal, steps, confirmed progress or era")
	}
	if len(after.Ideas) != 2 {
		t.Fatal("capture unavailable")
	}
	if s := r.app.Summary(); s.Pending == nil || s.CurrentStep.ID != stepID {
		t.Fatal("explicit summary lost current step/question")
	}
	if f := r.app.Full(); f.Pending == nil || f.CurrentStep.ID != stepID {
		t.Fatal("explicit dashboard blocked")
	}
	if _, err := r.app.RespondStep(context.Background(), application.StepInput{StepID: stepID, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}
	if r.app.Summary().Progress.Percent == 0 {
		t.Fatal("explicit confirmation blocked")
	}
}

func TestSilenceBrowseScopeAndUnrelatedSites(t *testing.T) {
	r := silenceRig(t)
	res := chooseSilence(t, r, domain.ChoiceBrowse)
	if res.DismissUnsolicited == nil || res.DismissUnsolicited.Site != "youtube.com" || len(res.DismissUnsolicited.Commands) != 3 {
		t.Fatalf("scope: %+v", res)
	}
	expect(t, r.ev(0, domain.EvHeartbeat, yt, true), domain.CmdDoNothing, "browse_today")
	expect(t, r.ev(0, domain.EvTabActivated, "https://m.youtube.com/watch?v=2", true), domain.CmdDoNothing, "browse_today")
	expect(t, r.ev(0, domain.EvTabActivated, work, true), domain.CmdDoNothing, "working")
	r.ev(time.Second, domain.EvTabActivated, rd, true)
	expect(t, r.ev(6*time.Minute, domain.EvHeartbeat, rd, true), domain.CmdShowRamp, "")
	r.ev(0, domain.EvTabActivated, work, true)
	var d application.Decision
	for i := 0; i < 9; i++ {
		d = r.ev(time.Minute, domain.EvHeartbeat, work, true)
		if d.Command == domain.CmdAskCompletion {
			break
		}
	}
	expect(t, d, domain.CmdAskCompletion, "")
	if _, err := r.app.StartCue(application.StartCueInput{StepID: d.Payload.Step.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.app.Capture(application.CaptureInput{Type: "task", Text: "later"}); err != nil {
		t.Fatal(err)
	}
}

func TestSilencePreservesExistingConfirmedProgressAndWorld(t *testing.T) {
	for _, choice := range []string{domain.ChoiceNotToday, domain.ChoiceBrowse} {
		t.Run(choice, func(t *testing.T) {
			r := silenceRig(t)
			r.ev(0, domain.EvTabActivated, work, true)
			for i := 0; i < 8; i++ {
				r.ev(time.Minute, domain.EvHeartbeat, work, true)
			}
			if _, err := r.app.RespondStep(context.Background(), application.StepInput{StepID: r.app.Summary().CurrentStep.ID, Answer: "yes"}); err != nil {
				t.Fatal(err)
			}
			before, err := r.repo.Load()
			if err != nil {
				t.Fatal(err)
			}
			before.Progress.Era = "existing-era"
			if err := r.repo.Save(before); err != nil {
				t.Fatal(err)
			}
			r.app, err = application.New(r.repo, r.clk, r.pl, application.NoShield{})
			if err != nil {
				t.Fatal(err)
			}
			world := r.app.Summary().Planet
			chooseSilence(t, r, choice)
			for i := 0; i < 8; i++ {
				r.ev(time.Minute, domain.EvHeartbeat, work, true)
			}
			after, err := r.repo.Load()
			if err != nil {
				t.Fatal(err)
			}
			if before.Progress.ConfirmedWeight == 0 || before.Day(r.clk.t).ConfirmedSteps != 1 {
				t.Fatal("test requires confirmed progress")
			}
			if !reflect.DeepEqual(before.Goal, after.Goal) || !reflect.DeepEqual(before.Steps, after.Steps) || !reflect.DeepEqual(before.Milestones, after.Milestones) || before.Progress != after.Progress || before.Day(r.clk.t).ConfirmedSteps != after.Day(r.clk.t).ConfirmedSteps || !reflect.DeepEqual(world, r.app.Full().Planet) {
				t.Fatal("silence changed goal/current step, confirmed progress or world")
			}
		})
	}
}

// (There used to be a greeting sub-test too; there is no greeting card now.)
func TestSilenceInvalidatesIssuedAcknowledgementsEvenAfterUndo(t *testing.T) {
	for _, kind := range []string{"ramp"} {
		t.Run(kind, func(t *testing.T) {
			r := silenceRig(t)
			// Answer the rig's open YouTube screen so a second site can get its own.
			chooseSilence(t, r, domain.ChoiceContinue)
			url := rd
			r.evUnacknowledged(0, domain.EvTabActivated, rd, true)
			d := r.evUnacknowledged(6*time.Minute, domain.EvHeartbeat, rd, true)
			if d.Command != domain.CmdShowRamp || d.Payload.DeliveryID == "" {
				t.Fatalf("no issued delivery: %+v", d)
			}
			chooseSilence(t, r, domain.ChoiceNotToday)
			ack := application.DeliveryAck{DeliveryID: d.Payload.DeliveryID, TabID: 7, URL: url}
			if err := r.app.AcknowledgeDelivery(ack); !errors.Is(err, application.ErrNoPending) {
				t.Fatalf("silenced ack: %v", err)
			}
			chooseSilence(t, r, domain.ChoiceUndo)
			if err := r.app.AcknowledgeDelivery(ack); !errors.Is(err, application.ErrNoPending) {
				t.Fatalf("old ack after undo: %v", err)
			}
			fresh := r.evUnacknowledged(0, domain.EvHeartbeat, url, true)
			if fresh.Command != d.Command || fresh.Payload.DeliveryID == d.Payload.DeliveryID {
				t.Fatal("eligibility consumed or old command replayed")
			}
			r.ack(fresh, url)
			r.ack(fresh, url)
		})
	}
}

func TestSilenceLocalMidnightRestoresEligibilityAndExpiresPending(t *testing.T) {
	for _, choice := range []string{domain.ChoiceNotToday, domain.ChoiceBrowse} {
		t.Run(choice, func(t *testing.T) {
			r := newRig(t)
			r.clk.t = time.Date(2026, 9, 18, 23, 40, 0, 0, time.FixedZone("App local", 3*60*60))
			r.onboard()
			r.evUnacknowledged(0, domain.EvTabActivated, work, true)
			r.ev(time.Minute, domain.EvTabActivated, yt, true)
			r.ev(6*time.Minute, domain.EvHeartbeat, yt, true)
			chooseSilence(t, r, choice)
			r.ev(0, domain.EvTabActivated, work, true)
			for i := 0; i < 9; i++ {
				r.ev(time.Minute, domain.EvHeartbeat, work, true)
			}
			if r.app.Summary().Pending == nil {
				t.Fatal("missing persistent pending question")
			}
			r.clk.t = time.Date(2026, 9, 19, 0, 0, 0, 0, r.clk.t.Location())
			// UTC is still the previous date: only App Clock's local midnight matters.
			// Yesterday's still-open question is taken down, nothing new shows.
			if d := r.ev(0, domain.EvTabActivated, work, true); d.Command != domain.CmdDoNothing && d.Command != domain.CmdCloseCard {
				t.Fatalf("after midnight: %s (%s)", d.Command, d.Reason)
			}
			if r.app.Summary().Pending != nil {
				t.Fatal("expired question replayed")
			}
			r.ev(time.Minute, domain.EvTabActivated, yt, true)
			expect(t, r.ev(6*time.Minute, domain.EvHeartbeat, yt, true), domain.CmdShowRamp, "")
			r.ev(0, domain.EvTabActivated, work, true)
			var d application.Decision
			for i := 0; i < 9; i++ {
				d = r.ev(time.Minute, domain.EvHeartbeat, work, true)
				if d.Command == domain.CmdAskCompletion {
					break
				}
			}
			expect(t, d, domain.CmdAskCompletion, "")
		})
	}
}
