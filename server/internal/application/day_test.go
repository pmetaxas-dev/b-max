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

// Good day / bad day (MVP §8) and the metadata shield (MVP §4).

const (
	pyDocs    = "https://docs.python.org/3/tutorial/"
	unrelated = "https://news.example.com/today"
)

func dayRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	r.pl.keywords = []string{"python", "docs.python.org"}
	r.onboard()
	return r
}

// evTitle is ev with a page title (relevance also reads titles).
func (r *rig) evTitle(after time.Duration, url, title string) application.Decision {
	r.t.Helper()
	r.clk.add(after)
	d, err := r.app.HandleEvent(application.Event{Type: domain.EvHeartbeat, TabID: 7, URL: url, Title: title, Focused: true, Activity: domain.ActivityActive})
	if err != nil {
		r.t.Fatal(err)
	}
	if d.Payload.DeliveryID != "" {
		r.ack(d, url)
	}
	return d
}

// browse spends `minutes` of active time on url, one heartbeat per minute,
// and returns every command that was issued.
func (r *rig) browse(url string, minutes int) []application.Decision {
	r.t.Helper()
	var out []application.Decision
	for range minutes {
		if d := r.ev(time.Minute, domain.EvHeartbeat, url, true); d.Command != domain.CmdDoNothing {
			out = append(out, d)
		}
	}
	return out
}

func commandsOf(ds []application.Decision) []domain.Command {
	var out []domain.Command
	for _, d := range ds {
		out = append(out, d.Command)
	}
	return out
}

func TestOnboardingStoresPlannerKeywords(t *testing.T) {
	r := dayRig(t)
	st, _ := r.repo.Load()
	if strings.Join(st.Goal.Keywords, ",") != "python,docs.python.org" {
		t.Fatalf("keywords %q", st.Goal.Keywords)
	}
}

// Regression (manual test): Google search "reddit" -> reddit.com as the first
// thing of the day. The search is not work: no return screen, and Continue
// must never be sent back to it.
func TestASearchThatIsNotAboutTheGoalIsNotWork(t *testing.T) {
	r := dayRig(t)
	search := "https://www.google.com/search?q=reddit&oq=red"
	r.ev(0, domain.EvTabActivated, search, true)
	r.ev(10*time.Second, domain.EvTabActivated, rd, true)
	for range 10 {
		if d := r.ev(time.Minute, domain.EvHeartbeat, rd, true); d.Command == domain.CmdShowRamp {
			t.Fatal("return screen after a search that was only a way to get to Reddit")
		}
	}
	st, _ := r.repo.Load()
	if st.WorkContext.LastWorkURL == search {
		t.Fatal("the search became the place Continue returns to")
	}
	// A search about the goal is work, and counts as relevant.
	expect(t, r.ev(time.Minute, domain.EvTabActivated, "https://www.google.com/search?q=python+basics", true), domain.CmdShowThumbsUp, "")
}

func continueAfterRamp(t *testing.T, r *rig) application.RampResult {
	t.Helper()
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	expect(t, r.ev(6*time.Minute, domain.EvHeartbeat, yt, true), domain.CmdShowRamp, "")
	res, err := r.app.RespondRamp(application.RampInput{Site: "youtube.com", Choice: domain.ChoiceContinue})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// Regression (manual test): Continue must lead to something about the
// current step, never to an unrelated page that happened to be open last.
func TestContinueNeverOffersAnUnrelatedPage(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, unrelated, true)
	res := continueAfterRamp(t, r)
	if res.LastWorkURL != "" || res.AnchorURL != "" {
		t.Fatalf("unrelated page offered: %+v", res)
	}
	if res.SearchQuery != "write the intro" { // the step's search (fallback: its text)
		t.Fatalf("no search for the step: %+v", res)
	}
}

func TestContinuePrefersTodaysPageAboutTheGoal(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, pyDocs, true)
	r.ev(time.Minute, domain.EvTabActivated, unrelated, true) // opened later, but unrelated
	res := continueAfterRamp(t, r)
	if res.LastWorkURL != pyDocs || res.LastWorkTabID != 7 {
		t.Fatalf("got %+v", res)
	}
}

