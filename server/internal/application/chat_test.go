package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"focuscompanion/internal/application"
	"focuscompanion/internal/domain"
)

// fakeChatter answers with the next scripted output and records what it was told.
type fakeChatter struct {
	outs []domain.ChatOutput
	err  error
	help domain.TaskHelp
	ctxs []domain.ChatContext
	hist [][]domain.ChatMessage
}

func (f *fakeChatter) Chat(_ context.Context, cc domain.ChatContext, h []domain.ChatMessage) (domain.ChatOutput, error) {
	f.ctxs, f.hist = append(f.ctxs, cc), append(f.hist, h)
	if f.err != nil {
		return domain.ChatOutput{}, f.err
	}
	out := f.outs[0]
	f.outs = f.outs[1:]
	return out, nil
}

func (f *fakeChatter) TaskHelp(_ context.Context, _, _ string) (domain.TaskHelp, error) {
	return f.help, f.err
}

func taskNamed(t *testing.T, tasks []application.TaskView, text string) application.TaskView {
	t.Helper()
	for _, tk := range tasks {
		if strings.Contains(tk.Text, text) {
			return tk
		}
	}
	t.Fatalf("no task %q in %+v", text, tasks)
	return application.TaskView{}
}

func TestChatFirstOpenIsAScriptedWelcomeWithoutTheAI(t *testing.T) {
	r := newRig(t)
	fc := &fakeChatter{}
	r.app.SetChatter(fc)
	res, err := r.app.Chat(context.Background(), application.ChatInput{Kind: "open", Lang: "el-GR"})
	if err != nil || len(fc.ctxs) != 0 {
		t.Fatalf("welcome must not call the AI: %v (calls %d)", err, len(fc.ctxs))
	}
	if len(res.NewMessages) != 3 || !strings.Contains(res.NewMessages[0], "Max") || !strings.Contains(res.NewMessages[1], "πλανήτη") ||
		len(res.Suggestions) != 2 || len(res.Messages) != 3 {
		t.Fatalf("welcome: %+v", res)
	}
	// Opened again straight away: nothing more to say.
	res, err = r.app.Chat(context.Background(), application.ChatInput{Kind: "open"})
	if err != nil || !res.Skipped {
		t.Fatalf("second open: %+v %v", res, err)
	}
}

