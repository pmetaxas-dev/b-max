package domain

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The life of a task (application/tasks_life.go): the user starts it, works on
// it, and Max checks in when the time they set has passed, brings them back
// when they drift to a distraction, and remembers where they left off, so
// "where was I today?" always has an answer.

const (
	TaskDoing  = "doing"  // started, not finished
	TaskPaused = "paused" // set aside with a note of where the user was

	MaxCheckInsPerDay = 3
	// Snoozes after the user's answer; a tenth of that in fast mode (demos).
	NotYetSnooze = 10 * time.Minute
	PauseSnooze  = 60 * time.Minute
	HelpSnooze   = 20 * time.Minute
	ResumeGap    = 30 * time.Minute // between two "back to where you were" cards
	// DefaultTaskMinutes: a task with no estimate is checked after this long.
	DefaultTaskMinutes = 30

	MaxJournal = 120

	// UrgentMargin: a task is urgent when its deadline is closer than the time
	// it takes plus this much.
	UrgentMargin = 20 * time.Minute
)

// Card modes for SHOW_TASK.
const (
	ModeRemind  = "remind"  // "don't forget" (tasks.go)
	ModeCheckIn = "checkin" // the time has passed: did you finish?
	ModeResume  = "resume"  // drifted away from a task in progress: back to it?
	ModeCarry   = "carry"   // left open from an earlier day
	ModeRefocus = "refocus" // working on another task than the one with priority
	ModeUrgent  = "urgent"  // another task's deadline is close: switch to it?
)

// JournalEntry is one line of what happened, for Max to answer "where was I".
type JournalEntry struct {
	At     time.Time `json:"at"`
	TaskID string    `json:"taskId,omitempty"`
	Text   string    `json:"text"`
}

// Log adds a line to the journal.
func (s *State) Log(now time.Time, taskID, text string) {
	s.Journal = append(s.Journal, JournalEntry{At: now, TaskID: taskID, Text: text})
	if n := len(s.Journal); n > MaxJournal {
		s.Journal = append([]JournalEntry{}, s.Journal[n-MaxJournal:]...)
	}
}

// RecentJournal is the last n entries as text lines, oldest first.
func (s *State) RecentJournal(n int) []string {
	from := max(0, len(s.Journal)-n)
	var out []string
	for _, e := range s.Journal[from:] {
		out = append(out, e.At.Format("2006-01-02 15:04")+" "+e.Text)
	}
	return out
}

// snooze scales a wait down in fast mode.
func (s Settings) snooze(d time.Duration) time.Duration {
	if s.Fast {
		return d / 10
	}
	return d
}

func (s Settings) NotYetWait() time.Duration  { return s.snooze(NotYetSnooze) }
func (s Settings) PauseWait() time.Duration   { return s.snooze(PauseSnooze) }
func (s Settings) HelpWait() time.Duration    { return s.snooze(HelpSnooze) }
func (s Settings) ResumeEvery() time.Duration { return s.snooze(ResumeGap) }

// DoingTask is the open task the user is working on, or nil.
func (s *State) DoingTask() *Idea {
	for i := range s.Ideas {
		if s.Ideas[i].OpenTask() && s.Ideas[i].Status == TaskDoing {
			return &s.Ideas[i]
		}
	}
	return nil
}

// StartTask makes id the task in progress; any other one is set aside.
func (s *State) StartTask(now time.Time, id string) *Idea {
	t := s.IdeaByID(id)
	if t == nil || !t.OpenTask() {
		return nil
	}
	if cur := s.DoingTask(); cur != nil && cur.ID != id {
		s.PauseTask(now, cur.ID, "")
	}
	if t.Status != TaskDoing {
		t.Status, t.StartedAt, t.WorkedMs, t.CheckAfter = TaskDoing, &now, 0, time.Time{}
		est := t.EstimateMin
		if est == 0 {
			est = DefaultTaskMinutes
		}
		s.Log(now, t.ID, "Started “"+t.Text+"” (estimate "+strconv.Itoa(est)+" min)")
	}
	return t
}

// PauseTask sets a task aside and remembers where the user was: the note they
// gave, else the last page about it.
func (s *State) PauseTask(now time.Time, id, note string) *Idea {
	t := s.IdeaByID(id)
	if t == nil || !t.OpenTask() {
		return nil
	}
	if note = strings.TrimSpace(note); note != "" {
		t.Note = note
	} else if t.LastTitle != "" {
		t.Note = "on the page “" + t.LastTitle + "”"
	}
	t.Status, t.CheckAfter = TaskPaused, now.Add(s.Settings.PauseWait())
	line := "Set aside “" + t.Text + "”"
	if t.Note != "" {
		line += "; left off " + t.Note
	}
	s.Log(now, t.ID, line)
	return t
}