func TestContinueIgnoresYesterdaysPageAndAnchor(t *testing.T) {
	r := dayRig(t)
	r.clk.t = time.Date(2026, 9, 18, 22, 0, 0, 0, time.Local)
	r.ev(0, domain.EvTabActivated, pyDocs, true)
	if _, err := r.app.StoreAnchor(domain.AnchorInput{Tier: 1, URL: pyDocs, Title: "Python tutorial", Snippet: "python"}); err != nil {
		t.Fatal(err)
	}
	r.clk.t = time.Date(2026, 9, 19, 9, 0, 0, 0, time.Local)
	r.ev(0, domain.EvTabActivated, unrelated, true)
	res := continueAfterRamp(t, r)
	if res.LastWorkURL != "" || res.AnchorURL != "" || res.SearchQuery == "" {
		t.Fatalf("yesterday offered: %+v", res)
	}
}

// Regression: "You had written: python", captured hours earlier, was shown.
func TestTheReturnScreenShowsOnlyTodaysAnchor(t *testing.T) {
	r := dayRig(t)
	r.clk.t = time.Date(2026, 9, 18, 22, 0, 0, 0, time.Local)
	r.ev(0, domain.EvTabActivated, pyDocs, true)
	if _, err := r.app.StoreAnchor(domain.AnchorInput{Tier: 1, URL: pyDocs, Title: "Python tutorial", Snippet: "python"}); err != nil {
		t.Fatal(err)
	}
	r.clk.t = time.Date(2026, 9, 19, 9, 0, 0, 0, time.Local)
	r.ev(0, domain.EvTabActivated, unrelated, true)
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	d := r.ev(6*time.Minute, domain.EvHeartbeat, yt, true)
	expect(t, d, domain.CmdShowRamp, "")
	if d.Payload.Anchor != nil {
		t.Fatalf("yesterday's anchor shown: %+v", d.Payload.Anchor)
	}
	// Today's anchor on a goal page is shown and offered to Continue.
	r2 := dayRig(t)
	r2.ev(0, domain.EvTabActivated, pyDocs, true)
	if _, err := r2.app.StoreAnchor(domain.AnchorInput{Tier: 1, URL: pyDocs, Title: "Python tutorial", Snippet: "print('hi')"}); err != nil {
		t.Fatal(err)
	}
	r2.ev(time.Minute, domain.EvTabActivated, yt, true)
	d2 := r2.ev(6*time.Minute, domain.EvHeartbeat, yt, true)
	if d2.Payload.Anchor == nil || d2.Payload.Anchor.Snippet != "print('hi')" {
		t.Fatalf("today's anchor missing: %+v", d2.Payload.Anchor)
	}
}

// Search pages never capture a return anchor: typing "reddit" into Google
// is not "what you were writing".
func TestNoAnchorCaptureOnASearchPage(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, pyDocs, true) // work context
	if d := r.ev(time.Minute, domain.EvTabActivated, "https://www.google.com/search?q=reddit", true); d.Payload.CaptureAnchor {
		t.Fatal("anchor capture allowed on an unrelated search page")
	}
	if d := r.ev(time.Minute, domain.EvTabActivated, pyDocs, true); !d.Payload.CaptureAnchor {
		t.Fatal("anchor capture must stay on for goal pages")
	}
}

