// Package domain holds entities and the deterministic rules of the product.
// Nothing here performs I/O and nothing here depends on Groq.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type SessionState string

type ActivityState string

const (
	ActivityActive ActivityState = "active"
	ActivityIdle   ActivityState = "idle"
	ActivityLocked ActivityState = "locked"
)

const (
	StateIdle       SessionState = "IDLE"
	StateWorking    SessionState = "WORKING"
	StateDistracted SessionState = "DISTRACTED"
	StatePaused     SessionState = "PAUSED"
)

type PageKind string

const (
	PageWeb     PageKind = "web"
	PageNeutral PageKind = "neutral" // chrome://, extension pages, about:, empty
)

type Command string

const (
	CmdDoNothing     Command = "DO_NOTHING"
	CmdShowRamp      Command = "SHOW_RAMP"
	CmdAskCompletion Command = "ASK_COMPLETION"
	CmdAskDay        Command = "ASK_DAY"        // "no goal work today?" with three answers
	CmdShowThumbsUp  Command = "SHOW_THUMBS_UP" // quiet badge on a relevant page, no card
	CmdShowNewEra    Command = "SHOW_NEW_ERA"   // the planet reached a new era; ignores the day's mode
	CmdCloseCard     Command = "CLOSE_CARD"     // take down payload.closeDeliveryId (card.go)
	CmdShowTask      Command = "SHOW_TASK"      // reminder about the user's own task for today (tasks.go)
)

type ShieldResult string

const (
	ShieldRelevant    ShieldResult = "RELEVANT"
	ShieldNotRelevant ShieldResult = "NOT_RELEVANT"
	ShieldUnknown     ShieldResult = "UNKNOWN"
)

const (
	StatusPending = "pending"
	StatusDone    = "done"

	GoalActive    = "active"
	GoalCompleted = "completed"

	ChoiceContinue = "continue"
	ChoiceBrowse   = "browse"
	ChoiceNotToday = "not_today"
	ChoiceUndo     = "undo"

	PromptCompletion = "completion"
)

// Event types accepted by POST /browser/event (architecture-plan-v2 §14).
const (
	EvTabActivated    = "tab_activated"
	EvTabUpdated      = "tab_updated"
	EvTabRemoved      = "tab_removed"
	EvWindowFocus     = "window_focus"
	EvWindowBlur      = "window_blur"
	EvHeartbeat       = "heartbeat"
	EvActivityChanged = "activity_changed"
)

func ValidEventType(t string) bool {
	switch t {
	case EvTabActivated, EvTabUpdated, EvTabRemoved, EvWindowFocus, EvWindowBlur, EvHeartbeat, EvActivityChanged:
		return true
	}
	return false
}

type Preferences struct {
	WorkBlock   string `json:"workBlock"`
	BestTime    string `json:"bestTime"`
	Distraction string `json:"distraction"`
	ReturnCue   string `json:"returnCue"`
}

type UserProfile struct {
	Preferences Preferences `json:"preferences"`
	Onboarded   bool        `json:"onboarded"`
	CreatedAt   time.Time   `json:"createdAt,omitzero"`
}

type PrimaryGoal struct {
	ID            string    `json:"id"`
	Statement     string    `json:"statement"`
	VoiceRef      string    `json:"voiceRef"`
	Status        string    `json:"status"`
	TargetHorizon string    `json:"targetHorizon"`
	CreatedAt     time.Time `json:"createdAt"`
	// Keywords decide which pages are about this goal (relevance.go). Empty
	// for plans made before keywords existed; GoalKeywords() falls back.
	Keywords []string `json:"keywords,omitempty"`
	// EstimatedHours of focused work for the whole goal (0: unknown).
	EstimatedHours float64 `json:"estimatedHours,omitempty"`
	// Fallback: the plan was made without the AI (fallback.go); later
	// milestones get fallback steps too if the AI is still unreachable.
	Fallback bool `json:"fallback,omitempty"`
}

type Milestone struct {
	ID             string  `json:"id"`
	GoalID         string  `json:"goalId"`
	Title          string  `json:"title"`
	Order          int     `json:"order"`
	Weight         float64 `json:"weight"`
	Status         string  `json:"status"`
	StepsGenerated bool    `json:"stepsGenerated"`
}

