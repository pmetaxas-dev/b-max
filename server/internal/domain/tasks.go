package domain

import (
	"slices"
	"strings"
	"time"
)

// Tasks are the user's own small to-dos, captured with Max ("send the email
// today"). They are not Steps: they are not planned by the AI and do not move
// the planet much. Each done task adds TaskBonusPercent, at most
// TaskBonusCapPercent in total, so a day of chores never replaces the goal.
//
// A task is for the day it was captured on (or tomorrow if the text says so).
// While it is open, Max reminds the user of it (SHOW_TASK): after
// TaskRemind of active web time since the last reminder, sooner
// (TaskDistractionRemind) when the user is on a blacklisted site, at most
// MaxTaskNudges times a day per task. Which task is reminded first is decided
// here, deterministically, by TaskScore.
const (
	TaskBonusPercent    = 0.5
	TaskBonusCapPercent = 10.0
	MaxTaskNudges       = 3
	// LateHour: from this local hour a task still open today is urgent.
	LateHour = 17
)

var tomorrowWords = []string{"tomorrow", "αύριο", "αυριο"}

// urgentWords make a task more pressing: a deadline, or something someone
// else is waiting for.
var urgentWords = []string{
	"today", "tonight", "urgent", "asap", "deadline", "now", "before",
	"email", "e-mail", "mail", "call", "reply", "send", "pay", "submit", "book",
	"σήμερα", "σημερα", "απόψε", "αποψε", "επείγον", "επειγον", "προθεσμία", "προθεσμια",
	"στείλε", "στειλε", "στείλω", "στειλω", "πλήρωσε", "πληρωσε", "τηλεφώνησε", "τηλεφωνησε", "απάντησε", "απαντησε",
}

func (s Settings) TaskRemind() time.Duration {
	return time.Duration(s.TaskRemindSeconds) * time.Second
}
func (s Settings) TaskDistractionRemind() time.Duration {
	return time.Duration(s.TaskDistractionRemindSeconds) * time.Second
}

func words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 0x370 && r <= 0x3ff || r >= 0x1f00 && r <= 0x1fff)
	})
}

func hasAny(ws []string, list []string) bool {
	for _, w := range ws {
		if slices.Contains(list, w) {
			return true
		}
	}
	return false
}

// TaskDue is the day a task captured at now is for.
func TaskDue(text string, now time.Time) string {
	if hasAny(words(text), tomorrowWords) {
		return DayKey(now.AddDate(0, 0, 1))
	}
	return DayKey(now)
}

// OpenTask reports whether i is a task not yet done.
func (i Idea) OpenTask() bool { return i.Type == "task" && i.DoneAt == nil }

// Overdue: an open task for an earlier day.
func (i Idea) Overdue(now time.Time) bool {
	return i.OpenTask() && i.Due != "" && i.Due < DayKey(now)
}

// DueBy: an open task for today or earlier (a task without a day is for today).
func (i Idea) DueBy(now time.Time) bool {
	return i.OpenTask() && (i.Due == "" || i.Due <= DayKey(now))
}

// TaskScore orders open tasks: overdue first, then today's, rising as the
// day gets late; a deadline word or a link to the goal adds one.
func TaskScore(i Idea, now time.Time, keywords []string) int {
	if !i.OpenTask() {
		return 0
	}
	score := 0
	switch {
	case i.Overdue(now):
		score = 4
	case i.DueBy(now):
		score = 2
		if now.Hour() >= LateHour {
			score++
		}
	}
	if hasAny(words(i.Text), urgentWords) {
		score++
	}
	if textRelevant(i.Text, keywords) {
		score++
	}
	return score
}

// textRelevant: the text names a (non-domain) goal keyword as whole words.
func textRelevant(text string, keywords []string) bool {
	hay := " " + normalizeText(text) + " "
	for _, k := range keywords {
		if !isDomainKeyword(k) && strings.Contains(hay, " "+k+" ") {
			return true
		}
	}
	return false
}

