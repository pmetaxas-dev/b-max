package domain

import (
	"regexp"
	"strconv"
	"time"
)

func NewState() *State {
	s := &State{Version: 1}
	s.Normalize()
	s.Blacklist = DefaultBlacklist()
	return s
}

// Normalize makes a freshly decoded state safe to use.
func (s *State) Normalize() {
	if s.Anchors == nil {
		s.Anchors = map[string]ReturnAnchor{}
	}
	if s.Days == nil {
		s.Days = map[string]*DayRecord{}
	}
	if s.Settings == (Settings{}) {
		s.Settings = DefaultSettings()
	}
	s.Settings.fillDefaults()
	// State files from before era announcements: never announce an era the
	// user already reached.
	if s.CelebratedEra == "" {
		s.CelebratedEra = EraForPercent(s.Progress.Percentage)
	}
	if s.Session.State == "" {
		s.Session.State = StateIdle
		s.Session.ContextStrength = "weak"
	}
}

func DayKey(t time.Time) string { return t.Format("2006-01-02") }

// Day returns (creating if needed) the record for the local day of t.
func (s *State) Day(t time.Time) *DayRecord {
	k := DayKey(t)
	d := s.Days[k]
	if d == nil {
		d = &DayRecord{Date: k}
		s.Days[k] = d
	}
	return d
}

// CurrentMilestone is the first milestone that is not done.
func (s *State) CurrentMilestone() *Milestone {
	for i := range s.Milestones {
		if s.Milestones[i].Status != StatusDone {
			return &s.Milestones[i]
		}
	}
	return nil
}

// StepsOf returns the steps of a milestone in order.
func (s *State) StepsOf(milestoneID string) []Step {
	var out []Step
	for _, st := range s.Steps {
		if st.MilestoneID == milestoneID {
			out = append(out, st)
		}
	}
	sortSteps(out)
	return out
}

func sortSteps(steps []Step) {
	for i := 1; i < len(steps); i++ {
		for j := i; j > 0 && steps[j].Order < steps[j-1].Order; j-- {
			steps[j], steps[j-1] = steps[j-1], steps[j]
		}
	}
}

// CurrentStep is the first step with status pending, in order (§9b).
func (s *State) CurrentStep() *Step {
	for _, m := range s.Milestones {
		var best *Step
		for i := range s.Steps {
			st := &s.Steps[i]
			if st.MilestoneID == m.ID && st.Status == StatusPending && (best == nil || st.Order < best.Order) {
				best = st
			}
		}
		if best != nil {
			return best
		}
	}
	return nil
}

func (s *State) StepByID(id string) *Step {
	for i := range s.Steps {
		if s.Steps[i].ID == id {
			return &s.Steps[i]
		}
	}
	return nil
}

func (s *State) MilestoneByID(id string) *Milestone {
	for i := range s.Milestones {
		if s.Milestones[i].ID == id {
			return &s.Milestones[i]
		}
	}
	return nil
}

// RecomputeProgress is confirmed weight over total weight. Only confirmed
// steps count; time, sessions and ideas never do (§10).
func (s *State) RecomputeProgress() {
	total := 0.0
	for _, m := range s.Milestones {
		total += m.Weight
	}
	conf := 0.0
	for _, st := range s.Steps {
		if st.Status == StatusDone {
			conf += st.Weight
		}
	}
	pct := 0.0
	if total > 0 {
		pct = conf / total * 100
	}
	// The user's own tasks help a little, never a lot (tasks.go).
	if len(s.Milestones) == 0 && s.LifeGoal != "" {
		// Chat-first flow: no step plan; the planet follows the life goal.
		pct = float64(s.DonePoints()) / s.LifeGoalPoints() * 100
	} else {
		pct += s.TaskBonus()
	}
	if pct > 100 {
		pct = 100
	}
	s.Progress = ProgressState{ConfirmedWeight: conf, TotalWeight: total, Percentage: pct, Era: EraForPercent(pct)}
}

// EraNames are the five progress-driven eras (§10), in threshold order, as
// the canonical values of the planet runtime's `era` URL parameter. They are
// the only era vocabulary: the numeric index is internal to this package.
var EraNames = [5]string{"prehistoric", "copper", "medieval", "industrial", "space"}

// EraLabels are the human-readable names of EraNames, in the same order.
var EraLabels = [5]string{"Prehistoric", "Copper", "Medieval", "Industrial", "Space"}

// EraForPercent returns the canonical era for a 0-100 progress percentage.
func EraForPercent(pct float64) string { return EraNames[EraIndexForPercent(pct)] }

// EraLabelForPercent returns the human-readable era for a 0-100 percentage.
func EraLabelForPercent(pct float64) string { return EraLabels[EraIndexForPercent(pct)] }

// Planet weather values accepted by the planet runtime's `weather` parameter.
const (
	WeatherClear = "clear"
	WeatherStorm = "storm"
)

// Weather is the planet's weather. No business rule sets a storm yet, so it
// is always clear; the runtime supports "storm" for when one does.
func (s *State) Weather() string { return WeatherClear }

