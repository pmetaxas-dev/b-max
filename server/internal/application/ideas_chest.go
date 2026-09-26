package application

import (
	"time"

	"focuscompanion/internal/domain"
)

// The idea chest (domain/ideas.go): a place to throw thoughts, kept for later.

type IdeaActionInput struct {
	ID string `json:"id"`
}

// DeleteIdea is POST /ideas/delete: the user let an idea go.
func (a *App) DeleteIdea(in IdeaActionInput) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	it := a.st.IdeaByID(in.ID)
	if it == nil || it.Type != "idea" {
		return ErrInvalid
	}
	a.st.RemoveIdea(in.ID)
	a.persist()
	return nil
}

// PromoteIdea is POST /ideas/promote: "do this today". It becomes a task.
func (a *App) PromoteIdea(in IdeaActionInput) (TaskView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	it := a.st.PromoteIdea(a.now(), in.ID)
	if it == nil {
		return TaskView{}, ErrInvalid
	}
	a.persist()
	return *a.taskView(it), nil
}

// decideIdeaNudge: the reminders about a task were not enough. After a long
// time on distraction sites with no task finished today, Max offers one of the
// user's own ideas, once a day, kindly, and never says why.
func (a *App) decideIdeaNudge(now time.Time, obs domain.Observation, day *domain.DayRecord, d *Decision) bool {
	st := a.st
	if !obs.Page.Blacklisted || day.IdeaOffered || day.DistractedMs < st.Settings.IdeaWait().Milliseconds() || st.DoneToday(now) > 0 {
		return false
	}
	idea := st.IdeaToRemind(now)
	if idea == nil {
		return false
	}
	day.IdeaOffered = true
	st.OfferedIdeas = append(st.OfferedIdeas, idea.ID)
	d.Command, d.Reason = domain.CmdOfferIdea, "idea_when_stuck"
	a.ideaPayload(now, idea, d)
	return true
}
