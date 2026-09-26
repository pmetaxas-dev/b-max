package application

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"focuscompanion/internal/domain"
)

// ErrChatUnavailable: the AI could not answer. The user's message is kept.
var ErrChatUnavailable = errors.New("chat is unavailable")

// RateLimitedError: the AI's per-minute limit was reached; try again in Seconds.
// It is told to the user as it is, instead of a mysterious failure.
type RateLimitedError struct{ Seconds int }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("the AI is busy: try again in %d seconds", e.Seconds)
}

// unavailable turns an AI failure into what the user is told.
func unavailable(err error) error {
	var limited interface{ RetryAfterSeconds() int }
	if errors.As(err, &limited) {
		return &RateLimitedError{Seconds: limited.RetryAfterSeconds()}
	}
	return ErrChatUnavailable
}

// Chatter is the conversation with Max. Implemented by groq.Chatter; faked in tests.
type Chatter interface {
	Chat(ctx context.Context, cc domain.ChatContext, history []domain.ChatMessage) (domain.ChatOutput, error)
	// TaskHelp: one tiny first action (and searches) for a task the user
	// does not know how to start.
	TaskHelp(ctx context.Context, task, lang string) (domain.TaskHelp, error)
}

// SetChatter wires the conversation. Without one, chat answers ErrChatUnavailable.
func (a *App) SetChatter(c Chatter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.chatter = c
}

type ChatInput struct {
	Message string `json:"message"`
	// Kind "open": the dashboard was opened. Max greets, or stays quiet when
	// there is nothing to say (no AI call). Empty: a normal message.
	Kind string `json:"kind"`
	// Lang is the browser's language, a hint for the very first greeting.
	Lang string `json:"lang"`
	// Channel is where the user talks to Max: "dashboard" (default) or "page"
	// (the lamp on a website).
	Channel string `json:"channel"`
}

// ChatState is the conversation and what the dashboard shows beside it.
type ChatState struct {
	Messages    []domain.ChatMessage `json:"messages"`
	Tasks       []TaskView           `json:"tasks"`
	Onboarded   bool                 `json:"onboarded"`
	LifeGoal    string               `json:"lifeGoal"`
	Motivation  string               `json:"motivation"`
	Suggestions []string             `json:"suggestions"` // quick replies for Max's last message
	Lang        string               `json:"lang"`
	Planet      PlanetVisualState    `json:"planet"`
	Reply       string               `json:"reply,omitempty"`
	// NewMessages are the texts Max just said, in order (a scripted welcome
	// is several); Reply is the last of them.
	NewMessages []string `json:"newMessages,omitempty"`
	Skipped     bool     `json:"skipped,omitempty"` // Kind "open" with nothing to say
	// The idea chest: how many ideas, and how many the user has not looked at.
	IdeasTotal  int `json:"ideasTotal"`
	IdeasUnseen int `json:"ideasUnseen"`
}

const (
	maxChatChars     = 2000
	maxNewTasksTurn  = 10
	maxSuggestions   = 2
	openGreetingIdle = 4 * time.Hour
	// guessConfirmAfter: how old a task is before Max asks whether his guess fits.
	guessConfirmAfter = 10 * time.Minute
)

// welcome is what Max says on the very first open, before any AI call: who he
// is, what the planet is, and the first question. The app writes it so it is
// always right and instant.
func welcome(lang string) (msgs []string, suggestions []string) {
	if strings.HasPrefix(strings.ToLower(lang), "el") {
		return []string{
				"Γεια σου! Είμαι ο Max, ο βοηθός σου. Θα είμαι πάντα δίπλα σου για να οργανώνουμε μαζί τη μέρα σου.",
				"Βλέπεις τον πλανήτη δίπλα μου; Είναι ο δικός σου κόσμος. Ξεκινά άγονος και κάθε φορά που ολοκληρώνεις κάτι μεγαλώνει και εξελίσσεται, μέχρι να γίνει ο κόσμος του στόχου σου.",
				"Πες μου, ποιο είναι το μεγάλο σου όνειρο, αυτό που θέλεις να πετύχεις στη ζωή;",
			}, []string{
				"Θέλω να τρέξω μαραθώνιο", "Θέλω να τελειώσω τη σχολή μου",
			}
	}
	return []string{
			"Hi! I'm Max, your companion. I'll always be by your side to help you organise your day.",
			"See the planet next to me? It's your own world. It starts barren, and every time you finish something it grows and evolves, until it becomes the world of your goal.",
			"Tell me, what is the big dream you want to achieve in life?",
		}, []string{
			"I want to run a marathon", "I want to finish my degree",
		}
}

