package application

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"focuscompanion/internal/domain"
)

type OnboardInput struct {
	Preferences domain.Preferences `json:"preferences"`
	Goal        string             `json:"goal"`
	// AllowSmall: the user chose "continue anyway" for a goal the planner
	// estimated as a task (ErrGoalTooSmall).
	AllowSmall bool `json:"allowSmall"`
	// Fallback: the AI is unreachable and the user chose "start with a
	// simple plan" (domain.FallbackPlan). No planner call.
	Fallback bool `json:"fallback"`
}

// A goal estimated under smallGoalHours of work is really a task: the planet
// would reach its last era in an afternoon. The user is asked first.
const smallGoalHours = 8

// ErrGoalTooSmall: the planner estimated the goal as a task; nothing stored.
var ErrGoalTooSmall = errors.New("this goal looks like a task")

// cachedPlan keeps the plan of a goal refused as too small, so "continue
// anyway" does not ask the planner again. In memory, briefly.
type cachedPlan struct {
	goal string
	raw  domain.RawPlan
	at   time.Time
}

const cachedPlanTTL = 15 * time.Minute

func clipRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > max {
		return string([]rune(s)[:max])
	}
	return s
}

// Onboard is POST /onboarding: profile, goal and first plan. There is exactly
// one active primary goal (§2). If the planner fails the caller gets
// ErrPlanUnavailable and nothing is stored: never a malformed plan (§17).
func (a *App) Onboard(ctx context.Context, in OnboardInput) error {
	goal := clipRunes(in.Goal, 300)
	if utf8.RuneCountInString(goal) < 3 {
		return ErrInvalid
	}
	prefs := domain.Preferences{
		WorkBlock:   clipRunes(in.Preferences.WorkBlock, 100),
		BestTime:    clipRunes(in.Preferences.BestTime, 100),
		Distraction: clipRunes(in.Preferences.Distraction, 100),
		ReturnCue:   clipRunes(in.Preferences.ReturnCue, 100),
	}

	a.mu.Lock()
	if a.st.Goal != nil && a.st.Goal.Status == domain.GoalActive {
		a.mu.Unlock()
		return ErrGoalExists
	}
	now := a.now()
	cached := a.smallPlan
	a.mu.Unlock()

	var raw domain.RawPlan
	if in.Fallback {
		raw = domain.FallbackPlan(goal, now)
	} else if in.AllowSmall && cached != nil && cached.goal == goal && now.Sub(cached.at) < cachedPlanTTL {
		raw = cached.raw // "continue anyway": the plan the user just saw estimated
	} else {
		// The Groq call happens outside the lock so events are not blocked.
		var err error
		raw, err = a.planner.Outline(ctx, goal, now)
		if err != nil {
			log.Printf("onboarding plan failed: %v", err)
			return ErrPlanUnavailable
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if !in.AllowSmall && !in.Fallback && raw.EstimatedHours > 0 && raw.EstimatedHours < smallGoalHours {
		a.smallPlan = &cachedPlan{goal: goal, raw: raw, at: now}
		return fmt.Errorf("%w: about %.0f hours of work", ErrGoalTooSmall, math.Ceil(raw.EstimatedHours))
	}
	a.smallPlan = nil
	if a.st.Goal != nil && a.st.Goal.Status == domain.GoalActive {
		return ErrGoalExists
	}
	st := a.st
	horizon := clipRunes(raw.TargetHorizon, 100)
	st.Goal = &domain.PrimaryGoal{
		ID: domain.NewID("goal"), Statement: goal, Status: domain.GoalActive,
		TargetHorizon: horizon, CreatedAt: now,
		Keywords:       domain.CleanKeywords(raw.Keywords),
		EstimatedHours: max(0, raw.EstimatedHours),
		Fallback:       in.Fallback,
	}
	st.Milestones, st.Steps = domain.BuildPlan(st.Goal.ID, raw)
	st.Anchors = map[string]domain.ReturnAnchor{}
	st.Pending = nil
	st.WorkContext = domain.WorkContext{}
	st.Session = domain.FocusSession{State: domain.StateIdle, ContextStrength: "weak"}
	st.Profile = domain.UserProfile{Preferences: prefs, Onboarded: true, CreatedAt: now}
	st.RecomputeProgress()
	st.CelebratedEra, st.OpenCard = st.Progress.Era, nil // a new goal starts its own eras
	a.persist()
	return nil
}

// keywordRetry: a failed keyword request is not retried sooner than this.
const keywordRetry = 10 * time.Minute

// fillKeywordsAsync asks the planner, in the background, for keywords when the
// goal has none (plans made before keywords existed). Until they arrive the
// narrow fallback in domain/relevance.go is used. Called from the heartbeat.
func (a *App) fillKeywordsAsync(now time.Time) {
	if a.st.Goal == nil || len(a.st.Goal.Keywords) > 0 || now.Sub(a.keywordAttemptAt) < keywordRetry {
		return
	}
	if !a.fillingKeywords.CompareAndSwap(false, true) {
		return
	}
	a.keywordAttemptAt = now
	go func() {
		defer a.fillingKeywords.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := a.BackfillKeywords(ctx); err != nil {
			log.Printf("goal keywords still pending: %v", err)
		}
	}()
}

// BackfillKeywords fetches and stores keywords for the active goal if it has
// none. The planner call runs outside the lock.
func (a *App) BackfillKeywords(ctx context.Context) error {
	a.mu.Lock()
	if a.st.Goal == nil || len(a.st.Goal.Keywords) > 0 {
		a.mu.Unlock()
		return nil
	}
	goalID, goal := a.st.Goal.ID, a.st.Goal.Statement
	titles := make([]string, 0, len(a.st.Milestones))
	for _, m := range a.st.Milestones {
		titles = append(titles, m.Title)
	}
	a.mu.Unlock()

	kw, err := a.planner.Keywords(ctx, goal, titles)
	if err != nil {
		return err
	}
	if kw = domain.CleanKeywords(kw); len(kw) == 0 {
		return domain.ErrInvalidPlan
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.st.Goal == nil || a.st.Goal.ID != goalID || len(a.st.Goal.Keywords) > 0 {
		return nil // the goal changed or got keywords meanwhile
	}
	a.st.Goal.Keywords = kw
	a.persist()
	return nil
}

// fillStepsAsync generates missing steps for the current milestone in the
// background, at most one at a time. Called from the heartbeat.
func (a *App) fillStepsAsync() {
	m := a.st.CurrentMilestone()
	if m == nil || m.StepsGenerated {
		return
	}
	if !a.filling.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.filling.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := a.fillMissingSteps(ctx); err != nil {
			log.Printf("steps for the next milestone are still pending: %v", err)
		}
	}()
}

// fillMissingSteps asks the planner for the current milestone's steps if it has
// none. The Groq call runs outside the lock.
func (a *App) fillMissingSteps(ctx context.Context) error {
	a.mu.Lock()
	m := a.st.CurrentMilestone()
	if m == nil || m.StepsGenerated || a.st.Goal == nil {
		a.mu.Unlock()
		return nil
	}
	ms, goal, fallback := *m, a.st.Goal.Statement, a.st.Goal.Fallback
	plan := a.st.PlanContextFor(ms)
	now := a.now()
	a.mu.Unlock()

	raw, err := a.planner.StepsFor(ctx, goal, ms, now, plan)
	if err != nil {
		// A plan made without the AI goes on without it (Phase 5).
		if raw = domain.FallbackSteps(goal, ms.Title, now); !fallback || raw == nil {
			return err
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	cur := a.st.CurrentMilestone()
	if cur == nil || cur.ID != ms.ID || cur.StepsGenerated {
		return nil
	}
	steps := domain.BuildSteps(*cur, raw)
	if len(steps) == 0 {
		return domain.ErrInvalidPlan
	}
	a.st.Steps = append(a.st.Steps, steps...)
	cur.StepsGenerated = true
	a.st.RecomputeProgress()
	a.persist()
	return nil
}
