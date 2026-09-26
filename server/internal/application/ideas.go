package application

import (
	"strings"
	"time"

	"focuscompanion/internal/domain"
)

type CaptureInput struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

// Capture parks an item without advancing time, context, the plan or progress.
// Persistence must succeed before either memory or the caller sees the item.
// A task is for today (tomorrow if it says so) and gets its priority now;
// the first reminder comes after TaskRemind of active web time from now.
func (a *App) Capture(in CaptureInput) (domain.Idea, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" || (in.Type != "idea" && in.Type != "task") {
		return domain.Idea{}, ErrInvalid
	}
	now := a.now()
	item := domain.Idea{ID: domain.NewID("idea"), Text: in.Text, Type: in.Type, CreatedAt: now}
	if in.Type == "task" {
		item.Due = domain.TaskDue(in.Text, now)
		item.Priority = domain.TaskScore(item, now, a.st.GoalKeywords())
		// Read the day without creating it: capturing touches nothing else.
		day := a.st.Days[domain.DayKey(now)]
		if day == nil {
			day = &domain.DayRecord{Date: domain.DayKey(now)}
		}
		item.Nudged(day, false)
	}
	next := *a.st
	next.Ideas = append(append([]domain.Idea{}, a.st.Ideas...), item)
	a.saveErr = a.repo.Save(&next)
	if a.saveErr != nil {
		return domain.Idea{}, a.saveErr
	}
	a.st = &next
	return item, nil
}

func (a *App) Ideas() []domain.Idea {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]domain.Idea{}, a.st.Ideas...)
}

// TaskView is a task as the dashboard and the reminder card show it.
type TaskView struct {
	ID              string     `json:"id"`
	Text            string     `json:"text"`
	Due             string     `json:"due"`
	Overdue         bool       `json:"overdue"`
	Urgent          bool       `json:"urgent"`
	Priority        int        `json:"priority"`
	EstimateMin     int        `json:"estimateMin"`
	Deadline        string     `json:"deadline"`
	NeedsEstimate   bool       `json:"needsEstimate"`
	EstimateAssumed bool       `json:"estimateAssumed"`
	DoneAt          *time.Time `json:"doneAt,omitempty"`
	// Life of the task (tasklife.go).
	Status    string     `json:"status,omitempty"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	WorkedMin int        `json:"workedMin"`
	Note      string     `json:"note,omitempty"`
	LastTitle string     `json:"lastTitle,omitempty"`
	Kind      string     `json:"kind,omitempty"`
}

// taskView must be called with the mutex held.
func (a *App) taskView(t *domain.Idea) *TaskView {
	if t == nil {
		return nil
	}
	now, kw := a.now(), a.st.GoalKeywords()
	return &TaskView{
		ID: t.ID, Text: t.Text, Due: t.Due, Overdue: t.Overdue(now),
		Urgent: domain.TaskUrgent(*t, now, kw), Priority: domain.TaskScore(*t, now, kw), DoneAt: t.DoneAt,
		EstimateMin: t.EstimateMin, Deadline: t.Deadline, NeedsEstimate: t.NeedsEstimate(), EstimateAssumed: t.EstimateAssumed,
		Status: t.Status, StartedAt: t.StartedAt, WorkedMin: int(t.WorkedMs / 60_000), Note: t.Note, LastTitle: t.LastTitle, Kind: t.Kind,
	}
}

// Tasks is GET /tasks: open tasks most pressing first, then today's done ones.
func (a *App) Tasks() []TaskView {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tasksLocked()
}

// tasksLocked must be called with the mutex held.
func (a *App) tasksLocked() []TaskView {
	now := a.now()
	out := []TaskView{}
	for _, t := range a.st.OpenTasksByPriority(now) {
		out = append(out, *a.taskView(&t))
	}
	for i := range a.st.Ideas {
		t := &a.st.Ideas[i]
		if t.Type == "task" && t.DoneAt != nil && domain.DayKey(*t.DoneAt) == domain.DayKey(now) {
			out = append(out, *a.taskView(t))
		}
	}
	return out
}

type TaskInput struct {
	ID string `json:"id"`
}

type TaskResult struct {
	Announcement string  `json:"announcement"`
	Percent      float64 `json:"percent"`
	Lang         string  `json:"lang"`
}

// CompleteTask is POST /tasks/done. A done task adds a little progress
// (domain.TaskBonus) and closes its reminder wherever it is open.
func (a *App) CompleteTask(in TaskInput) (TaskResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.st
	t := st.IdeaByID(in.ID)
	if t == nil || t.Type != "task" {
		return TaskResult{}, ErrInvalid
	}
	if t.DoneAt == nil {
		a.finishTask(a.now(), t)
		a.persist()
	}
	return TaskResult{Announcement: "Task done: " + t.Text + ".", Percent: st.Progress.Percentage, Lang: st.Language()}, nil
}

// decideTask reminds the user of their tasks. First Max checks in on a task
// whose time has passed (or that was left open yesterday); then, on a
// distraction site, he brings the user back to the task in progress; then the
// most pressing open task, sooner on a distraction site and on a goal page
// only if it is urgent. It runs even on a "not today" day: that answer was
// about the goal, not the user's own errands.
func (a *App) decideTask(now time.Time, obs domain.Observation, day *domain.DayRecord, d *Decision) bool {
	if t, mode := a.st.TaskToCheckIn(now); t != nil {
		a.issueTaskCard(now, t, mode, day, d)
		return true
	}
	if urgent, cur := a.st.TaskToPrioritize(now); urgent != nil {
		a.issueTaskCard(now, urgent, domain.ModeUrgent, day, d)
		d.Payload.Other = a.taskView(cur)
		return true
	}
	if !obs.Page.Blacklisted {
		if cur, other := a.st.TaskToRefocus(now, obs.Page, obs.URL, obs.Title); cur != nil {
			a.issueTaskCard(now, cur, domain.ModeRefocus, day, d)
			d.Payload.Other = a.taskView(other)
			return true
		}
	}
	if obs.Page.Blacklisted {
		if t := a.st.TaskToResume(now); t != nil {
			a.issueTaskCard(now, t, domain.ModeResume, day, d)
			return true
		}
	}
	onGoal := obs.Page.Relevant && !obs.Page.Blacklisted
	t := a.st.TaskToRemind(now, day, obs.Page.Blacklisted, onGoal)
	if t == nil {
		return false
	}
	t.Nudged(day, true)
	d.Command, d.Reason = domain.CmdShowTask, "task_reminder"
	if obs.Page.Blacklisted {
		d.Reason = "task_reminder_distraction"
	}
	d.Payload.Task = a.taskView(t)
	d.Payload.TaskMode = domain.ModeRemind
	return true
}