type Step struct {
	ID               string  `json:"id"`
	MilestoneID      string  `json:"milestoneId"`
	Order            int     `json:"order"`
	Text             string  `json:"text"`
	EstimatedMinutes int     `json:"estimatedMinutes"`
	Weight           float64 `json:"weight"`
	Status           string  `json:"status"`
	ScheduledFor     string  `json:"scheduledFor,omitempty"` // YYYY-MM-DD, optional
	// SearchQueries are up to three search phrases (never URLs) for "I don't
	// know where to start"; see State.SearchQueries for older steps.
	SearchQueries []string `json:"searchQueries,omitempty"`
}

type ReturnAnchor struct {
	StepID        string    `json:"stepId"`
	Tier          int       `json:"tier"`
	URL           string    `json:"url"`
	Title         string    `json:"title"`
	Snippet       string    `json:"snippet"`
	ScrollPercent int       `json:"scrollPercent"`
	CapturedAt    time.Time `json:"capturedAt"`
}

type WorkContext struct {
	LastWorkTabID   int    `json:"lastWorkTabId"`
	LastWorkURL     string `json:"lastWorkUrl"`
	FocusedMsOnStep int64  `json:"focusedTimeOnStep"`
	// The latest page that was about the goal (relevance.go): where Continue
	// sends the user back to, if it was today.
	LastRelevantTabID int       `json:"lastRelevantTabId,omitempty"`
	LastRelevantURL   string    `json:"lastRelevantUrl,omitempty"`
	LastRelevantAt    time.Time `json:"lastRelevantAt,omitzero"`
}

type PendingPrompt struct {
	Type         string    `json:"type"`
	StepID       string    `json:"stepId"`
	CreatedAt    time.Time `json:"createdAt"`
	SnoozedUntil time.Time `json:"snoozedUntil,omitzero"`
	// ShownAt is not in the data model table; it stops a heartbeat from
	// re-showing a prompt the user already saw or dismissed with Escape.
	ShownAt time.Time `json:"shownAt,omitzero"`
	// Asks: how many times the question was shown (today: the prompt expires
	// at midnight). After "not yet" it comes back only after more WORK on the
	// step (WorkAtAskMs + CompletionReask), gently, at most MaxCompletionAsks.
	Asks        int   `json:"asks,omitempty"`
	WorkAtAskMs int64 `json:"workAtAskMs,omitempty"`
}

// MaxCompletionAsks: "did you finish?" is asked at most this often per step
// per day, so a user who keeps saying "not yet" is not nagged.
const MaxCompletionAsks = 3

// LastObservation is what the previous event showed, kept so the next event can
// close the elapsed interval into a segment.
type LastObservation struct {
	TabID        int           `json:"tabId"`
	URL          string        `json:"url"`
	Activity     ActivityState `json:"activity"`
	Focused      bool          `json:"focused"`
	Kind         PageKind      `json:"kind"`
	Blacklisted  bool          `json:"blacklisted"`
	Domain       string        `json:"domain"`
	PageKey      string        `json:"pageKey"`
	Relevant     bool          `json:"relevant,omitempty"`
	NotWork      bool          `json:"notWork,omitempty"`
	TaskRelevant bool          `json:"taskRelevant,omitempty"` // the page was about the task in progress
}

type FocusSession struct {
	State                  SessionState `json:"state"`
	ContextStrength        string       `json:"contextStrength"` // "weak" in Phase 2; "strong" needs Groq
	StartedAt              time.Time    `json:"startedAt,omitzero"`
	LastRelevantActivityAt time.Time    `json:"lastRelevantActivityAt,omitzero"`
	DistractionStartedAt   time.Time    `json:"distractionStartedAt,omitzero"`
	LastEventAt            time.Time    `json:"lastEventAt,omitzero"`

	// Not in the data model table; required to implement §8 grace accounting.
	DistractionSite     string          `json:"distractionSite,omitempty"`
	DistractionPausedMs int64           `json:"distractionPausedMs,omitempty"`
	NonBlacklistedSince time.Time       `json:"nonBlacklistedSince,omitzero"`
	SessionFocusedMs    int64           `json:"sessionFocusedMs"`
	Last                LastObservation `json:"last"`
}

