package domain

import (
	"testing"
	"time"
)

func activeWeb(relevant bool) LastObservation {
	return LastObservation{Focused: true, Activity: ActivityActive, Kind: PageWeb, Relevant: relevant}
}

func TestDayModes(t *testing.T) {
	d := &DayRecord{}
	if d.Mode() != DayUndecided {
		t.Fatalf("new day: %s", d.Mode())
	}
	d.Observe(Observation{Focused: true, Activity: ActivityActive, Page: Page{Kind: PageWeb, Relevant: true}})
	if d.Mode() != DayGood {
		t.Fatalf("after a relevant page: %s", d.Mode())
	}
	d.NotToday = true
	if d.Mode() != DayBad {
		t.Fatalf("not today wins: %s", d.Mode())
	}
	// An unfocused or idle relevant page does not make the day good.
	d2 := &DayRecord{}
	d2.Observe(Observation{Focused: false, Activity: ActivityActive, Page: Page{Kind: PageWeb, Relevant: true}})
	d2.Observe(Observation{Focused: true, Activity: ActivityIdle, Page: Page{Kind: PageWeb, Relevant: true}})
	if d2.Mode() != DayUndecided {
		t.Fatalf("unattended relevant page counted: %s", d2.Mode())
	}
}

func TestDayAccountingOnlyActiveFocusedWeb(t *testing.T) {
	d := &DayRecord{}
	d.Account(activeWeb(false), time.Minute)
	d.Account(activeWeb(true), 2*time.Minute)
	d.Account(LastObservation{Focused: false, Activity: ActivityActive, Kind: PageWeb, Relevant: true}, time.Hour) // Chrome unfocused
	d.Account(LastObservation{Focused: true, Activity: ActivityIdle, Kind: PageWeb}, time.Hour)                    // user away
	d.Account(LastObservation{Focused: true, Activity: ActivityActive, Kind: PageNeutral}, time.Hour)              // new tab page
	if d.ActiveMs != 3*60_000 || d.RelevantMs != 2*60_000 {
		t.Fatalf("active %d relevant %d", d.ActiveMs, d.RelevantMs)
	}
	d.SawRelevant = true
	d.Account(activeWeb(true), time.Minute)
	if d.ActiveMs != 3*60_000 || d.RelevantMs != 3*60_000 {
		t.Fatalf("after good: active %d relevant %d", d.ActiveMs, d.RelevantMs)
	}
}

func TestDayAskTiming(t *testing.T) {
	cfg := DefaultSettings()
	d := &DayRecord{}
	d.ActiveMs = cfg.DayAskAfter().Milliseconds() - 1
	if d.AskDue(cfg) {
		t.Fatal("asked before 30 minutes")
	}
	d.ActiveMs++
	if !d.AskDue(cfg) {
		t.Fatal("not asked at 30 minutes")
	}
	d.Reask(cfg)
	if d.AskDue(cfg) {
		t.Fatal("asked again right after an answer")
	}
	d.ActiveMs += cfg.DayReask().Milliseconds()
	if !d.AskDue(cfg) {
		t.Fatal("not asked again after another hour")
	}
	for _, mode := range []func(*DayRecord){func(d *DayRecord) { d.NotToday = true }, func(d *DayRecord) { d.SawRelevant = true }} {
		x := *d
		mode(&x)
		if x.AskDue(cfg) {
			t.Fatalf("asked on a decided day: %s", x.Mode())
		}
	}
}

func TestOpenCardCountsOnlyActiveUse(t *testing.T) {
	cfg := DefaultSettings()
	c := &OpenCard{Command: CmdAskDay}
	c.Account(LastObservation{Focused: false, Activity: ActivityActive, Kind: PageWeb}, time.Hour) // Chrome in the background
	c.Account(LastObservation{Focused: true, Activity: ActivityIdle, Kind: PageWeb}, time.Hour)    // user away
	c.Account(LastObservation{Focused: true, Activity: ActivityLocked, Kind: PageWeb}, time.Hour)  // screen locked
	if c.Ignored(cfg) || c.ActiveMs != 0 {
		t.Fatalf("away time counted: %d", c.ActiveMs)
	}
	c.Account(activeWeb(false), 4*time.Minute)
	if c.Ignored(cfg) {
		t.Fatal("ignored before 5 active minutes")
	}
	c.Account(activeWeb(false), time.Minute)
	if !c.Ignored(cfg) {
		t.Fatal("not ignored after 5 active minutes")
	}
}

func TestOpenCardAppliesTo(t *testing.T) {
	web, black, neutral := Page{Kind: PageWeb}, Page{Kind: PageWeb, Blacklisted: true}, Page{Kind: PageNeutral}
	ramp := &OpenCard{Command: CmdShowRamp}
	if ramp.AppliesTo(web) || !ramp.AppliesTo(black) || ramp.AppliesTo(neutral) {
		t.Fatal("the return screen shows on blacklisted sites only")
	}
	for _, cmd := range []Command{CmdAskCompletion, CmdAskDay, CmdShowNewEra} {
		c := &OpenCard{Command: cmd}
		if !c.AppliesTo(web) || !c.AppliesTo(black) || c.AppliesTo(neutral) {
			t.Fatalf("%s must follow to every web page", cmd)
		}
	}
	if PersistentCard(CmdShowThumbsUp) || PersistentCard(CmdCloseCard) || !PersistentCard(CmdAskDay) {
		t.Fatal("persistent card set")
	}
}

func TestThumbOncePerPage(t *testing.T) {
	d := &DayRecord{}
	if !d.Thumb("docs.python.org/3/") || d.Thumb("docs.python.org/3/") || d.Thumb("") {
		t.Fatal("thumb must be once per page, never for an empty key")
	}
	for i := range maxThumbed + 10 {
		d.Thumb(string(rune('a'+i%26)) + string(rune(i)))
	}
	if len(d.Thumbed) != maxThumbed {
		t.Fatalf("thumbed list unbounded: %d", len(d.Thumbed))
	}
}

func TestEraToCelebrate(t *testing.T) {
	s := &State{}
	s.Normalize()
	if s.EraToCelebrate() != "" || s.CelebratedEra != "prehistoric" {
		t.Fatalf("fresh state celebrates %q (celebrated %q)", s.EraToCelebrate(), s.CelebratedEra)
	}
	s.Progress.Percentage = 50
	if s.EraToCelebrate() != "medieval" {
		t.Fatalf("got %q", s.EraToCelebrate())
	}
	s.CelebratedEra = "medieval"
	if s.EraToCelebrate() != "" {
		t.Fatal("celebrated twice")
	}
	// An older state file already at 70% never announces eras it passed.
	old := &State{Progress: ProgressState{Percentage: 70}}
	old.Normalize()
	if old.EraToCelebrate() != "" {
		t.Fatalf("old state announces %q", old.EraToCelebrate())
	}
}

func TestNewSettingsGetDefaultsInOldStateFiles(t *testing.T) {
	s := &State{Settings: Settings{GraceSeconds: 300, InactivitySeconds: 900}}
	s.Normalize()
	if s.Settings.DayAskAfterSeconds != 1800 || s.Settings.DayReaskSeconds != 3600 {
		t.Fatalf("got %+v", s.Settings)
	}
	s.Settings.DayAskAfterSeconds = 60
	s.Normalize()
	if s.Settings.DayAskAfterSeconds != 60 {
		t.Fatal("a set value was overwritten")
	}
}