// A plan made before keywords existed gets real ones from the planner, once;
// they replace the narrow fallback.
func TestKeywordBackfillForAnOlderPlan(t *testing.T) {
	r := newRig(t) // the fake outline has no keywords, like an older plan
	r.onboard()
	r.pl.backfill = []string{"thesis", "zotero", "latex", "video"}
	if err := r.app.BackfillKeywords(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _ := r.repo.Load()
	if strings.Join(st.Goal.Keywords, ",") != "thesis,zotero,latex" { // cleaned: "video" is generic
		t.Fatalf("keywords %q", st.Goal.Keywords)
	}
	// Once present, never asked again.
	calls := r.pl.keywordCalls.Load()
	if err := r.app.BackfillKeywords(context.Background()); err != nil || r.pl.keywordCalls.Load() != calls {
		t.Fatalf("asked again: %v", err)
	}
	if d := r.ev(0, domain.EvTabActivated, "https://www.zotero.org/", true); d.Command != domain.CmdShowThumbsUp {
		t.Fatalf("backfilled keyword not used: %s", d.Command)
	}
}

func TestKeywordBackfillFailureKeepsTheFallback(t *testing.T) {
	r := newRig(t)
	r.onboard()
	if err := r.app.BackfillKeywords(context.Background()); err == nil {
		t.Fatal("expected the planner failure")
	}
	r.pl.backfill = []string{"video", "learn"} // nothing specific survives cleaning
	if err := r.app.BackfillKeywords(context.Background()); err == nil {
		t.Fatal("expected an invalid, empty keyword list to be refused")
	}
	st, _ := r.repo.Load()
	if len(st.Goal.Keywords) != 0 {
		t.Fatalf("stored %q", st.Goal.Keywords)
	}
}

func TestDayQuestionAfterThirtyActiveMinutesWithoutGoalPages(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, rd, true)
	// Time with Chrome unfocused does not count.
	for range 60 {
		r.ev(time.Minute, domain.EvHeartbeat, rd, false)
	}
	got := r.browse(rd, 29)
	if len(got) != 0 {
		t.Fatalf("asked before 30 active minutes: %v", commandsOf(got))
	}
	d := r.ev(time.Minute, domain.EvHeartbeat, rd, true)
	expect(t, d, domain.CmdAskDay, "no_goal_activity_today")
	if d.Payload.Step == nil || d.Payload.Step.Text != "write the intro" || d.Payload.Lang != "en" {
		t.Fatalf("payload %+v", d.Payload)
	}
	// Unanswered, it stays (same card) for 5 active minutes, then closes.
	got = r.browse(rd, 5)
	for _, x := range got[:4] {
		if x.Command != domain.CmdAskDay || x.Payload.DeliveryID != d.Payload.DeliveryID {
			t.Fatalf("expected the same open card, got %s", x.Command)
		}
	}
	if last := got[len(got)-1]; last.Command != domain.CmdCloseCard || last.Payload.CloseDeliveryID != d.Payload.DeliveryID {
		t.Fatalf("ignored card not closed: %v", commandsOf(got))
	}
	// Ignored: asked again only after another hour of active use.
	if got := r.browse(rd, 59); len(got) != 0 {
		t.Fatalf("asked again too soon: %v", commandsOf(got))
	}
	expect(t, r.ev(time.Minute, domain.EvHeartbeat, rd, true), domain.CmdAskDay, "")
}

// The user walks away with the question on screen: idle, locked or
// another app never counts, so the card is still there when they return.
func TestAnOpenCardWaitsForAUserWhoWalkedAway(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, rd, true)
	r.browse(rd, 29)
	card := r.ev(time.Minute, domain.EvHeartbeat, rd, true)
	expect(t, card, domain.CmdAskDay, "")
	idle := func(activity domain.ActivityState, focused bool) {
		r.clk.add(time.Minute)
		if _, err := r.app.HandleEvent(application.Event{Type: domain.EvActivityChanged, TabID: 7, URL: rd, Title: "t", Focused: focused, Activity: activity}); err != nil {
			t.Fatal(err)
		}
	}
	for range 60 {
		idle(domain.ActivityIdle, true) // away for an hour
	}
	for range 60 {
		idle(domain.ActivityLocked, true) // screen locked for an hour
	}
	for range 60 {
		idle(domain.ActivityActive, false) // another app for an hour
	}
	back := r.ev(time.Minute, domain.EvTabActivated, unrelated, true)
	expect(t, back, domain.CmdAskDay, "card_open")
	if back.Payload.DeliveryID != card.Payload.DeliveryID {
		t.Fatal("the card waiting for the user was replaced")
	}
}

// A tab switch or a site that rewrites its URL no longer loses the card.
func TestTheCardFollowsAcrossTabsAndSites(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, rd, true)
	r.browse(rd, 29)
	card := r.ev(time.Minute, domain.EvHeartbeat, rd, true)
	for _, url := range []string{"https://www.reddit.com/r/all/?feed=home", unrelated, "https://www.google.com/search?q=weather", rd} {
		d := r.ev(20*time.Second, domain.EvTabActivated, url, true)
		expect(t, d, domain.CmdAskDay, "card_open")
		if d.Payload.DeliveryID != card.Payload.DeliveryID {
			t.Fatalf("%s: a new card instead of the open one", url)
		}
	}
	// The answer closes it everywhere.
	if _, err := r.app.RespondDay(application.DayInput{Choice: domain.DayChoiceWill}); err != nil {
		t.Fatal(err)
	}
	if d := r.ev(time.Minute, domain.EvTabActivated, unrelated, true); d.Command == domain.CmdAskDay {
		t.Fatal("answered card still follows")
	}
}

