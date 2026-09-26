package domain

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local)

type driver struct {
	s   FocusSession
	cfg Settings
	bl  []BlacklistEntry
	now time.Time
}

func newDriver() *driver {
	return &driver{s: FocusSession{State: StateIdle}, cfg: DefaultSettings(), bl: DefaultBlacklist(), now: t0}
}

// at moves the clock forward and applies one browser snapshot, exactly as the
// application layer does.
func (d *driver) at(after time.Duration, url string, focused bool) SessionState {
	d.now = d.now.Add(after)
	obs := Observation{Activity: ActivityActive, Focused: focused, URL: url, Page: ClassifyPage(url, d.bl)}
	d.s.AccountElapsed(d.now, d.cfg)
	d.s.Advance(obs, d.now, d.cfg)
	d.s.Record(obs, d.now)
	return d.s.State
}

const (
	work = "https://docs.google.com/document/d/1"
	yt   = "https://www.youtube.com/watch?v=1"
	rd   = "https://www.reddit.com/r/all"
)

func want(t *testing.T, got, exp SessionState, msg string) {
	t.Helper()
	if got != exp {
		t.Fatalf("%s: got %s want %s", msg, got, exp)
	}
}

func TestWorkContextNeedsNoNetwork(t *testing.T) {
	d := newDriver()
	// §5.2: onboarding done + non-blacklisted tab + Chrome focused => WORKING.
	want(t, d.at(0, work, true), StateWorking, "first work tab")
	if d.s.ContextStrength != "weak" {
		t.Fatalf("strength %q", d.s.ContextStrength)
	}
}

func TestBlacklistedTabWithoutWorkContextStaysIdle(t *testing.T) {
	d := newDriver()
	want(t, d.at(0, yt, true), StateIdle, "youtube first")
	want(t, d.at(10*time.Minute, yt, true), StateIdle, "still idle")
	if !d.s.DistractionStartedAt.IsZero() {
		t.Fatal("an episode started without a work context")
	}
}

func TestGraceThenDistracted(t *testing.T) {
	d := newDriver()
	d.at(0, work, true)
	want(t, d.at(time.Minute, yt, true), StateWorking, "entering blacklist keeps WORKING")
	if d.s.DistractionStartedAt.IsZero() {
		t.Fatal("distractionStartedAt not set")
	}
	want(t, d.at(4*time.Minute, yt, true), StateWorking, "inside grace")
	want(t, d.at(time.Minute, yt, true), StateDistracted, "grace elapsed")
}

func TestShortReturnDoesNotResetGrace(t *testing.T) {
	d := newDriver()
	d.at(0, work, true)
	d.at(time.Minute, yt, true)
	d.at(4*time.Minute+30*time.Second, yt, true) // 4m30s of grace used
	d.at(20*time.Second, work, true)             // shorter than 60s
	want(t, d.at(20*time.Second, yt, true), StateDistracted, "grace not reset by a short return")
}

func TestSixtySecondsOnWorkPageResets(t *testing.T) {
	d := newDriver()
	d.at(0, work, true)
	d.at(time.Minute, yt, true)
	want(t, d.at(5*time.Minute, yt, true), StateDistracted, "distracted")
	want(t, d.at(10*time.Second, work, true), StateDistracted, "before 60s still DISTRACTED")
	want(t, d.at(61*time.Second, work, true), StateWorking, "60s on a work page")
	want(t, d.at(10*time.Second, yt, true), StateWorking, "fresh episode, fresh grace")
}

func TestNewSiteIsNewEpisode(t *testing.T) {
	d := newDriver()
	d.at(0, work, true)
	d.at(time.Minute, yt, true)
	want(t, d.at(5*time.Minute, yt, true), StateDistracted, "youtube distracted")
	want(t, d.at(10*time.Second, rd, true), StateWorking, "reddit gets its own grace")
	want(t, d.at(5*time.Minute, rd, true), StateDistracted, "reddit grace elapsed")
}

func TestPausedTimeDoesNotCountTowardGrace(t *testing.T) {
	d := newDriver()
	d.at(0, work, true)
	d.at(time.Minute, yt, true) // episode starts at 1m
	want(t, d.at(2*time.Minute, yt, false), StatePaused, "Chrome unfocused at 3m")
	d.at(4*time.Minute, yt, false) // 4 minutes paused, still inside the inactivity timeout
	want(t, d.at(0, yt, true), StateWorking, "refocused at 7m; only 2m of grace used")
	want(t, d.at(2*time.Minute, yt, true), StateWorking, "9m: 4m used, the 4m pause is excluded")
	want(t, d.at(2*time.Minute, yt, true), StateDistracted, "11m: 6m used, past grace")
}

