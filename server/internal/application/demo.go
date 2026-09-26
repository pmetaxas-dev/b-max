package application

import (
	"time"

	"focuscompanion/internal/domain"
)

// Phase 5 test tools behind the dashboard's ⚙️ panel: the App Clock and a
// seeded demo state. Hackathon build only, like dev.go.

type ClockInput struct {
	// AddDays moves the App Clock forward. With Visited, today first counts
	// as a day the user was here (with no step done, a difficult day);
	// without it, the days in between are an absence.
	AddDays int  `json:"addDays"`
	Visited bool `json:"visited"`
	Reset   bool `json:"reset"`
}

type ClockView struct {
	OffsetDays int    `json:"offsetDays"`
	Today      string `json:"today"`
}

const maxClockOffsetDays = 90

func (a *App) SetClock(in ClockInput) (ClockView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.st
	switch {
	case in.Reset:
		st.ClockOffsetDays = 0
	case in.AddDays > 0 && st.ClockOffsetDays+in.AddDays <= maxClockOffsetDays:
		if in.Visited {
			st.Day(a.now())
			st.LastActiveDay = domain.DayKey(a.now())
		}
		st.ClockOffsetDays += in.AddDays
		st.OpenCard = nil // a card from "yesterday" does not follow into the new day
	default:
		return ClockView{}, ErrInvalid
	}
	a.persist()
	return ClockView{OffsetDays: st.ClockOffsetDays, Today: domain.DayKey(a.now())}, nil
}

// Seed replaces the profile with a ready demo: a goal with a plan, one step
// done, a week and a half of ideas in the lamp and a task for today. No AI
// call. The blacklist, timings, App Clock and voice recording stay.
func (a *App) Seed() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.st
	now := a.now()
	st := domain.NewState()
	st.Settings, st.Blacklist, st.ClockOffsetDays = old.Settings, old.Blacklist, old.ClockOffsetDays
	st.Profile = domain.UserProfile{Onboarded: true, CreatedAt: now.AddDate(0, 0, -12),
		Preferences: domain.Preferences{WorkBlock: "45", BestTime: "evening", Distraction: "YouTube"}}
	st.Goal = &domain.PrimaryGoal{
		ID: domain.NewID("goal"), Statement: "Become a YouTuber", Status: domain.GoalActive,
		TargetHorizon: "6 months", CreatedAt: now.AddDate(0, 0, -12), EstimatedHours: 200,
		Keywords: []string{"youtuber", "obs studio", "video editing", "davinci resolve", "thumbnail", "channel", "studio.youtube.com", "obsproject.com"},
	}
	if old.Goal != nil {
		st.Goal.VoiceRef = old.Goal.VoiceRef
	}
	day := func(n int) string { return domain.DayKey(now.AddDate(0, 0, n)) }
	// Weights put the first milestone at 30%: two steps done is 15%, and
	// confirming the OBS step on stage reaches Copper (over 20%).
	raw := domain.RawPlan{Milestones: []domain.RawMilestone{
		{Title: "Set up the channel", Weight: 1.5, Steps: []domain.RawStep{
			{Text: "Pick a channel name and topic", EstimatedMinutes: 20, ScheduledFor: day(-2), SearchQueries: []string{"how to choose a youtube channel name"}},
			{Text: "Write the channel description", EstimatedMinutes: 15, ScheduledFor: day(-1), SearchQueries: []string{"youtube channel description examples"}},
			{Text: "Record a 1-minute test video with OBS Studio", EstimatedMinutes: 30, ScheduledFor: day(0), SearchQueries: []string{"obs studio beginner setup", "obs studio record screen and webcam"}},
			{Text: "Design the channel banner", EstimatedMinutes: 40, ScheduledFor: day(1), SearchQueries: []string{"youtube banner size", "canva youtube banner"}},
		}},
		{Title: "Make the first video", Weight: 1.5},
		{Title: "Publish weekly for a month", Weight: 1},
		{Title: "Grow to the first 100 subscribers", Weight: 1},
	}}
	st.Milestones, st.Steps = domain.BuildPlan(st.Goal.ID, raw)
	st.Steps[0].Status, st.Steps[1].Status = domain.StatusDone, domain.StatusDone
	for i, text := range []string{
		"A video about my desk setup", "Series: learning to edit in 30 days", "Thumbnail with my face and big text",
		"Collab with a friend who games", "Short about my morning routine", "Behind the scenes of the first video",
		"Ask viewers what to film next",
	} {
		st.Ideas = append(st.Ideas, domain.Idea{ID: domain.NewID("idea"), Type: "idea", Text: text, CreatedAt: now.AddDate(0, 0, -11+i)})
	}
	task := domain.Idea{ID: domain.NewID("idea"), Type: "task", Text: "Send an email to the sponsor today", CreatedAt: now.Add(-time.Hour), Due: day(0)}
	task.Nudged(&domain.DayRecord{Date: day(0)}, false)
	st.Ideas = append(st.Ideas, task)
	// Two ordinary days behind: a step done, then some time on the goal.
	st.Days[day(-2)] = &domain.DayRecord{Date: day(-2), ConfirmedSteps: 1, SawRelevant: true, RelevantMs: 35 * 60_000}
	st.Days[day(-1)] = &domain.DayRecord{Date: day(-1), ConfirmedSteps: 1, SawRelevant: true, RelevantMs: 10 * 60_000}
	st.LastActiveDay = day(-1)
	st.RecomputeProgress()
	st.CelebratedEra = st.Progress.Era
	a.st = st
	a.persist()
	return a.saveErr
}