// chatState must be called with the mutex held.
func (a *App) chatState() ChatState {
	st := a.st
	cs := ChatState{
		Messages: append([]domain.ChatMessage{}, st.Chat...), Tasks: a.tasksLocked(),
		Onboarded: st.Profile.Onboarded, LifeGoal: st.LifeGoal, Motivation: st.Motivation,
		Suggestions: append([]string{}, st.Suggestions...),
		Lang:        st.Language(), Planet: a.summary().Planet,
		IdeasUnseen: len(st.UnseenIdeas()),
	}
	for _, i := range st.Ideas {
		if i.Type == "idea" {
			cs.IdeasTotal++
		}
	}
	return cs
}

// ChatState is GET /chat.
func (a *App) ChatState() ChatState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.chatState()
}

// awaitsEstimate: Max may still ask the user how long or by when.
func awaitsEstimate(i domain.Idea) bool {
	return i.NeedsEstimate() && i.EstimateAsks < domain.MaxEstimateAsks && !i.EstimateAssumed
}

// guessToConfirm: a time Max chose himself, that he has not yet asked about.
func guessToConfirm(i domain.Idea, now time.Time) bool {
	return i.OpenTask() && i.EstimateAssumed && i.EstimateMin > 0 && !i.AssumedAsked && now.Sub(i.CreatedAt) > guessConfirmAfter
}

// chatContext must be called with the mutex held.
func (a *App) chatContext(now time.Time, in ChatInput, appOpened bool) domain.ChatContext {
	st := a.st
	cc := domain.ChatContext{
		Now:       now.Format("Monday 2006-01-02 15:04"),
		Onboarded: st.Profile.Onboarded, LifeGoal: st.LifeGoal, Progress: int(st.Progress.Percentage),
		AppOpened: appOpened, Channel: "dashboard", LangHint: st.Language(),
		Weekend: now.Weekday() == time.Saturday || now.Weekday() == time.Sunday,
		Tasks:   []domain.ChatTaskContext{}, Ideas: []string{}, Journal: a.journalFor(),
		StepsLeft: len(st.GoalSteps),
	}
	if in.Channel == "page" {
		cc.Channel = "page"
	}
	for _, i := range st.Ideas {
		if i.Type == "idea" && len(cc.Ideas) < 10 {
			cc.Ideas = append(cc.Ideas, i.Text)
		}
		if i.Type != "task" || (i.DoneAt != nil && domain.DayKey(*i.DoneAt) != domain.DayKey(now)) {
			continue
		}
		cc.Tasks = append(cc.Tasks, domain.ChatTaskContext{
			ID: i.ID, Text: i.Text, EstimateMin: i.EstimateMin, Deadline: i.Deadline,
			Done: i.DoneAt != nil, NeedsEstimate: awaitsEstimate(i), AskedTimes: i.EstimateAsks,
			Assumed: i.EstimateAssumed, MustEstimate: i.OpenTask() && i.EstimateAssumed && i.EstimateMin == 0,
			ConfirmGuess: guessToConfirm(i, now), Status: i.Status, WorkedMin: int(i.WorkedMs / 60_000),
			Note: i.Note, LastTitle: i.LastTitle, Kind: i.Kind, GoalStep: i.GoalStep,
			CarriedOver: i.OpenTask() && i.Due != "" && i.Due < domain.DayKey(now),
		})
	}
	return cc
}

// needsToSpeak: on opening the dashboard Max speaks when it is a new
// conversation, after a long silence, or while a task still lacks an estimate.
func (a *App) needsToSpeak(now time.Time) bool {
	st := a.st
	if len(st.Chat) == 0 || now.Sub(st.Chat[len(st.Chat)-1].At) > openGreetingIdle {
		return true
	}
	today := domain.DayKey(now)
	spokeToday := domain.DayKey(st.Chat[len(st.Chat)-1].At) == today
	for _, i := range st.Ideas {
		if awaitsEstimate(i) || guessToConfirm(i, now) {
			return true
		}
		// Left open from an earlier day: the first open of the day recaps it.
		if !spokeToday && i.OpenTask() && i.Due != "" && i.Due < today {
			return true
		}
	}
	return false
}

