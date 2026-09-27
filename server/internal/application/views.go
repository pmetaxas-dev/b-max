package application

import "focuscompanion/internal/domain"

// PlanetVisualState drives both the mini and the full planet renderer (§10).
// Era and Weather are the planet runtime's canonical `era` and `weather` URL
// values (domain.EraNames, domain.WeatherClear/Storm), so the extension only
// forwards them and never needs the era thresholds itself.
type PlanetVisualState struct {
	Progress                  float64            `json:"progress"` // 0..1
	Era                       string             `json:"era"`
	EraLabel                  string             `json:"eraLabel"`
	Weather                   string             `json:"weather"`
	Description               string             `json:"description"` // the era in words, for anyone who cannot see it
	Parameters                map[string]float64 `json:"parameters"`
	UnlockedSignatureElements []string           `json:"unlockedSignatureElements"`
}

type ProgressView struct {
	StepsDone  int     `json:"stepsDone"`
	StepsTotal int     `json:"stepsTotal"`
	Percent    float64 `json:"percent"`
}

type PromptView struct {
	Type string    `json:"type"`
	Step *StepView `json:"step"`
}

type Summary struct {
	Onboarded        bool              `json:"onboarded"`
	Goal             string            `json:"goal"`
	GoalCompleted    bool              `json:"goalCompleted"`
	CurrentStep      *StepView         `json:"currentStep"`
	Progress         ProgressView      `json:"progress"`
	Planet           PlanetVisualState `json:"planet"`
	Pending          *PromptView       `json:"pendingPrompt"`
	NextStepsPending bool              `json:"nextStepsPending"`
	Lang             string            `json:"lang"` // the goal's language
	Day              DayView           `json:"day"`
}

// DayView is today's mode and effort score. The score is shown, never
// turned into planet progress: only confirmed steps move the planet.
type DayView struct {
	Mode            string `json:"mode"` // good | bad | undecided
	RelevantMinutes int    `json:"relevantMinutes"`
}

type MilestoneView struct {
	ID      string        `json:"id"`
	Title   string        `json:"title"`
	Weight  float64       `json:"weight"`
	Status  string        `json:"status"`
	Steps   []domain.Step `json:"steps"`
	Current bool          `json:"current"`
}

type Full struct {
	Summary
	TargetHorizon string                  `json:"targetHorizon"`
	VoiceRecorded bool                    `json:"voiceRecorded"`
	Milestones    []MilestoneView         `json:"milestones"`
	Week          []domain.Step           `json:"week"`
	Today         []domain.Step           `json:"today"`
	Blacklist     []domain.BlacklistEntry `json:"blacklist"`
	FastTimings   bool                    `json:"fastTimings"`     // the ⚙️ test panel's switch (dev.go)
	StormForced   bool                    `json:"stormForced"`     // the ⚙️ test panel's storm switch (dev.go)
	ClockOffset   int                     `json:"clockOffsetDays"` // the ⚙️ App Clock (demo.go)
	TodayDate     string                  `json:"todayDate"`       // today by the App Clock
	Session       struct {
		State    domain.SessionState `json:"state"`
		Strength string              `json:"contextStrength"`
	} `json:"session"`
}

// summary must be called with the mutex held.
func (a *App) summary() Summary {
	st := a.st
	s := Summary{Onboarded: st.Profile.Onboarded, Lang: st.Language()}
	if day := st.Days[domain.DayKey(a.now())]; day != nil {
		s.Day = DayView{Mode: day.Mode(), RelevantMinutes: int(day.RelevantMs / 60_000)}
	} else {
		s.Day.Mode = domain.DayUndecided
	}
	s.Planet = PlanetVisualState{
		// Derived from the percentage rather than the stored Progress.Era, so a
		// state file saved with the older era vocabulary still maps correctly.
		Progress:    st.Progress.Percentage / 100,
		Era:         domain.EraForPercent(st.Progress.Percentage),
		EraLabel:    domain.EraLabelForPercent(st.Progress.Percentage),
		Weather:     st.WeatherAt(a.now()),
		Description: EraDescription(st.Progress.Percentage, st.Language()),
		Parameters:  map[string]float64{}, UnlockedSignatureElements: []string{},
	}
	if st.Goal == nil {
		return s
	}
	s.Goal = st.Goal.Statement
	s.GoalCompleted = st.Goal.Status == domain.GoalCompleted
	s.CurrentStep = a.stepView(st.CurrentStep())
	s.Progress.Percent = st.Progress.Percentage

	// Progress text counts steps of the current milestone, because only the
	// current milestone's steps exist (§9b).
	ms := st.CurrentMilestone()
	if ms == nil && len(st.Milestones) > 0 {
		ms = &st.Milestones[len(st.Milestones)-1]
	}
	if ms != nil {
		steps := st.StepsOf(ms.ID)
		s.Progress.StepsTotal = len(steps)
		for _, x := range steps {
			if x.Status == domain.StatusDone {
				s.Progress.StepsDone++
			}
		}
		s.NextStepsPending = !ms.StepsGenerated
	}
	if st.Pending != nil {
		s.Pending = &PromptView{Type: st.Pending.Type, Step: a.stepView(st.StepByID(st.Pending.StepID))}
	}
	return s
}

func (a *App) Summary() Summary {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.summary()
}

func (a *App) Full() Full {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.st
	now := a.now()
	f := Full{
		Summary: a.summary(), Blacklist: st.Blacklist, FastTimings: st.Settings.Fast,
		StormForced: st.ForceStorm, ClockOffset: st.ClockOffsetDays, TodayDate: domain.DayKey(now),
	}
	f.Session.State = st.Session.State
	f.Session.Strength = st.Session.ContextStrength
	f.Week, f.Today = st.WeekSteps(now), st.TodaySteps(now)
	if f.Week == nil {
		f.Week = []domain.Step{}
	}
	if f.Today == nil {
		f.Today = []domain.Step{}
	}
	f.Milestones = []MilestoneView{}
	if st.Goal != nil {
		f.TargetHorizon = st.Goal.TargetHorizon
		f.VoiceRecorded = st.Goal.VoiceRef != ""
	}
	cur := st.CurrentMilestone()
	for _, m := range st.Milestones {
		steps := st.StepsOf(m.ID)
		if steps == nil {
			steps = []domain.Step{}
		}
		f.Milestones = append(f.Milestones, MilestoneView{
			ID: m.ID, Title: m.Title, Weight: m.Weight, Status: m.Status,
			Steps: steps, Current: cur != nil && cur.ID == m.ID,
		})
	}
	return f
}