func TestInactivityTimeout(t *testing.T) {
	d := newDriver()
	d.at(0, work, true)
	want(t, d.at(time.Minute, work, false), StatePaused, "blur")
	want(t, d.at(20*time.Minute, work, false), StateIdle, "PAUSED -> IDLE after 15 minutes")
	// A blacklisted page now has no work context.
	want(t, d.at(time.Second, yt, true), StateIdle, "no context after timeout")
}

func TestWorkingToIdleWhenEventsStop(t *testing.T) {
	d := newDriver()
	d.at(0, work, true)
	want(t, d.at(16*time.Minute, "chrome://newtab/", true), StateIdle, "WORKING -> IDLE")
}

func TestDistractedToIdleAfterInactivity(t *testing.T) {
	d := newDriver()
	d.at(0, work, true)
	d.at(time.Minute, yt, true)
	want(t, d.at(5*time.Minute, yt, true), StateDistracted, "distracted")
	want(t, d.at(12*time.Minute, yt, true), StateIdle, "DISTRACTED -> IDLE")
}

func TestNeutralPageChangesNothing(t *testing.T) {
	d := newDriver()
	d.at(0, work, true)
	want(t, d.at(time.Minute, "chrome://newtab/", true), StateWorking, "neutral keeps WORKING")
	if !d.s.NonBlacklistedSince.IsZero() && d.s.NonBlacklistedSince != t0 {
		t.Fatal("neutral page disturbed the 60s clock")
	}
}

func TestInactiveIntervalsNeverEarnFocusedTime(t *testing.T) {
	for _, activity := range []ActivityState{ActivityIdle, ActivityLocked, ""} {
		t.Run(string(activity), func(t *testing.T) {
			d := newDriver()
			d.at(0, work, true)
			lastRelevant := d.s.LastRelevantActivityAt
			obs := Observation{Activity: activity, Focused: true, URL: work, Page: ClassifyPage(work, d.bl)}
			d.s.Advance(obs, d.now, d.cfg)
			d.s.Record(obs, d.now)
			for i := 0; i < 20; i++ {
				d.now = d.now.Add(time.Minute)
				seg, credit := d.s.AccountElapsed(d.now, d.cfg)
				if seg != nil || credit != 0 {
					t.Fatalf("inactive interval credited: %+v %d", seg, credit)
				}
				d.s.Advance(obs, d.now, d.cfg)
				d.s.Record(obs, d.now)
				if d.s.LastRelevantActivityAt != lastRelevant {
					t.Fatal("inactive heartbeat refreshed work activity")
				}
			}
			want(t, d.s.State, StateIdle, "inactive work page must expire")
			obs.Activity = ActivityActive
			d.s.Advance(obs, d.now, d.cfg)
			d.s.Record(obs, d.now)
			_, credit := d.s.AccountElapsed(d.now.Add(time.Minute), d.cfg)
			if credit != 60_000 {
				t.Fatalf("active work did not resume: %d", credit)
			}
		})
	}
}

func TestActiveSegmentCreditRemainsCapped(t *testing.T) {
	d := newDriver()
	d.at(0, work, true)
	seg, credit := d.s.AccountElapsed(d.now.Add(3*time.Hour), d.cfg)
	if credit != 180_000 || seg.DurationMs != 180_000 {
		t.Fatalf("lost 180-second cap: %+v %d", seg, credit)
	}
}

func TestIdleAndLockedPauseGrace(t *testing.T) {
	for _, activity := range []ActivityState{ActivityIdle, ActivityLocked} {
		t.Run(string(activity), func(t *testing.T) {
			d := newDriver()
			d.at(0, work, true)
			d.at(time.Minute, yt, true)
			obs := Observation{Activity: activity, Focused: true, URL: yt, Page: ClassifyPage(yt, d.bl)}
			d.s.Advance(obs, d.now, d.cfg)
			d.s.Record(obs, d.now)
			d.at(6*time.Minute, yt, true)
			if got := d.s.EpisodeElapsed(d.now); got != 0 {
				t.Fatalf("inactive grace counted: %v", got)
			}
			want(t, d.s.State, StateWorking, "resumed with unused grace")
		})
	}
}
