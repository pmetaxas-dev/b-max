package application_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"focuscompanion/internal/application"
	"focuscompanion/internal/domain"
	"focuscompanion/internal/storage"
)

type manualClock struct{ t time.Time }

func (c *manualClock) Now() time.Time      { return c.t }
func (c *manualClock) add(d time.Duration) { c.t = c.t.Add(d) }

// fakePlanner stands in for Groq. Once down is set every call fails, which is
// what "Groq completely unreachable" looks like to the application.
type fakePlanner struct {
	down     bool
	calls    int
	keywords []string // returned with the outline, like Groq's "keywords"
	backfill []string // returned by Keywords (the background backfill); nil = fail
	hours    float64  // returned as the outline's estimatedHours (0: none)
	lastPlan domain.PlanContext

	keywordCalls atomic.Int32
}

func (p *fakePlanner) Outline(context.Context, string, time.Time) (domain.RawPlan, error) {
	p.calls++
	if p.down {
		return domain.RawPlan{}, errors.New("unreachable")
	}
	return domain.RawPlan{
		TargetHorizon: "2027-06",
		Milestones: []domain.RawMilestone{
			{Title: "Draft", Weight: 2, Steps: []domain.RawStep{
				{Text: "write the intro", EstimatedMinutes: 10},
				{Text: "write the outline", EstimatedMinutes: 10},
			}},
			{Title: "Polish", Weight: 1},
		},
		Keywords:       p.keywords,
		EstimatedHours: p.hours,
	}, nil
}

// Keywords answers only when a test sets backfill, so the heartbeat's
// background request never changes relevance in the middle of other tests.
// It has its own counter: `calls` proves plan/steps calls.
func (p *fakePlanner) Keywords(context.Context, string, []string) ([]string, error) {
	p.keywordCalls.Add(1)
	if p.backfill == nil {
		return nil, errors.New("keyword backfill not enabled in this test")
	}
	return p.backfill, nil
}

func (p *fakePlanner) StepsFor(_ context.Context, _ string, _ domain.Milestone, _ time.Time, plan domain.PlanContext) ([]domain.RawStep, error) {
	p.lastPlan = plan
	p.calls++
	if p.down {
		return nil, errors.New("unreachable")
	}
	return []domain.RawStep{{Text: "proofread", EstimatedMinutes: 10}}, nil
}

const (
	work = "https://docs.google.com/document/d/1"
	yt   = "https://www.youtube.com/watch?v=1"
	rd   = "https://www.reddit.com/r/all"
)

type rig struct {
	repo *storage.JSONRepo
	t    *testing.T
	app  *application.App
	clk  *manualClock
	pl   *fakePlanner
}

func newRig(t *testing.T) *rig {
	t.Helper()
	repo, err := storage.NewJSON(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clk := &manualClock{t: time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local)}
	pl := &fakePlanner{}
	app, err := application.New(repo, clk, pl, application.NoShield{})
	if err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, app: app, clk: clk, pl: pl, repo: repo}
}

func (r *rig) onboard() {
	r.t.Helper()
	err := r.app.Onboard(context.Background(), application.OnboardInput{
		Goal:        "finish my thesis",
		Preferences: domain.Preferences{WorkBlock: "45"},
	})
	if err != nil {
		r.t.Fatal(err)
	}
}

// ev models successful rendering for the existing core-loop tests.
func (r *rig) ev(after time.Duration, typ, url string, focused bool) application.Decision {
	d := r.evUnacknowledged(after, typ, url, focused)
	if d.Payload.DeliveryID != "" {
		r.ack(d, url)
	}
	return d
}

func (r *rig) ack(d application.Decision, url string) {
	r.t.Helper()
	if err := r.app.AcknowledgeDelivery(application.DeliveryAck{DeliveryID: d.Payload.DeliveryID, TabID: 7, URL: url}); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) evUnacknowledged(after time.Duration, typ, url string, focused bool) application.Decision {
	r.t.Helper()
	r.clk.add(after)
	d, err := r.app.HandleEvent(application.Event{Type: typ, TabID: 7, URL: url, Title: "t", Focused: focused, Activity: domain.ActivityActive})
	if err != nil {
		r.t.Fatal(err)
	}
	return d
}

