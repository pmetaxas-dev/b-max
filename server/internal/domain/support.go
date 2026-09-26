package domain

import (
	"fmt"
	"slices"
	"time"
)

// Phase 4, "supporting value" (architecture-plan-v2 §11, MVP §3, §7, §8, §9):
// the lamp's brightness, the weekend treasure, absence recovery, the
// difficult-day escalation and the voice's cooldown. All deterministic.

const (
	// AbsenceDays of no activity make the next visit a recovery (§11).
	AbsenceDays = 5
	// VoiceCooldown: the user's own recording is offered at most this often.
	VoiceCooldown = 5 * 24 * time.Hour
	// LampFull: more unseen ideas than this and the lamp is at full brightness.
	LampFull = 10
	// OldIdeaAge: an idea offered on a difficult day was written at least
	// this long ago ("you wrote this ten days ago").
	OldIdeaAge = 2 * 24 * time.Hour
)

const (
	CmdWelcomeBack  Command = "SHOW_WELCOME_BACK" // after AbsenceDays away: new date and one small step
	CmdShowTreasure Command = "SHOW_TREASURE"     // weekend: "you collected N ideas this week"
	CmdOfferIdea    Command = "SHOW_OLD_IDEA"     // 3rd difficult day: the user's own old idea
)

// Recovery is the welcome back after an absence. It never counts the days
// or says what was missed: only the new date and the next step (§11).
type Recovery struct {
	Date    string `json:"date"`              // the day the user came back
	NewDate string `json:"newDate,omitempty"` // the replanned last scheduled day
	Shown   bool   `json:"shown,omitempty"`
}

// --- lamp and treasure -----------------------------------------------------------

// UnseenIdeas are ideas (not tasks) the user has not reviewed in the treasure.
func (s *State) UnseenIdeas() []Idea {
	var out []Idea
	for _, i := range s.Ideas {
		if i.Type == "idea" && !i.Reviewed {
			out = append(out, i)
		}
	}
	return out
}

// LampLevel is the lamp's brightness for n unseen ideas (MVP §3): off with
// none, medium up to LampFull, full beyond.
func LampLevel(n int) string {
	switch {
	case n <= 0:
		return "off"
	case n <= LampFull:
		return "medium"
	default:
		return "full"
	}
}

// WeekKey identifies the ISO week of t, e.g. "2026-W38".
func WeekKey(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// TreasureDue: at the weekend, once a week, when there are unseen ideas.
func (s *State) TreasureDue(now time.Time) bool {
	wd := now.Weekday()
	return (wd == time.Saturday || wd == time.Sunday) && s.TreasureWeek != WeekKey(now) && len(s.UnseenIdeas()) > 0
}

// --- absence recovery -------------------------------------------------------------

// DaysBetween is the number of calendar days from day a to day b (YYYY-MM-DD).
func DaysBetween(a, b string) int {
	ta, err1 := time.Parse("2006-01-02", a)
	tb, err2 := time.Parse("2006-01-02", b)
	if err1 != nil || err2 != nil {
		return 0
	}
	return int(tb.Sub(ta).Hours() / 24)
}

// NoteActive records that the user is active today and returns how many
// days they were away (0 if they were here yesterday or today).
func (s *State) NoteActive(now time.Time) int {
	today := DayKey(now)
	last := s.LastActiveDay
	s.LastActiveDay = today
	if last == "" || last >= today {
		return 0
	}
	return DaysBetween(last, today)
}

// Reschedule moves every pending scheduled step `days` later, keeping their
// spacing, and returns the latest pending scheduled day ("" if none).
func (s *State) Reschedule(days int) string {
	latest := ""
	for i := range s.Steps {
		st := &s.Steps[i]
		if st.Status == StatusDone || st.ScheduledFor == "" {
			continue
		}
		if t, err := time.Parse("2006-01-02", st.ScheduledFor); err == nil {
			st.ScheduledFor = t.AddDate(0, 0, days).Format("2006-01-02")
		}
		latest = max(latest, st.ScheduledFor)
	}
	return latest
}

// VoiceAllowed: a recording exists and it was not offered within VoiceCooldown.
func (s *State) VoiceAllowed(now time.Time) bool {
	return s.Goal != nil && s.Goal.VoiceRef != "" && (s.VoicePlayedAt.IsZero() || now.Sub(s.VoicePlayedAt) >= VoiceCooldown)
}

// --- difficult days ----------------------------------------------------------------

// Difficult: a day the user was here and confirmed no step (§11). Accepting
// their own old idea breaks the zero too.
func (d *DayRecord) Difficult() bool { return d.ConfirmedSteps == 0 && !d.IdeaAccepted }

// DifficultStreakBefore counts the consecutive difficult days before today.
// Days without any activity are skipped (a weekend away is not a bad day),
// up to AbsenceDays back; longer absences are a recovery instead.
func (s *State) DifficultStreakBefore(today string) int {
	t, err := time.Parse("2006-01-02", today)
	if err != nil {
		return 0
	}
	n := 0
	for back := 1; back <= AbsenceDays; back++ {
		d := s.Days[t.AddDate(0, 0, -back).Format("2006-01-02")]
		if d == nil {
			continue
		}
		if !d.Difficult() {
			break
		}
		n++
	}
	return n
}

// DifficultTier is today's escalation (§11): 0 nothing (even after one bad
// day), 1 the step becomes a door, 2 the user's own old idea is offered.
// Once a step is confirmed today, it is 0 again. It is never announced.
func (s *State) DifficultTier(day *DayRecord) int {
	if !day.Difficult() {
		return 0
	}
	switch n := s.DifficultStreakBefore(day.Date); {
	case n >= 2:
		return 2
	case n == 1:
		return 1
	}
	return 0
}

// DoorText turns a step into a door, not a task: motion, not output.
func DoorText(step, lang string) string {
	if lang == LangGreek {
		return "Σήμερα μόνο αυτό: άνοιξε ό,τι χρειάζεσαι για «" + step + "». Τίποτα άλλο. 2 λεπτά."
	}
	return "Today, just this: open what you need for “" + step + "”. Nothing else. 2 minutes."
}

// IdeaToOffer is the user's oldest unseen idea written at least OldIdeaAge
// ago, or any old idea if all were seen; nil if there is none.
func (s *State) IdeaToOffer(now time.Time) *Idea {
	var best *Idea
	for i := range s.Ideas {
		it := &s.Ideas[i]
		if it.Type != "idea" || now.Sub(it.CreatedAt) < OldIdeaAge || slices.Contains(s.OfferedIdeas, it.ID) {
			continue
		}
		if best == nil || (!it.Reviewed && best.Reviewed) || (it.Reviewed == best.Reviewed && it.CreatedAt.Before(best.CreatedAt)) {
			best = it
		}
	}
	return best
}
