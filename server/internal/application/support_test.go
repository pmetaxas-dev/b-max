package application_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"focuscompanion/internal/application"
	"focuscompanion/internal/domain"
)

// untilCmd sends heartbeats on url, a minute apart, until cmd (at most n).
func (r *rig) untilCmd(url string, n int, cmd domain.Command) *application.Decision {
	r.t.Helper()
	for range n {
		if d := r.ev(time.Minute, domain.EvHeartbeat, url, true); d.Command == cmd {
			return &d
		}
	}
	return nil
}

// newDay moves the clock to 09:00 on the next day(s).
func (r *rig) newDay(days int) {
	now := r.clk.Now()
	r.clk.t = time.Date(now.Year(), now.Month(), now.Day()+days, 9, 0, 0, 0, time.Local)
}

// --- absence recovery ---------------------------------------------------------

func TestWelcomeBackAfterFiveDaysAwayWithoutFailureLanguage(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.ev(0, domain.EvTabActivated, work, true)
	r.newDay(4)
	if d := r.ev(time.Minute, domain.EvHeartbeat, work, true); d.Command == domain.CmdWelcomeBack {
		t.Fatal("4 days away is not an absence")
	}
	r.newDay(6)
	d := r.ev(time.Minute, domain.EvHeartbeat, work, true)
	if d.Command != domain.CmdWelcomeBack || d.Payload.Recovery == nil || d.Payload.Recovery.Step == nil || d.Payload.Recovery.Step.Text != "write the intro" {
		t.Fatalf("welcome back: %+v", d)
	}
	raw, _ := json.Marshal(d.Payload)
	for _, word := range []string{"days", "missed", "streak", "fail"} {
		if strings.Contains(strings.ToLower(string(raw)), word) {
			t.Fatalf("the welcome back mentions %q: %s", word, raw)
		}
	}
	// It is a card like the others: it follows, then is done for good.
	if again := r.ev(time.Minute, domain.EvHeartbeat, work, true); again.Command != domain.CmdWelcomeBack || again.Payload.DeliveryID != d.Payload.DeliveryID {
		t.Fatalf("did not follow: %+v", again)
	}
	if err := r.app.CloseCard(application.CardInput{DeliveryID: d.Payload.DeliveryID, Reason: "done"}); err != nil {
		t.Fatal(err)
	}
	for range 30 {
		if d := r.ev(time.Minute, domain.EvHeartbeat, work, true); d.Command == domain.CmdWelcomeBack {
			t.Fatal("welcomed back twice")
		}
	}
}

func TestRescheduleMovesOnlyUnfinishedSteps(t *testing.T) {
	s := &domain.State{Steps: []domain.Step{
		{ID: "a", Status: domain.StatusDone, ScheduledFor: "2026-09-10"},
		{ID: "b", Status: domain.StatusPending, ScheduledFor: "2026-09-11"},
		{ID: "c", Status: domain.StatusPending, ScheduledFor: "2026-09-14"},
		{ID: "d", Status: domain.StatusPending},
	}}
	if got := s.Reschedule(6); got != "2026-09-20" {
		t.Fatalf("new date %s", got)
	}
	if s.Steps[0].ScheduledFor != "2026-09-10" || s.Steps[1].ScheduledFor != "2026-09-17" || s.Steps[3].ScheduledFor != "" {
		t.Fatalf("steps: %+v", s.Steps)
	}
}

