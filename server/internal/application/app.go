// Package application wires the domain rules to persistence and the planner.
// It owns the single in-memory State; every public method takes the mutex, so
// concurrent HTTP handlers cannot interleave.
package application

import (
	"context"
	"errors"
	"io"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"focuscompanion/internal/clock"
	"focuscompanion/internal/domain"
	"focuscompanion/internal/storage"
)

var (
	ErrInvalid         = errors.New("invalid request")
	ErrGoalExists      = errors.New("an active goal already exists")
	ErrNoGoal          = errors.New("no goal yet")
	ErrPlanUnavailable = errors.New("plan generation is unavailable")
	ErrNoPending       = errors.New("no matching pending prompt")
)

// Planner generates plans. Implemented by groq.Planner; faked in tests.
type Planner interface {
	Outline(ctx context.Context, goal string, today time.Time) (domain.RawPlan, error)
	// StepsFor gets every milestone and the completed steps, so it never
	// repeats earlier work.
	StepsFor(ctx context.Context, goal string, m domain.Milestone, today time.Time, plan domain.PlanContext) ([]domain.RawStep, error)
	// Keywords backfills keywords for a plan made before keywords existed.
	Keywords(ctx context.Context, goal string, milestones []string) ([]string, error)
}

// Shield may only suppress an approved interruption (§6). Phase 2 has no
// Relevance service, so the only implementation answers UNKNOWN, which behaves
// exactly like Groq being unreachable.
type Shield interface {
	Assess(url, title string) domain.ShieldResult
}

type NoShield struct{}

func (NoShield) Assess(string, string) domain.ShieldResult { return domain.ShieldUnknown }

const maxSegments = 1000 // not in the spec; keeps state.json bounded

type App struct {
	mu      sync.Mutex
	repo    storage.Repository
	clock   clock.Clock
	planner Planner
	shield  Shield
	st      *domain.State
	saveErr error
	filling atomic.Bool
	// Keyword backfill (goalplan.go): one request at a time, at most one
	// attempt per keywordRetry. In memory only: a restart may retry sooner.
	fillingKeywords  atomic.Bool
	keywordAttemptAt time.Time
	// metadata: channel/description of blacklisted pages, in memory only
	// (metadata.go). Guarded by mu.
	metadata map[string]pageMetadata
	// smallPlan: the plan of a goal refused as too small (goalplan.go). Guarded by mu.
	smallPlan *cachedPlan
	// chatter: the conversation with Max (chat.go). Guarded by mu.
	chatter Chatter
	// transcriber: the user's voice to text (stt.go). Guarded by mu.
	transcriber Transcriber
}

func New(repo storage.Repository, clk clock.Clock, planner Planner, shield Shield) (*App, error) {
	st, err := repo.Load()
	if err != nil {
		return nil, err
	}
	return &App{repo: repo, clock: clk, planner: planner, shield: shield, st: st}, nil
}

// persist writes state. On failure the last good state stays in memory and the
// error is surfaced through Health (§17).
func (a *App) persist() {
	a.saveErr = a.repo.Save(a.st)
	if a.saveErr != nil {
		log.Printf("state write failed (kept in memory): %v", a.saveErr)
	}
}

func (a *App) Health() (persistenceOK bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.saveErr == nil
}

// --- events -----------------------------------------------------------------

type Event struct {
	Activity domain.ActivityState `json:"activity"`
	Type     string               `json:"type"`
	TabID    int                  `json:"tabId"`
	URL      string               `json:"url"`
	Title    string               `json:"title"`
	Focused  bool                 `json:"focused"`
}

type StepView struct {
	ID               string `json:"id"`
	Text             string `json:"text"`
	EstimatedMinutes int    `json:"estimatedMinutes"`
	Milestone        string `json:"milestone"`
	// Door: on the second difficult day in a row, the step as a door, not a
	// task ("open the file"). Shown instead of Text, never explained.
	Door string `json:"door,omitempty"`
}

type AnchorView struct {
	Tier          int    `json:"tier"`
	Snippet       string `json:"snippet"`
	Title         string `json:"title"`
	URL           string `json:"url"`
	ScrollPercent int    `json:"scrollPercent"`
}

