package application

import (
	"time"

	"focuscompanion/internal/domain"
)

// The life of a task (domain/tasklife.go): start, work, check in, get back
// after a distraction, set aside with a note, finish.

type TaskActionInput struct {
	ID     string `json:"id"`
	Action string `json:"action"` // start | pause | yes | not_yet | later | resume | helped
	Note   string `json:"note"`
}

type TaskActionResult struct {
	Announcement string    `json:"announcement,omitempty"`
	Percent      float64   `json:"percent"`
	Lang         string    `json:"lang"`
	ResumeURL    string    `json:"resumeUrl,omitempty"`   // "resume": where the user was
	ResumeTitle  string    `json:"resumeTitle,omitempty"` //
	SearchQuery  string    `json:"searchQuery,omitempty"` // no page to go back to: a search that starts the task
	Note         string    `json:"note,omitempty"`
	Task         *TaskView `json:"task,omitempty"`
}

// closeTaskCard takes down the open card about this task, if it is the one.
func (a *App) closeTaskCard(id string) {
	if c := a.st.OpenCard; c != nil && c.Command == domain.CmdShowTask && c.TaskID == id {
		a.st.OpenCard = nil
	}
}

// finishTask marks a task done. It must be called with the mutex held.
func (a *App) finishTask(now time.Time, t *domain.Idea) {
	if t.DoneAt != nil {
		return
	}
	t.DoneAt, t.Status = &now, ""
	a.st.Log(now, t.ID, "Finished “"+t.Text+"”")
	a.closeTaskCard(t.ID)
	a.advanceGoalStep(now) // the next step of the life goal appears
	a.st.RecomputeProgress()
	a.st.EnsureCurrentTask(now) // the next task takes over
}

// TaskAction is POST /tasks/action.
func (a *App) TaskAction(in TaskActionInput) (TaskActionResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.st
	t := st.IdeaByID(in.ID)
	if t == nil || t.Type != "task" {
		return TaskActionResult{}, ErrInvalid
	}
	now := a.now()
	res := TaskActionResult{Lang: st.Language()}
	switch in.Action {
	case "start":
		if st.StartTask(now, t.ID) == nil {
			return TaskActionResult{}, ErrInvalid
		}
		a.closeTaskCard(t.ID)
	case "pause", "later":
		if st.PauseTask(now, t.ID, clipRunes(in.Note, 200)) == nil {
			return TaskActionResult{}, ErrInvalid
		}
		a.closeTaskCard(t.ID)
		res.Note = t.Note
	case "reopen":
		if t.DoneAt == nil {
			return TaskActionResult{}, ErrInvalid
		}
		t.DoneAt = nil // done by mistake: the task and the planet go back
		st.Log(now, t.ID, "Reopened “"+t.Text+"” (it was ticked by mistake)")
		st.RecomputeProgress()
	case "yes":
		if !t.OpenTask() {
			return TaskActionResult{}, ErrInvalid
		}
		a.finishTask(now, t)
		res.Announcement = "Task done: " + t.Text + "."
	case "not_yet":
		t.CheckAfter = now.Add(st.Settings.NotYetWait())
		if t.Due != "" && t.Due < domain.DayKey(now) {
			t.Due = domain.DayKey(now) // yesterday's task is today's now, and its clock starts again
			if t.Status == domain.TaskDoing {
				t.StartedAt = &now
			}
		}
		st.Log(now, t.ID, "“"+t.Text+"” is not finished yet")
		a.closeTaskCard(t.ID)
	case "resume":
		if !t.OpenTask() {
			return TaskActionResult{}, ErrInvalid
		}
		t.Status, t.ResumeAt, t.CheckAfter = domain.TaskDoing, now, time.Time{}
		if t.StartedAt == nil {
			t.StartedAt = &now
		}
		// Back to the page they were on today; if there is none, to where the task
		// starts: a search for it (never an old or unrelated page).
		if t.LastURL != "" && domain.DayKey(t.LastAt) == domain.DayKey(now) {
			res.ResumeURL, res.ResumeTitle = t.LastURL, t.LastTitle
		} else if t.Kind != "offline" {
			res.SearchQuery = t.StartQuery
			if res.SearchQuery == "" {
				res.SearchQuery = clipRunes(t.Text, 100)
			}
		}
		res.Note = t.Note
		st.Log(now, t.ID, "Went back to “"+t.Text+"”")
		a.closeTaskCard(t.ID)
	case "keep":
		a.closeTaskCard(t.ID) // the user keeps the task they are on: nothing changes
	case "helped":
		t.CheckAfter, t.ResumeAt = now.Add(st.Settings.HelpWait()), now
		st.Log(now, t.ID, "Asked Max for a first step on “"+t.Text+"”")
		a.closeTaskCard(t.ID)
	default:
		return TaskActionResult{}, ErrInvalid
	}
	st.EnsureCurrentTask(now)
	res.Percent = st.Progress.Percentage
	res.Task = a.taskView(t)
	a.persist()
	return res, nil
}

// issueTaskCard fills d with the SHOW_TASK card for t in the given mode and
// records that the user was asked.
func (a *App) issueTaskCard(now time.Time, t *domain.Idea, mode string, day *domain.DayRecord, d *Decision) {
	st := a.st
	today := domain.DayKey(now)
	switch mode {
	case domain.ModeCheckIn:
		if t.CheckDate != today {
			t.CheckDate, t.CheckCount = today, 0
		}
		t.CheckCount++
		if at, ok := t.DeadlineAt(now); ok && t.Status != domain.TaskDoing {
			t.DeadlineAsked = today + "@" + at.Format("15:04")
		}
		st.Log(now, t.ID, "Asked whether “"+t.Text+"” is done")
	case domain.ModeCarry:
		t.CarryAsked = today
		st.Log(now, t.ID, "Asked about “"+t.Text+"”, left open from an earlier day")
	case domain.ModeUrgent:
		t.UrgentAsked = today
		st.Log(now, t.ID, "The deadline of “"+t.Text+"” is close; asked whether to switch to it")
	case domain.ModeRefocus:
		t.ResumeAt = now
		st.Log(now, t.ID, "Started on another task while “"+t.Text+"” had priority")
	case domain.ModeResume:
		t.ResumeAt = now
		st.Log(now, t.ID, "Drifted away from “"+t.Text+"” while it was in progress")
	}
	t.Nudged(day, false)
	d.Command, d.Reason = domain.CmdShowTask, "task_"+mode
	d.Payload.Task = a.taskView(t)
	d.Payload.TaskMode = mode
}

// journalFor is what Max is told happened lately.
func (a *App) journalFor() []string { return a.st.RecentJournal(10) }