func TestChatOnboardsAddsAndRanksTasks(t *testing.T) {
	r := newRig(t)
	fc := &fakeChatter{outs: []domain.ChatOutput{
		{Reply: "Nice. Until when for the email, and how long?", LifeGoal: "Become a developer", Suggestions: []string{"By 5pm", "I don't know", "a", "b"},
			AddTasks: []domain.ChatNewTask{{Text: "send the email", Asking: true}, {Text: "buy milk", EstimateMin: 10, Deadline: "18:00"}},
			AddIdeas: []string{"learn the guitar"}},
	}}
	r.app.SetChatter(fc)

	res, err := r.app.Chat(context.Background(), application.ChatInput{Message: "I want to become a developer, and I must send an email and buy milk"})
	if err != nil {
		t.Fatal(err)
	}
	if res.LifeGoal != "Become a developer" || len(res.Tasks) != 2 || res.Onboarded {
		t.Fatalf("after goal: %+v", res)
	}
	if len(res.Suggestions) != 2 { // capped
		t.Fatalf("suggestions: %v", res.Suggestions)
	}
	email, milk := taskNamed(t, res.Tasks, "email"), taskNamed(t, res.Tasks, "milk")
	if !email.NeedsEstimate || milk.NeedsEstimate || milk.EstimateMin != 10 || milk.Deadline != "18:00" {
		t.Fatalf("estimates: email=%+v milk=%+v", email, milk)
	}
	if got := r.app.Ideas(); len(got) != 3 || got[0].Type != "idea" && got[2].Type != "idea" {
		t.Fatalf("the idea was not kept: %+v", got)
	}

	// The user answers; the AI updates the task, ranks, finishes onboarding and marks one done.
	fc.outs = append(fc.outs, domain.ChatOutput{
		Reply: "Great, the planet is ready.", OnboardingComplete: true, Motivation: "One step at a time.",
		UpdateTasks: []domain.ChatTaskUpdate{{ID: email.ID, EstimateMin: 15, Deadline: "17:00"}, {ID: "idea_nope", EstimateMin: 5}},
		Ranking:     []string{milk.ID, email.ID, "idea_nope"},
		DoneTasks:   []string{milk.ID},
	})
	res, err = r.app.Chat(context.Background(), application.ChatInput{Message: "until 17:00, about 15 minutes"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Onboarded || res.Motivation != "One step at a time." || len(res.Suggestions) != 0 {
		t.Fatalf("onboarding: %+v", res)
	}
	if res.Tasks[0].ID != email.ID || res.Tasks[0].EstimateMin != 15 || res.Tasks[0].NeedsEstimate {
		t.Fatalf("open task first: %+v", res.Tasks)
	}
	if res.Tasks[1].DoneAt == nil {
		t.Fatalf("done task should follow the open ones: %+v", res.Tasks)
	}
	if res.Planet.Progress != 1.0/domain.DefaultGoalPoints {
		t.Fatalf("planet progress: %v", res.Planet.Progress)
	}
	// The user replied without a time, so Max was told the time is already chosen.
	if c := fc.ctxs[1].Tasks[0]; c.NeedsEstimate || !c.Assumed || c.AskedTimes != 1 {
		t.Fatalf("context: %+v", fc.ctxs[1].Tasks)
	}
	if len(fc.ctxs[1].Ideas) != 1 {
		t.Fatalf("saved ideas missing from the context: %+v", fc.ctxs[1])
	}
}

// The loop the user hit: Max kept asking. After MaxEstimateAsks questions
// without an answer the server assumes half an hour itself.
func TestChatNeverAsksForeverAboutATasksTime(t *testing.T) {
	r := newRig(t)
	fc := &fakeChatter{}
	r.app.SetChatter(fc)
	fc.outs = []domain.ChatOutput{{Reply: "How long?", LifeGoal: "Live well",
		AddTasks: []domain.ChatNewTask{{Text: "prepare the pitch", Asking: true}}}}
	res, err := r.app.Chat(context.Background(), application.ChatInput{Message: "prepare the pitch"})
	if err != nil {
		t.Fatal(err)
	}
	pitch := taskNamed(t, res.Tasks, "pitch")
	for range domain.MaxEstimateAsks - 1 {
		fc.outs = append(fc.outs, domain.ChatOutput{Reply: "And how long?", UpdateTasks: []domain.ChatTaskUpdate{{ID: pitch.ID, Asking: true}}})
		res, err = r.app.Chat(context.Background(), application.ChatInput{Message: "I don't know"})
		if err != nil {
			t.Fatal(err)
		}
	}
	pitch = taskNamed(t, res.Tasks, "pitch")
	if pitch.EstimateMin != domain.DefaultEstimateMin || !pitch.EstimateAssumed || pitch.NeedsEstimate {
		t.Fatalf("Max should have assumed an estimate: %+v", pitch)
	}
	// And once the user gives a real answer the assumption is dropped.
	fc.outs = append(fc.outs, domain.ChatOutput{Reply: "Got it.", UpdateTasks: []domain.ChatTaskUpdate{{ID: pitch.ID, EstimateMin: 90}}})
	res, _ = r.app.Chat(context.Background(), application.ChatInput{Message: "an hour and a half"})
	if pitch = taskNamed(t, res.Tasks, "pitch"); pitch.EstimateMin != 90 || pitch.EstimateAssumed {
		t.Fatalf("real answer: %+v", pitch)
	}
}

func TestChatOpenStaysQuietWhenThereIsNothingToSay(t *testing.T) {
	r := newRig(t)
	fc := &fakeChatter{outs: []domain.ChatOutput{{Reply: "hello", LifeGoal: "Run a marathon", OnboardingComplete: true}}}
	r.app.SetChatter(fc)
	if _, err := r.app.Chat(context.Background(), application.ChatInput{Message: "hi"}); err != nil {
		t.Fatal(err)
	}
	res, err := r.app.Chat(context.Background(), application.ChatInput{Kind: "open"})
	if err != nil || !res.Skipped || len(fc.ctxs) != 1 {
		t.Fatalf("open should skip without an AI call: %+v %v (calls %d)", res, err, len(fc.ctxs))
	}
}

func TestChatUnavailableKeepsTheUserMessage(t *testing.T) {
	r := newRig(t)
	r.app.SetChatter(&fakeChatter{err: errors.New("groq down")})
	_, err := r.app.Chat(context.Background(), application.ChatInput{Message: "hello"})
	if !errors.Is(err, application.ErrChatUnavailable) {
		t.Fatalf("err: %v", err)
	}
	if st := r.app.ChatState(); len(st.Messages) != 1 || st.Messages[0].Text != "hello" {
		t.Fatalf("message lost: %+v", st.Messages)
	}
	if _, err := r.app.Chat(context.Background(), application.ChatInput{}); !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("empty message: %v", err)
	}
}

func TestTaskHelpGivesATinyFirstStep(t *testing.T) {
	r := newRig(t)
	r.app.SetChatter(&fakeChatter{help: domain.TaskHelp{Step: "Open a blank document.", Searches: []string{"pitch outline", "", "a", "b", "c"}}})
	task := r.task("prepare the pitch")
	res, err := r.app.TaskHelp(context.Background(), application.TaskInput{ID: task.ID})
	if err != nil || res.Step != "Open a blank document." || len(res.SearchQueries) != 3 || res.TaskID != task.ID {
		t.Fatalf("help: %+v %v", res, err)
	}
	if _, err := r.app.TaskHelp(context.Background(), application.TaskInput{ID: "nope"}); !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("unknown task: %v", err)
	}
}