// EraIndexForPercent maps a 0-100 progress percentage to its era index
// (0-4) per the five thresholds in §10: 0-20, 21-40, 41-60, 61-80, 81-100.
func EraIndexForPercent(pct float64) int {
	switch {
	case pct <= 20:
		return 0
	case pct <= 40:
		return 1
	case pct <= 60:
		return 2
	case pct <= 80:
		return 3
	default:
		return 4
	}
}

// WeekSteps are steps scheduled in the current Monday-Sunday week; TodaySteps
// are those scheduled today. Neither is stored (§9b).
func (s *State) WeekSteps(now time.Time) []Step {
	offset := (int(now.Weekday()) + 6) % 7 // Monday = 0
	start := time.Date(now.Year(), now.Month(), now.Day()-offset, 0, 0, 0, 0, now.Location())
	from, to := DayKey(start), DayKey(start.AddDate(0, 0, 6))
	var out []Step
	for _, st := range s.Steps {
		if st.ScheduledFor != "" && st.ScheduledFor >= from && st.ScheduledFor <= to {
			out = append(out, st)
		}
	}
	return out
}

func (s *State) TodaySteps(now time.Time) []Step {
	k := DayKey(now)
	var out []Step
	for _, st := range s.Steps {
		if st.ScheduledFor == k {
			out = append(out, st)
		}
	}
	return out
}

// InterventionShown reports whether the return screen was already shown for a
// site key on a day. Issued but unacknowledged commands do not count (§8).
func (s *State) InterventionShown(day, site string) bool {
	for _, r := range s.Interventions {
		if r.Date == day && r.Site == site && !r.ShownAt.IsZero() {
			return true
		}
	}
	return false
}

// LatestIntervention finds the newest record for a site on a day.
func (s *State) LatestIntervention(day, site string) *InterventionRecord {
	for i := len(s.Interventions) - 1; i >= 0; i-- {
		r := &s.Interventions[i]
		if r.Date == day && r.Site == site {
			return r
		}
	}
	return nil
}

var firstInt = regexp.MustCompile(`\d+`)

// WorkBlockMinutes reads the onboarding answer ("10", "25", "45" or free text).
// It returns 0 when no number can be found.
func (p Preferences) WorkBlockMinutes() int {
	m := firstInt.FindString(p.WorkBlock)
	if m == "" {
		return 0
	}
	n, err := strconv.Atoi(m)
	if err != nil || n > 600 {
		return 0
	}
	return n
}

// SessionGoalMet is true when focused work in this session already exceeded the
// onboarding "work before a break" answer: no interruption for the rest of the
// session (§8).
func (s *State) SessionGoalMet() bool {
	n := s.Profile.Preferences.WorkBlockMinutes()
	return n > 0 && s.Session.SessionFocusedMs >= int64(n)*60_000
}

// UpdatePendingPrompt creates or expires the completion prompt (§9b). It uses
// no Groq. The prompt is created once focused time on the current step reaches
// the threshold; where it is shown is decided separately.
func (s *State) UpdatePendingPrompt(now time.Time) {
	step := s.CurrentStep()
	if step == nil {
		s.Pending = nil
		return
	}
	if s.Pending != nil {
		expired := DayKey(s.Pending.CreatedAt) != DayKey(now)
		if s.Pending.StepID != step.ID || expired {
			s.Pending = nil
			if expired {
				// Expiry drops the question; restart accumulation so it is
				// not immediately recreated.
				s.WorkContext.FocusedMsOnStep = 0
			}
		}
	}
	if s.Pending == nil && s.WorkContext.FocusedMsOnStep >= step.thresholdMs(s.Settings) {
		s.Pending = &PendingPrompt{Type: PromptCompletion, StepID: step.ID, CreatedAt: now}
	}
}

func (st Step) thresholdMs(cfg Settings) int64 {
	return int64(st.EstimatedMinutes) * 60_000 * int64(cfg.CompletionThresholdPercent) / 100
}

// PromptDeliverable is true when the pending prompt may be shown now: the
// first time as soon as it exists; after "not yet" (or ignoring it) only once
// the user has worked CompletionReask more on the step, and never more than
// MaxCompletionAsks times. Time alone never brings it back: someone who
// stopped working on the step is not asked again.
func (s *State) PromptDeliverable(now time.Time) bool {
	p := s.Pending
	if p == nil || !p.ShownAt.IsZero() || p.Asks >= MaxCompletionAsks {
		return false
	}
	if p.Asks == 0 {
		return true
	}
	return s.WorkContext.FocusedMsOnStep-p.WorkAtAskMs >= s.Settings.CompletionReask().Milliseconds()
}

// SetPromptAside records "not yet" (or an ignored question): it may come back
// after more work on the step (PromptDeliverable).
func (s *State) SetPromptAside() {
	if p := s.Pending; p != nil {
		p.ShownAt = time.Time{}
		p.WorkAtAskMs = s.WorkContext.FocusedMsOnStep
	}
}
