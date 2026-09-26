package domain

import "time"

// The chat with Max (application/chat.go). The AI only proposes changes; the
// application validates and applies them, so a bad answer can never corrupt
// the state.

// MaxChatKept bounds the stored conversation.
const MaxChatKept = 60

// The planet follows the LIFE GOAL, not the number of tasks. Each task has an
// impact (0 to 3) on that goal, and the goal has a size in points, both
// judged by the AI: a shower is worth 0, a milestone 3, and a goal that takes
// years needs hundreds of points. Progress is the impact of the finished
// tasks over the goal's size.
const (
	DefaultGoalPoints = 150.0
	MaxImpact         = 3
	DefaultImpact     = 1 // a task whose impact is not known
)

// Max asks how long a task takes, or by when, a few times at most; after that
// he assumes DefaultEstimateMin himself and says so, instead of nagging.
const (
	MaxEstimateAsks    = 3
	DefaultEstimateMin = 30
)

type ChatMessage struct {
	Role string    `json:"role"` // "user" | "max"
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// ChatTaskContext is a task as the AI sees it.
type ChatTaskContext struct {
	ID            string `json:"id"`
	Text          string `json:"text"`
	EstimateMin   int    `json:"estimateMinutes"`
	Deadline      string `json:"deadline"`
	Done          bool   `json:"done"`
	NeedsEstimate bool   `json:"needsEstimate"`
	AskedTimes    int    `json:"askedTimes"`
	// Assumed: Max already chose the time himself (the user did not say), so
	// he must not ask again.
	Assumed bool `json:"estimateAssumed"`
	// MustEstimate: the user did not say how long; Max chooses a time from the
	// kind of task. ConfirmGuess: he guessed one earlier and may ask, once,
	// whether it fits.
	MustEstimate bool `json:"youMustEstimate"`
	ConfirmGuess bool `json:"confirmYourGuess"`
	// Life of the task (tasklife.go): "doing" | "paused" | "", the relevant
	// browser minutes so far, where the user left off, and whether it was
	// left open from an earlier day.
	Status      string `json:"status"`
	WorkedMin   int    `json:"workedMinutes"`
	Note        string `json:"leftOffAt"`
	LastTitle   string `json:"lastPage"`
	Kind        string `json:"kind"`
	CarriedOver bool   `json:"carriedOver"`
	GoalStep    bool   `json:"stepOfLifeGoal"`
}

// ChatContext is everything about the app the AI is told, besides the
// conversation itself.
type ChatContext struct {
	Now       string            `json:"now"` // e.g. "Saturday 2026-09-26 14:05"
	Onboarded bool              `json:"onboarded"`
	LifeGoal  string            `json:"lifeGoal"`
	Progress  int               `json:"planetProgressPercent"`
	Tasks     []ChatTaskContext `json:"tasks"`
	Ideas     []string          `json:"savedIdeas"`
	Journal   []string          `json:"journal"`              // what happened lately, oldest first
	AppOpened bool              `json:"userJustOpenedTheApp"` // no new user message: greet
	Channel   string            `json:"channel"`              // "dashboard" | "page"
	Weekend   bool              `json:"weekend"`
	LangHint  string            `json:"languageHint,omitempty"`
	StepsLeft int               `json:"lifeGoalStepsLeft"` // steps after the current one
}

type ChatNewTask struct {
	Text        string `json:"text"`
	EstimateMin int    `json:"estimateMinutes"`
	Deadline    string `json:"deadline"`
	Asking      bool   `json:"asking"` // this reply asks the user how long or by when
	Guess       bool   `json:"guess"`  // the time is Max's own guess, not what the user said
	Impact      *int   `json:"impact"` // 0 to 3: how much it moves the life goal
	// Kind: "browser" when the work happens in a web browser, "offline" when
	// it does not (laundry, a phone call); Keywords are its subject terms.
	Kind     string   `json:"kind"`
	Keywords []string `json:"keywords"`
	// Query: a short web search that starts the task; the app opens it.
	Query string `json:"query"`
}

type ChatTaskUpdate struct {
	ID          string `json:"id"`
	EstimateMin int    `json:"estimateMinutes"`
	Deadline    string `json:"deadline"`
	Asking      bool   `json:"asking"`
	Guess       bool   `json:"guess"`
}

// ChatOutput is the AI's answer to one turn.
type ChatOutput struct {
	Reply              string           `json:"reply"`
	Suggestions        []string         `json:"suggestions"`
	Motivation         string           `json:"motivation"`
	LifeGoal           string           `json:"lifeGoal"`
	GoalPoints         float64          `json:"goalPoints"` // the size of the life goal, when it is set or changed
	AddTasks           []ChatNewTask    `json:"addTasks"`
	GoalSteps          []ChatNewTask    `json:"goalSteps"` // the life goal broken into ordered steps
	AddIdeas           []string         `json:"addIdeas"`
	UpdateTasks        []ChatTaskUpdate `json:"updateTasks"`
	DoneTasks          []string         `json:"doneTasks"`
	StartTasks         []string         `json:"startTasks"`
	RemoveTasks        []string         `json:"removeTasks"`
	Ranking            []string         `json:"ranking"`
	OnboardingComplete bool             `json:"onboardingComplete"`
}

// TaskHelp is Max's answer to "I don't know where to start" on a task: one
// tiny first action and, when a search would help, a few search phrases
// (never links: the extension turns them into search pages).
type TaskHelp struct {
	Step     string   `json:"step"`
	Searches []string `json:"searches"`
}

// ImpactPoints is what finishing the task is worth toward the life goal.
func (i Idea) ImpactPoints() int {
	if i.Impact == nil {
		return DefaultImpact
	}
	return min(max(*i.Impact, 0), MaxImpact)
}

// LifeGoalPoints is the size of the life goal.
func (s *State) LifeGoalPoints() float64 {
	if s.GoalPoints > 0 {
		return s.GoalPoints
	}
	return DefaultGoalPoints
}

// DonePoints is the impact of the tasks finished so far.
func (s *State) DonePoints() int {
	n := 0
	for _, i := range s.Ideas {
		if i.Type == "task" && i.DoneAt != nil {
			n += i.ImpactPoints()
		}
	}
	return n
}

// DoneTaskCount is the number of tasks the user has finished.
func (s *State) DoneTaskCount() int {
	n := 0
	for _, i := range s.Ideas {
		if i.Type == "task" && i.DoneAt != nil {
			n++
		}
	}
	return n
}
