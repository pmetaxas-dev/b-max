package application

import (
	"time"

	"focuscompanion/internal/domain"
)

// The open card (domain/card.go): opened when a persistent card is issued,
// re-sent on every page it applies to, closed by an answer, by being ignored
// for CardIgnore of active use, or when it no longer makes sense.

func (a *App) openCard(now time.Time, day *domain.DayRecord, d *Decision) {
	if d.Payload.DeliveryID == "" {
		d.Payload.DeliveryID = domain.NewID("delivery")
	}
	d.Payload.Greeting = !day.Greeted
	day.Greeted = true
	a.st.OpenCard = &domain.OpenCard{
		Command: d.Command, DeliveryID: d.Payload.DeliveryID, Site: d.Payload.Site,
		Date: day.Date, OpenedAt: now, Greeting: d.Payload.Greeting,
	}
	if d.Payload.Task != nil {
		a.st.OpenCard.TaskID = d.Payload.Task.ID
		a.st.OpenCard.Mode = d.Payload.TaskMode
		if d.Payload.Other != nil {
			a.st.OpenCard.OtherID = d.Payload.Other.ID
		}
	}
	if d.Payload.Idea != nil {
		a.st.OpenCard.IdeaID = d.Payload.Idea.ID
	}
}

// followCard re-sends the open card, same delivery id, so the page shows it
// (or keeps showing it) without a new entrance.
func (a *App) followCard(d *Decision) {
	st := a.st
	c := st.OpenCard
	d.Command, d.Reason = c.Command, "card_open"
	d.Payload.DeliveryID = c.DeliveryID
	d.Payload.Greeting = c.Greeting // the same card, the same words
	switch c.Command {
	case domain.CmdShowRamp:
		d.Payload.Site = c.Site
		if step := st.CurrentStep(); step != nil {
			d.Payload.Step = a.stepView(step)
			d.Payload.Anchor = a.anchorView(step.ID)
		}
	case domain.CmdAskCompletion:
		if st.Pending != nil {
			d.Payload.Step = a.stepView(st.StepByID(st.Pending.StepID))
			d.Payload.Gentle = st.Pending.Asks > 1
		}
	case domain.CmdAskDay:
		d.Payload.Step = a.stepView(st.CurrentStep())
	case domain.CmdShowNewEra:
		a.eraPayload(d)
	case domain.CmdShowTask:
		d.Payload.Task = a.taskView(st.IdeaByID(c.TaskID))
		d.Payload.TaskMode = c.Mode
		d.Payload.Other = a.taskView(st.IdeaByID(c.OtherID))
	case domain.CmdWelcomeBack:
		a.recoveryPayload(a.now(), d)
	case domain.CmdShowTreasure:
		d.Payload.Ideas = len(st.UnseenIdeas())
	case domain.CmdOfferIdea:
		a.ideaPayload(a.now(), st.IdeaByID(c.IdeaID), d)
	}
}