type Payload struct {
	DeliveryID string `json:"deliveryId,omitempty"`
	// CaptureAnchor tells the extension whether the content script may read the
	// page for a return anchor. It is false on blacklisted pages.
	CaptureAnchor bool `json:"captureAnchor"`
	// ReadMetadata allows reading the page's channel/description; true only
	// on blacklisted pages (metadata.go).
	ReadMetadata bool        `json:"readMetadata,omitempty"`
	Site         string      `json:"site,omitempty"`
	Step         *StepView   `json:"step,omitempty"`
	Anchor       *AnchorView `json:"anchor,omitempty"`
	Lang         string      `json:"lang,omitempty"` // the goal's language, for the card text
	Era          string      `json:"era,omitempty"`  // SHOW_NEW_ERA only
	EraLabel     string      `json:"eraLabel,omitempty"`
	// CloseDeliveryID: the server closed this card (answered elsewhere,
	// ignored, or no longer relevant); the page takes it down.
	CloseDeliveryID string `json:"closeDeliveryId,omitempty"`
	// Greeting: this is the day's first card, so it opens with a hello.
	Greeting bool `json:"greeting,omitempty"`
	// Gentle: a repeated "did you finish?" (after "not yet"), shown small.
	Gentle bool `json:"gentle,omitempty"`
	// Task: SHOW_TASK only, the user's own task Max reminds them of (ideas.go),
	// and how the card asks: remind, checkin, resume or carry (tasklife.go).
	Task     *TaskView `json:"task,omitempty"`
	TaskMode string    `json:"taskMode,omitempty"`
	// Other: refocus only, the task the user drifted to.
	Other *TaskView `json:"other,omitempty"`
	// Phase 4 cards (support.go): the welcome back after an absence, the
	// weekend treasure's idea count, the old idea of a difficult day, and
	// whether the user's own recording may be offered (its cooldown).
	Recovery *RecoveryView `json:"recovery,omitempty"`
	Ideas    int           `json:"ideas,omitempty"`
	Idea     *IdeaView     `json:"idea,omitempty"`
	Voice    bool          `json:"voice,omitempty"`
}

type Decision struct {
	Command domain.Command `json:"command"`
	Reason  string         `json:"reason"`
	Payload Payload        `json:"payload"`
}

func (a *App) stepView(s *domain.Step) *StepView {
	if s == nil {
		return nil
	}
	v := &StepView{ID: s.ID, Text: s.Text, EstimatedMinutes: s.EstimatedMinutes}
	if m := a.st.MilestoneByID(s.MilestoneID); m != nil {
		v.Milestone = m.Title
	}
	if cur := a.st.CurrentStep(); cur != nil && cur.ID == s.ID {
		if day := a.st.Days[domain.DayKey(a.now())]; day != nil && a.st.DifficultTier(day) >= 1 {
			v.Door = domain.DoorText(s.Text, a.st.Language())
		}
	}
	return v
}

// anchorView is the step's return anchor, only if captured today: a snippet
// from hours or days ago ("You had written: python") is not where you were.
func (a *App) anchorView(stepID string) *AnchorView {
	an, ok := a.st.Anchors[stepID]
	if !ok || domain.DayKey(an.CapturedAt) != domain.DayKey(a.now()) {
		return nil
	}
	return &AnchorView{Tier: an.Tier, Snippet: an.Snippet, Title: an.Title, URL: an.URL, ScrollPercent: an.ScrollPercent}
}