type BrowsingSegment struct {
	Domain     string    `json:"domain"`
	PageKey    string    `json:"pageKey"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	DurationMs int64     `json:"durationMs"`
}

type BlacklistEntry struct {
	Domain  string `json:"domain"`
	Enabled bool   `json:"enabled"`
	Source  string `json:"source"` // "default" | "user"
}

type Delivery struct {
	ID       string    `json:"id"`
	TabID    int       `json:"tabId"`
	URL      string    `json:"url"`
	IssuedAt time.Time `json:"issuedAt"`
}

type InterventionRecord struct {
	Delivery Delivery  `json:"delivery"`
	Status   string    `json:"status"` // issued until acknowledged by the rendered page
	Site     string    `json:"site"`
	Date     string    `json:"date"`
	Choice   string    `json:"choice"` // "" until answered
	ShownAt  time.Time `json:"shownAt"`
}

type DayRecord struct {
	Date               string `json:"date"`
	ConfirmedSteps     int    `json:"confirmedSteps"`
	DifficultDayStreak int    `json:"difficultDayStreak"` // Phase 4; untouched here
	NotToday           bool   `json:"notToday"`           // the bad-day flag (day.go): set by either "not today" answer
	Greeted            bool   `json:"greeted"`            // the day's first card already said hello (card.go)

	// Good day / bad day (day.go).
	SawRelevant  bool  `json:"sawRelevant,omitempty"`  // a relevant page, or an answer about the current step
	ActiveMs     int64 `json:"activeMs,omitempty"`     // active web time before the first relevant page
	RelevantMs   int64 `json:"relevantMs,omitempty"`   // the day's effort score; never moves the planet
	WebMs        int64 `json:"webMs,omitempty"`        // all active web time today (task reminders, tasks.go)
	DistractedMs int64 `json:"distractedMs,omitempty"` // active time on distraction sites today (ideas.go)
	// Difficult-day escalation (support.go): the old idea was offered today,
	// and the user took it (which breaks the zero).
	IdeaOffered  bool     `json:"ideaOffered,omitempty"`
	IdeaAccepted bool     `json:"ideaAccepted,omitempty"`
	NextAskMs    int64    `json:"nextAskMs,omitempty"` // ActiveMs at which to ask again (0: DayAskAfter)
	DayAnswer    string   `json:"dayAnswer,omitempty"` // last answer to the day question
	Thumbed      []string `json:"thumbed,omitempty"`   // page keys that already got a thumbs-up
}

type ProgressState struct {
	ConfirmedWeight float64 `json:"confirmedWeight"`
	TotalWeight     float64 `json:"totalWeight"`
	Percentage      float64 `json:"percentage"`
	Era             string  `json:"era"` // one of EraNames, set by RecomputeProgress; views derive the era from Percentage
}

// Idea is parked for later, including items captured as tasks. It is not a Step.
type Idea struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Type      string    `json:"type"`
	Priority  int       `json:"priority"`
	CreatedAt time.Time `json:"createdAt"`
	Reviewed  bool      `json:"reviewed"`
	// Tasks only (tasks.go): the day it is for, when it was done, and the
	// reminders about it (NudgeAtMs is the day's WebMs at the last one).
	Due       string     `json:"due,omitempty"`
	DoneAt    *time.Time `json:"doneAt,omitempty"`
	NudgeDate string     `json:"nudgeDate,omitempty"`
	NudgeAtMs int64      `json:"nudgeAtMs,omitempty"`
	Nudges    int        `json:"nudges,omitempty"` // reminders on NudgeDate
	// Chat-made tasks (chat.go): how long it takes (0: not told yet), by when in
	// the user's own words ("17:00", "tonight"), and the AI's priority (1 = first).
	EstimateMin int    `json:"estimateMin,omitempty"`
	Deadline    string `json:"deadline,omitempty"`
	Rank        int    `json:"rank,omitempty"`
	// EstimateAsks: how often Max asked how long or by when (chat.go); after
	// MaxEstimateAsks unanswered questions he assumes DefaultEstimateMin.
	EstimateAsks    int  `json:"estimateAsks,omitempty"`
	EstimateAssumed bool `json:"estimateAssumed,omitempty"`
	// Impact: how much finishing this task moves the life goal, 0 (rest, chores)
	// to 3 (a major step); nil (unknown) counts as 1. See chat.go.
	Impact *int `json:"impact,omitempty"`
	// AssumedAsked: Max already asked whether the time he guessed fits.
	AssumedAsked bool `json:"assumedAsked,omitempty"`
	// Life of a task (tasklife.go): in progress since StartedAt, the relevant
	// browser time so far, where the user left off, and the reminders already made.
	Status        string     `json:"status,omitempty"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	WorkedMs      int64      `json:"workedMs,omitempty"`
	Note          string     `json:"note,omitempty"`
	LastURL       string     `json:"lastUrl,omitempty"`
	LastTitle     string     `json:"lastTitle,omitempty"`
	Kind          string     `json:"kind,omitempty"`       // "browser" | "offline"
	StartQuery    string     `json:"startQuery,omitempty"` // the web search that starts a browser task
	LastAt        time.Time  `json:"lastAt,omitzero"`      // when LastURL was seen
	Keywords      []string   `json:"keywords,omitempty"`
	CheckAfter    time.Time  `json:"checkAfter,omitzero"`
	CheckDate     string     `json:"checkDate,omitempty"`
	CheckCount    int        `json:"checkCount,omitempty"`
	DeadlineAsked string     `json:"deadlineAsked,omitempty"`
	CarryAsked    string     `json:"carryAsked,omitempty"`
	UrgentAsked   string     `json:"urgentAsked,omitempty"`
	ResumeAt      time.Time  `json:"resumeAt,omitzero"`
	// GoalStep: a step of the life goal, shown one at a time (State.GoalSteps).
	GoalStep bool `json:"goalStep,omitempty"`
}