// Chat is POST /chat: one turn of the conversation. The AI call happens
// outside the lock so browser events are not blocked.
func (a *App) Chat(ctx context.Context, in ChatInput) (ChatState, error) {
	msg := clipRunes(in.Message, maxChatChars)
	opened := in.Kind == "open"
	if !opened && msg == "" {
		return ChatState{}, ErrInvalid
	}

	a.mu.Lock()
	now := a.now()
	if a.st.AppLang == "" && in.Lang != "" {
		a.st.AppLang = langOf(in.Lang) // the first time: the browser's language
	}
	if opened && len(a.st.Chat) == 0 && !a.st.Profile.Onboarded {
		// The very first time: the app introduces Max and the planet itself.
		texts, suggestions := welcome(a.st.Language())
		for _, t := range texts {
			a.st.Chat = append(a.st.Chat, domain.ChatMessage{Role: "max", Text: t, At: now})
		}
		a.st.Suggestions = suggestions
		a.persist()
		res := a.chatState()
		res.NewMessages, res.Reply = texts, texts[len(texts)-1]
		a.mu.Unlock()
		return res, nil
	}
	if opened && !a.needsToSpeak(now) {
		res := a.chatState()
		res.Skipped = true
		a.mu.Unlock()
		return res, nil
	}
	if !opened {
		a.assumeUnansweredEstimates()
		a.st.Chat = append(a.st.Chat, domain.ChatMessage{Role: "user", Text: msg, At: now})
		a.st.Suggestions = nil
		a.trimChat()
		a.persist()
	}
	chatter := a.chatter
	cc := a.chatContext(now, in, opened)
	history := append([]domain.ChatMessage{}, a.st.Chat...)
	a.mu.Unlock()

	if chatter == nil {
		return ChatState{}, ErrChatUnavailable
	}
	out, err := chatter.Chat(ctx, cc, history)
	if err != nil {
		log.Printf("chat failed: %v", err)
		return ChatState{}, unavailable(err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.applyChat(a.now(), out)
	res := a.chatState()
	res.Reply = a.st.Chat[len(a.st.Chat)-1].Text
	res.NewMessages = []string{res.Reply}
	return res, nil
}

func (a *App) trimChat() {
	if n := len(a.st.Chat); n > domain.MaxChatKept {
		a.st.Chat = append([]domain.ChatMessage{}, a.st.Chat[n-domain.MaxChatKept:]...)
	}
}

// applyChat validates and applies what the AI proposed. Unknown ids and empty
// values are ignored. It must be called with the mutex held.
func (a *App) applyChat(now time.Time, out domain.ChatOutput) {
	st := a.st
	hadGoal := st.LifeGoal != "" // the goal was already known before this turn
	if g := clipRunes(out.LifeGoal, 300); g != "" {
		st.LifeGoal = g
	}
	if p := out.GoalPoints; p >= 20 && p <= 1000 {
		st.GoalPoints = p
	}
	if m := clipRunes(out.Motivation, 120); m != "" {
		st.Motivation = m
	}

	day := st.Days[domain.DayKey(now)]
	if day == nil {
		day = &domain.DayRecord{Date: domain.DayKey(now)}
	}
	added := 0
	for _, t := range out.AddTasks {
		text := clipRunes(t.Text, 200)
		if text == "" || added >= maxNewTasksTurn || st.HasOpenTask(text) {
			continue
		}
		st.Ideas = append(st.Ideas, a.buildTask(t, now, day))
		added++
	}
	// The life goal broken into steps: the user sees one at a time, the next
	// appears when it is finished (advanceGoalStep).
	if steps := cleanGoalSteps(out.GoalSteps); len(steps) > 0 {
		st.GoalSteps = steps
	}
	for _, text := range out.AddIdeas {
		if text = clipRunes(text, 300); text != "" {
			st.Ideas = append(st.Ideas, domain.Idea{ID: domain.NewID("idea"), Text: text, Type: "idea", CreatedAt: now})
		}
	}
	for _, u := range out.UpdateTasks {
		t := st.IdeaByID(u.ID)
		if t == nil || !t.OpenTask() {
			continue
		}
		if m := clampMinutes(u.EstimateMin); m > 0 {
			t.EstimateMin, t.EstimateAssumed = m, u.Guess // what the user said, or Max's own guess
		}
		if d := clipRunes(u.Deadline, 40); d != "" {
			t.Deadline = d
			t.Due = domain.TaskDue(t.Text+" "+d, now)
		}
		if u.Asking && t.NeedsEstimate() {
			t.EstimateAsks++
		} else if u.Asking && t.EstimateAssumed && t.EstimateMin > 0 {
			t.AssumedAsked = true // he asked whether his guess fits: once is enough
		}
	}
	for _, id := range out.DoneTasks {
		if t := st.IdeaByID(id); t != nil && t.OpenTask() {
			a.finishTask(now, t)
		}
	}
	for _, id := range out.StartTasks {
		st.StartTask(now, id)
	}
	for _, id := range out.RemoveTasks {
		if t := st.IdeaByID(id); t != nil && t.Type == "task" {
			st.RemoveIdea(id)
		}
	}
	// A task Max has asked about enough times gets his own estimate, so he
	// never nags: he says so and the user can still correct it.
	for i := range st.Ideas {
		t := &st.Ideas[i]
		if t.NeedsEstimate() && (t.EstimateAsks >= domain.MaxEstimateAsks || t.EstimateAssumed) {
			t.EstimateMin, t.EstimateAssumed = domain.DefaultEstimateMin, true // Max did not choose one: half an hour
		}
	}
	// The AI's ranking: listed open tasks in order; the rest go after them.
	for i := range st.Ideas {
		st.Ideas[i].Rank = 0
	}
	rank := 0
	for _, id := range out.Ranking {
		if t := st.IdeaByID(id); t != nil && t.OpenTask() && t.Rank == 0 {
			rank++
			t.Rank = rank
		}
	}

	// Getting to know each other is over only when nothing is left to ask: a
	// task still waiting for its time keeps the conversation going.
	waiting := false
	for _, i := range st.Ideas {
		waiting = waiting || awaitsEstimate(i)
	}
	// The goal alone does not end it: Max first asks about today's tasks and the
	// user answers (a turn after the goal was known).
	if out.OnboardingComplete && hadGoal && st.LifeGoal != "" && !waiting && !st.Profile.Onboarded {
		st.Profile.Onboarded = true
		if st.Profile.CreatedAt.IsZero() {
			st.Profile.CreatedAt = now
		}
	}
	st.Suggestions = nil
	for _, s := range out.Suggestions {
		if s = clipRunes(s, 60); s != "" && len(st.Suggestions) < maxSuggestions {
			st.Suggestions = append(st.Suggestions, s)
		}
	}
	st.Chat = append(st.Chat, domain.ChatMessage{Role: "max", Text: clipRunes(out.Reply, 2000), At: now})
	a.trimChat()
	a.advanceGoalStep(now)
	st.RecomputeProgress()
	st.EnsureCurrentTask(now)
	a.persist()
}

// maxGoalSteps bounds the plan toward the life goal.
const maxGoalSteps = 8

// buildTask makes a task from what the AI proposed.
func (a *App) buildTask(t domain.ChatNewTask, now time.Time, day *domain.DayRecord) domain.Idea {
	text := clipRunes(t.Text, 200)
	item := domain.Idea{
		ID: domain.NewID("idea"), Text: text, Type: "task", CreatedAt: now,
		EstimateMin: clampMinutes(t.EstimateMin), Deadline: clipRunes(t.Deadline, 40),
	}
	if t.Asking && item.NeedsEstimate() {
		item.EstimateAsks = 1
	}
	if t.Impact != nil {
		v := min(max(*t.Impact, 0), domain.MaxImpact)
		item.Impact = &v
	}
	if t.Kind == "browser" || t.Kind == "offline" {
		item.Kind = t.Kind
	}
	item.Keywords = domain.CleanKeywords(t.Keywords)
	item.StartQuery = clipRunes(t.Query, 100)
	item.EstimateAssumed = t.Guess && item.EstimateMin > 0
	item.Due = domain.TaskDue(text+" "+item.Deadline, now)
	item.Priority = domain.TaskScore(item, now, a.st.GoalKeywords())
	item.Nudged(day, false)
	return item
}

// cleanGoalSteps keeps the usable steps, in order.
func cleanGoalSteps(in []domain.ChatNewTask) []domain.ChatNewTask {
	var out []domain.ChatNewTask
	for _, s := range in {
		if s.Text = clipRunes(strings.TrimSpace(s.Text), 200); s.Text != "" && len(out) < maxGoalSteps {
			out = append(out, s)
		}
	}
	return out
}

// advanceGoalStep puts the next step of the life goal among today's tasks when
// none is open: the user sees one step at a time. It must be called with the
// mutex held.
func (a *App) advanceGoalStep(now time.Time) {
	st := a.st
	for _, i := range st.Ideas {
		if i.GoalStep && i.OpenTask() {
			return
		}
	}
	for len(st.GoalSteps) > 0 {
		next := st.GoalSteps[0]
		st.GoalSteps = st.GoalSteps[1:]
		if st.HasOpenTask(next.Text) {
			continue
		}
		day := st.Days[domain.DayKey(now)]
		if day == nil {
			day = &domain.DayRecord{Date: domain.DayKey(now)}
		}
		if next.Impact == nil {
			two := 2
			next.Impact = &two // a step of the goal clearly advances it
		}
		item := a.buildTask(next, now, day)
		item.GoalStep = true
		st.Ideas = append(st.Ideas, item)
		return
	}
}

func clampMinutes(m int) int {
	switch {
	case m < 0:
		return 0
	case m > 600:
		return 600
	}
	return m
}

// TaskHelpResult is one tiny first action for a task, and searches that may help.
type TaskHelpResult struct {
	TaskID        string   `json:"taskId"`
	Step          string   `json:"step"`
	SearchQueries []string `json:"searchQueries"`
	Lang          string   `json:"lang"`
}

// TaskHelp is POST /tasks/help: "I don't know where to start" on a task.
func (a *App) TaskHelp(ctx context.Context, in TaskInput) (TaskHelpResult, error) {
	a.mu.Lock()
	t := a.st.IdeaByID(in.ID)
	if t == nil || !t.OpenTask() {
		a.mu.Unlock()
		return TaskHelpResult{}, ErrInvalid
	}
	text, lang, chatter := t.Text, domain.GoalLanguage(t.Text), a.chatter
	a.mu.Unlock()

	if chatter == nil {
		return TaskHelpResult{}, ErrChatUnavailable
	}
	help, err := chatter.TaskHelp(ctx, text, lang)
	if err != nil {
		log.Printf("task help failed: %v", err)
		return TaskHelpResult{}, unavailable(err)
	}
	res := TaskHelpResult{TaskID: in.ID, Step: clipRunes(help.Step, 200), SearchQueries: []string{}, Lang: lang}
	for _, q := range help.Searches {
		if q = clipRunes(q, 60); q != "" && len(res.SearchQueries) < 3 {
			res.SearchQueries = append(res.SearchQueries, q)
		}
	}
	return res, nil
}

// assumeUnansweredEstimates: Max asks how long a task takes once. If the user
// replies with anything but a time, Max must choose one himself from the kind
// of task (an email is quick, studying is not), and say so; the AI is told
// "youMustEstimate", and if it does not, half an hour is used. So the same
// question can never come twice in a conversation. It must be called with
// the mutex held, when a user message arrives.
func (a *App) assumeUnansweredEstimates() {
	for i := range a.st.Ideas {
		t := &a.st.Ideas[i]
		if t.NeedsEstimate() && t.EstimateAsks >= 1 {
			t.EstimateAssumed = true
		}
	}
}

// langOf maps a browser language ("el-GR", "en-US") to "el" or "en".
func langOf(tag string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(tag)), "el") {
		return domain.LangGreek
	}
	return domain.LangEnglish
}

// SetLang is POST /lang: the user chose the language of the app.
func (a *App) SetLang(lang string) (string, error) {
	if lang != domain.LangGreek && lang != domain.LangEnglish {
		return "", ErrInvalid
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.st.AppLang = lang
	a.persist()
	return lang, nil
}