func expect(t *testing.T, d application.Decision, cmd domain.Command, reason string) {
	t.Helper()
	if d.Command != cmd || (reason != "" && d.Reason != reason) {
		t.Fatalf("got %s/%q, want %s/%q", d.Command, d.Reason, cmd, reason)
	}
}

func TestNothingHappensBeforeOnboarding(t *testing.T) {
	r := newRig(t)
	expect(t, r.ev(0, domain.EvTabActivated, work, true), domain.CmdDoNothing, "onboarding_incomplete")
}

func TestOnboardingFailureStoresNothing(t *testing.T) {
	r := newRig(t)
	r.pl.down = true
	err := r.app.Onboard(context.Background(), application.OnboardInput{Goal: "finish my thesis"})
	if !errors.Is(err, application.ErrPlanUnavailable) {
		t.Fatalf("got %v", err)
	}
	if r.app.Summary().Onboarded {
		t.Fatal("a failed plan must not leave a half-onboarded state")
	}
}

func TestExactlyOneActiveGoal(t *testing.T) {
	r := newRig(t)
	r.onboard()
	err := r.app.Onboard(context.Background(), application.OnboardInput{Goal: "another goal"})
	if !errors.Is(err, application.ErrGoalExists) {
		t.Fatalf("got %v", err)
	}
}

func TestPlanNormalizationAndCurrentStep(t *testing.T) {
	r := newRig(t)
	r.onboard()
	f := r.app.Full()
	if f.CurrentStep == nil || f.CurrentStep.Text != "write the intro" {
		t.Fatalf("current step %+v", f.CurrentStep)
	}
	sum := 0.0
	for _, m := range f.Milestones {
		sum += m.Weight
	}
	if sum < 0.999 || sum > 1.001 {
		t.Fatalf("milestone weights sum to %v", sum)
	}
	if len(f.Milestones[0].Steps) != 2 || len(f.Milestones[1].Steps) != 0 {
		t.Fatal("only the first milestone should have steps")
	}
	if f.Milestones[0].Steps[0].Weight != f.Milestones[0].Weight/2 {
		t.Fatal("step weight is not the milestone weight divided evenly")
	}
}

func TestReturnScreenWorksWithGroqDown(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.pl.down = true // Groq completely unreachable from here on
	expect(t, r.ev(0, domain.EvTabActivated, work, true), domain.CmdDoNothing, "working")
	expect(t, r.ev(time.Minute, domain.EvTabActivated, yt, true), domain.CmdDoNothing, "grace_not_elapsed")
	expect(t, r.ev(4*time.Minute, domain.EvHeartbeat, yt, true), domain.CmdDoNothing, "grace_not_elapsed")
	d := r.ev(time.Minute+time.Second, domain.EvHeartbeat, yt, true)
	expect(t, d, domain.CmdShowRamp, "")
	if d.Payload.Step == nil || d.Payload.Step.Text != "write the intro" || d.Payload.Site != "youtube.com" {
		t.Fatalf("payload %+v", d.Payload)
	}
	if d.Payload.CaptureAnchor {
		t.Fatal("anchor capture must be off on a blacklisted page")
	}
}

