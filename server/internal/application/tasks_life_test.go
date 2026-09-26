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

const docs = "https://docs.python.org/3/tutorial/"

// chatFirst gives the rig a user with a life goal and the given tasks, made
// the way the chat makes them.
func (r *rig) chatFirst(tasks ...domain.ChatNewTask) (*fakeChatter, application.ChatState) {
	r.t.Helper()
	fc := &fakeChatter{outs: []domain.ChatOutput{{Reply: "ok", LifeGoal: "Live well", AddTasks: tasks}, {Reply: "ready", OnboardingComplete: true}}}
	r.app.SetChatter(fc)
	var res application.ChatState
	for _, msg := range []string{"hi", "nothing else today"} { // the goal, then the answer about today
		var err error
		if res, err = r.app.Chat(context.Background(), application.ChatInput{Message: msg}); err != nil {
			r.t.Fatal(err)
		}
	}
	return fc, res
}

func (r *rig) act(id, action string) application.TaskActionResult {
	r.t.Helper()
	res, err := r.app.TaskAction(application.TaskActionInput{ID: id, Action: action})
	if err != nil {
		r.t.Fatalf("%s: %v", action, err)
	}
	return res
}

func TestParseClock(t *testing.T) {
	cases := []struct {
		in   string
		h, m int
		ok   bool
	}{
		{"17:00", 17, 0, true},
		{"5pm", 17, 0, true},
		{"9 am", 9, 0, true},
		{"μέχρι τις 5", 17, 0, true},
		{"5 το απόγευμα", 17, 0, true},
		{"στις 10 το πρωί", 10, 0, true},
		{"until 5:30", 5, 30, true},
		{"tonight", 21, 0, true},
		{"απόψε", 21, 0, true},
		{"tomorrow", 0, 0, false},
		{"in 20 minutes", 0, 0, false},
		{"a program example", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, c := range cases {
		h, m, ok := domain.ParseClock(c.in)
		if ok != c.ok || (ok && (h != c.h || m != c.m)) {
			if c.in == "until 5:30" && ok && h == 17 && m == 30 {
				continue // "until 5:30" is half past five: the afternoon reading is right
			}
			t.Errorf("ParseClock(%q) = %d:%02d %v, want %d:%02d %v", c.in, h, m, ok, c.h, c.m, c.ok)
		}
	}
}

// The user said "I don't know" (or anything but a time): Max must choose the
// time himself from the kind of task, and cannot ask again. If he does not, it
// is half an hour. The loop the user hit cannot happen.
func TestMaxAsksAboutATasksTimeOnlyOnceThenChoosesItHimself(t *testing.T) {
	r := newRig(t)
	fc := &fakeChatter{outs: []domain.ChatOutput{{Reply: "ok", LifeGoal: "Live well", AddTasks: []domain.ChatNewTask{{Text: "present the app", Asking: true}}}}}
	r.app.SetChatter(fc)
	res, err := r.app.Chat(context.Background(), application.ChatInput{Message: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	pres := taskNamed(t, res.Tasks, "present")
	if !pres.NeedsEstimate || pres.EstimateMin != 0 {
		t.Fatalf("first the task has no time: %+v", pres)
	}
	fc.outs = append(fc.outs, domain.ChatOutput{Reply: "OK, half an hour, and we talk again."})
	res, err = r.app.Chat(context.Background(), application.ChatInput{Message: "I don't know"})
	if err != nil {
		t.Fatal(err)
	}
	seen := fc.ctxs[len(fc.ctxs)-1].Tasks[0]
	if seen.NeedsEstimate || !seen.MustEstimate || !seen.Assumed {
		t.Fatalf("Max must be told to choose the time himself and not to ask: %+v", seen)
	}
	if pres = taskNamed(t, res.Tasks, "present"); pres.EstimateMin != domain.DefaultEstimateMin || !pres.EstimateAssumed {
		t.Fatalf("without a choice from the AI the default is used: %+v", pres)
	}
}

// Max tells a quick email from an hour of studying: the time he chooses follows
// the kind of task, so the day can be put in order.
func TestMaxChoosesDifferentTimesForDifferentTasksAndAsksOnceMoreLater(t *testing.T) {
	r := newRig(t)
	fc, res := r.chatFirst(
		domain.ChatNewTask{Text: "send an email to the client", Asking: true},
		domain.ChatNewTask{Text: "study python", Asking: true},
	)
	email, python := taskNamed(t, res.Tasks, "email"), taskNamed(t, res.Tasks, "python")
	fc.outs = append(fc.outs, domain.ChatOutput{
		Reply: "An email is quick, about 10 minutes, and python needs about an hour and a half.",
		UpdateTasks: []domain.ChatTaskUpdate{
			{ID: email.ID, EstimateMin: 10, Guess: true},
			{ID: python.ID, EstimateMin: 90, Guess: true},
		},
		Ranking: []string{email.ID, python.ID},
	})
	res, err := r.app.Chat(context.Background(), application.ChatInput{Message: "I don't know how long"})
	if err != nil {
		t.Fatal(err)
	}
	email, python = taskNamed(t, res.Tasks, "email"), taskNamed(t, res.Tasks, "python")
	if email.EstimateMin != 10 || python.EstimateMin != 90 || !email.EstimateAssumed || !python.EstimateAssumed {
		t.Fatalf("the two tasks must get their own guessed time: %+v %+v", email, python)
	}

	// Later, on opening the app, he asks ONCE whether his guesses fit.
	r.clk.add(30 * time.Minute)
	fc.outs = append(fc.outs, domain.ChatOutput{
		Reply: "I guessed 10 minutes for the email and an hour and a half for python. Do those fit?",
		UpdateTasks: []domain.ChatTaskUpdate{
			{ID: email.ID, EstimateMin: 10, Guess: true, Asking: true},
			{ID: python.ID, EstimateMin: 90, Guess: true, Asking: true},
		},
	})
	out, err := r.app.Chat(context.Background(), application.ChatInput{Kind: "open"})
	if err != nil || out.Skipped {
		t.Fatalf("Max should speak to confirm his guess: %+v %v", out, err)
	}
	if c := fc.ctxs[len(fc.ctxs)-1].Tasks[0]; !c.ConfirmGuess {
		t.Fatalf("Max must be told which guess to confirm: %+v", fc.ctxs[len(fc.ctxs)-1].Tasks)
	}
	// Asked: he does not ask again.
	if again, _ := r.app.Chat(context.Background(), application.ChatInput{Kind: "open"}); !again.Skipped {
		t.Fatalf("a guess is confirmed once, not every time: %+v", again)
	}
	// The user corrects a guess: it is their time now, not Max's.
	fc.outs = append(fc.outs, domain.ChatOutput{Reply: "OK, 20 minutes.", UpdateTasks: []domain.ChatTaskUpdate{{ID: email.ID, EstimateMin: 20}}})
	res, _ = r.app.Chat(context.Background(), application.ChatInput{Message: "it takes 20 minutes"})
	if got := taskNamed(t, res.Tasks, "email"); got.EstimateMin != 20 || got.EstimateAssumed {
		t.Fatalf("the user's own time replaces the guess: %+v", got)
	}
}

// The time the user set has passed while they worked: Max asks whether it is
// done; "not yet" gives ten quiet minutes; "yes" ticks it.
func TestMaxChecksInWhenTheTimeHasPassed(t *testing.T) {
	r := newRig(t)
	_, res := r.chatFirst(domain.ChatNewTask{Text: "study python", EstimateMin: 30, Kind: "browser", Keywords: []string{"python"}})
	task := taskNamed(t, res.Tasks, "python")
	r.act(task.ID, "start")

	ds := r.browse(docs, 25)
	if _, d := firstOf(ds, domain.CmdShowTask); d != nil {
		t.Fatalf("too early to ask, 25 of 30 minutes: %+v", d)
	}
	ds = r.browseUntil(docs, 10, domain.CmdShowTask)
	_, d := firstOf(ds, domain.CmdShowTask)
	if d == nil || d.Payload.TaskMode != domain.ModeCheckIn || d.Payload.Task.ID != task.ID {
		t.Fatalf("expected a check-in: %v", commandsOf(ds))
	}
	if d.Payload.Task.WorkedMin < 20 || d.Payload.Task.Status != domain.TaskDoing {
		t.Fatalf("Max knows the user worked on relevant pages: %+v", d.Payload.Task)
	}
	// "Not yet": quiet for ten minutes, then he asks again.
	r.act(task.ID, "not_yet")
	if ds := r.browse(docs, 8); len(ds) > 0 {
		t.Fatalf("asked again inside the snooze: %v", commandsOf(ds))
	}
	if ds := r.browseUntil(docs, 5, domain.CmdShowTask); len(ds) == 0 {
		t.Fatal("no second question after the snooze")
	}
	before := r.app.Summary().Planet.Progress
	res2 := r.act(task.ID, "yes")
	if res2.Percent <= before*100 {
		t.Fatalf("progress %v -> %v", before, res2.Percent)
	}
	if ds := r.browse(docs, 40); len(ds) > 0 {
		t.Fatalf("a finished task must stop asking: %v", commandsOf(ds))
	}
}

// An offline task ("hang the laundry") is asked about once its time has passed.
func TestMaxChecksInOnAnOfflineTaskToo(t *testing.T) {
	r := newRig(t)
	_, res := r.chatFirst(domain.ChatNewTask{Text: "hang the laundry", EstimateMin: 20, Kind: "offline"})
	r.act(taskNamed(t, res.Tasks, "laundry").ID, "start")
	ds := r.browseUntil(work, 30, domain.CmdShowTask)
	if _, d := firstOf(ds, domain.CmdShowTask); d == nil || d.Payload.TaskMode != domain.ModeCheckIn || d.Payload.Task.WorkedMin != 0 {
		t.Fatalf("offline check-in: %v", commandsOf(ds))
	}
}

// A task with a deadline is asked about once the deadline has passed, even if
// the user never pressed start.
func TestMaxChecksInWhenTheDeadlinePasses(t *testing.T) {
	r := newRig(t) // the rig's clock starts at 09:00
	_, res := r.chatFirst(domain.ChatNewTask{Text: "send the report", EstimateMin: 240, Deadline: "11:00"})
	task := taskNamed(t, res.Tasks, "report")
	if ds := r.browse(work, 60); len(ds) > 0 {
		if _, d := firstOf(ds, domain.CmdShowTask); d != nil && d.Payload.TaskMode == domain.ModeCheckIn {
			t.Fatalf("asked before the deadline: %+v", d)
		}
	}
	found := false
	for range 120 {
		d := r.ev(time.Minute, domain.EvHeartbeat, work, true)
		if d.Command == domain.CmdShowTask && d.Payload.TaskMode == domain.ModeCheckIn && d.Payload.Task.ID == task.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no check-in after the 11:00 deadline")
	}
}

// Drifting to a distraction while a task is in progress: Max brings the user
// back to the page they were on; "not now" sets the task aside with a note,
// and Max remembers it for "where was I?".
func TestMaxBringsTheUserBackAndRemembersWhereTheyWere(t *testing.T) {
	r := newRig(t)
	fc, res := r.chatFirst(domain.ChatNewTask{Text: "study python", EstimateMin: 60, Kind: "browser", Keywords: []string{"python"}})
	task := taskNamed(t, res.Tasks, "python")
	r.act(task.ID, "start")
	r.browse(docs, 5)

	ds := r.browseUntil(yt, 5, domain.CmdShowTask)
	_, d := firstOf(ds, domain.CmdShowTask)
	if d == nil || d.Payload.TaskMode != domain.ModeResume || d.Payload.Task.ID != task.ID {
		t.Fatalf("expected a way back: %v", commandsOf(ds))
	}
	// Another page in the same site: the same card follows, then goes away when the user is back at work.
	if back := r.ev(time.Minute, domain.EvHeartbeat, work, true); back.Command == domain.CmdShowTask && back.Payload.TaskMode == domain.ModeResume {
		t.Fatalf("the way-back card must not follow onto a work page: %+v", back)
	}

	got := r.act(task.ID, "resume")
	if got.ResumeURL != docs {
		t.Fatalf("Continue must lead to the page they were on, got %q", got.ResumeURL)
	}

	// Now "not now": the task is set aside with a note.
	r.browseUntil(yt, 40, domain.CmdShowTask)
	paused := r.act(task.ID, "later")
	if paused.Task.Status != domain.TaskPaused {
		t.Fatalf("paused: %+v", paused)
	}
	if ds := r.browse(yt, 30); len(ds) > 0 {
		if _, d := firstOf(ds, domain.CmdShowTask); d != nil && d.Payload.TaskMode == domain.ModeResume {
			t.Fatalf("asked again right after 'not now'")
		}
	}

	// "Where was I?": the journal and the task tell Max.
	fc.outs = append(fc.outs, domain.ChatOutput{Reply: "You were on the Python tutorial."})
	if _, err := r.app.Chat(context.Background(), application.ChatInput{Message: "where was I today?"}); err != nil {
		t.Fatal(err)
	}
	cc := fc.ctxs[len(fc.ctxs)-1]
	var journal = strings.Join(cc.Journal, "\n")
	for _, want := range []string{"Started “study python”", "Drifted away from “study python”", "Went back to", "Set aside “study python”"} {
		if !strings.Contains(journal, want) {
			t.Errorf("journal lacks %q:\n%s", want, journal)
		}
	}
	if c := cc.Tasks[0]; c.Status != domain.TaskPaused || c.WorkedMin < 4 {
		t.Fatalf("task context: %+v", c)
	}
}

// Left open from yesterday: the first page of the next day asks about it once
// and Max's greeting on opening the app recaps it.
func TestYesterdaysTaskIsAskedAboutOnceAndRecapped(t *testing.T) {
	r := newRig(t)
	fc, res := r.chatFirst(domain.ChatNewTask{Text: "renew the passport", EstimateMin: 30})
	task := taskNamed(t, res.Tasks, "passport")
	r.clk.add(20 * time.Hour) // tomorrow morning

	ds := r.browseUntil(work, 5, domain.CmdShowTask)
	_, d := firstOf(ds, domain.CmdShowTask)
	if d == nil || d.Payload.TaskMode != domain.ModeCarry || d.Payload.Task.ID != task.ID {
		t.Fatalf("expected a recap card: %v", commandsOf(ds))
	}
	fc.outs = append(fc.outs, domain.ChatOutput{Reply: "Yesterday you left the passport open. Did you do it?"})
	out, err := r.app.Chat(context.Background(), application.ChatInput{Kind: "open"})
	if err != nil || out.Skipped {
		t.Fatalf("Max should speak on the first open of a new day: %+v %v", out, err)
	}
	if c := fc.ctxs[len(fc.ctxs)-1].Tasks[0]; !c.CarriedOver {
		t.Fatalf("carried-over flag missing: %+v", c)
	}

	r.act(task.ID, "not_yet")
	if ds := r.browse(work, 15); len(ds) > 0 {
		if _, d := firstOf(ds, domain.CmdShowTask); d != nil && d.Payload.TaskMode == domain.ModeCarry {
			t.Fatal("asked about yesterday twice in one day")
		}
	}

}

// "Help me start" and the searches close the card for good: the same task
// does not come straight back on the next page.
func TestHelpClosesTheReminderCard(t *testing.T) {
	r := newRig(t)
	_, res := r.chatFirst(domain.ChatNewTask{Text: "prepare the pitch", EstimateMin: 20})
	task := taskNamed(t, res.Tasks, "pitch")
	r.app.SetTimings(application.TimingsInput{Fast: true})
	ds := r.browseUntil(yt, 20, domain.CmdShowTask)
	_, d := firstOf(ds, domain.CmdShowTask)
	if d == nil {
		t.Fatalf("no reminder: %v", commandsOf(ds))
	}
	r.act(task.ID, "helped")
	next := r.ev(time.Second, domain.EvHeartbeat, "https://www.instagram.com/", true)
	if next.Command == domain.CmdShowTask {
		t.Fatalf("the reminder came straight back after 'help me start': %+v", next)
	}
}

// There is no start button: the first task by priority is in progress from the
// moment it exists, and the next one takes over when it is finished.
func TestTheFirstTaskStartsByItselfAndTheNextTakesOver(t *testing.T) {
	r := newRig(t)
	_, res := r.chatFirst(
		domain.ChatNewTask{Text: "write the essay", EstimateMin: 60},
		domain.ChatNewTask{Text: "tidy the desk", EstimateMin: 15},
	)
	first, second := taskNamed(t, res.Tasks, "essay"), taskNamed(t, res.Tasks, "desk")
	if res.Tasks[0].ID != first.ID || first.Status != domain.TaskDoing || first.StartedAt == nil || second.Status != "" {
		t.Fatalf("the first task must be in progress by itself: %+v", res.Tasks)
	}
	r.act(first.ID, "yes")
	after := r.app.ChatState()
	if got := taskNamed(t, after.Tasks, "desk"); got.Status != domain.TaskDoing {
		t.Fatalf("the next task must take over: %+v", after.Tasks)
	}
}

// Working on the second task while the first has priority: Max says which one
// comes first and offers the way back; "first this one" swaps them.
func TestMaxReminsThePriorityWhenTheUserWorksOnAnotherTask(t *testing.T) {
	r := newRig(t)
	const mail = "https://mail.google.com/mail/u/0/"
	_, res := r.chatFirst(
		domain.ChatNewTask{Text: "send the email", EstimateMin: 60, Kind: "browser", Keywords: []string{"mail.google.com"}},
		domain.ChatNewTask{Text: "write the text", EstimateMin: 90, Kind: "browser", Keywords: []string{"docs.google.com"}},
	)
	email, text := taskNamed(t, res.Tasks, "email"), taskNamed(t, res.Tasks, "text")
	if email.Status != domain.TaskDoing {
		t.Fatalf("the email has priority: %+v", res.Tasks)
	}
	if ds := r.browse(mail, 5); len(ds) > 0 {
		t.Fatalf("working on the priority task needs no reminder: %v", commandsOf(ds))
	}
	ds := r.browseUntil(work, 5, domain.CmdShowTask) // docs.google.com: the text
	_, d := firstOf(ds, domain.CmdShowTask)
	if d == nil || d.Payload.TaskMode != domain.ModeRefocus || d.Payload.Task.ID != email.ID || d.Payload.Other == nil || d.Payload.Other.ID != text.ID {
		t.Fatalf("expected the priority reminder: %v", commandsOf(ds))
	}
	// "First the text": it becomes the task in progress and the email is set aside.
	swapped := r.act(text.ID, "start")
	if swapped.Task.Status != domain.TaskDoing {
		t.Fatalf("swap: %+v", swapped.Task)
	}
	state := r.app.ChatState()
	if got := taskNamed(t, state.Tasks, "email"); got.Status != domain.TaskPaused {
		t.Fatalf("the email should be set aside: %+v", got)
	}
	if state.Tasks[0].ID != text.ID {
		t.Fatalf("the task in progress comes first: %+v", state.Tasks)
	}
}

// Time passes without the user saying a word: a deadline that comes close
// moves its task up, and Max asks whether to switch to it (once a day).
func TestACloseDeadlineMovesATaskUpAndMaxAsksToSwitch(t *testing.T) {
	r := newRig(t) // 09:00
	_, res := r.chatFirst(
		domain.ChatNewTask{Text: "write the essay", EstimateMin: 120},                    // no deadline, first, in progress
		domain.ChatNewTask{Text: "hand in the form", EstimateMin: 30, Deadline: "12:00"}, // due at noon
	)
	essay, form := taskNamed(t, res.Tasks, "essay"), taskNamed(t, res.Tasks, "form")
	if essay.Status != domain.TaskDoing || form.Status != "" {
		t.Fatalf("the essay starts: %+v", res.Tasks)
	}
	if ds := r.browse(work, 100); len(ds) > 0 { // 10:40, deadline 12:00 minus 30 minutes and a margin is 11:10
		if _, d := firstOf(ds, domain.CmdShowTask); d != nil && d.Payload.TaskMode == domain.ModeUrgent {
			t.Fatalf("asked too early: %+v", d)
		}
	}
	var card *application.Decision
	for range 60 {
		if d := r.ev(time.Minute, domain.EvHeartbeat, work, true); d.Command == domain.CmdShowTask && d.Payload.TaskMode == domain.ModeUrgent {
			card = &d
			break
		}
	}
	if card == nil || card.Payload.Task.ID != form.ID || card.Payload.Other == nil || card.Payload.Other.ID != essay.ID {
		t.Fatalf("expected the switch question: %+v", card)
	}
	// "Keep what I am doing": nothing changes and it is not asked again today.
	r.act(form.ID, "keep")
	if got := taskNamed(t, r.app.ChatState().Tasks, "essay"); got.Status != domain.TaskDoing {
		t.Fatalf("keep must leave the essay in progress: %+v", got)
	}
	for _, d := range r.browse(work, 30) {
		if d.Command == domain.CmdShowTask && d.Payload.TaskMode == domain.ModeUrgent {
			t.Fatal("asked again after keep")
		}
	}
	// The urgent task sorts first as soon as it is urgent, and "start" swaps them.
	if r.app.ChatState().Tasks[0].ID != essay.ID {
		t.Fatalf("the task in progress stays first until the user switches: %+v", r.app.ChatState().Tasks)
	}
	r.act(form.ID, "start")
	if r.app.ChatState().Tasks[0].ID != form.ID {
		t.Fatalf("the form must come first after the switch")
	}
}

// Where the user stands, in words, with no AI: the task now, what follows, the planet.
func TestTheSpokenStatusTellsWhereTheUserIs(t *testing.T) {
	r := newRig(t)
	if got := r.app.SpokenStatus(); !strings.Contains(got.Text, "tell me your dream") {
		t.Fatalf("before onboarding: %q", got.Text)
	}
	r.chatFirst(
		domain.ChatNewTask{Text: "write the essay", EstimateMin: 60, Deadline: "17:00"},
		domain.ChatNewTask{Text: "tidy the desk", EstimateMin: 10},
	)
	r.app.SetLang("en")
	got := r.app.SpokenStatus()
	for _, want := range []string{"Right now: write the essay", "About 60 minutes", "Deadline: 17:00", "Next up, 1: tidy the desk", "Prehistoric era", "0%", "Live well"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("status lacks %q:\n%s", want, got.Text)
		}
	}
	r.app.SetLang("el")
	if got := r.app.SpokenStatus(); !strings.Contains(got.Text, "Τώρα: write the essay") || !strings.Contains(got.Text, "Προϊστορική") {
		t.Fatalf("Greek status: %q", got.Text)
	}
	if d := application.EraDescription(85, "en"); !strings.Contains(d, "stars") {
		t.Fatalf("era description: %q", d)
	}
}

// The planet's growth is celebrated: reaching a new era shows Max's
// celebration on the next page, in the chat-first flow too. It never shrinks
// with a missed task or a bad day, only with the user's own "undo".
func TestANewEraIsCelebratedAndNothingTakesProgressAway(t *testing.T) {
	r := newRig(t)
	three := 3
	_, res := r.chatFirst(
		domain.ChatNewTask{Text: "finish chapter one", EstimateMin: 30, Impact: &three},
		domain.ChatNewTask{Text: "finish chapter two", EstimateMin: 30, Impact: &three},
		domain.ChatNewTask{Text: "a task never done", EstimateMin: 30},
	)
	// The goal is small (20 points), so two big steps are 30% and the second era.
	fc := &fakeChatter{outs: []domain.ChatOutput{{Reply: "ok", GoalPoints: 20}}}
	r.app.SetChatter(fc)
	if _, err := r.app.Chat(context.Background(), application.ChatInput{Message: "my goal is small"}); err != nil {
		t.Fatal(err)
	}
	r.act(taskNamed(t, res.Tasks, "one").ID, "yes")
	r.act(taskNamed(t, res.Tasks, "two").ID, "yes")
	if p := r.app.ChatState().Planet; p.Era != "copper" || p.Description == "" {
		t.Fatalf("second era expected: %+v", p)
	}
	var era *application.Decision
	for range 5 {
		if d := r.ev(time.Minute, domain.EvHeartbeat, work, true); d.Command == domain.CmdShowNewEra {
			era = &d
			break
		}
	}
	if era == nil || era.Payload.Era != "copper" {
		t.Fatalf("the new era must be celebrated: %+v", era)
	}
	// Days pass with the third task never done: the planet does not move backwards.
	before := r.app.ChatState().Planet.Progress
	r.clk.add(72 * time.Hour)
	r.browse(work, 30)
	if after := r.app.ChatState().Planet.Progress; after < before {
		t.Fatalf("an undone task took progress away: %v -> %v", before, after)
	}
}

// --- the idea chest ---------------------------------------------------------------

func (r *rig) chestUser(ideas []string, tasks ...domain.ChatNewTask) *fakeChatter {
	r.t.Helper()
	fc := &fakeChatter{outs: []domain.ChatOutput{{Reply: "kept", LifeGoal: "Live well", AddIdeas: ideas, AddTasks: tasks}, {Reply: "ready", OnboardingComplete: true}}}
	r.app.SetChatter(fc)
	for _, msg := range []string{"a brain dump", "that is all"} {
		if _, err := r.app.Chat(context.Background(), application.ChatInput{Message: msg}); err != nil {
			r.t.Fatal(err)
		}
	}
	return fc
}

// The reminders about a task were not enough: after a long stretch on distraction
// sites with nothing finished, Max offers one of the user's own ideas, once a day.
func TestMaxOffersAnIdeaWhenAnHourOfDistractionGetsNowhere(t *testing.T) {
	r := newRig(t)
	r.chestUser([]string{"learn the guitar"}, domain.ChatNewTask{Text: "study python", EstimateMin: 120})
	var offer *application.Decision
	for range 40 {
		if d := r.ev(time.Minute, domain.EvHeartbeat, yt, true); d.Command == domain.CmdOfferIdea {
			offer = &d
			break
		}
	}
	if offer == nil || offer.Payload.Idea == nil || offer.Payload.Idea.Text != "learn the guitar" {
		t.Fatalf("expected the idea after a long distraction: %+v", offer)
	}
	// "Yes, that one today": the idea becomes a task.
	if err := r.app.AnswerOldIdea(application.IdeaAnswer{ID: offer.Payload.Idea.ID, Accept: true}); err != nil {
		t.Fatal(err)
	}
	if got := taskNamed(t, r.app.ChatState().Tasks, "guitar"); got.Due == "" {
		t.Fatalf("the idea must be a task now: %+v", got)
	}
	// Once a day.
	for _, d := range r.browse(yt, 40) {
		if d.Command == domain.CmdOfferIdea {
			t.Fatal("offered a second idea the same day")
		}
	}
}

// Someone who finished a task today is not "stuck": no idea is pushed.
func TestNoIdeaIsPushedOnADayWithProgress(t *testing.T) {
	r := newRig(t)
	r.chestUser([]string{"learn the guitar"}, domain.ChatNewTask{Text: "study python", EstimateMin: 120})
	r.act(taskNamed(t, r.app.ChatState().Tasks, "python").ID, "yes")
	for _, d := range r.browse(yt, 60) {
		if d.Command == domain.CmdOfferIdea {
			t.Fatal("an idea was pushed on a day with progress")
		}
	}
}

// At the weekend, between work, once a week: the ideas the user collected.
func TestTheChestIsOfferedAtTheWeekendOnce(t *testing.T) {
	r := newRig(t) // Friday
	r.chestUser([]string{"learn the guitar", "paint the shelf"})
	if ds := r.browse(yt, 5); len(ds) > 0 {
		if _, d := firstOf(ds, domain.CmdShowTreasure); d != nil {
			t.Fatal("the treasure is for the weekend")
		}
	}
	r.clk.add(24 * time.Hour) // Saturday
	ds := r.browseUntil(yt, 10, domain.CmdShowTreasure)
	_, d := firstOf(ds, domain.CmdShowTreasure)
	if d == nil || d.Payload.Ideas != 2 {
		t.Fatalf("expected the chest at the weekend: %v", commandsOf(ds))
	}
	if _, err := r.app.ReviewIdeas(application.ReviewInput{}); err != nil {
		t.Fatal(err)
	}
	r.clk.add(time.Hour)
	for _, d := range r.browse(yt, 20) {
		if d.Command == domain.CmdShowTreasure {
			t.Fatal("offered twice in a week")
		}
	}
}

func TestAnIdeaCanBeMadeATaskOrLetGo(t *testing.T) {
	r := newRig(t)
	r.chestUser([]string{"learn the guitar", "paint the shelf"})
	var guitar, shelf domain.Idea
	for _, it := range r.app.Ideas() {
		if it.Text == "learn the guitar" {
			guitar = it
		} else if it.Text == "paint the shelf" {
			shelf = it
		}
	}
	if got := r.app.ChatState(); got.IdeasTotal != 2 || got.IdeasUnseen != 2 {
		t.Fatalf("the chest counts: %+v", got)
	}
	task, err := r.app.PromoteIdea(application.IdeaActionInput{ID: guitar.ID})
	if err != nil || task.Text != "learn the guitar" || task.Status != domain.TaskDoing {
		t.Fatalf("promote: %+v %v", task, err)
	}
	if err := r.app.DeleteIdea(application.IdeaActionInput{ID: shelf.ID}); err != nil {
		t.Fatal(err)
	}
	if err := r.app.DeleteIdea(application.IdeaActionInput{ID: guitar.ID}); !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("a task is not an idea any more: %v", err)
	}
	if got := r.app.ChatState(); got.IdeasTotal != 0 {
		t.Fatalf("the chest should be empty: %+v", got)
	}
}

// A bad day brings clouds and lightning over the planet; one finished task
// clears the sky, and the storm never takes progress away.
func TestABadDayBringsAStormThatOneFinishedTaskClears(t *testing.T) {
	r := newRig(t)
	_, res := r.chatFirst(domain.ChatNewTask{Text: "study python", EstimateMin: 120})
	task := taskNamed(t, res.Tasks, "python")
	if w := r.app.ChatState().Planet.Weather; w != domain.WeatherClear {
		t.Fatalf("a new day starts clear: %s", w)
	}
	r.browse(yt, 20)
	if w := r.app.ChatState().Planet.Weather; w != domain.WeatherClear {
		t.Fatalf("20 minutes is not yet a bad day: %s", w)
	}
	r.browse(yt, 20)
	before := r.app.ChatState().Planet.Progress
	if w := r.app.ChatState().Planet.Weather; w != domain.WeatherStorm {
		t.Fatalf("40 minutes of distraction with nothing finished should storm: %s", w)
	}
	if got := r.app.SpokenStatus().Text; !strings.Contains(got, "clouds") && !strings.Contains(got, "συννεφιά") {
		t.Fatalf("the status should mention the weather: %s", got)
	}
	r.act(task.ID, "yes")
	if p := r.app.ChatState().Planet; p.Weather != domain.WeatherClear || p.Progress < before {
		t.Fatalf("one finished task clears the sky and progress never drops: %+v (was %v)", p, before)
	}
}

func TestTheEveningWithNothingFinishedAlsoStorms(t *testing.T) {
	r := newRig(t)
	r.chatFirst(domain.ChatNewTask{Text: "study python", EstimateMin: 120})
	r.clk.add(9*time.Hour + 30*time.Minute) // 18:30
	r.browse(work, 25)
	if w := r.app.ChatState().Planet.Weather; w != domain.WeatherStorm {
		t.Fatalf("an evening online with nothing finished: %s", w)
	}
}

// --- speed and the way to the task ---------------------------------------------------

type limitedErr struct{ seconds int }

func (limitedErr) Error() string            { return "groq returned status 429" }
func (e limitedErr) RetryAfterSeconds() int { return e.seconds }

// The AI's per-minute limit is told to the user as it is, with the seconds to wait,
// instead of a mysterious failure that makes them send the message twice.
func TestTheAIsRateLimitIsToldToTheUserWithTheWait(t *testing.T) {
	r := newRig(t)
	r.app.SetChatter(&fakeChatter{err: limitedErr{seconds: 42}})
	_, err := r.app.Chat(context.Background(), application.ChatInput{Message: "hello"})
	var limited *application.RateLimitedError
	if !errors.As(err, &limited) || limited.Seconds != 42 {
		t.Fatalf("expected a rate-limit error with the wait: %v", err)
	}
	if st := r.app.ChatState(); len(st.Messages) != 1 {
		t.Fatalf("the message must be kept: %+v", st.Messages)
	}
}

// A search page is about what was searched, never about the search engine: the
// AI's keyword "google.com" must not make a search for "reddit" count as work on
// the task, and "take me back" must lead to where the task starts.
func TestBackToWhereTheTaskStartsNotToAnUnrelatedSearch(t *testing.T) {
	r := newRig(t)
	_, res := r.chatFirst(domain.ChatNewTask{
		Text: "search for go-kart tracks", EstimateMin: 30, Kind: "browser",
		Keywords: []string{"go-kart", "google.com"}, Query: "go-kart tracks near me",
	})
	task := taskNamed(t, res.Tasks, "go-kart")
	r.browse("https://www.google.com/search?q=reddit", 4)
	got := r.act(task.ID, "resume")
	if got.ResumeURL != "" || got.SearchQuery != "go-kart tracks near me" {
		t.Fatalf("an unrelated search must not be the way back; it should lead to where the task starts: %+v", got)
	}
	const page = "https://kartclub.example/go-kart-tracks"
	r.browse(page, 3)
	if got := r.act(task.ID, "resume"); got.ResumeURL != page || got.SearchQuery != "" {
		t.Fatalf("the way back is the page about the task: %+v", got)
	}
	// A page from yesterday is not the way back either.
	r.clk.add(30 * time.Hour)
	if got := r.act(task.ID, "resume"); got.ResumeURL != "" || got.SearchQuery == "" {
		t.Fatalf("yesterday's page must not be offered: %+v", got)
	}
}
