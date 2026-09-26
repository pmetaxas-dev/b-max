package domain

import "time"

// Observation is one snapshot of the browser, stamped by the server on receipt.
type Observation struct {
	Activity ActivityState
	Focused  bool
	TabID    int
	URL      string
	Title    string
	Page     Page
}

// AccountElapsed closes the interval since the previous event. It returns the
// segment to record (nil if none) and the focused-work milliseconds to credit.
// Time while Chrome is unfocused or the user is inactive is added to the grace exclusion (§8: PAUSED
// time does not count toward grace).
func (s *FocusSession) AccountElapsed(now time.Time, cfg Settings) (*BrowsingSegment, int64) {
	if s.LastEventAt.IsZero() {
		return nil, 0
	}
	dur := now.Sub(s.LastEventAt)
	if dur < 0 {
		dur = 0
	}
	if !s.Last.Focused || s.Last.Activity != ActivityActive {
		if !s.DistractionStartedAt.IsZero() {
			s.DistractionPausedMs += dur.Milliseconds()
		}
		return nil, 0
	}
	if s.Last.Kind != PageWeb {
		return nil, 0
	}
	credit := min(dur, cfg.SegmentCreditCap())
	seg := &BrowsingSegment{
		Domain: s.Last.Domain, PageKey: s.Last.PageKey,
		Start: s.LastEventAt, End: s.LastEventAt.Add(credit), DurationMs: credit.Milliseconds(),
	}
	var focused int64
	// A blacklisted page about the goal (a Python video on YouTube) is work
	// too: the metadata shield (MVP §4).
	if (!s.Last.Blacklisted && !s.Last.NotWork) || s.Last.Relevant {
		focused = credit.Milliseconds()
	}
	return seg, focused
}

// Credit is the interval since the previous event, capped like a segment.
func (s *FocusSession) Credit(now time.Time, cfg Settings) time.Duration {
	if s.LastEventAt.IsZero() {
		return 0
	}
	return max(0, min(now.Sub(s.LastEventAt), cfg.SegmentCreditCap()))
}

// Record stores what this event showed so the next event can account for it.
func (s *FocusSession) Record(o Observation, now time.Time) {
	s.LastEventAt = now
	s.Last = LastObservation{
		TabID: o.TabID, URL: o.URL,
		Activity: o.Activity,
		Focused:  o.Focused, Kind: o.Page.Kind, Blacklisted: o.Page.Blacklisted,
		Domain: o.Page.Domain, PageKey: o.Page.PageKey, Relevant: o.Page.Relevant, NotWork: o.Page.NotWork,
	}
}

// ContextRecent reports whether a work context exists: relevant activity within
// the inactivity timeout.
func (s *FocusSession) ContextRecent(now time.Time, cfg Settings) bool {
	return !s.LastRelevantActivityAt.IsZero() && now.Sub(s.LastRelevantActivityAt) < cfg.Inactivity()
}

// EpisodeElapsed is grace time consumed by the current distraction episode.
func (s *FocusSession) EpisodeElapsed(now time.Time) time.Duration {
	if s.DistractionStartedAt.IsZero() {
		return 0
	}
	d := now.Sub(s.DistractionStartedAt) - time.Duration(s.DistractionPausedMs)*time.Millisecond
	if d < 0 {
		return 0
	}
	return d
}

func (s *FocusSession) clearEpisode() {
	s.DistractionStartedAt = time.Time{}
	s.DistractionSite = ""
	s.DistractionPausedMs = 0
}

func (s *FocusSession) toIdle() {
	s.State = StateIdle
	s.clearEpisode()
	s.NonBlacklistedSince = time.Time{}
}

// Advance applies architecture-plan-v2 §5.2. There is no AI input anywhere in
// this function: the shield decides only whether a screen is shown, never the state.
func (s *FocusSession) Advance(o Observation, now time.Time, cfg Settings) {
	if s.State == "" {
		s.State = StateIdle
	}
	if s.ContextStrength == "" {
		s.ContextStrength = "weak"
	}
	// WORKING/DISTRACTED/PAUSED --> IDLE: inactivity timeout.
	if s.State != StateIdle && !s.ContextRecent(now, cfg) {
		s.toIdle()
	}

	if !o.Focused || o.Activity != ActivityActive {
		// WORKING/DISTRACTED --> PAUSED: Chrome unfocused or user inactive. An episode is kept
		// but the paused time is excluded from grace by AccountElapsed.
		s.NonBlacklistedSince = time.Time{}
		if s.State == StateWorking || s.State == StateDistracted {
			s.State = StatePaused
		}
		return
	}

	switch {
	case o.Page.Kind != PageWeb || o.Page.NotWork:
		// Neutral pages (chrome://, extension pages, a search not about the
		// goal) are neither work nor distraction. They change nothing except
		// ending a pause.
		if s.State == StatePaused {
			s.State = StateWorking
		}

	case !o.Page.Blacklisted:
		// Deterministic entry: onboarding complete (checked by the caller),
		// non-blacklisted tab, Chrome focused. Needs no network.
		s.LastRelevantActivityAt = now
		switch s.State {
		case StateIdle:
			s.State = StateWorking
			s.StartedAt = now
			s.SessionFocusedMs = 0
			s.ContextStrength = "weak"
		case StatePaused:
			s.State = StateWorking
		}
		if s.NonBlacklistedSince.IsZero() {
			s.NonBlacklistedSince = now
		}
		// DISTRACTED --> WORKING and episode reset: 60 continuous seconds.
		if now.Sub(s.NonBlacklistedSince) >= cfg.EpisodeReset() {
			s.clearEpisode()
			if s.State == StateDistracted {
				s.State = StateWorking
			}
		}

	default: // blacklisted
		s.NonBlacklistedSince = time.Time{}
		if s.State == StateIdle {
			return // no work context: stay silent, start no episode
		}
		if s.State == StatePaused {
			s.State = StateWorking
		}
		if s.DistractionStartedAt.IsZero() || s.DistractionSite != o.Page.Site {
			// New episode; a different site gets its own grace (§8).
			s.DistractionStartedAt = now
			s.DistractionSite = o.Page.Site
			s.DistractionPausedMs = 0
		}
		if s.EpisodeElapsed(now) >= cfg.Grace() {
			s.State = StateDistracted
		} else {
			s.State = StateWorking
		}
	}
}