// The ⚙️ clock: "+5 days away" is an absence, like real days.
func TestAppClockMovesDaysForTheDemo(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.ev(0, domain.EvTabActivated, work, true)
	view, err := r.app.SetClock(application.ClockInput{AddDays: 5})
	if err != nil || view.OffsetDays != 5 || view.Today != "2026-09-23" {
		t.Fatalf("clock: %+v %v", view, err)
	}
	if d := r.ev(time.Minute, domain.EvHeartbeat, work, true); d.Command != domain.CmdWelcomeBack {
		t.Fatalf("no welcome back after +5 days: %+v", d)
	}
	if _, err := r.app.SetClock(application.ClockInput{Reset: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.app.SetClock(application.ClockInput{AddDays: 0}); err == nil {
		t.Fatal("zero days accepted")
	}
	if _, err := r.app.SetClock(application.ClockInput{AddDays: 91}); err == nil {
		t.Fatal("more than 90 days accepted")
	}
}

// --- difficult days -----------------------------------------------------------

func TestDifficultDaysEscalateQuietly(t *testing.T) {
	r := newRig(t)
	r.onboard()
	idea, _ := r.app.Capture(application.CaptureInput{Text: "film my desk setup", Type: "idea"})
	// Day 1: here, no step done. Nothing changes, not even the next day.
	r.ev(0, domain.EvTabActivated, work, true)
	r.newDay(1)
	r.ev(time.Minute, domain.EvHeartbeat, work, true)
	if s := r.app.Summary(); s.CurrentStep.Door == "" || s.CurrentStep.Text != "write the intro" {
		// Day 2 is the second difficult day in a row: the step is a door.
		t.Fatalf("day 2: %+v", s.CurrentStep)
	}
	if !strings.Contains(r.app.Summary().CurrentStep.Door, "write the intro") || strings.Contains(r.app.Summary().CurrentStep.Door, "day") && strings.Contains(r.app.Summary().CurrentStep.Door, "bad") {
		t.Fatalf("door text: %q", r.app.Summary().CurrentStep.Door)
	}
	// Day 3: the user's own old idea, once.
	r.newDay(1)
	d := r.untilCmd(work, 5, domain.CmdOfferIdea)
	if d == nil || d.Payload.Idea == nil || d.Payload.Idea.ID != idea.ID || d.Payload.Idea.DaysAgo != 2 {
		t.Fatalf("old idea: %+v", d)
	}
	if err := r.app.AnswerOldIdea(application.IdeaAnswer{ID: idea.ID, Accept: true}); err != nil {
		t.Fatal(err)
	}
	// Taking it breaks the zero: no door today.
	if s := r.app.Summary(); s.CurrentStep.Door != "" {
		t.Fatalf("door after taking the idea: %q", s.CurrentStep.Door)
	}
	for range 30 {
		if d := r.ev(time.Minute, domain.EvHeartbeat, work, true); d.Command == domain.CmdOfferIdea {
			t.Fatal("offered twice in a day")
		}
	}
}

func TestOneDifficultDayChangesNothingAndAStepEndsTheDoor(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, pyDocs, true)
	r.newDay(1)
	r.ev(time.Minute, domain.EvHeartbeat, pyDocs, true)
	if door := r.app.Summary().CurrentStep.Door; door == "" {
		t.Fatal("test setup: expected a door on the 2nd day")
	}
	// Yesterday had a step: a new difficult day is the first again.
	r.browse(pyDocs, 10)
	step := r.app.Summary().Pending.Step
	if _, err := r.app.RespondStep(context.Background(), application.StepInput{StepID: step.ID, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}
	if door := r.app.Summary().CurrentStep.Door; door != "" {
		t.Fatalf("door after a confirmed step: %q", door)
	}
	r.newDay(1)
	r.ev(time.Minute, domain.EvHeartbeat, pyDocs, true)
	if door := r.app.Summary().CurrentStep.Door; door != "" {
		t.Fatalf("door after a good day: %q", door)
	}
}

func TestDifficultStreakSkipsDaysAway(t *testing.T) {
	s := &domain.State{Days: map[string]*domain.DayRecord{
		"2026-09-15": {Date: "2026-09-15"},
		"2026-09-17": {Date: "2026-09-17"},
		"2026-09-14": {Date: "2026-09-14", ConfirmedSteps: 1},
	}}
	if n := s.DifficultStreakBefore("2026-09-18"); n != 2 {
		t.Fatalf("streak %d, want 2 (the 16th was a day away)", n)
	}
}

// --- lamp, treasure, voice, day summary ---------------------------------------

func TestLampCountsUnseenIdeasAndTheTreasureDimsIt(t *testing.T) {
	r := newRig(t)
	r.onboard()
	if l := r.app.Lamp(); l.Level != "off" || l.Unseen != 0 {
		t.Fatalf("empty lamp: %+v", l)
	}
	for i := range 11 {
		r.app.Capture(application.CaptureInput{Text: "idea " + string(rune('a'+i)), Type: "idea"})
	}
	r.app.Capture(application.CaptureInput{Text: "a task is not an idea", Type: "task"})
	if l := r.app.Lamp(); l.Level != "full" || l.Unseen != 11 {
		t.Fatalf("full lamp: %+v", l)
	}
	ideas := r.app.Ideas()
	if l, _ := r.app.ReviewIdeas(application.ReviewInput{IDs: []string{ideas[0].ID}}); l.Level != "medium" || l.Unseen != 10 {
		t.Fatalf("one seen: %+v", l)
	}
	if l, _ := r.app.ReviewIdeas(application.ReviewInput{}); l.Level != "off" {
		t.Fatalf("all seen: %+v", l)
	}
}

func TestTreasureOnceAWeekendBetweenWork(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.app.Capture(application.CaptureInput{Text: "a video idea", Type: "idea"})
	// Friday: never.
	if d := r.ev(time.Minute, domain.EvHeartbeat, work, true); d.Command == domain.CmdShowTreasure {
		t.Fatal("treasure on a weekday")
	}
	r.newDay(1) // Saturday
	d := r.ev(time.Minute, domain.EvHeartbeat, work, true)
	if d.Command != domain.CmdShowTreasure || d.Payload.Ideas != 1 {
		t.Fatalf("Saturday: %+v", d)
	}
	r.app.ReviewIdeas(application.ReviewInput{})
	r.app.Capture(application.CaptureInput{Text: "another idea", Type: "idea"})
	r.newDay(1) // Sunday, same week: not again
	for range 20 {
		if d := r.ev(time.Minute, domain.EvHeartbeat, work, true); d.Command == domain.CmdShowTreasure {
			t.Fatal("treasure twice in a weekend")
		}
	}
}

func TestVoiceCooldown(t *testing.T) {
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local)
	s := &domain.State{Goal: &domain.PrimaryGoal{VoiceRef: "goal-voice.webm"}}
	if !s.VoiceAllowed(now) {
		t.Fatal("never played: allowed")
	}
	s.VoicePlayedAt = now.AddDate(0, 0, -4)
	if s.VoiceAllowed(now) {
		t.Fatal("played 4 days ago: not yet")
	}
	s.VoicePlayedAt = now.AddDate(0, 0, -5)
	if !s.VoiceAllowed(now) {
		t.Fatal("played 5 days ago: allowed")
	}
	s.Goal.VoiceRef = ""
	if s.VoiceAllowed(now) {
		t.Fatal("no recording")
	}
}

func TestDaySummary(t *testing.T) {
	r := dayRig(t)
	r.browse(pyDocs, 10)
	task := r.task("buy milk")
	r.app.CompleteTask(application.TaskInput{ID: task.ID})
	r.app.Capture(application.CaptureInput{Text: "an idea", Type: "idea"})
	s := r.app.DaySummary()
	if s.Date != "2026-09-18" || s.Mode != domain.DayGood || s.RelevantMinutes < 8 || s.TasksDone != 1 || s.IdeasSaved != 2 {
		t.Fatalf("day summary: %+v", s)
	}
}

// --- Groq fallback and the seeded demo ------------------------------------------

func TestSimplePlanWhenTheAIIsDown(t *testing.T) {
	r := newRig(t)
	r.pl.down = true
	err := r.app.Onboard(context.Background(), application.OnboardInput{Goal: "Να μάθω κιθάρα"})
	if err == nil {
		t.Fatal("test setup: onboarding must fail without the AI")
	}
	calls := r.pl.calls
	if err := r.app.Onboard(context.Background(), application.OnboardInput{Goal: "Να μάθω κιθάρα", Fallback: true}); err != nil {
		t.Fatal(err)
	}
	if r.pl.calls != calls {
		t.Fatal("the simple plan called the AI")
	}
	f := r.app.Full()
	if len(f.Milestones) != 4 || f.CurrentStep == nil || !strings.Contains(f.Milestones[0].Title, "ξεκινάς") {
		t.Fatalf("simple plan: %+v", f.Milestones)
	}
	// The next milestone gets simple steps too while the AI is still down:
	// finish the first milestone but its last step, then answer "yes".
	st, _ := r.repo.Load()
	last := len(f.Milestones[0].Steps) - 1
	for i := range st.Steps[:last] {
		st.Steps[i].Status = domain.StatusDone
	}
	st.Pending = &domain.PendingPrompt{Type: domain.PromptCompletion, StepID: st.Steps[last].ID}
	r.repo.Save(st)
	app, _ := application.New(r.repo, r.clk, r.pl, application.NoShield{})
	res, err := app.RespondStep(context.Background(), application.StepInput{StepID: st.Steps[last].ID, Answer: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if res.CurrentStep == nil || res.NextStepsPending || !strings.Contains(res.CurrentStep.Milestone, "εκδοχή") {
		t.Fatalf("second milestone: %+v", res)
	}
}

func TestSeededDemo(t *testing.T) {
	r := newRig(t)
	r.pl.down = true
	if err := r.app.Seed(); err != nil {
		t.Fatal(err)
	}
	s := r.app.Summary()
	if !s.Onboarded || s.Goal != "Become a YouTuber" || s.CurrentStep == nil || s.Progress.Percent <= 0 || s.Planet.Era != "prehistoric" {
		t.Fatalf("seed: %+v", s)
	}
	if l := r.app.Lamp(); l.Unseen != 7 || l.Level != "medium" {
		t.Fatalf("lamp: %+v", l)
	}
	if tasks := r.app.Tasks(); len(tasks) != 1 || tasks[0].DoneAt != nil {
		t.Fatalf("tasks: %+v", tasks)
	}
	if s.CurrentStep.Door != "" || s.CurrentStep.Text != "Record a 1-minute test video with OBS Studio" {
		t.Fatalf("the seed starts on an ordinary day, on the OBS step: %+v", s.CurrentStep)
	}
	// A page about the goal counts (its keywords are seeded).
	if d := r.evTitle(time.Minute, "https://obsproject.com/kb/quick-start-guide", "OBS quick start"); d.Command != domain.CmdShowThumbsUp {
		t.Fatalf("relevant page: %+v", d)
	}
	// The demo's payoff: the task, then one confirmed step, reaches Copper.
	task := r.app.Tasks()[0]
	r.app.CompleteTask(application.TaskInput{ID: task.ID})
	if era := r.app.Summary().Planet.Era; era != "prehistoric" {
		t.Fatalf("the task alone changed the era: %s", era)
	}
	st, _ := r.repo.Load()
	st.Pending = &domain.PendingPrompt{Type: domain.PromptCompletion, StepID: st.CurrentStep().ID}
	r.repo.Save(st)
	app, _ := application.New(r.repo, r.clk, r.pl, application.NoShield{})
	if _, err := app.RespondStep(context.Background(), application.StepInput{StepID: st.CurrentStep().ID, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}
	if era := app.Summary().Planet.Era; era != "copper" {
		t.Fatalf("one step on stage should reach Copper, got %s (%.1f%%)", era, app.Summary().Progress.Percent)
	}
	r.app = app
	if d := r.evTitle(time.Minute, "https://obsproject.com/kb/quick-start-guide", "OBS quick start"); d.Command != domain.CmdShowNewEra || d.Payload.Era != "copper" {
		t.Fatalf("era announcement: %+v", d)
	}
}