func TestReturnScreenClosesWhenTheUserGoesBackToWork(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, unrelated, true)
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	ramp := r.ev(6*time.Minute, domain.EvHeartbeat, yt, true)
	expect(t, ramp, domain.CmdShowRamp, "")
	// Follows to another blacklisted site, not to a work page.
	expect(t, r.ev(time.Minute, domain.EvTabActivated, rd, true), domain.CmdShowRamp, "card_open")
	d := r.ev(time.Minute, domain.EvTabActivated, unrelated, true)
	if d.Payload.CloseDeliveryID != ramp.Payload.DeliveryID {
		t.Fatalf("back at work, the screen was not closed: %+v", d)
	}
	// It counted as shown: no second return screen for YouTube today.
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	if d := r.ev(6*time.Minute, domain.EvHeartbeat, yt, true); d.Command == domain.CmdShowRamp {
		t.Fatal("return screen shown twice")
	}
}

func TestEscapeSetsTheDayQuestionAside(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, rd, true)
	r.browse(rd, 29)
	card := r.ev(time.Minute, domain.EvHeartbeat, rd, true)
	if err := r.app.CloseCard(application.CardInput{DeliveryID: "other", Reason: "ignored"}); !errors.Is(err, application.ErrNoPending) {
		t.Fatalf("closing another card: %v", err)
	}
	if err := r.app.CloseCard(application.CardInput{DeliveryID: card.Payload.DeliveryID, Reason: "whatever"}); !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("bad reason: %v", err)
	}
	if err := r.app.CloseCard(application.CardInput{DeliveryID: card.Payload.DeliveryID, Reason: "ignored"}); err != nil {
		t.Fatal(err)
	}
	if got := r.browse(rd, 59); len(got) != 0 {
		t.Fatalf("set aside, but asked again within the hour: %v", commandsOf(got))
	}
	expect(t, r.ev(time.Minute, domain.EvHeartbeat, rd, true), domain.CmdAskDay, "")
}

func TestTheDayQuestionExpiresAtMidnight(t *testing.T) {
	r := dayRig(t)
	r.clk.t = time.Date(2026, 9, 18, 23, 20, 0, 0, time.Local)
	r.ev(0, domain.EvTabActivated, rd, true)
	r.browse(rd, 29)
	card := r.ev(time.Minute, domain.EvHeartbeat, rd, true)
	expect(t, card, domain.CmdAskDay, "")
	r.clk.t = time.Date(2026, 9, 19, 8, 0, 0, 0, time.Local) // next morning
	d := r.ev(0, domain.EvTabActivated, unrelated, true)
	if d.Command == domain.CmdAskDay && d.Payload.DeliveryID == card.Payload.DeliveryID {
		t.Fatal("yesterday's question followed into today")
	}
}

func TestAGoalPageMakesTheDayGoodAndStopsTheQuestion(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, unrelated, true)
	r.browse(unrelated, 5) // under the 8-minute completion prompt of a 10-minute step
	d := r.ev(time.Minute, domain.EvTabActivated, pyDocs, true)
	expect(t, d, domain.CmdShowThumbsUp, "relevant_page")
	for _, c := range commandsOf(r.browse(unrelated, 180)) {
		if c == domain.CmdAskDay {
			t.Fatal("asked on a good day")
		}
	}
	if s := r.app.Summary(); s.Day.Mode != domain.DayGood {
		t.Fatalf("mode %s", s.Day.Mode)
	}
}

