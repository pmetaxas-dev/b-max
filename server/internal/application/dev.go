package application

import (
	"context"
	"time"

	"focuscompanion/internal/domain"
)

// Test tools behind the dashboard's ⚙️ panel, for trying the app by hand
// without editing state.json. They exist for the hackathon build only.

const (
	ResetDay      = "day"      // a fresh day: today's record, return screens, open card, session
	ResetProgress = "progress" // every step pending again, 0%, Prehistoric; the plan stays
	ResetProfile  = "profile"  // everything but the blacklist and timings: onboarding again
)

type ResetInput struct {
	Scope string `json:"scope"`
}

func (a *App) Reset(in ResetInput) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.st
	now := a.now()
	switch in.Scope {
	case ResetDay:
		today := domain.DayKey(now)
		delete(st.Days, today)
		kept := st.Interventions[:0]
		for _, r := range st.Interventions {
			if r.Date != today {
				kept = append(kept, r)
			}
		}
		st.Interventions = kept
		st.OpenCard = nil
		st.Session = domain.FocusSession{State: domain.StateIdle, ContextStrength: "weak"}
		wc := &st.WorkContext
		wc.LastWorkTabID, wc.LastWorkURL = 0, ""
		wc.LastRelevantTabID, wc.LastRelevantURL, wc.LastRelevantAt = 0, "", time.Time{}
	case ResetProgress:
		if st.Goal == nil && st.LifeGoal == "" {
			return ErrNoGoal
		}
		for i := range st.Steps {
			st.Steps[i].Status = domain.StatusPending
		}
		for i := range st.Milestones {
			st.Milestones[i].Status = domain.StatusPending
		}
		for _, d := range st.Days {
			d.ConfirmedSteps = 0
		}
		for i := range st.Ideas {
			st.Ideas[i].DoneAt = nil // done tasks add progress too (tasks.go)
		}
		if st.Goal != nil {
			st.Goal.Status = domain.GoalActive
		}
		st.Pending = nil
		st.OpenCard = nil
		st.WorkContext.FocusedMsOnStep = 0
		st.RecomputeProgress()
		st.CelebratedEra = st.Progress.Era
	case ResetProfile:
		if err := a.repo.DeleteVoice(); err != nil {
			return err
		}
		fresh := domain.NewState()
		fresh.Settings, fresh.Blacklist = st.Settings, st.Blacklist
		a.st = fresh
	default:
		return ErrInvalid
	}
	a.persist()
	return nil
}

// RegenerateSteps asks the planner again for the current milestone's steps,
// with the full plan and the completed steps, and replaces the steps that are
// not done yet. Done steps and their progress stay.
func (a *App) RegenerateSteps(ctx context.Context) error {
	a.mu.Lock()
	m := a.st.CurrentMilestone()
	if m == nil || a.st.Goal == nil {
		a.mu.Unlock()
		return ErrNoGoal
	}
	ms, goal := *m, a.st.Goal.Statement
	plan := a.st.PlanContextFor(ms)
	now := a.now()
	a.mu.Unlock()

	raw, err := a.planner.StepsFor(ctx, goal, ms, now, plan)
	if err != nil {
		return ErrPlanUnavailable
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.st
	cur := st.CurrentMilestone()
	if cur == nil || cur.ID != ms.ID {
		return nil // the milestone changed meanwhile: nothing to replace
	}
	built := domain.BuildSteps(*cur, raw)
	if len(built) == 0 {
		return ErrPlanUnavailable
	}
	doneWeight, lastOrder := 0.0, 0
	kept := st.Steps[:0]
	for _, s := range st.Steps {
		if s.MilestoneID == cur.ID && s.Status != domain.StatusDone {
			continue // replaced
		}
		if s.MilestoneID == cur.ID {
			doneWeight += s.Weight
			lastOrder = max(lastOrder, s.Order)
		}
		kept = append(kept, s)
	}
	per := max(0, cur.Weight-doneWeight) / float64(len(built))
	for i := range built {
		built[i].Weight, built[i].Order = per, lastOrder+i+1
	}
	st.Steps = append(kept, built...)
	cur.StepsGenerated = true
	if st.Pending != nil && st.StepByID(st.Pending.StepID) == nil {
		st.Pending = nil
		st.WorkContext.FocusedMsOnStep = 0
		a.closeOpen(domain.CmdAskCompletion)
	}
	st.RecomputeProgress()
	a.persist()
	return nil
}

type TimingsInput struct {
	Fast bool `json:"fast"`
}

// SetTimings switches between the normal timings and FastSettings.
func (a *App) SetTimings(in TimingsInput) domain.Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	if in.Fast {
		a.st.Settings = domain.FastSettings()
	} else {
		a.st.Settings = domain.DefaultSettings()
	}
	a.persist()
	return a.st.Settings
}

type StormInput struct {
	On bool `json:"on"`
}

// SetStorm switches the planet's storm weather on or off on demand, for
// presentations: no need to actually earn a bad day to show it.
func (a *App) SetStorm(in StormInput) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.st.ForceStorm = in.On
	a.persist()
	return a.st.ForceStorm
}
