package domain

import "time"

// The planet's weather follows the day. A bad day, one with a long stretch on
// distraction sites and nothing finished, brings clouds and lightning. It is a
// mood, never a punishment: the planet's progress does not move, and the sky
// clears as soon as one task is finished.

const (
	// StormAfter: this much time on distraction sites, with no task finished today.
	StormAfter = 30 * time.Minute
	// StormEveningHour: from this hour, some time online and nothing finished
	// is enough.
	StormEveningHour = 18
	StormEveningWeb  = 20 * time.Minute
)

func (s Settings) StormWait() time.Duration { return s.snooze(StormAfter) }

// BadDay: nothing finished today and either a long time on distraction sites,
// or the evening has come with tasks still open. A day the user chose to
// rest ("not today") is never a bad day.
func (s *State) BadDay(now time.Time) bool {
	day := s.Days[DayKey(now)]
	if day == nil || day.NotToday || s.DoneToday(now) > 0 || day.ConfirmedSteps > 0 {
		return false
	}
	if day.DistractedMs >= s.Settings.StormWait().Milliseconds() {
		return true
	}
	return now.Hour() >= StormEveningHour && day.WebMs >= s.Settings.snooze(StormEveningWeb).Milliseconds() && len(s.OpenTasksByPriority(now)) > 0
}

// WeatherAt is the planet's weather now: clear, or a storm on a bad day, or
// a storm on demand (ForceStorm, ⚙️ dev.go, for presentations).
func (s *State) WeatherAt(now time.Time) string {
	if s.ForceStorm || s.BadDay(now) {
		return WeatherStorm
	}
	return WeatherClear
}