func TestThumbsUpOncePerPageAndScoreNeverMovesThePlanet(t *testing.T) {
	r := dayRig(t)
	expect(t, r.ev(0, domain.EvTabActivated, unrelated, true), domain.CmdDoNothing, "working")
	expect(t, r.ev(time.Minute, domain.EvTabActivated, pyDocs, true), domain.CmdShowThumbsUp, "")
	r.ev(time.Minute, domain.EvTabActivated, unrelated, true)
	if d := r.ev(time.Minute, domain.EvTabActivated, pyDocs, true); d.Command == domain.CmdShowThumbsUp {
		t.Fatal("second thumbs-up for the same page")
	}
	r.browse(pyDocs, 5)
	s := r.app.Summary()
	if s.Day.RelevantMinutes < 5 {
		t.Fatalf("score %d", s.Day.RelevantMinutes)
	}
	if s.Progress.Percent != 0 || s.Planet.Era != "prehistoric" {
		t.Fatalf("relevant browsing moved the planet: %v %s", s.Progress.Percent, s.Planet.Era)
	}
}

// VERY IMPORTANT: after "I won't work today" nothing interrupts, whatever
// happens, except the thumbs-up badge and a new era.
func TestBadDayNeverShowsACard(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, rd, true)
	r.browse(rd, 30)
	res, err := r.app.RespondDay(application.DayInput{Choice: domain.DayChoiceWont})
	if err != nil || res.Mode != domain.DayBad || res.DismissUnsolicited == nil {
		t.Fatalf("res %+v err %v", res, err)
	}
	var seen []application.Decision
	// Work context, then long distraction (ramp conditions), long work on a
	// goal page (completion-prompt conditions), the next day's greeting time.
	seen = append(seen, r.browse(unrelated, 5)...)
	seen = append(seen, r.browse(rd, 20)...)
	seen = append(seen, r.browse(yt, 20)...)
	seen = append(seen, r.browse(pyDocs, 60)...)
	seen = append(seen, r.browse(unrelated, 120)...)
	for _, d := range seen {
		if d.Command != domain.CmdShowThumbsUp {
			t.Fatalf("bad day showed %s (%s)", d.Command, d.Reason)
		}
	}
	if len(seen) != 1 {
		t.Fatalf("expected exactly one thumbs-up, got %v", commandsOf(seen))
	}
	// The completion prompt waits in the popup/dashboard instead.
	if s := r.app.Summary(); s.Pending == nil {
		t.Fatal("the completion prompt should still be pending for the popup")
	}
}

func TestNewEraInterruptsEvenABadDay(t *testing.T) {
	r := dayRig(t)
	if _, err := r.app.RespondDay(application.DayInput{Choice: domain.DayChoiceWont}); err != nil {
		t.Fatal(err)
	}
	r.ev(0, domain.EvTabActivated, pyDocs, true)
	r.browse(pyDocs, 10) // reaches the completion threshold (80% of 10 minutes)
	step := r.app.Summary().Pending.Step
	if _, err := r.app.RespondStep(context.Background(), application.StepInput{StepID: step.ID, Answer: "yes"}); err != nil {
		t.Fatal(err) // confirmed from the popup: 1 of 2 steps of a 2/3 milestone = 33%
	}
	d := r.ev(time.Minute, domain.EvHeartbeat, rd, true)
	expect(t, d, domain.CmdShowNewEra, "new_era")
	if d.Payload.Era != "copper" || d.Payload.EraLabel != "Copper" {
		t.Fatalf("payload %+v", d.Payload)
	}
	// It follows (same card) until the user responds, then never again.
	again := r.ev(time.Minute, domain.EvTabActivated, unrelated, true)
	expect(t, again, domain.CmdShowNewEra, "card_open")
	if again.Payload.DeliveryID != d.Payload.DeliveryID || again.Payload.Era != "copper" {
		t.Fatalf("followed era card %+v", again.Payload)
	}
	if err := r.app.CloseCard(application.CardInput{DeliveryID: d.Payload.DeliveryID, Reason: "done"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range commandsOf(r.browse(rd, 30)) {
		if c == domain.CmdShowNewEra {
			t.Fatal("era announced twice")
		}
	}
}

// Ignored for 5 active minutes, the era card closes and is not repeated.
func TestAnIgnoredEraCardIsNotRepeated(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, pyDocs, true)
	r.browse(pyDocs, 10)
	step := r.app.Summary().Pending.Step
	if _, err := r.app.RespondStep(context.Background(), application.StepInput{StepID: step.ID, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}
	var eras, closes int
	var eraID string
	for _, d := range r.browse(pyDocs, 30) {
		if d.Command == domain.CmdShowNewEra {
			eras++
			eraID = d.Payload.DeliveryID
		}
		if eraID != "" && d.Payload.CloseDeliveryID == eraID {
			closes++
		}
	}
	if eras != 5 || closes != 1 {
		t.Fatalf("era shown %d times, closed %d times", eras, closes)
	}
}

func TestWillWorkAsksAgainAfterAnHourAndNeedStartGivesSearches(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, rd, true)
	r.browse(rd, 30)
	if res, err := r.app.RespondDay(application.DayInput{Choice: domain.DayChoiceWill}); err != nil || res.Mode != domain.DayUndecided {
		t.Fatalf("res %+v err %v", res, err)
	}
	if got := r.browse(rd, 59); len(got) != 0 {
		t.Fatalf("asked within the hour: %v", commandsOf(got))
	}
	expect(t, r.ev(time.Minute, domain.EvHeartbeat, rd, true), domain.CmdAskDay, "")

	res, err := r.app.RespondDay(application.DayInput{Choice: domain.DayChoiceNeedStart})
	if err != nil || res.Step == nil || len(res.SearchQueries) != 3 {
		t.Fatalf("res %+v err %v", res, err)
	}
	for _, q := range res.SearchQueries {
		if strings.Contains(q, "://") {
			t.Fatalf("a search phrase is a URL: %q", q)
		}
	}
	if got := r.browse(rd, 59); len(got) != 0 {
		t.Fatalf("asked within the hour after need_start: %v", commandsOf(got))
	}
}

func TestUndoBadDay(t *testing.T) {
	r := dayRig(t)
	if _, err := r.app.RespondDay(application.DayInput{Choice: domain.ChoiceUndo}); !errors.Is(err, application.ErrNoPending) {
		t.Fatalf("undo without a bad day: %v", err)
	}
	if _, err := r.app.RespondDay(application.DayInput{Choice: domain.DayChoiceWont}); err != nil {
		t.Fatal(err)
	}
	res, err := r.app.RespondDay(application.DayInput{Choice: domain.ChoiceUndo})
	if err != nil || res.Mode == domain.DayBad {
		t.Fatalf("res %+v err %v", res, err)
	}
	if _, err := r.app.RespondDay(application.DayInput{Choice: "maybe"}); !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("invalid choice: %v", err)
	}
}