// Without a step plan (the chat-first flow) Max still reminds the user of a
// task on a distraction site, and asks no goal questions.
func TestChatFirstUserIsRemindedOfTasksOnDistractionSites(t *testing.T) {
	r := newRig(t)
	fc := &fakeChatter{outs: []domain.ChatOutput{{Reply: "ok", LifeGoal: "Live well",
		AddTasks: []domain.ChatNewTask{{Text: "send the email today", EstimateMin: 10}}}, {Reply: "ready", OnboardingComplete: true}}}
	r.app.SetChatter(fc)
	for _, msg := range []string{"hi", "that is all"} {
		if _, err := r.app.Chat(context.Background(), application.ChatInput{Message: msg}); err != nil {
			t.Fatal(err)
		}
	}
	ds := r.browseUntil(yt, 15, domain.CmdShowTask)
	if _, d := firstOf(ds, domain.CmdShowTask); d == nil || d.Payload.Task == nil || d.Payload.Task.Text != "send the email today" {
		t.Fatalf("no task reminder without a plan: %v", commandsOf(ds))
	}
	for _, d := range ds {
		if d.Command == domain.CmdShowRamp || d.Command == domain.CmdAskDay {
			t.Fatalf("goal cards must not appear in the chat-first flow: %v", d.Command)
		}
	}
}