// Urgent: late today, overdue, or pressing enough to interrupt goal work.
func TaskUrgent(i Idea, now time.Time, keywords []string) bool {
	return i.DueBy(now) && TaskScore(i, now, keywords) >= 4
}

// OpenTasksByPriority returns the open tasks, most pressing first (older
// first on a tie). It never includes tasks for a later day at the top.
func (s *State) OpenTasksByPriority(now time.Time) []Idea {
	var out []Idea
	for _, i := range s.Ideas {
		if i.OpenTask() {
			out = append(out, i)
		}
	}
	kw := s.GoalKeywords()
	slices.SortStableFunc(out, func(a, b Idea) int {
		// The AI's ranking (chat.go) comes first; a task it has not ranked yet
		// goes after the ranked ones, ordered by the deterministic score.
		// The task in progress is always first: it is what the user is doing.
		if da, db := a.Status == TaskDoing, b.Status == TaskDoing; da != db {
			if da {
				return -1
			}
			return 1
		}
		// A deadline that is close comes before the AI's ranking, which only
		// changes when the user talks to Max: time passes by itself.
		if ua, ub := a.DeadlineUrgent(now), b.DeadlineUrgent(now); ua != ub {
			if ua {
				return -1
			}
			return 1
		}
		if ra, rb := rankKey(a), rankKey(b); ra != rb {
			return ra - rb
		}
		if d := TaskScore(b, now, kw) - TaskScore(a, now, kw); d != 0 {
			return d
		}
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	return out
}

// IdeaByID returns the parked item with id, or nil.
func (s *State) IdeaByID(id string) *Idea {
	for i := range s.Ideas {
		if s.Ideas[i].ID == id {
			return &s.Ideas[i]
		}
	}
	return nil
}

// TaskToRemind is the task Max should remind the user of now, or nil.
// onDistraction: the user is on a blacklisted site, so the shorter interval
// applies. onGoal: the user is working on the goal; only an urgent task
// interrupts that.
func (s *State) TaskToRemind(now time.Time, day *DayRecord, onDistraction, onGoal bool) *Idea {
	every := s.Settings.TaskRemind()
	if onDistraction {
		every = s.Settings.TaskDistractionRemind()
	}
	kw := s.GoalKeywords()
	for _, t := range s.OpenTasksByPriority(now) {
		if !t.DueBy(now) || t.Status == TaskDoing || t.CheckAfter.After(now) {
			continue // in progress: the user is on it; snoozed: they asked for quiet
		}
		if onGoal && !TaskUrgent(t, now, kw) {
			continue
		}
		since := day.WebMs
		nudges := 0
		if t.NudgeDate == day.Date {
			since -= t.NudgeAtMs
			nudges = t.Nudges
		}
		if nudges < MaxTaskNudges && since >= every.Milliseconds() {
			return s.IdeaByID(t.ID)
		}
	}
	return nil
}

// Nudged records a reminder (or the capture) at the day's current web time.
func (i *Idea) Nudged(day *DayRecord, counts bool) {
	if i.NudgeDate != day.Date {
		i.NudgeDate, i.Nudges = day.Date, 0
	}
	i.NudgeAtMs = day.WebMs
	if counts {
		i.Nudges++
	}
}

// TaskBonus is the progress, in percent, the done tasks add.
func (s *State) TaskBonus() float64 {
	n := 0
	for _, i := range s.Ideas {
		if i.Type == "task" && i.DoneAt != nil {
			n++
		}
	}
	return min(float64(n)*TaskBonusPercent, TaskBonusCapPercent)
}

func rankKey(i Idea) int {
	if i.Rank <= 0 {
		return 1 << 20
	}
	return i.Rank
}

func (s *State) HasOpenTask(text string) bool {
	for _, i := range s.Ideas {
		if i.OpenTask() && strings.EqualFold(strings.TrimSpace(i.Text), strings.TrimSpace(text)) {
			return true
		}
	}
	return false
}

func (s *State) RemoveIdea(id string) {
	s.Ideas = slices.DeleteFunc(s.Ideas, func(i Idea) bool { return i.ID == id })
}
