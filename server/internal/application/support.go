package application

import (
	"io"
	"slices"
	"time"

	"focuscompanion/internal/domain"
)

// Phase 4, "supporting value" (domain/support.go): the lamp, the weekend
// treasure, the voice, absence recovery, the difficult-day escalation and
// the day summary.

// now is the App Clock: real time, moved forward by the ⚙️ panel for demos
// (dev.go). Must be called with the mutex held.
func (a *App) now() time.Time {
	return a.clock.Now().AddDate(0, 0, a.st.ClockOffsetDays)
}

// --- lamp and treasure -------------------------------------------------------

type LampView struct {
	Unseen int    `json:"unseen"`
	Level  string `json:"level"` // off | medium | full
	Lang   string `json:"lang"`
}

// Lamp is GET /lamp: how bright the lamp is, from the ideas not seen yet.
func (a *App) Lamp() LampView {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := len(a.st.UnseenIdeas())
	return LampView{Unseen: n, Level: domain.LampLevel(n), Lang: a.st.Language()}
}

type ReviewInput struct {
	IDs []string `json:"ids"` // empty: every unseen idea
}

// ReviewIdeas is POST /ideas/review: the treasure was opened; the lamp dims.
func (a *App) ReviewIdeas(in ReviewInput) (LampView, error) {
	a.mu.Lock()
	for i := range a.st.Ideas {
		it := &a.st.Ideas[i]
		if it.Type == "idea" && (len(in.IDs) == 0 || slices.Contains(in.IDs, it.ID)) {
			it.Reviewed = true
		}
	}
	if c := a.st.OpenCard; c != nil && c.Command == domain.CmdShowTreasure {
		a.st.OpenCard = nil
	}
	a.persist()
	a.mu.Unlock()
	return a.Lamp(), nil
}

// --- voice ----------------------------------------------------------------------

// Voice is GET /voice: the user's own recording, from this computer only.
func (a *App) Voice() (io.ReadCloser, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.st.Goal == nil || a.st.Goal.VoiceRef == "" {
		return nil, "", ErrNoGoal
	}
	return a.repo.OpenVoice(a.st.Goal.VoiceRef)
}

// VoicePlayed is POST /voice/played: starts the cooldown before Max offers
// the recording again (the dashboard's own play button is always there).
func (a *App) VoicePlayed() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.st.VoicePlayedAt = a.now()
	a.persist()
}

// --- absence recovery -------------------------------------------------------

// noteActive runs on an active, focused web event. After AbsenceDays away
// the unfinished plan moves later by the time away and a welcome back waits.
func (a *App) noteActive(now time.Time) {
	st := a.st
	away := st.NoteActive(now)
	if away < domain.AbsenceDays {
		return
	}
	newDate := st.Reschedule(away)
	st.Recovery = &domain.Recovery{Date: domain.DayKey(now), NewDate: newDate}
	st.Pending = nil // an old "did you finish?" is not how to say hello
	st.OpenCard = nil
}

type RecoveryView struct {
	NewDate string    `json:"newDate,omitempty"`
	Step    *StepView `json:"step,omitempty"`
}

func (a *App) issueWelcomeBack(now time.Time, d *Decision) {
	st := a.st
	st.Recovery.Shown = true
	d.Command, d.Reason = domain.CmdWelcomeBack, "back_after_absence"
	a.recoveryPayload(now, d)
}

func (a *App) recoveryPayload(now time.Time, d *Decision) {
	st := a.st
	if st.Recovery == nil {
		return
	}
	d.Payload.Recovery = &RecoveryView{NewDate: st.Recovery.NewDate, Step: a.stepView(st.CurrentStep())}
	d.Payload.Voice = st.VoiceAllowed(now)
}

// --- difficult days ----------------------------------------------------------

type IdeaView struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	DaysAgo int    `json:"daysAgo"`
}