// NeedsEstimate: an open task the user has not said how long or by when.
func (i Idea) NeedsEstimate() bool {
	return i.OpenTask() && i.EstimateMin == 0 && i.Deadline == ""
}

// Elapsed is how long the task has been in progress.
func (i Idea) Elapsed(now time.Time) time.Duration {
	if i.Status != TaskDoing || i.StartedAt == nil {
		return 0
	}
	return now.Sub(*i.StartedAt)
}

// Planned is how long the user said the task takes (a default if they did not).
func (i Idea) Planned(cfg Settings) time.Duration {
	m := i.EstimateMin
	if m == 0 {
		m = DefaultTaskMinutes
	}
	return cfg.snooze(time.Duration(m) * time.Minute)
}

var (
	clockColon = regexp.MustCompile(`(\d{1,2})[:.](\d{2})`)
	clockAmPm  = regexp.MustCompile(`(\d{1,2})\s*(am|pm)\b`)
	clockAt    = regexp.MustCompile(`(?:στις|στην|at|by|until|before|μέχρι τις|μέχρι|πριν τις|πριν|έως τις|έως)\s*(\d{1,2})\b`)
	afternoon  = []string{"απόγευμα", "απογευμα", "afternoon", "evening", "βράδυ", "βραδυ", "tonight", "απόψε", "αποψε"}
	pmWord     = regexp.MustCompile(`pm`)
	amWord     = regexp.MustCompile(`am`)
	anyNumber  = regexp.MustCompile(`(d{1,2})`)
	morning    = []string{"πρωί", "πρωι", "morning"}
)

func anyOf(text string, words []string) bool {
	for _, w := range words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// ParseClock reads a time of day from the user's own words ("17:00", "5pm",
// "μέχρι τις 5", "5 το απόγευμα", "tonight"). false when there is none.
func ParseClock(text string) (hour, minute int, ok bool) {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return 0, 0, false
	}
	pm, am := anyOf(t, afternoon) || pmWord.MatchString(t), anyOf(t, morning) || amWord.MatchString(t)
	num := func(s string) int { n, _ := strconv.Atoi(s); return n }
	switch {
	case clockColon.MatchString(t):
		m := clockColon.FindStringSubmatch(t)
		hour, minute = num(m[1]), num(m[2])
		if pm && hour < 12 {
			hour += 12
		}
	case clockAmPm.MatchString(t):
		m := clockAmPm.FindStringSubmatch(t)
		hour = num(m[1]) % 12
		if m[2] == "pm" {
			hour += 12
		}
	case clockAt.MatchString(t) || (anyNumber.MatchString(t) && (pm || am)):
		digits := anyNumber.FindStringSubmatch(t)
		if m := clockAt.FindStringSubmatch(t); m != nil {
			digits = m
		}
		hour = num(digits[1])
		switch {
		case pm && hour < 12:
			hour += 12
		case !am && !pm && hour >= 1 && hour <= 7:
			hour += 12 // "until 5" is five in the afternoon
		}
	case strings.Contains(t, "tonight") || strings.Contains(t, "απόψε") || strings.Contains(t, "αποψε"):
		hour = 21
	case strings.Contains(t, "evening") || strings.Contains(t, "βράδυ") || strings.Contains(t, "βραδυ"):
		hour = 20
	case strings.Contains(t, "afternoon") || strings.Contains(t, "απόγευμα") || strings.Contains(t, "απογευμα"):
		hour = 17
	case strings.Contains(t, "noon") || strings.Contains(t, "μεσημέρι") || strings.Contains(t, "μεσημερι"):
		hour = 14
	case am:
		hour = 11
	default:
		return 0, 0, false
	}
	if hour > 23 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

// DeadlineAt is when the task is due today (or on its own day), if the user
// gave a time of day.
func (i Idea) DeadlineAt(now time.Time) (time.Time, bool) {
	h, m, ok := ParseClock(i.Deadline)
	if !ok {
		return time.Time{}, false
	}
	day := now
	if i.Due != "" {
		if d, err := time.ParseInLocation("2006-01-02", i.Due, now.Location()); err == nil {
			day = d
		}
	}
	return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, now.Location()), true
}