// expireCard closes the open card if it was ignored or no longer makes
// sense on this web page, and returns its delivery id ("" if still open).
func (a *App) expireCard(now time.Time, obs domain.Observation, day *domain.DayRecord) string {
	st := a.st
	c := st.OpenCard
	if c == nil {
		return ""
	}
	if c.Ignored(st.Settings) {
		return a.closeCard(now, true)
	}
	// Yesterday's card never follows into today; only a new era waits.
	if c.Date != day.Date && c.Command != domain.CmdShowNewEra {
		return a.closeCard(now, false)
	}
	switch c.Command {
	case domain.CmdShowRamp:
		if !obs.Page.Blacklisted {
			// Back on a work page: what the return screen asked for. Counted
			// as shown, like an ignored screen, so it is not shown again today.
			return a.closeCard(now, true)
		}
	case domain.CmdAskCompletion:
		if st.Pending == nil {
			return a.closeCard(now, false)
		}
	case domain.CmdAskDay:
		if day.Mode() != domain.DayUndecided {
			return a.closeCard(now, false) // a goal page, or "not today"
		}
	case domain.CmdShowTask:
		if t := st.IdeaByID(c.TaskID); t == nil || !t.OpenTask() {
			return a.closeCard(now, false) // done from the dashboard
		}
		if c.Mode == domain.ModeResume && !obs.Page.Blacklisted {
			return a.closeCard(now, false) // already back at work
		}
		if c.Mode == domain.ModeUrgent {
			if cur := st.DoingTask(); cur != nil && cur.ID == c.TaskID {
				return a.closeCard(now, false) // the user switched to it
			}
		}
		if c.Mode == domain.ModeRefocus {
			if cur := st.DoingTask(); cur == nil || cur.ID != c.TaskID {
				return a.closeCard(now, false) // the priority changed
			}
		}
		return "" // the user's own errand: "not today" was about the goal
	case domain.CmdShowTreasure:
		if len(st.UnseenIdeas()) == 0 {
			return a.closeCard(now, false) // opened from the dashboard
		}
	case domain.CmdOfferIdea:
		if st.IdeaByID(c.IdeaID) == nil {
			return a.closeCard(now, false)
		}
	}
	// A bad day silences every card but the era announcement.
	if day.NotToday && c.Command != domain.CmdShowNewEra {
		return a.closeCard(now, false)
	}
	return "" // still open, and it applies to this web page
}

// closeCard closes the open card. ignored applies what an unanswered card
// means: the return screen counts as shown today, the completion prompt is
// snoozed like "not yet", the day question is asked again after DayReask.
func (a *App) closeCard(now time.Time, ignored bool) string {
	st := a.st
	c := st.OpenCard
	st.OpenCard = nil
	if c == nil {
		return ""
	}
	if ignored {
		switch c.Command {
		case domain.CmdShowRamp:
			if rec := st.LatestIntervention(c.Date, c.Site); rec != nil && rec.ShownAt.IsZero() {
				rec.ShownAt, rec.Status = now, "shown"
			}
		case domain.CmdAskCompletion:
			st.SetPromptAside() // like "not yet"
		case domain.CmdAskDay:
			st.Day(now).Reask(st.Settings)
		}
	}
	return c.DeliveryID
}

// closeOpen closes the open card of kind cmd after the user answered it.
func (a *App) closeOpen(cmd domain.Command) {
	if c := a.st.OpenCard; c != nil && c.Command == cmd {
		a.st.OpenCard = nil
	}
}

// continueTargets fills where "Continue" may take the user, in the order the
// extension tries them (actions.js): something about the current step, never
// just the last page that was open. 1) today's latest page about the goal
// (its tab if still there), 2) today's return anchor if its page is about the
// goal, 3) a search for the step. Yesterday's pages, searches like "reddit"
// and unrelated work pages are never offered.
func (a *App) continueTargets(now time.Time, res *RampResult) {
	st := a.st
	today := domain.DayKey(now)
	if wc := st.WorkContext; wc.LastRelevantURL != "" && domain.DayKey(wc.LastRelevantAt) == today {
		res.LastWorkTabID, res.LastWorkURL = wc.LastRelevantTabID, wc.LastRelevantURL
	}
	step := st.CurrentStep()
	if step == nil {
		return
	}
	if an, ok := st.Anchors[step.ID]; ok && domain.DayKey(an.CapturedAt) == today && domain.PageRelevant(an.URL, an.Title, st.GoalKeywords()) {
		res.AnchorURL = an.URL
	}
	if q := st.SearchQueries(step); len(q) > 0 {
		res.SearchQuery = q[0]
	}
}

// CardInput is POST /card/close: the era card's buttons ("done") and Escape
// on any card ("ignored", the keyboard's way to set a card aside).
type CardInput struct {
	DeliveryID string `json:"deliveryId"`
	Reason     string `json:"reason"` // "done" | "ignored"
}

func (a *App) CloseCard(in CardInput) error {
	if in.Reason != "done" && in.Reason != "ignored" {
		return ErrInvalid
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if c := a.st.OpenCard; c == nil || c.DeliveryID != in.DeliveryID {
		return ErrNoPending
	}
	a.closeCard(a.now(), in.Reason == "ignored")
	a.persist()
	return nil
}
