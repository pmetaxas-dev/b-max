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

// The dashboard's ⚙️ test panel: fresh day, progress reset, profile reset,
// fast timings.

func TestResetDayGivesAFreshDayAndKeepsTheGoal(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, pyDocs, true)
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	expect(t, r.ev(6*time.Minute, domain.EvHeartbeat, yt, true), domain.CmdShowRamp, "")
	if err := r.app.Reset(application.ResetInput{Scope: application.ResetDay}); err != nil {
		t.Fatal(err)
	}
	st, _ := r.repo.Load()
	if _, ok := st.Days[domain.DayKey(r.clk.t)]; ok || len(st.Interventions) != 0 || st.OpenCard != nil {
		t.Fatalf("today not cleared: days %v interventions %d card %+v", st.Days, len(st.Interventions), st.OpenCard)
	}
	if st.Session.State != domain.StateIdle || st.WorkContext.LastRelevantURL != "" {
		t.Fatalf("session/work context not fresh: %+v %+v", st.Session.State, st.WorkContext)
	}
	if st.Goal == nil || len(st.Steps) == 0 {
		t.Fatal("the goal and plan must stay")
	}
	// The return screen is available again today, with its hello.
	r.ev(time.Minute, domain.EvTabActivated, pyDocs, true)
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	d := r.ev(6*time.Minute, domain.EvHeartbeat, yt, true)
	expect(t, d, domain.CmdShowRamp, "")
	if !d.Payload.Greeting {
		t.Fatal("a fresh day's first card says hello again")
	}
}