// TaskToCheckIn is the task Max should ask about now, and why: "checkin"
// (its time has passed) or "carry" (left open from an earlier day). At most
// MaxCheckInsPerDay questions a day per task, never inside a snooze.
func (s *State) TaskToCheckIn(now time.Time) (*Idea, string) {
	today := DayKey(now)
	for _, c := range s.OpenTasksByPriority(now) {
		t := s.IdeaByID(c.ID)
		if t == nil || t.CheckAfter.After(now) {
			continue
		}
		if t.CheckDate == today && t.CheckCount >= MaxCheckInsPerDay {
			continue
		}
		// Left open from an earlier day: recap it first (the clock restarts when
		// the user answers), then it is like any other task of today.
		if t.Due != "" && t.Due < today && t.CarryAsked != today {
			return t, ModeCarry
		}
		if t.Status == TaskDoing && t.Elapsed(now) >= t.Planned(s.Settings) {
			return t, ModeCheckIn
		}
		if at, ok := t.DeadlineAt(now); ok && !now.Before(at) && t.DeadlineAsked != today+"@"+at.Format("15:04") && t.Due <= today {
			return t, ModeCheckIn
		}
	}
	return nil, ""
}

// ChatFirst: the user has a life goal and no step plan, so tasks are what the
// app tracks (chat.go).
func (s *State) ChatFirst() bool { return s.Goal == nil && s.LifeGoal != "" }

// EnsureCurrentTask makes the first open task by priority the one in progress,
// when nothing is: there is no start button, Max simply tracks the task that
// comes first. A task the user set aside stays aside until its snooze ends.
func (s *State) EnsureCurrentTask(now time.Time) {
	if !s.ChatFirst() || s.DoingTask() != nil {
		return
	}
	for _, c := range s.OpenTasksByPriority(now) {
		if t := s.IdeaByID(c.ID); t != nil && !t.CheckAfter.After(now) {
			s.StartTask(now, t.ID)
			return
		}
	}
}

// TaskToRefocus: the user is on a page about another task while the one with
// priority is in progress. Returns the priority task and the other one; nil
// when there is nothing to say or Max already said it lately.
func (s *State) TaskToRefocus(now time.Time, page Page, url, title string) (current, other *Idea) {
	cur := s.DoingTask()
	if cur == nil || cur.CheckAfter.After(now) || (!cur.ResumeAt.IsZero() && now.Sub(cur.ResumeAt) < s.Settings.ResumeEvery()) {
		return nil, nil
	}
	if len(cur.Keywords) > 0 && TaskPageRelevant(cur, page, url, title) {
		return nil, nil // the page is about the priority task too
	}
	for i := range s.Ideas {
		o := &s.Ideas[i]
		if o.ID != cur.ID && o.OpenTask() && len(o.Keywords) > 0 && TaskPageRelevant(o, page, url, title) {
			return cur, o
		}
	}
	return nil, nil
}

// DeadlineUrgent: the task has a deadline today that is close: less time is
// left than the task takes (plus a margin), or it has already passed.
func (i Idea) DeadlineUrgent(now time.Time) bool {
	if !i.OpenTask() {
		return false
	}
	at, ok := i.DeadlineAt(now)
	if !ok || DayKey(at) != DayKey(now) {
		return false
	}
	est := time.Duration(i.EstimateMin) * time.Minute
	if i.EstimateMin == 0 {
		est = DefaultTaskMinutes * time.Minute
	}
	return !now.Before(at.Add(-est - UrgentMargin))
}

// TaskToPrioritize: another task's deadline is close while the user works on a
// task that is not urgent. Returns the urgent one and the one in progress.
// Asked once a day per task.
func (s *State) TaskToPrioritize(now time.Time) (urgent, current *Idea) {
	cur := s.DoingTask()
	if cur == nil || cur.DeadlineUrgent(now) {
		return nil, nil
	}
	today := DayKey(now)
	for _, c := range s.OpenTasksByPriority(now) {
		o := s.IdeaByID(c.ID)
		if o != nil && o.ID != cur.ID && o.DeadlineUrgent(now) && o.UrgentAsked != today && !o.CheckAfter.After(now) {
			return o, cur
		}
	}
	return nil, nil
}

// TaskToResume is the task in progress, when the user drifted to a
// distraction and it has been a while since they were last brought back.
func (s *State) TaskToResume(now time.Time) *Idea {
	t := s.DoingTask()
	if t == nil || t.CheckAfter.After(now) || (!t.ResumeAt.IsZero() && now.Sub(t.ResumeAt) < s.Settings.ResumeEvery()) {
		return nil
	}
	return t
}

// TaskPageRelevant: a page counts as work on a browser task when it matches
// the task's keywords; a browser task with none counts any ordinary page.
func TaskPageRelevant(t *Idea, page Page, url, text string) bool {
	if t == nil || t.Kind != "browser" || page.Kind != PageWeb || page.Blacklisted {
		return false
	}
	if len(t.Keywords) > 0 {
		kws := t.Keywords
		if IsSearchResults(url) {
			// A search page is about what was searched, never about the search
			// engine: "google.com" as a keyword must not make every search count.
			kws = slices.DeleteFunc(slices.Clone(kws), isDomainKeyword)
		}
		return PageRelevant(url, text, kws)
	}
	return !page.NotWork && !IsSearchResults(url)
}