func TestRampOncePerSitePerDayAndAliases(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.ev(0, domain.EvTabActivated, work, true)
	r.ev(time.Minute, domain.EvTabActivated, "https://twitter.com/home", true)
	first := r.ev(6*time.Minute, domain.EvHeartbeat, "https://twitter.com/home", true)
	expect(t, first, domain.CmdShowRamp, "")
	// The unanswered screen follows the user (same card, never a second one),
	// here to x.com, the same product.
	d := r.ev(time.Minute, domain.EvHeartbeat, "https://x.com/home", true)
	expect(t, d, domain.CmdShowRamp, "card_open")
	if d.Payload.DeliveryID != first.Payload.DeliveryID || d.Payload.Site != "x.com" {
		t.Fatalf("followed card %+v", d.Payload)
	}
	// Ignored for 5 active minutes: it closes, and is not re-armed today.
	var closed bool
	for range 5 {
		if r.ev(time.Minute, domain.EvHeartbeat, "https://x.com/home", true).Command == domain.CmdCloseCard {
			closed = true
		}
	}
	if !closed {
		t.Fatal("the ignored screen was never closed")
	}
	expect(t, r.ev(time.Minute, domain.EvTabUpdated, "https://twitter.com/other", true), domain.CmdDoNothing, "already_shown_today")
}

func TestNoRampWithoutWorkContext(t *testing.T) {
	r := newRig(t)
	r.onboard()
	expect(t, r.ev(0, domain.EvTabActivated, yt, true), domain.CmdDoNothing, "no_work_context")
	expect(t, r.ev(10*time.Minute, domain.EvHeartbeat, yt, true), domain.CmdDoNothing, "no_work_context")
}

func TestSessionTimeMetSuppressesRamp(t *testing.T) {
	r := newRig(t)
	r.onboard() // work block: 45 minutes
	r.ev(0, domain.EvTabActivated, work, true)
	for i := 0; i < 46; i++ {
		r.ev(time.Minute, domain.EvHeartbeat, work, true)
	}
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	// The completion prompt that came up during the work block was ignored
	// and is being closed; what matters here is that no return screen shows.
	if d := r.ev(6*time.Minute, domain.EvHeartbeat, yt, true); d.Command == domain.CmdShowRamp {
		t.Fatalf("return screen after the session goal was met: %s", d.Reason)
	}
	expect(t, r.ev(time.Minute, domain.EvHeartbeat, yt, true), domain.CmdDoNothing, "session_time_met")
}

func TestNotTodaySuppressesEverythingAndUndoRestores(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.ev(0, domain.EvTabActivated, work, true)
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	expect(t, r.ev(6*time.Minute, domain.EvHeartbeat, yt, true), domain.CmdShowRamp, "")
	if _, err := r.app.RespondRamp(application.RampInput{Site: "youtube.com", Choice: domain.ChoiceNotToday}); err != nil {
		t.Fatal(err)
	}
	// A different site is now suppressed too. (Timeline stays inside the 15-minute
	// inactivity timeout, measured from the last work-page activity.)
	r.ev(30*time.Second, domain.EvTabActivated, rd, true)
	expect(t, r.ev(5*time.Minute+30*time.Second, domain.EvHeartbeat, rd, true), domain.CmdDoNothing, "not_today")

	// Undo reverts the suppression only.
	if _, err := r.app.RespondRamp(application.RampInput{Site: "youtube.com", Choice: domain.ChoiceUndo}); err != nil {
		t.Fatal(err)
	}
	expect(t, r.ev(30*time.Second, domain.EvHeartbeat, rd, true), domain.CmdShowRamp, "")
}

func TestNotTodayEndsAtMidnight(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.ev(0, domain.EvTabActivated, work, true)
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	r.ev(6*time.Minute, domain.EvHeartbeat, yt, true)
	if _, err := r.app.RespondRamp(application.RampInput{Site: "youtube.com", Choice: domain.ChoiceNotToday}); err != nil {
		t.Fatal(err)
	}
	r.clk.t = time.Date(2026, 9, 19, 9, 0, 0, 0, time.Local)
	r.ev(0, domain.EvTabActivated, work, true)
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	expect(t, r.ev(6*time.Minute, domain.EvHeartbeat, yt, true), domain.CmdShowRamp, "")
}

