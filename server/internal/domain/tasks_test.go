package domain

import (
	"testing"
	"time"
)

var nine = time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local)

func TestTaskDueTodayUnlessItSaysTomorrow(t *testing.T) {
	if got := TaskDue("send an email today", nine); got != "2026-09-18" {
		t.Fatalf("today's task due %s", got)
	}
	for _, text := range []string{"call mum tomorrow", "Να στείλω email αύριο"} {
		if got := TaskDue(text, nine); got != "2026-09-19" {
			t.Fatalf("%q due %s", text, got)
		}
	}
}

func TestTaskScoreRisesWithDeadlineLatenessAndGoal(t *testing.T) {
	kw := []string{"youtube", "channel"}
	plain := Idea{Type: "task", Text: "buy milk", Due: "2026-09-18"}
	email := Idea{Type: "task", Text: "send an email to the sponsor", Due: "2026-09-18"}
	goal := Idea{Type: "task", Text: "send the channel trailer", Due: "2026-09-18"}
	later := Idea{Type: "task", Text: "buy milk", Due: "2026-09-19"}
	if s := TaskScore(plain, nine, kw); s != 2 {
		t.Fatalf("plain today: %d", s)
	}
	if s := TaskScore(email, nine, kw); s != 3 {
		t.Fatalf("deadline word: %d", s)
	}
	if s := TaskScore(goal, nine, kw); s != 4 {
		t.Fatalf("deadline word + goal: %d", s)
	}
	if s := TaskScore(later, nine, kw); s != 0 {
		t.Fatalf("tomorrow's task: %d", s)
	}
	evening := nine.Add(9 * time.Hour)
	if s := TaskScore(email, evening, kw); s != 4 || !TaskUrgent(email, evening, kw) {
		t.Fatalf("an open task gets urgent late in the day: %d", s)
	}
	if TaskUrgent(email, nine, kw) {
		t.Fatal("not urgent in the morning")
	}
	if s := TaskScore(plain, nine.AddDate(0, 0, 1), kw); s != 4 {
		t.Fatalf("overdue: %d", s)
	}
	done := nine
	plain.DoneAt = &done
	if TaskScore(plain, nine, kw) != 0 || plain.OpenTask() {
		t.Fatal("a done task has no priority")
	}
}

func TestTaskBonusIsSmallAndCapped(t *testing.T) {
	s := &State{Milestones: []Milestone{{Weight: 1}}}
	done := nine
	for range 3 {
		s.Ideas = append(s.Ideas, Idea{Type: "task", DoneAt: &done}, Idea{Type: "idea"}, Idea{Type: "task"})
	}
	s.RecomputeProgress()
	if s.Progress.Percentage != 1.5 {
		t.Fatalf("3 done tasks: %v%%", s.Progress.Percentage)
	}
	for range 30 {
		s.Ideas = append(s.Ideas, Idea{Type: "task", DoneAt: &done})
	}
	s.RecomputeProgress()
	if s.Progress.Percentage != TaskBonusCapPercent {
		t.Fatalf("capped at %v%%, got %v%%", TaskBonusCapPercent, s.Progress.Percentage)
	}
}

func TestTaskToRemindPacesAndStops(t *testing.T) {
	s := &State{Settings: DefaultSettings()}
	s.Ideas = []Idea{{ID: "a", Type: "task", Text: "buy milk", Due: "2026-09-18"}}
	day := &DayRecord{Date: "2026-09-18"}
	s.Ideas[0].Nudged(day, false) // captured at 0 min
	min := int64(60_000)
	day.WebMs = 9 * min
	if s.TaskToRemind(nine, day, true, false) != nil {
		t.Fatal("too soon on a distraction site")
	}
	day.WebMs = 10 * min
	if s.TaskToRemind(nine, day, false, false) != nil {
		t.Fatal("work pages wait the longer interval")
	}
	for n := 1; n <= MaxTaskNudges; n++ {
		got := s.TaskToRemind(nine, day, true, false)
		if got == nil {
			t.Fatalf("reminder %d missing", n)
		}
		got.Nudged(day, true)
		day.WebMs += 10 * min
	}
	if s.TaskToRemind(nine, day, true, false) != nil {
		t.Fatal("more than MaxTaskNudges reminders in a day")
	}
	// A new day starts over.
	tomorrow := &DayRecord{Date: "2026-09-19", WebMs: 10 * min}
	if s.TaskToRemind(nine.AddDate(0, 0, 1), tomorrow, true, false) == nil {
		t.Fatal("an overdue task is reminded again the next day")
	}
}

// "Sleep before 00:00" is the end of today. It used to be read as the midnight
// that had already passed, which made it urgent and put it first all day.
func TestMidnightDeadlineIsTheEndOfTheDay(t *testing.T) {
	sleep := Idea{Type: "task", Text: "Sleep", Deadline: "00:00", EstimateMin: 60, Due: "2026-09-18"}
	evening := time.Date(2026, 9, 18, 20, 0, 0, 0, time.Local)
	if at, ok := sleep.DeadlineAt(evening); !ok || !at.Equal(time.Date(2026, 9, 19, 0, 0, 0, 0, time.Local)) {
		t.Fatalf("deadline at %v %v, want the next midnight", at, ok)
	}
	if sleep.DeadlineUrgent(evening) {
		t.Fatal("sleep at 20:00 with a midnight deadline is not urgent yet")
	}
	if late := time.Date(2026, 9, 18, 23, 30, 0, 0, time.Local); !sleep.DeadlineUrgent(late) {
		t.Fatal("sleep at 23:30 is urgent")
	}
}