// decideOldIdea: on the third difficult day in a row, once, the user's own
// old idea instead of the plan. Never says why.
func (a *App) decideOldIdea(now time.Time, day *domain.DayRecord, d *Decision) bool {
	st := a.st
	if day.IdeaOffered || st.DifficultTier(day) < 2 {
		return false
	}
	idea := st.IdeaToOffer(now)
	if idea == nil {
		return false
	}
	day.IdeaOffered = true
	st.OfferedIdeas = append(st.OfferedIdeas, idea.ID)
	d.Command, d.Reason = domain.CmdOfferIdea, "difficult_days"
	a.ideaPayload(now, idea, d)
	return true
}

func (a *App) ideaPayload(now time.Time, idea *domain.Idea, d *Decision) {
	if idea == nil {
		return
	}
	d.Payload.Idea = &IdeaView{ID: idea.ID, Text: idea.Text, DaysAgo: domain.DaysBetween(domain.DayKey(idea.CreatedAt), domain.DayKey(now))}
	d.Payload.Voice = a.st.VoiceAllowed(now)
}

type IdeaAnswer struct {
	ID     string `json:"id"`
	Accept bool   `json:"accept"`
}

// AnswerOldIdea is POST /ideas/offer: "yes, that one today" breaks the zero
// of a difficult day; either answer closes the card.
func (a *App) AnswerOldIdea(in IdeaAnswer) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.st
	idea := st.IdeaByID(in.ID)
	if idea == nil || idea.Type != "idea" {
		return ErrInvalid
	}
	if in.Accept {
		st.Day(a.now()).IdeaAccepted = true
		idea.Reviewed = true
		if st.ChatFirst() {
			st.PromoteIdea(a.now(), idea.ID) // "yes, that one today": it becomes today's task
		}
	}
	if c := st.OpenCard; c != nil && c.Command == domain.CmdOfferIdea {
		st.OpenCard = nil
	}
	a.persist()
	return nil
}

// --- treasure ----------------------------------------------------------------

// treasureWindow: a session that has just started still counts as "between".
const treasureWindow = 2 * time.Minute

// decideTreasure: at the weekend, once, between work, never in it: on a
// break (a blacklisted site), or as a session starts, never on a goal page.
func (a *App) decideTreasure(now time.Time, obs domain.Observation, d *Decision) bool {
	st := a.st
	starting := st.Session.State != domain.StateWorking || now.Sub(st.Session.StartedAt) <= treasureWindow
	if !st.TreasureDue(now) || obs.Page.Relevant || !(obs.Page.Blacklisted || starting) {
		return false
	}
	st.TreasureWeek = domain.WeekKey(now)
	d.Command, d.Reason = domain.CmdShowTreasure, "weekend_treasure"
	d.Payload.Ideas = len(st.UnseenIdeas())
	return true
}

// --- day summary -------------------------------------------------------------

type DaySummary struct {
	Date            string `json:"date"`
	Mode            string `json:"mode"`
	ConfirmedSteps  int    `json:"confirmedSteps"`
	RelevantMinutes int    `json:"relevantMinutes"` // time on pages about the goal
	TasksDone       int    `json:"tasksDone"`
	IdeasSaved      int    `json:"ideasSaved"`
	Lang            string `json:"lang"`
}

// DaySummary is GET /summary/day: today in numbers, without judgement.
func (a *App) DaySummary() DaySummary {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.st
	now := a.now()
	today := domain.DayKey(now)
	s := DaySummary{Date: today, Mode: domain.DayUndecided, Lang: st.Language()}
	if d := st.Days[today]; d != nil {
		s.Mode, s.ConfirmedSteps, s.RelevantMinutes = d.Mode(), d.ConfirmedSteps, int(d.RelevantMs/60_000)
	}
	for _, it := range st.Ideas {
		if it.DoneAt != nil && domain.DayKey(*it.DoneAt) == today {
			s.TasksDone++
		}
		if domain.DayKey(it.CreatedAt) == today {
			s.IdeasSaved++
		}
	}
	return s
}