// Continue returns to the latest page about the goal ("Thesis draft" for the
// goal "finish my thesis"), not merely the last page that was open.
func TestContinueReturnsLastWorkTab(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.evTitle(0, work, "Thesis draft - Google Docs")
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	r.ev(6*time.Minute, domain.EvHeartbeat, yt, true)
	res, err := r.app.RespondRamp(application.RampInput{Site: "youtube.com", Choice: domain.ChoiceContinue})
	if err != nil {
		t.Fatal(err)
	}
	if res.LastWorkTabID != 7 || res.LastWorkURL != work {
		t.Fatalf("got %+v", res)
	}
}

// The card can be on screen while its acknowledgement was refused (Chrome
// reported no window focus at that instant). Continue must still work.
func TestContinueWorksWhenAcknowledgementWasRefused(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.evTitle(0, work, "Thesis draft - Google Docs")
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	d := r.evUnacknowledged(6*time.Minute, domain.EvHeartbeat, yt, true)
	expect(t, d, domain.CmdShowRamp, "")
	res, err := r.app.RespondRamp(application.RampInput{Site: "youtube.com", Choice: domain.ChoiceContinue})
	if err != nil {
		t.Fatalf("Continue on an unacknowledged card: %v", err)
	}
	if res.LastWorkTabID != 7 || res.LastWorkURL != work {
		t.Fatalf("got %+v", res)
	}
	// It now counts as shown: once per site per day still holds.
	expect(t, r.ev(time.Minute, domain.EvHeartbeat, yt, true), domain.CmdDoNothing, "already_shown_today")
}

// A site that never had a return screen today cannot be answered. (Before
// cards followed the user, a second site's screen could discard the first
// one's delivery; with one open card at a time that cannot happen, so this
// now covers the remaining refusal.)
func TestAnswerForASiteWithoutAScreenIsRefused(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.ev(0, domain.EvTabActivated, work, true)
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	expect(t, r.evUnacknowledged(6*time.Minute, domain.EvHeartbeat, yt, true), domain.CmdShowRamp, "")
	if _, err := r.app.RespondRamp(application.RampInput{Site: "reddit.com", Choice: domain.ChoiceContinue}); !errors.Is(err, application.ErrNoPending) {
		t.Fatalf("got %v, want ErrNoPending", err)
	}
	// The open YouTube screen is untouched and still follows. (Its copy on
	// Reddit cannot acknowledge a YouTube screen that was never shown.)
	expect(t, r.evUnacknowledged(time.Minute, domain.EvHeartbeat, rd, true), domain.CmdShowRamp, "card_open")
}