// The planet follows the life goal, not the number of tasks: chores and rest
// are worth nothing, and a big goal needs many milestones.
func TestThePlanetFollowsTheLifeGoalNotTheTaskCount(t *testing.T) {
	r := newRig(t)
	zero, three := 0, 3
	fc := &fakeChatter{outs: []domain.ChatOutput{{
		Reply: "ok", LifeGoal: "Become a father in two years", GoalPoints: 300, OnboardingComplete: true,
		AddTasks: []domain.ChatNewTask{
			{Text: "take a shower", EstimateMin: 10, Impact: &zero},
			{Text: "text my partner", EstimateMin: 5, Impact: &zero},
			{Text: "finish the app", EstimateMin: 120, Impact: &three},
		},
	}}}
	r.app.SetChatter(fc)
	res, err := r.app.Chat(context.Background(), application.ChatInput{Message: "my tasks"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range res.Tasks {
		r.act(tk.ID, "yes")
	}
	got := r.app.ChatState().Planet.Progress
	if want := 3.0 / 300; got != want {
		t.Fatalf("five ticked tasks should move a two-year goal by 1%%, got %v (want %v)", got, want)
	}
	// Ticked by mistake: reopening takes the progress back.
	finish := taskNamed(t, r.app.ChatState().Tasks, "finish")
	r.act(finish.ID, "reopen")
	if p := r.app.ChatState().Planet.Progress; p != 0 {
		t.Fatalf("reopened task must give the progress back: %v", p)
	}
}

// The AI says "we are done" as soon as the tasks are listed; while a task
// still waits for its time the conversation continues.
func TestOnboardingDoesNotEndWhileATaskStillNeedsItsTime(t *testing.T) {
	r := newRig(t)
	fc := &fakeChatter{outs: []domain.ChatOutput{{
		Reply: "Great, the planet is ready!", LifeGoal: "Live well", OnboardingComplete: true,
		AddTasks: []domain.ChatNewTask{{Text: "prepare the pitch", Asking: true}},
	}}}
	r.app.SetChatter(fc)
	res, err := r.app.Chat(context.Background(), application.ChatInput{Message: "goal and a task"})
	if err != nil || res.Onboarded {
		t.Fatalf("too early to be done: %+v %v", res, err)
	}
	fc.outs = append(fc.outs, domain.ChatOutput{Reply: "Half an hour then. All set.", OnboardingComplete: true})
	res, err = r.app.Chat(context.Background(), application.ChatInput{Message: "I don't know"})
	if err != nil || !res.Onboarded {
		t.Fatalf("now it can end: %+v %v", res, err)
	}
}

func TestSpeechToTextGoesThroughTheTranscriber(t *testing.T) {
	r := newRig(t)
	if _, err := r.app.Transcribe(context.Background(), []byte("x"), "audio/webm", "el"); !errors.Is(err, application.ErrSttUnavailable) {
		t.Fatalf("no transcriber: %v", err)
	}
	r.app.SetTranscriber(fakeTranscriber{text: "Το app έχει bug"})
	if text, err := r.app.Transcribe(context.Background(), []byte("x"), "audio/webm", "el"); err != nil || text != "Το app έχει bug" {
		t.Fatalf("%q %v", text, err)
	}
	r.app.SetTranscriber(fakeTranscriber{err: errors.New("groq down")})
	if _, err := r.app.Transcribe(context.Background(), []byte("x"), "audio/webm", "el"); !errors.Is(err, application.ErrSttUnavailable) {
		t.Fatalf("failure: %v", err)
	}
}

type fakeTranscriber struct {
	text string
	err  error
}

func (f fakeTranscriber) Transcribe(context.Context, []byte, string, string) (string, error) {
	return f.text, f.err
}

// The user chooses the language of the app (top right); Max answers in it,
// and the choice reaches the lamp and the cards too.
func TestTheAppLanguageIsTheUsersChoice(t *testing.T) {
	r := newRig(t)
	fc := &fakeChatter{outs: []domain.ChatOutput{{Reply: "ok"}, {Reply: "ok"}}}
	r.app.SetChatter(fc)
	if res, _ := r.app.Chat(context.Background(), application.ChatInput{Kind: "open", Lang: "el-GR"}); res.Lang != "el" {
		t.Fatalf("the first time the browser's language decides: %q", res.Lang)
	}
	if _, err := r.app.Chat(context.Background(), application.ChatInput{Message: "hi", Lang: "en-US"}); err != nil {
		t.Fatal(err)
	}
	if got := fc.ctxs[0].LangHint; got != "el" {
		t.Fatalf("Max is told the app language, not the browser's: %q", got)
	}
	if lang, err := r.app.SetLang("en"); err != nil || lang != "en" {
		t.Fatalf("set: %q %v", lang, err)
	}
	if got := r.app.ChatState().Lang; got != "en" {
		t.Fatalf("chosen language: %q", got)
	}
	if _, err := r.app.SetLang("is"); !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("a third language must be refused: %v", err)
	}
}

// The life goal is broken into steps and the user sees one at a time; the next
// appears when it is finished. The goal alone does not end the onboarding: Max
// asks about today's tasks first.
func TestTheLifeGoalComesAsOneStepAtATime(t *testing.T) {
	r := newRig(t)
	one := 2
	steps := []domain.ChatNewTask{
		{Text: "watch a beginner kart video", EstimateMin: 20, Guess: true, Kind: "browser", Query: "go-kart beginner video"},
		{Text: "find a kart track near me", EstimateMin: 30, Guess: true, Kind: "browser"},
		{Text: "book a first session", EstimateMin: 15, Guess: true, Impact: &one},
	}
	fc := &fakeChatter{outs: []domain.ChatOutput{
		{Reply: "Goal set. First step: a video. Any other task today?", LifeGoal: "Race in a go-kart race", GoalSteps: steps, OnboardingComplete: true},
		{Reply: "No other tasks. The planet is ready.", OnboardingComplete: true},
	}}
	r.app.SetChatter(fc)

	res, err := r.app.Chat(context.Background(), application.ChatInput{Message: "I want to race go-karts"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Onboarded {
		t.Fatal("the goal alone must not end the onboarding")
	}
	if len(res.Tasks) != 1 || res.Tasks[0].Text != "watch a beginner kart video" {
		t.Fatalf("only the first step is shown: %+v", res.Tasks)
	}

	res, err = r.app.Chat(context.Background(), application.ChatInput{Message: "nothing else"})
	if err != nil || !res.Onboarded {
		t.Fatalf("after the answer: %+v %v", res.Onboarded, err)
	}
	if fc.ctxs[1].StepsLeft != 2 || !fc.ctxs[1].Tasks[0].GoalStep {
		t.Fatalf("Max is told about the steps: %+v", fc.ctxs[1])
	}

	// Finishing a step brings the next one, and only that one.
	if _, err := r.app.CompleteTask(application.TaskInput{ID: res.Tasks[0].ID}); err != nil {
		t.Fatal(err)
	}
	open := 0
	var current application.TaskView
	for _, tk := range r.app.ChatState().Tasks {
		if tk.DoneAt == nil {
			open++
			current = tk
		}
	}
	if open != 1 || current.Text != "find a kart track near me" {
		t.Fatalf("the next step should be the only open task: %+v", r.app.ChatState().Tasks)
	}
}