type State struct {
	Ideas         []Idea                  `json:"ideas"`
	Version       int                     `json:"version"`
	Profile       UserProfile             `json:"profile"`
	Goal          *PrimaryGoal            `json:"goal"`
	Milestones    []Milestone             `json:"milestones"`
	Steps         []Step                  `json:"steps"`
	Anchors       map[string]ReturnAnchor `json:"anchors"` // step id -> best anchor
	WorkContext   WorkContext             `json:"workContext"`
	Pending       *PendingPrompt          `json:"pendingPrompt"`
	Session       FocusSession            `json:"session"`
	Segments      []BrowsingSegment       `json:"segments"`
	Blacklist     []BlacklistEntry        `json:"blacklist"`
	Interventions []InterventionRecord    `json:"interventions"`
	Days          map[string]*DayRecord   `json:"days"`
	Progress      ProgressState           `json:"progress"`
	Settings      Settings                `json:"settings"`
	// CelebratedEra is the last era the user was shown arriving; a higher era
	// is announced once, whatever the day's mode (day.go).
	CelebratedEra string `json:"celebratedEra,omitempty"`
	// OpenCard is the unsolicited card that follows the user until answered
	// or ignored (card.go).
	OpenCard *OpenCard `json:"openCard,omitempty"`

	// Phase 4 (support.go).
	LastActiveDay string    `json:"lastActiveDay,omitempty"` // the last day with active web use
	Recovery      *Recovery `json:"recovery,omitempty"`      // a welcome back to show
	TreasureWeek  string    `json:"treasureWeek,omitempty"`  // the week the treasure was offered
	VoicePlayedAt time.Time `json:"voicePlayedAt,omitzero"`  // the recording's cooldown
	OfferedIdeas  []string  `json:"offeredIdeas,omitempty"`  // ideas offered on a difficult day
	// ClockOffsetDays moves the App Clock forward, for demos (⚙️, dev.go).
	ClockOffsetDays int `json:"clockOffsetDays,omitempty"`
	// ForceStorm shows the planet's storm weather on demand, for presentations
	// (⚙️, dev.go), whatever WeatherAt would otherwise compute from the day.
	ForceStorm bool `json:"forceStorm,omitempty"`

	// Chat (chat.go): the life goal the planet grows toward, Max's latest
	// encouragement, and the conversation.
	// AppLang: the language the user chose for the app, "el" or "en" ("": by
	// the language of their goal). It drives Max's words, the lamp and the cards.
	AppLang  string `json:"appLang,omitempty"`
	LifeGoal string `json:"lifeGoal,omitempty"`
	// GoalPoints: the size of the life goal in impact points (0: DefaultGoalPoints).
	GoalPoints float64       `json:"goalPoints,omitempty"`
	Motivation string        `json:"motivation,omitempty"`
	Chat       []ChatMessage `json:"chat,omitempty"`
	// Suggestions: quick replies shown under the chat for Max's last message.
	Suggestions []string `json:"suggestions,omitempty"`
	// Journal: what happened to the tasks, for "where was I today?" (tasklife.go).
	Journal []JournalEntry `json:"journal,omitempty"`
	// GoalSteps: the steps of the life goal still to come, in order. Only the
	// first becomes a task, when the one before it is finished.
	GoalSteps []ChatNewTask `json:"goalSteps,omitempty"`
}

func NewID(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}