func TestBestAnchorTiers(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.ev(0, domain.EvTabActivated, work, true)
	store := func(tier int, snippet, url string) application.AnchorResult {
		res, err := r.app.StoreAnchor(domain.AnchorInput{Tier: tier, URL: url, Title: "T", Snippet: snippet})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if !store(3, "Heading", work).Stored {
		t.Fatal("first anchor rejected")
	}
	if !store(3, "Heading two", work).Stored {
		t.Fatal("same tier must replace an older anchor")
	}
	if !store(1, "the problem with focus apps is", work).Stored {
		t.Fatal("higher tier must replace")
	}
	if res := store(4, "Inbox", "https://mail.google.com/"); res.Stored || res.Reason != "lower_tier_than_best" {
		t.Fatalf("Gmail must not destroy a tier 1 anchor: %+v", res)
	}
	if res := store(1, "secret", yt); res.Stored || res.Reason != "blacklisted_page" {
		t.Fatalf("blacklisted page anchored: %+v", res)
	}
	if res := store(1, "x", "chrome://settings"); res.Stored {
		t.Fatalf("non-web page anchored: %+v", res)
	}
	// The anchor shows up on the return screen.
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	d := r.ev(6*time.Minute, domain.EvHeartbeat, yt, true)
	if d.Payload.Anchor == nil || d.Payload.Anchor.Snippet != "the problem with focus apps is" {
		t.Fatalf("anchor missing on return screen: %+v", d.Payload.Anchor)
	}
}

func TestAnchorSnippetIsTruncatedPlainText(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.ev(0, domain.EvTabActivated, work, true)
	long := ""
	for i := 0; i < 50; i++ {
		long += "word\n\t "
	}
	if _, err := r.app.StoreAnchor(domain.AnchorInput{Tier: 1, URL: work, Snippet: long, ScrollPercent: 900}); err != nil {
		t.Fatal(err)
	}
	f := r.app.Full()
	_ = f
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	d := r.ev(6*time.Minute, domain.EvHeartbeat, yt, true)
	if n := len([]rune(d.Payload.Anchor.Snippet)); n > domain.MaxSnippetRunes {
		t.Fatalf("snippet has %d runes", n)
	}
	if d.Payload.Anchor.ScrollPercent != 100 {
		t.Fatalf("scroll not clamped: %d", d.Payload.Anchor.ScrollPercent)
	}
}

func TestCaptureAnchorFlag(t *testing.T) {
	r := newRig(t)
	r.onboard()
	if !r.ev(0, domain.EvTabActivated, work, true).Payload.CaptureAnchor {
		t.Fatal("work page should allow capture")
	}
	if r.ev(time.Second, domain.EvTabActivated, "chrome://newtab/", true).Payload.CaptureAnchor {
		t.Fatal("non-web page allowed capture")
	}
	if r.ev(time.Second, domain.EvTabActivated, yt, true).Payload.CaptureAnchor {
		t.Fatal("blacklisted page allowed capture")
	}
}

func TestCompletionFlowAndMilestoneRollover(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.ev(0, domain.EvTabActivated, work, true)
	// Step 1 is estimated at 10 minutes: the prompt fires at 80% = 8 focused minutes.
	var d application.Decision
	minutes := 0
	for d.Command != domain.CmdAskCompletion && minutes < 20 {
		d = r.ev(time.Minute, domain.EvHeartbeat, work, true)
		minutes++
	}
	if d.Command != domain.CmdAskCompletion || minutes != 8 {
		t.Fatalf("prompt fired after %d minutes with %s", minutes, d.Command)
	}
	stepID := d.Payload.Step.ID
	// A heartbeat never raises a second prompt: the open one is re-sent, same
	// card, so the page keeps it without a new entrance (card.go).
	again := r.ev(time.Minute, domain.EvHeartbeat, work, true)
	expect(t, again, domain.CmdAskCompletion, "card_open")
	if again.Payload.DeliveryID != d.Payload.DeliveryID {
		t.Fatal("a second completion prompt was raised")
	}

	// "Not yet": it comes back only after 20 more minutes of WORK on the step,
	// and gently.
	if _, err := r.app.RespondStep(context.Background(), application.StepInput{StepID: stepID, Answer: "not_yet"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 19; i++ {
		if c := r.ev(time.Minute, domain.EvHeartbeat, work, true).Command; c != domain.CmdDoNothing {
			t.Fatalf("prompt returned before 20 more minutes of work, at minute %d", i)
		}
	}
	d = r.ev(time.Minute, domain.EvTabActivated, work, true)
	expect(t, d, domain.CmdAskCompletion, "more_work_after_not_yet")
	if !d.Payload.Gentle {
		t.Fatal("the repeated question must be the gentle one")
	}

	res, err := r.app.RespondStep(context.Background(), application.StepInput{StepID: stepID, Answer: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Announcement != "Step 1 of 2 complete." || res.CurrentStep == nil || res.CurrentStep.Text != "write the outline" {
		t.Fatalf("got %+v", res)
	}
	if p := r.app.Summary().Progress.Percent; p < 33 || p > 34 { // half of a 2/3-weight milestone
		t.Fatalf("progress %v", p)
	}
	// Confirmed progress drives the planet: 33% is the second era.
	if pl := r.app.Summary().Planet; pl.Era != "copper" || pl.EraLabel != "Copper" || pl.Weather != "clear" {
		t.Fatalf("planet after first confirmation: %+v", pl)
	}

	// Complete step 2: the milestone finishes and steps for the next one are generated.
	var second application.Decision
	for i := 0; i < 20 && second.Command != domain.CmdAskCompletion; i++ {
		second = r.ev(time.Minute, domain.EvHeartbeat, work, true)
	}
	res, err = r.app.RespondStep(context.Background(), application.StepInput{StepID: second.Payload.Step.ID, Answer: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Announcement != "Step 2 of 2 complete. Milestone complete: Draft." {
		t.Fatalf("got %q", res.Announcement)
	}
	if res.CurrentStep == nil || res.CurrentStep.Text != "proofread" || res.NextStepsPending {
		t.Fatalf("next milestone steps not generated: %+v", res)
	}
}

func TestProgressOnlyChangesOnConfirmation(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.ev(0, domain.EvTabActivated, work, true)
	for i := 0; i < 60; i++ {
		r.ev(time.Minute, domain.EvHeartbeat, work, true)
	}
	if p := r.app.Summary().Progress.Percent; p != 0 {
		t.Fatalf("an hour of browsing moved progress to %v", p)
	}
	if pl := r.app.Full().Planet; pl.Era != "prehistoric" || pl.Weather != "clear" {
		t.Fatalf("an hour of browsing moved the planet to %+v", pl)
	}
}

func TestRespondWithoutPendingPrompt(t *testing.T) {
	r := newRig(t)
	r.onboard()
	_, err := r.app.RespondStep(context.Background(), application.StepInput{StepID: "nope", Answer: "yes"})
	if !errors.Is(err, application.ErrNoPending) {
		t.Fatalf("got %v", err)
	}
}

// There is no greeting card: the day's first card says hello, once a day.
func TestTheDaysFirstCardSaysHello(t *testing.T) {
	r := newRig(t)
	r.onboard()
	expect(t, r.ev(0, domain.EvTabActivated, work, true), domain.CmdDoNothing, "working") // no card just to say hello
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	first := r.ev(6*time.Minute, domain.EvHeartbeat, yt, true)
	expect(t, first, domain.CmdShowRamp, "")
	if !first.Payload.Greeting {
		t.Fatal("the day's first card must say hello")
	}
	if again := r.ev(time.Minute, domain.EvHeartbeat, yt, true); !again.Payload.Greeting {
		t.Fatal("the same card, followed, must keep its hello")
	}
	if _, err := r.app.RespondRamp(application.RampInput{Site: "youtube.com", Choice: domain.ChoiceContinue}); err != nil {
		t.Fatal(err)
	}
	var second application.Decision
	for i := 0; i < 12 && second.Command != domain.CmdAskCompletion; i++ {
		second = r.ev(time.Minute, domain.EvHeartbeat, work, true)
	}
	expect(t, second, domain.CmdAskCompletion, "")
	if second.Payload.Greeting {
		t.Fatal("hello twice in one day")
	}
	// The next day starts with a hello again.
	r.clk.t = time.Date(2026, 9, 19, 9, 0, 0, 0, time.Local)
	r.ev(0, domain.EvTabActivated, work, true)
	r.ev(time.Minute, domain.EvTabActivated, rd, true)
	next := r.ev(6*time.Minute, domain.EvHeartbeat, rd, true)
	expect(t, next, domain.CmdShowRamp, "")
	if !next.Payload.Greeting {
		t.Fatal("no hello on the next day's first card")
	}
}

func TestStatePersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	repo, _ := storage.NewJSON(dir)
	clk := &manualClock{t: time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local)}
	app, _ := application.New(repo, clk, &fakePlanner{}, application.NoShield{})
	if err := app.Onboard(context.Background(), application.OnboardInput{Goal: "finish my thesis"}); err != nil {
		t.Fatal(err)
	}
	repo2, _ := storage.NewJSON(dir)
	app2, err := application.New(repo2, clk, &fakePlanner{}, application.NoShield{})
	if err != nil {
		t.Fatal(err)
	}
	if s := app2.Summary(); !s.Onboarded || s.CurrentStep == nil {
		t.Fatalf("state lost: %+v", s)
	}
}