// "Not today" on the return screen is the same bad day: tracking and the
// thumbs-up continue, no card appears.
func TestReturnScreenNotTodayIsABadDay(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, unrelated, true)
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	expect(t, r.ev(6*time.Minute, domain.EvHeartbeat, yt, true), domain.CmdShowRamp, "")
	if _, err := r.app.RespondRamp(application.RampInput{Site: "youtube.com", Choice: domain.ChoiceNotToday}); err != nil {
		t.Fatal(err)
	}
	if s := r.app.Summary(); s.Day.Mode != domain.DayBad {
		t.Fatalf("mode %s", s.Day.Mode)
	}
	expect(t, r.ev(time.Minute, domain.EvTabActivated, pyDocs, true), domain.CmdShowThumbsUp, "")
}

// Metadata as a shield (MVP §4): a goal-related video on a blacklisted site
// never triggers the return screen, and counts as work.
func TestGoalContentOnABlacklistedSiteIsShielded(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, unrelated, true)
	r.evTitle(time.Minute, yt, "Python for Beginners - Full Course")
	for range 10 {
		d := r.evTitle(time.Minute, yt, "Python for Beginners - Full Course")
		if d.Command == domain.CmdShowRamp {
			t.Fatal("return screen on a goal-related video")
		}
	}
	if s := r.app.Summary(); s.Day.RelevantMinutes < 10 {
		t.Fatalf("goal video did not count: %d minutes", s.Day.RelevantMinutes)
	}
	// A deterministic distraction path is never shielded, never relevant and
	// never gets a thumbs-up, even with the goal in its title. (The YouTube
	// episode already ran past grace during the video, so it shows at once.)
	d := r.evTitle(time.Minute, "https://www.youtube.com/shorts/abc", "Python shorts")
	expect(t, d, domain.CmdShowRamp, "grace_elapsed_distraction_path")
	for range 5 {
		if d := r.evTitle(time.Minute, "https://www.youtube.com/shorts/abc", "Python shorts"); d.Command == domain.CmdShowThumbsUp {
			t.Fatal("thumbs-up on a shorts page")
		}
	}
}