func TestResetProgressKeepsThePlan(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, pyDocs, true)
	r.browse(pyDocs, 10)
	step := r.app.Summary().Pending.Step
	if _, err := r.app.RespondStep(context.Background(), application.StepInput{StepID: step.ID, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}
	if r.app.Summary().Planet.Era != "copper" {
		t.Fatal("test setup: expected Copper after one step")
	}
	// A done task's bonus is progress too, and is reset with it.
	task := r.task("buy milk")
	if _, err := r.app.CompleteTask(application.TaskInput{ID: task.ID}); err != nil {
		t.Fatal(err)
	}
	if err := r.app.Reset(application.ResetInput{Scope: application.ResetProgress}); err != nil {
		t.Fatal(err)
	}
	s := r.app.Summary()
	if s.Progress.Percent != 0 || s.Planet.Era != "prehistoric" || s.CurrentStep == nil || s.CurrentStep.Text != "write the intro" || s.Pending != nil {
		t.Fatalf("progress not reset: %+v", s)
	}
	// Reaching Copper again is announced again.
	r.browse(pyDocs, 10)
	step = r.app.Summary().Pending.Step
	if _, err := r.app.RespondStep(context.Background(), application.StepInput{StepID: step.ID, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}
	expect(t, r.ev(time.Minute, domain.EvHeartbeat, pyDocs, true), domain.CmdShowNewEra, "")
}

func TestResetProfileStartsOnboardingAgain(t *testing.T) {
	r := dayRig(t)
	r.app.SetTimings(application.TimingsInput{Fast: true})
	if err := r.app.Reset(application.ResetInput{Scope: application.ResetProfile}); err != nil {
		t.Fatal(err)
	}
	if s := r.app.Summary(); s.Onboarded || s.Goal != "" {
		t.Fatalf("profile not deleted: %+v", s)
	}
	st, _ := r.repo.Load()
	if !st.Settings.Fast || len(st.Blacklist) == 0 {
		t.Fatal("timings and blacklist must survive a profile reset")
	}
	expect(t, r.ev(0, domain.EvTabActivated, pyDocs, true), domain.CmdDoNothing, "onboarding_incomplete")
	r.onboard() // a new, smaller goal can be set up
}

func TestRegenerateStepsReplacesOnlyTheUndoneOnes(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, pyDocs, true)
	r.browse(pyDocs, 10)
	first := r.app.Summary().Pending.Step
	if _, err := r.app.RespondStep(context.Background(), application.StepInput{StepID: first.ID, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}
	before := r.app.Summary().Progress.Percent
	if err := r.app.RegenerateSteps(context.Background()); err != nil {
		t.Fatal(err)
	}
	full := r.app.Full()
	steps := full.Milestones[0].Steps
	if len(steps) != 2 || steps[0].Text != "write the intro" || steps[0].Status != domain.StatusDone || steps[1].Text != "proofread" || steps[1].Order != 2 {
		t.Fatalf("steps %+v", steps)
	}
	if full.CurrentStep == nil || full.CurrentStep.Text != "proofread" {
		t.Fatalf("current step %+v", full.CurrentStep)
	}
	if got := r.app.Summary().Progress.Percent; got != before {
		t.Fatalf("progress changed from %v to %v", before, got)
	}
	var sum float64
	for _, s := range steps {
		sum += s.Weight
	}
	if sum < full.Milestones[0].Weight-1e-9 || sum > full.Milestones[0].Weight+1e-9 {
		t.Fatalf("step weights %v do not add up to the milestone weight %v", sum, full.Milestones[0].Weight)
	}
	if strings.Join(r.pl.lastPlan.DoneSteps, ",") != "write the intro" {
		t.Fatalf("the planner was not told what was done: %+v", r.pl.lastPlan)
	}
}

func TestResetRejectsUnknownScope(t *testing.T) {
	r := dayRig(t)
	if err := r.app.Reset(application.ResetInput{Scope: "everything"}); !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestFastTimingsSwitch(t *testing.T) {
	r := dayRig(t)
	s := r.app.SetTimings(application.TimingsInput{Fast: true})
	if !s.Fast || s.GraceSeconds != 10 || s.DayAskAfterSeconds != 60 || s.CompletionThresholdPercent != 5 || s.CardIgnoreSeconds != 300 {
		t.Fatalf("fast timings %+v", s)
	}
	if !r.app.Full().FastTimings {
		t.Fatal("the full view must show the switch as on")
	}
	// With fast timings the return screen comes after 10 seconds.
	r.ev(0, domain.EvTabActivated, pyDocs, true)
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	expect(t, r.ev(11*time.Second, domain.EvHeartbeat, yt, true), domain.CmdShowRamp, "")
	s = r.app.SetTimings(application.TimingsInput{Fast: false})
	if s != domain.DefaultSettings() || s.GraceSeconds != 300 {
		t.Fatalf("normal timings %+v", s)
	}
	if r.app.Full().FastTimings {
		t.Fatal("switch still on")
	}
}

func TestStormSwitch(t *testing.T) {
	r := dayRig(t)
	if r.app.Full().StormForced {
		t.Fatal("storm must start off")
	}
	if on := r.app.SetStorm(application.StormInput{On: true}); !on {
		t.Fatal("SetStorm(true) must report on")
	}
	if !r.app.Full().StormForced {
		t.Fatal("the full view must show the switch as on")
	}
	if w := r.app.ChatState().Planet.Weather; w != domain.WeatherStorm {
		t.Fatalf("weather = %q, want storm", w)
	}
	if on := r.app.SetStorm(application.StormInput{On: false}); on {
		t.Fatal("SetStorm(false) must report off")
	}
	if r.app.Full().StormForced {
		t.Fatal("switch still on")
	}
	if w := r.app.ChatState().Planet.Weather; w != domain.WeatherClear {
		t.Fatalf("weather = %q, want clear once switched off", w)
	}
}

func TestNextEraCyclesThroughAllFiveAndWrapsAround(t *testing.T) {
	r := dayRig(t)
	if era := r.app.ChatState().Planet.Era; era != domain.EraNames[0] {
		t.Fatalf("a fresh goal must start %q, got %q", domain.EraNames[0], era)
	}
	for i, want := range domain.EraNames[1:] {
		if got := r.app.NextEra(); got != want {
			t.Fatalf("NextEra() #%d = %q, want %q", i, got, want)
		}
		if got := r.app.ChatState().Planet.Era; got != want {
			t.Fatalf("shown era after NextEra() #%d = %q, want %q", i, got, want)
		}
	}
	// One more press after Space wraps back to the first era.
	if got := r.app.NextEra(); got != domain.EraNames[0] {
		t.Fatalf("NextEra() after Space = %q, want it to wrap to %q", got, domain.EraNames[0])
	}
}

// Reaching Space this way must be a real celebration too (the confetti
// moment), not just a label swap: the next browser event picks up the same
// CmdShowNewEra card an earned transition would open.
func TestNextEraToSpaceOpensTheRealNewEraCard(t *testing.T) {
	r := dayRig(t)
	last := domain.EraNames[len(domain.EraNames)-1]
	for era := r.app.NextEra(); era != last; era = r.app.NextEra() {
	}
	d := r.ev(time.Minute, domain.EvHeartbeat, pyDocs, true)
	expect(t, d, domain.CmdShowNewEra, "card_open")
	if d.Payload.Era != last {
		t.Fatalf("payload era = %q, want %q", d.Payload.Era, last)
	}
}
