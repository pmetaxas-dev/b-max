package application_test

import (
	"testing"
	"time"

	"focuscompanion/internal/application"
	"focuscompanion/internal/domain"
)

func (r *rig) task(text string) domain.Idea {
	r.t.Helper()
	item, err := r.app.Capture(application.CaptureInput{Text: text, Type: "task"})
	if err != nil {
		r.t.Fatal(err)
	}
	return item
}

// browseUntil browses url a minute at a time until cmd is issued (at most
// `minutes`), so an open card is not left to be ignored.
func (r *rig) browseUntil(url string, minutes int, cmd domain.Command) []application.Decision {
	r.t.Helper()
	var out []application.Decision
	for range minutes {
		d := r.ev(time.Minute, domain.EvHeartbeat, url, true)
		if d.Command != domain.CmdDoNothing {
			out = append(out, d)
		}
		if d.Command == cmd {
			break
		}
	}
	return out
}

func firstOf(ds []application.Decision, cmd domain.Command) (int, *application.Decision) {
	for i := range ds {
		if ds[i].Command == cmd {
			return i, &ds[i]
		}
	}
	return -1, nil
}

// "Send the email today", captured with Max: scrolling YouTube, the user is
// reminded of it after TaskDistractionRemind, before the goal's return screen.
func TestTaskIsRemindedOnADistractionSite(t *testing.T) {
	r := newRig(t)
	r.onboard()
	task := r.task("send an email today")
	if task.Due != "2026-09-18" || task.Priority < 3 {
		t.Fatalf("captured task: %+v", task)
	}
	ds := r.browseUntil(yt, 15, domain.CmdShowTask)
	_, d := firstOf(ds, domain.CmdShowTask)
	if d == nil {
		t.Fatalf("no reminder in 15 minutes on YouTube: %v", commandsOf(ds))
	}
	if d.Reason != "task_reminder_distraction" || d.Payload.Task == nil || d.Payload.Task.ID != task.ID || d.Payload.Task.Text != "send an email today" {
		t.Fatalf("reminder: %+v", d)
	}
	// It follows the user like any card, same delivery.
	again := r.ev(time.Minute, domain.EvHeartbeat, work, true)
	if again.Command != domain.CmdShowTask || again.Payload.DeliveryID != d.Payload.DeliveryID || again.Payload.Task.ID != task.ID {
		t.Fatalf("card did not follow: %+v", again)
	}
	before := r.app.Summary().Progress.Percent
	res, err := r.app.CompleteTask(application.TaskInput{ID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if res.Percent != before+domain.TaskBonusPercent {
		t.Fatalf("progress %v -> %v", before, res.Percent)
	}
	// Done: the card is gone and the task never comes back.
	if ds := r.browse(yt, 60); len(ds) > 0 {
		if _, d := firstOf(ds, domain.CmdShowTask); d != nil {
			t.Fatal("a done task was reminded")
		}
	}
	if len(r.app.Tasks()) != 1 || r.app.Tasks()[0].DoneAt == nil {
		t.Fatalf("today's done task is listed as done: %+v", r.app.Tasks())
	}
}

// Ignored ("Later"): reminded again after more active time, three times a day.
func TestIgnoredTaskComesBackAtMostThreeTimesADay(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.task("buy milk")
	reminders := 0
	for range 200 {
		d := r.ev(time.Minute, domain.EvHeartbeat, yt, true)
		if d.Command == domain.CmdShowTask && d.Reason != "card_open" {
			reminders++
			if err := r.app.CloseCard(application.CardInput{DeliveryID: d.Payload.DeliveryID, Reason: "ignored"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if reminders != domain.MaxTaskNudges {
		t.Fatalf("%d reminders, want %d", reminders, domain.MaxTaskNudges)
	}
}

// On a page about the goal an ordinary task waits; late in the day, or with
// a deadline word, it becomes urgent and interrupts.
func TestGoalWorkIsOnlyInterruptedByAnUrgentTask(t *testing.T) {
	r := dayRig(t)
	r.task("buy milk")
	for range 90 {
		if d := r.evTitle(time.Minute, "https://docs.python.org/3/", "Python docs"); d.Command == domain.CmdShowTask {
			t.Fatal("goal work interrupted by a plain task")
		}
	}
	r.clk.t = time.Date(2026, 9, 18, 17, 30, 0, 0, time.Local)
	r.task("send the invoice email")
	got := false
	for range 90 {
		d := r.evTitle(time.Minute, "https://docs.python.org/3/", "Python docs")
		if d.Command == domain.CmdShowTask {
			if d.Payload.Task.Text != "send the invoice email" || !d.Payload.Task.Urgent {
				t.Fatalf("wrong task first: %+v", d.Payload.Task)
			}
			got = true
			break
		}
	}
	if !got {
		t.Fatal("an urgent task never interrupted")
	}
}

func TestTomorrowsTaskWaitsAndTasksAreOrdered(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.task("call the bank tomorrow")
	r.task("buy milk")
	r.task("pay rent today")
	tasks := r.app.Tasks()
	if len(tasks) != 3 || tasks[0].Text != "pay rent today" || tasks[2].Text != "call the bank tomorrow" {
		t.Fatalf("order: %+v", tasks)
	}
	ds := r.browseUntil(yt, 15, domain.CmdShowTask)
	_, d := firstOf(ds, domain.CmdShowTask)
	if d == nil || d.Payload.Task.Text != "pay rent today" {
		t.Fatalf("the most pressing task first: %+v", d)
	}
}

func TestCompleteTaskValidation(t *testing.T) {
	r := newRig(t)
	r.onboard()
	idea, _ := r.app.Capture(application.CaptureInput{Text: "a video idea", Type: "idea"})
	for _, id := range []string{"", "missing", idea.ID} {
		if _, err := r.app.CompleteTask(application.TaskInput{ID: id}); err == nil {
			t.Fatalf("completed %q", id)
		}
	}
	task := r.task("buy milk")
	first, _ := r.app.CompleteTask(application.TaskInput{ID: task.ID})
	second, _ := r.app.CompleteTask(application.TaskInput{ID: task.ID})
	if first.Percent != second.Percent {
		t.Fatal("completing twice counted twice")
	}
}
