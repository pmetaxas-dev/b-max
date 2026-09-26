package domain

import (
	"slices"
	"time"
)

// The day's mode (MVP §8, "good day / bad day"):
//
//   - bad:       the user said "not today" (from the day question or the return
//     screen). No card appears for the rest of the day; relevant pages are still
//     counted and get a quiet thumbs-up. Only a new era may interrupt.
//   - good:      a page about the goal was seen today. Normal behaviour.
//   - undecided: neither yet. After DayAskAfter of active Chrome time the user
//     is asked about the day, and again every DayReask while still undecided.
const (
	DayBad       = "bad"
	DayGood      = "good"
	DayUndecided = "undecided"

	DayChoiceWont      = "wont_work"
	DayChoiceWill      = "will_work"
	DayChoiceNeedStart = "need_start"
)

const maxThumbed = 200 // keeps state.json bounded

func (d *DayRecord) Mode() string {
	switch {
	case d.NotToday:
		return DayBad
	case d.SawRelevant:
		return DayGood
	default:
		return DayUndecided
	}
}

// Account credits the interval the previous observation covered: active,
// focused web time counts toward the day question until a relevant page is
// seen, and relevant time is the day's score.
func (d *DayRecord) Account(last LastObservation, credit time.Duration) {
	if credit <= 0 || !last.Focused || last.Activity != ActivityActive || last.Kind != PageWeb {
		return
	}
	ms := credit.Milliseconds()
	d.WebMs += ms
	if last.Blacklisted {
		d.DistractedMs += ms
	}
	if last.Relevant {
		d.RelevantMs += ms
	}
	if !d.SawRelevant {
		d.ActiveMs += ms
	}
}

// Observe marks the day good once the user is looking at a relevant page.
func (d *DayRecord) Observe(o Observation) {
	if o.Focused && o.Activity == ActivityActive && o.Page.Relevant {
		d.SawRelevant = true
	}
}

// AskDue reports whether the day question should be asked now. Once asked it
// stays open as an OpenCard (card.go) until answered or ignored.
func (d *DayRecord) AskDue(cfg Settings) bool {
	if d.Mode() != DayUndecided {
		return false
	}
	threshold := d.NextAskMs
	if threshold == 0 {
		threshold = cfg.DayAskAfter().Milliseconds()
	}
	return d.ActiveMs >= threshold
}

// Reask schedules the next question DayReask of active time from now: after
// "I will", "I don't know where to start", or ignoring the question.
func (d *DayRecord) Reask(cfg Settings) {
	d.NextAskMs = d.ActiveMs + cfg.DayReask().Milliseconds()
}

// Thumb records a thumbs-up for pageKey and reports whether it is new today.
func (d *DayRecord) Thumb(pageKey string) bool {
	if pageKey == "" || slices.Contains(d.Thumbed, pageKey) {
		return false
	}
	d.Thumbed = append(d.Thumbed, pageKey)
	if len(d.Thumbed) > maxThumbed {
		d.Thumbed = d.Thumbed[len(d.Thumbed)-maxThumbed:]
	}
	return true
}

// EraToCelebrate is the new era the user has not been shown yet, or "".
func (s *State) EraToCelebrate() string {
	era := EraForPercent(s.Progress.Percentage)
	if EraIndex(era) > EraIndex(s.CelebratedEra) {
		return era
	}
	return ""
}

// EraIndex is the position of an era name, or -1.
func EraIndex(era string) int { return slices.Index(EraNames[:], era) }
