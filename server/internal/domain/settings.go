package domain

import "time"

// Settings is the one place for timing configuration (architecture-plan-v2 §5.3).
type Settings struct {
	GraceSeconds               int `json:"graceSeconds"`               // §5.3: about five minutes
	InactivitySeconds          int `json:"inactivitySeconds"`          // §5.3: fifteen minutes
	EpisodeResetSeconds        int `json:"episodeResetSeconds"`        // §8: 60 continuous seconds on a non-blacklisted page
	CompletionThresholdPercent int `json:"completionThresholdPercent"` // §9b: 80% of estimated duration
	SnoozeMinutes              int `json:"snoozeMinutes"`              // §9b: "Not yet" silences for 30 minutes
	// SegmentCreditCapSeconds is not in the spec. Without a cap, a laptop that
	// sleeps for hours would credit the whole gap as focused work.
	SegmentCreditCapSeconds int `json:"segmentCreditCapSeconds"`
	// Good day / bad day (day.go): active Chrome time without any relevant
	// page before the day question, and before asking again.
	DayAskAfterSeconds int `json:"dayAskAfterSeconds"`
	DayReaskSeconds    int `json:"dayReaskSeconds"`
	// An unanswered card closes after this much ACTIVE use of Chrome (card.go).
	CardIgnoreSeconds int `json:"cardIgnoreSeconds"`
	// After "not yet", "did you finish?" returns after this much more WORK on
	// the step (not clock time).
	CompletionReaskSeconds int `json:"completionReaskSeconds"`
	// Task reminders (tasks.go): after this much active web time since the
	// last one, or the shorter distraction interval on a blacklisted site.
	TaskRemindSeconds            int `json:"taskRemindSeconds"`
	TaskDistractionRemindSeconds int `json:"taskDistractionRemindSeconds"`
	// Fast: the short timings for manual testing are on (FastSettings, set
	// from the dashboard's test panel).
	Fast bool `json:"fast,omitempty"`
}

// FastSettings are short timings for trying the app by hand: return screen
// after 10 s, day question after 1 min, completion prompt at 5% of a step.
// CardIgnoreSeconds is deliberately left at its default: shortening it here
// made the Max card vanish before there was time to read it or respond,
// even though Fast mode is meant to speed up waiting for OTHER prompts,
// not cut down how long an already-open card stays up.
func FastSettings() Settings {
	s := DefaultSettings()
	s.GraceSeconds = 10
	s.DayAskAfterSeconds = 60
	s.DayReaskSeconds = 120
	s.CompletionThresholdPercent = 5
	s.CompletionReaskSeconds = 60
	s.TaskRemindSeconds = 90
	s.TaskDistractionRemindSeconds = 20
	s.Fast = true
	return s
}

func DefaultSettings() Settings {
	return Settings{
		GraceSeconds:                 300,
		InactivitySeconds:            900,
		EpisodeResetSeconds:          60,
		CompletionThresholdPercent:   80,
		SnoozeMinutes:                30,
		SegmentCreditCapSeconds:      180,
		DayAskAfterSeconds:           1800,
		DayReaskSeconds:              3600,
		CardIgnoreSeconds:            300,
		CompletionReaskSeconds:       1200,
		TaskRemindSeconds:            2700,
		TaskDistractionRemindSeconds: 600,
	}
}

// fillDefaults gives fields added after a state file was written their
// default, instead of the zero value (which would e.g. ask the day question
// immediately).
func (s *Settings) fillDefaults() {
	def := DefaultSettings()
	if s.DayAskAfterSeconds <= 0 {
		s.DayAskAfterSeconds = def.DayAskAfterSeconds
	}
	if s.DayReaskSeconds <= 0 {
		s.DayReaskSeconds = def.DayReaskSeconds
	}
	if s.CardIgnoreSeconds <= 0 {
		s.CardIgnoreSeconds = def.CardIgnoreSeconds
	}
	if s.CompletionReaskSeconds <= 0 {
		s.CompletionReaskSeconds = def.CompletionReaskSeconds
	}
	if s.TaskRemindSeconds <= 0 {
		s.TaskRemindSeconds = def.TaskRemindSeconds
	}
	if s.TaskDistractionRemindSeconds <= 0 {
		s.TaskDistractionRemindSeconds = def.TaskDistractionRemindSeconds
	}
}

func (s Settings) CompletionReask() time.Duration {
	return time.Duration(s.CompletionReaskSeconds) * time.Second
}

func (s Settings) CardIgnore() time.Duration { return time.Duration(s.CardIgnoreSeconds) * time.Second }

func (s Settings) DayAskAfter() time.Duration {
	return time.Duration(s.DayAskAfterSeconds) * time.Second
}
func (s Settings) DayReask() time.Duration { return time.Duration(s.DayReaskSeconds) * time.Second }

func (s Settings) Grace() time.Duration      { return time.Duration(s.GraceSeconds) * time.Second }
func (s Settings) Inactivity() time.Duration { return time.Duration(s.InactivitySeconds) * time.Second }
func (s Settings) EpisodeReset() time.Duration {
	return time.Duration(s.EpisodeResetSeconds) * time.Second
}
func (s Settings) Snooze() time.Duration { return time.Duration(s.SnoozeMinutes) * time.Minute }
func (s Settings) SegmentCreditCap() time.Duration {
	return time.Duration(s.SegmentCreditCapSeconds) * time.Second
}