// HandleEvent is POST /browser/event: it records the signal, advances the
// deterministic session state and returns exactly one command with a reason.
// Nothing here calls Groq.
func (a *App) HandleEvent(ev Event) (Decision, error) {
	if !domain.ValidEventType(ev.Type) {
		return Decision{}, ErrInvalid
	}
	// Older clients/state have no activity signal: never assume they are active.
	if ev.Activity != "" && ev.Activity != domain.ActivityActive && ev.Activity != domain.ActivityIdle && ev.Activity != domain.ActivityLocked {
		return Decision{}, ErrInvalid
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.st
	if !st.Profile.Onboarded || (st.Goal == nil && st.LifeGoal == "") {
		return Decision{Command: domain.CmdDoNothing, Reason: "onboarding_incomplete"}, nil
	}
	now := a.now()
	cfg := st.Settings
	page := domain.ClassifyPage(ev.URL, st.Blacklist)
	// Shorts/reels are distraction by definition (§7), whatever their title.
	if page.Kind == domain.PageWeb && !page.DeterministicDistraction {
		// The channel/description read from a blacklisted page count too (MVP §4).
		text := strings.TrimSpace(ev.Title + " " + a.metadataFor(ev.URL, now))
		page.Relevant = domain.PageRelevant(ev.URL, text, st.GoalKeywords())
	}
	// A search that is not about the goal ("reddit") is not work: it starts no
	// work session, credits no step time and is never a place to return to.
	// It stays a web page, so an open card still follows the user onto it.
	page.NotWork = page.Kind == domain.PageWeb && !page.Relevant && domain.IsSearchResults(ev.URL)
	obs := domain.Observation{Activity: ev.Activity, Focused: ev.Focused, TabID: ev.TabID, URL: ev.URL, Title: ev.Title, Page: page}

	// Good day / bad day: credit the interval the previous page covered, then
	// note whether this page is about the goal (domain/day.go).
	day := st.Day(now)
	credit := st.Session.Credit(now, cfg)
	day.Account(st.Session.Last, credit)
	// Time on pages about the task in progress (tasklife.go).
	doing := st.DoingTask()
	if doing != nil && st.Session.Last.TaskRelevant && credit > 0 && st.Session.Last.Focused && st.Session.Last.Activity == domain.ActivityActive {
		doing.WorkedMs += credit.Milliseconds()
	}
	day.Observe(obs)
	if st.OpenCard != nil {
		st.OpenCard.Account(st.Session.Last, credit) // toward "ignored" (card.go)
	}

	seg, focusedMs := st.Session.AccountElapsed(now, cfg)
	if seg != nil {
		st.Segments = append(st.Segments, *seg)
		if len(st.Segments) > maxSegments {
			st.Segments = st.Segments[len(st.Segments)-maxSegments:]
		}
	}
	if focusedMs > 0 {
		st.Session.SessionFocusedMs += focusedMs
		if st.CurrentStep() != nil {
			st.WorkContext.FocusedMsOnStep += focusedMs
		}
	}

	st.EnsureCurrentTask(now)
	st.Session.Advance(obs, now, cfg)
	st.Session.Record(obs, now)
	if taskRel := domain.TaskPageRelevant(doing, page, ev.URL, ev.Title); taskRel {
		st.Session.Last.TaskRelevant = true
		if ev.Focused && ev.Activity == domain.ActivityActive {
			doing.LastURL, doing.LastTitle, doing.LastAt = clipRunes(ev.URL, 500), clipRunes(ev.Title, 120), now
		}
	}
	// Track the latest active work page, not an unattended tab (§5.4, §9).
	if ev.Focused && ev.Activity == domain.ActivityActive && page.Kind == domain.PageWeb && !page.Blacklisted && !page.NotWork {
		st.WorkContext.LastWorkTabID = ev.TabID
		st.WorkContext.LastWorkURL = ev.URL
	}
	if ev.Focused && ev.Activity == domain.ActivityActive && page.Relevant {
		st.WorkContext.LastRelevantTabID, st.WorkContext.LastRelevantURL, st.WorkContext.LastRelevantAt = ev.TabID, ev.URL, now
	}
	if ev.Focused && ev.Activity == domain.ActivityActive && page.Kind == domain.PageWeb {
		a.noteActive(now) // a welcome back after days away (support.go)
	}
	st.UpdatePendingPrompt(now)

	dec := a.decide(now, obs)
	if ev.Type == domain.EvHeartbeat {
		a.fillStepsAsync()
		a.fillKeywordsAsync(now)
	}
	a.persist()
	return dec, nil
}

// decide maps the new state to one command. Order follows the §8 flowchart.
func (a *App) decide(now time.Time, obs domain.Observation) Decision {
	st := a.st
	step := st.CurrentStep()
	day := st.Day(now)
	d := Decision{Command: domain.CmdDoNothing}
	d.Payload.Site = obs.Page.Site
	d.Payload.Lang = st.Language()

	// Content scripts may read a page only if it is a real, non-blacklisted page
	// inside a work context.
	d.Payload.CaptureAnchor = obs.Focused && obs.Activity == domain.ActivityActive && obs.Page.Kind == domain.PageWeb && !obs.Page.Blacklisted &&
		!obs.Page.NotWork && st.Session.ContextRecent(now, st.Settings) && step != nil
	// The page's channel/description may be read on blacklisted pages only,
	// never on shorts/reels (always a distraction) (MVP §4, metadata.go).
	d.Payload.ReadMetadata = obs.Focused && obs.Activity == domain.ActivityActive && obs.Page.Kind == domain.PageWeb &&
		obs.Page.Blacklisted && !obs.Page.DeterministicDistraction

	switch {
	case !obs.Focused:
		d.Reason = "chrome_unfocused"
		return d
	case obs.Activity != domain.ActivityActive:
		d.Reason = "user_inactive"
		return d
	case obs.Page.Kind != domain.PageWeb || obs.TabID < 0:
		d.Reason = "neutral_page"
		return d
	}

	closed := a.expireCard(now, obs, day)
	switch {
	case st.EraToCelebrate() != "" && (st.OpenCard == nil || st.OpenCard.Command != domain.CmdShowNewEra):
		// A new era interrupts whatever the day's mode, even "not today",
		// and takes the place of any other open card. It is the app's
		// celebration: it is never withheld, with or without a step plan.
		if st.OpenCard != nil {
			closed = a.closeCard(now, true)
		}
		a.issueNewEra(&d)
	case st.Goal == nil:
		a.decideChatFirst(now, obs, day, &d)
	case st.OpenCard != nil:
		// expireCard left only a card that applies to this page: it follows.
		a.followCard(&d)
	case st.Recovery != nil && !st.Recovery.Shown && st.Recovery.Date == day.Date:
		// Back after days away: a calm hello, the new date, one small step.
		a.issueWelcomeBack(now, &d)
	case a.decideTask(now, obs, day, &d):
		// The user's own task for today, before the goal's return screen:
		// what they asked to be reminded of comes first.
	case a.silenceReason(day, obs.Page.Site) != "":
		d.Reason = a.silenceReason(day, obs.Page.Site)
		a.decideThumbsUp(obs, day, &d) // a bad day still gets its quiet thumbs-up
	case obs.Page.Blacklisted:
		a.decideRamp(now, obs, step, day, &d)
		a.decideQuiet(obs, day, &d)
	default:
		a.decideWorkPage(now, step, &d)
		a.decideQuiet(obs, day, &d)
	}
	if domain.PersistentCard(d.Command) && st.OpenCard == nil {
		a.openCard(now, day, &d)
	}
	if closed != "" && closed != d.Payload.DeliveryID {
		// Tell the page to take down the card the server just closed.
		d.Payload.CloseDeliveryID = closed
		if d.Command == domain.CmdDoNothing {
			d.Command, d.Reason = domain.CmdCloseCard, "card_closed"
		}
	}
	return d
}

// decideQuiet runs when no ramp, completion prompt or greeting was chosen:
// the day question if it is due, otherwise a thumbs-up on a relevant page.
func (a *App) decideQuiet(obs domain.Observation, day *domain.DayRecord, d *Decision) {
	if d.Command != domain.CmdDoNothing {
		return
	}
	now := a.now()
	if a.decideTreasure(now, obs, d) || a.decideOldIdea(now, day, d) {
		return
	}
	if day.AskDue(a.st.Settings) {
		d.Command, d.Reason = domain.CmdAskDay, "no_goal_activity_today"
		d.Payload.Step = a.stepView(a.st.CurrentStep())
		return
	}
	a.decideThumbsUp(obs, day, d)
}

func (a *App) decideThumbsUp(obs domain.Observation, day *domain.DayRecord, d *Decision) {
	if d.Command == domain.CmdDoNothing && obs.Page.Relevant && day.Thumb(obs.Page.PageKey) {
		d.Command, d.Reason = domain.CmdShowThumbsUp, "relevant_page"
	}
}

// issueNewEra records the era as announced now; the card then follows the
// user (card.go) until they respond to it or ignore it.
func (a *App) issueNewEra(d *Decision) {
	a.st.CelebratedEra = a.st.EraToCelebrate()
	d.Command, d.Reason = domain.CmdShowNewEra, "new_era"
	a.eraPayload(d)
}

func (a *App) eraPayload(d *Decision) {
	era := a.st.CelebratedEra
	d.Payload.Era = era
	if i := domain.EraIndex(era); i >= 0 {
		d.Payload.EraLabel = domain.EraLabels[i]
	}
}

// silenceReason applies only to unsolicited commands, never explicit requests.
func (a *App) silenceReason(day *domain.DayRecord, site string) string {
	if day.NotToday {
		return "not_today"
	}
	if rec := a.st.LatestIntervention(day.Date, site); rec != nil && rec.Choice == domain.ChoiceBrowse {
		return "browse_today"
	}
	return ""
}

func (a *App) decideRamp(now time.Time, obs domain.Observation, step *domain.Step, day *domain.DayRecord, d *Decision) {
	st := a.st
	switch {
	case st.Session.State == domain.StateIdle:
		d.Reason = "no_work_context"
	case st.Session.State != domain.StateDistracted:
		d.Reason = "grace_not_elapsed"
	case st.SessionGoalMet():
		d.Reason = "session_time_met"
	case st.InterventionShown(day.Date, obs.Page.Site):
		d.Reason = "already_shown_today"
	case step == nil:
		d.Reason = "no_current_step"
	default:
		// The shield is the last check and can only cancel (§6, §8). Deterministic
		// paths never reach it; UNKNOWN shows the screen.
		result := domain.ShieldUnknown
		switch {
		case obs.Page.DeterministicDistraction:
		case obs.Page.Relevant:
			// Metadata as a shield (MVP §4): the page is about the goal, e.g. a
			// Python video on YouTube for a Python goal. It only silences.
			result = domain.ShieldRelevant
		default:
			result = a.shield.Assess(obs.URL, obs.Title)
		}
		if result == domain.ShieldRelevant {
			d.Reason = "shield_relevant"
			return
		}
		delivery := newDelivery(now, obs)
		rec := st.LatestIntervention(day.Date, obs.Page.Site)
		if rec == nil {
			st.Interventions = append(st.Interventions, domain.InterventionRecord{Site: obs.Page.Site, Date: day.Date})
			rec = &st.Interventions[len(st.Interventions)-1]
		}
		rec.Delivery, rec.Status = delivery, "issued"
		d.Payload.DeliveryID = delivery.ID
		d.Command = domain.CmdShowRamp
		d.Reason = "grace_elapsed_shield_" + string(result)
		if obs.Page.DeterministicDistraction {
			d.Reason = "grace_elapsed_distraction_path"
		}
		d.Payload.Site = obs.Page.Site
		d.Payload.Step = a.stepView(step)
		d.Payload.Anchor = a.anchorView(step.ID)
	}
}

func (a *App) decideWorkPage(now time.Time, step *domain.Step, d *Decision) {
	st := a.st
	if st.PromptDeliverable(now) {
		st.Pending.ShownAt = now
		st.Pending.Asks++
		d.Command = domain.CmdAskCompletion
		d.Reason = "focused_time_reached_threshold"
		if st.Pending.Asks > 1 {
			d.Reason = "more_work_after_not_yet"
		}
		d.Payload.Step = a.stepView(step)
		d.Payload.Gentle = st.Pending.Asks > 1
		return
	}
	// No separate greeting card: the day's first card says hello (card.go).
	d.Reason = "working"
}

// --- ramp and step responses --------------------------------------------------

type RampInput struct {
	Site   string `json:"site"`
	Choice string `json:"choice"`
}

type RampResult struct {
	DismissUnsolicited *DismissUnsolicited `json:"dismissUnsolicited,omitempty"`
	LastWorkTabID      int                 `json:"lastWorkTabId"`
	LastWorkURL        string              `json:"lastWorkUrl"`
	AnchorURL          string              `json:"anchorUrl"`
	// SearchQuery: the last resort, a search for the current step, never a
	// made-up link. The extension turns it into a search page.
	SearchQuery string `json:"searchQuery,omitempty"`
}

type DismissUnsolicited struct {
	Commands []domain.Command `json:"commands"`
	Site     string           `json:"site,omitempty"`
}

// RespondRamp is POST /ramp/respond. Continue returns where to send the user.
// Undo (5-second window enforced by the client) reverts the suppression only.
func (a *App) RespondRamp(in RampInput) (RampResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch in.Choice {
	case domain.ChoiceContinue, domain.ChoiceBrowse, domain.ChoiceNotToday, domain.ChoiceUndo:
	default:
		return RampResult{}, ErrInvalid
	}
	now := a.now()
	day := a.st.Day(now)
	rec := a.st.LatestIntervention(day.Date, in.Site)
	if rec == nil || (rec.ShownAt.IsZero() && rec.Delivery.ID == "") {
		return RampResult{}, ErrNoPending
	}
	if rec.ShownAt.IsZero() {
		// Issued but never acknowledged: the acknowledgement can be refused
		// (e.g. Chrome briefly reported no window focus) while the card is on
		// screen. An answer from the card proves it was shown, so record it as
		// the acknowledgement would have; otherwise Continue silently did
		// nothing. A delivery discarded by a dismissal has no ID and still fails.
		rec.ShownAt, rec.Status = now, "shown"
	}
	res := RampResult{}
	if c := a.st.OpenCard; c != nil && c.Command != domain.CmdShowNewEra {
		answered := in.Choice != domain.ChoiceUndo && c.Command == domain.CmdShowRamp && c.Site == in.Site
		if answered || in.Choice == domain.ChoiceNotToday { // "not today" closes every card but a new era
			a.st.OpenCard = nil
		}
	}
	switch in.Choice {
	case domain.ChoiceUndo:
		rec.Choice = ""
		day.NotToday = day.DayAnswer == domain.DayChoiceWont // a "not today" from the day question stands
		for _, r := range a.st.Interventions {
			if r.Date == day.Date && r.Choice == domain.ChoiceNotToday {
				day.NotToday = true
			}
		}
	case domain.ChoiceNotToday:
		rec.Choice = in.Choice
		day.NotToday = true
		res.DismissUnsolicited = &DismissUnsolicited{Commands: []domain.Command{domain.CmdShowRamp, domain.CmdAskCompletion, domain.CmdAskDay}}
	case domain.ChoiceBrowse:
		rec.Choice = in.Choice
		res.DismissUnsolicited = &DismissUnsolicited{Commands: []domain.Command{domain.CmdShowRamp, domain.CmdAskCompletion, domain.CmdAskDay}, Site: rec.Site}
	case domain.ChoiceContinue:
		rec.Choice = in.Choice
		a.continueTargets(now, &res)
	}
	if scope := res.DismissUnsolicited; scope != nil {
		// Discard issued deliveries without consuming eligibility. Undo must not
		// resurrect an old command or its acknowledgement.
		for i := range a.st.Interventions {
			r := &a.st.Interventions[i]
			if r.Date == day.Date && r.ShownAt.IsZero() && (scope.Site == "" || r.Site == scope.Site) {
				r.Delivery = domain.Delivery{}
			}
		}
	}
	a.persist()
	return res, nil
}

type StepInput struct {
	StepID string `json:"stepId"`
	Answer string `json:"answer"` // "yes" | "not_yet"
}

type StepResult struct {
	Announcement     string    `json:"announcement"`
	StepsDone        int       `json:"stepsDone"`
	StepsTotal       int       `json:"stepsTotal"`
	CurrentStep      *StepView `json:"currentStep"`
	NextStepsPending bool      `json:"nextStepsPending"`
	GoalCompleted    bool      `json:"goalCompleted"`
	// SearchQueries for the (new) current step: "Where do I start?" on the
	// next-step card. Search phrases, never links.
	SearchQueries []string `json:"searchQueries,omitempty"`
	Lang          string   `json:"lang"`
}

// RespondStep is POST /steps/respond. Only an explicit "yes" changes progress.
func (a *App) RespondStep(ctx context.Context, in StepInput) (StepResult, error) {
	a.mu.Lock()
	st := a.st
	now := a.now()
	step := st.CurrentStep()
	if step == nil || st.Pending == nil || st.Pending.StepID != in.StepID || step.ID != in.StepID {
		a.mu.Unlock()
		return StepResult{}, ErrNoPending
	}
	if in.Answer == "not_yet" || in.Answer == "yes" {
		// Answering about the current step is goal work, even on pages the
		// keywords miss: never ask this user "no goal work today?".
		st.Day(now).SawRelevant = true
		a.closeOpen(domain.CmdAskCompletion)
	}
	switch in.Answer {
	case "not_yet":
		st.SetPromptAside() // back after more work on the step, not after clock time
		res := a.stepResult("")
		a.persist()
		a.mu.Unlock()
		return res, nil
	case "yes":
	default:
		a.mu.Unlock()
		return StepResult{}, ErrInvalid
	}

	step.Status = domain.StatusDone
	st.Pending = nil
	st.WorkContext.FocusedMsOnStep = 0
	st.Day(now).ConfirmedSteps++
	ms := st.MilestoneByID(step.MilestoneID)
	msSteps := st.StepsOf(step.MilestoneID)
	done := 0
	for _, s := range msSteps {
		if s.Status == domain.StatusDone {
			done++
		}
	}
	announce := "Step " + strconv.Itoa(done) + " of " + strconv.Itoa(len(msSteps)) + " complete."
	if done == len(msSteps) && ms != nil {
		ms.Status = domain.StatusDone
		announce += " Milestone complete: " + ms.Title + "."
	}
	if st.CurrentMilestone() == nil && st.Goal != nil {
		st.Goal.Status = domain.GoalCompleted
	}
	st.RecomputeProgress()
	res := a.stepResult(announce)
	res.StepsDone, res.StepsTotal = done, len(msSteps)
	a.persist()
	needFill := st.CurrentMilestone() != nil && !st.CurrentMilestone().StepsGenerated
	a.mu.Unlock()

	if needFill {
		// Steps for the next milestone are generated when the previous one
		// completes (§9b). Failure is not fatal: the heartbeat retries.
		if err := a.fillMissingSteps(ctx); err != nil {
			log.Printf("next milestone steps not generated yet: %v", err)
		}
		a.mu.Lock()
		res = a.stepResult(announce)
		res.StepsDone, res.StepsTotal = done, len(msSteps)
		a.mu.Unlock()
	}
	return res, nil
}

// stepResult must be called with the mutex held.
func (a *App) stepResult(announce string) StepResult {
	st := a.st
	res := StepResult{Announcement: announce, CurrentStep: a.stepView(st.CurrentStep()), GoalCompleted: st.Goal != nil && st.Goal.Status == domain.GoalCompleted, Lang: st.Language()}
	res.SearchQueries = st.SearchQueries(st.CurrentStep())
	if m := st.CurrentMilestone(); m != nil && !m.StepsGenerated {
		res.NextStepsPending = true
	}
	return res
}

// --- anchor -------------------------------------------------------------------

type AnchorResult struct {
	Stored bool   `json:"stored"`
	Reason string `json:"reason"`
}

// StoreAnchor is POST /anchor. The server re-checks every hard rule: the
// extension is only a sensor.
func (a *App) StoreAnchor(in domain.AnchorInput) (AnchorResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.st
	now := a.now()
	step := st.CurrentStep()
	page := domain.ClassifyPage(in.URL, st.Blacklist)
	switch {
	case !st.Profile.Onboarded || step == nil:
		return AnchorResult{Reason: "no_current_step"}, nil
	case page.Kind != domain.PageWeb:
		return AnchorResult{Reason: "not_a_web_page"}, nil
	case page.Blacklisted:
		return AnchorResult{Reason: "blacklisted_page"}, nil
	case !st.Session.ContextRecent(now, st.Settings):
		return AnchorResult{Reason: "no_work_context"}, nil
	}
	next, ok := domain.NewAnchor(step.ID, in, now)
	if !ok {
		return AnchorResult{}, ErrInvalid
	}
	stored, has := st.Anchors[step.ID]
	if !domain.ReplacesAnchor(stored, has, next) {
		return AnchorResult{Reason: "lower_tier_than_best"}, nil
	}
	st.Anchors[step.ID] = next
	a.persist()
	return AnchorResult{Stored: true, Reason: "stored"}, nil
}

// --- voice --------------------------------------------------------------------

func (a *App) StoreVoice(ext string, r io.Reader, maxBytes int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.st.Goal == nil {
		return ErrNoGoal
	}
	name, err := a.repo.SaveVoice(ext, r, maxBytes)
	if err != nil {
		return err
	}
	a.st.Goal.VoiceRef = name
	a.persist()
	return nil
}

// decideChatFirst: with a life goal and no step plan (chat.go) there are no
// return screens or goal questions. An open card still follows the user and
// their own tasks are reminded of.
func (a *App) decideChatFirst(now time.Time, obs domain.Observation, day *domain.DayRecord, d *Decision) {
	if c := a.st.OpenCard; c != nil {
		// A check-in ("did you finish?") is worth more than a plain reminder.
		if t, _ := a.st.TaskToCheckIn(now); !(c.Command == domain.CmdShowTask && c.Mode == domain.ModeRemind && t != nil) {
			a.followCard(d)
			return
		}
		a.closeCard(now, false)
	}
	if a.decideTask(now, obs, day, d) {
		return
	}
	// Between work, at the weekend: the ideas the user collected. And when a
	// long stretch on distraction sites got nowhere, one of them, kindly.
	if !a.decideTreasure(now, obs, d) {
		a.decideIdeaNudge(now, obs, day, d)
	}
}
