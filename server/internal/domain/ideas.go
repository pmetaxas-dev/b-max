package domain

import (
	"slices"
	"time"
)

// The idea chest: everything the user throws in that is not a task for today.
// Max brings ideas back when the user is not getting anywhere (a long time on
// distraction sites and nothing finished) and at the weekend (support.go).

const (
	// IdeaAfter: this much time on distraction sites with no task finished today
	// makes Max offer one of the user's own ideas (a tenth of it in fast mode).
	IdeaAfter = 15 * time.Minute
	// IdeaMinAge: an idea has to be at least this old to be offered, so what
	// the user just said is not thrown back at them.
	IdeaMinAge = 10 * time.Minute
)

func (s Settings) IdeaWait() time.Duration { return s.snooze(IdeaAfter) }

// DoneToday is the number of tasks finished today.
func (s *State) DoneToday(now time.Time) int {
	n := 0
	for _, i := range s.Ideas {
		if i.Type == "task" && i.DoneAt != nil && DayKey(*i.DoneAt) == DayKey(now) {
			n++
		}
	}
	return n
}

// IdeaToRemind is the user's oldest idea that was not offered yet, unseen
// ones first; nil if there is none.
func (s *State) IdeaToRemind(now time.Time) *Idea {
	var best *Idea
	for i := range s.Ideas {
		it := &s.Ideas[i]
		if it.Type != "idea" || now.Sub(it.CreatedAt) < IdeaMinAge || slices.Contains(s.OfferedIdeas, it.ID) {
			continue
		}
		if best == nil || (!it.Reviewed && best.Reviewed) || (it.Reviewed == best.Reviewed && it.CreatedAt.Before(best.CreatedAt)) {
			best = it
		}
	}
	return best
}

// PromoteIdea turns an idea into a task for today.
func (s *State) PromoteIdea(now time.Time, id string) *Idea {
	it := s.IdeaByID(id)
	if it == nil || it.Type != "idea" {
		return nil
	}
	it.Type, it.Reviewed, it.Due, it.DoneAt = "task", true, DayKey(now), nil
	it.Priority = TaskScore(*it, now, s.GoalKeywords())
	s.Log(now, it.ID, "Moved the idea “"+it.Text+"” to today's tasks")
	s.EnsureCurrentTask(now)
	return it
}
