package domain

import "time"

// OpenCard is the one unsolicited card that is currently open. The server,
// not the page, owns it: it follows the user to every page where it applies
// (a tab switch or a site that rewrites its URL no longer loses it) and
// closes only when answered, or after CardIgnore of ACTIVE use of Chrome
// while it was open. Time with Chrome unfocused, the user idle or the screen
// locked does not count, so a user who walks away finds it waiting.
type OpenCard struct {
	Command    Command   `json:"command"`
	DeliveryID string    `json:"deliveryId"`
	Site       string    `json:"site,omitempty"`   // SHOW_RAMP: the blacklisted site it is about
	TaskID     string    `json:"taskId,omitempty"` // SHOW_TASK: the task it is about
	IdeaID     string    `json:"ideaId,omitempty"` // SHOW_OLD_IDEA: the idea it offers
	Date       string    `json:"date"`             // ASK_DAY expires at midnight
	OpenedAt   time.Time `json:"openedAt"`
	ActiveMs   int64     `json:"activeMs"` // active, focused web time since it opened
	// Greeting: the day's first card says hello; there is no separate
	// greeting card, which would be an interruption with nothing to say.
	Greeting bool `json:"greeting,omitempty"`
	// Mode: how a SHOW_TASK card asks (tasklife.go: remind, checkin, resume, carry).
	Mode string `json:"mode,omitempty"`
	// OtherID: the task the user drifted to (refocus, tasklife.go).
	OtherID string `json:"otherId,omitempty"`
}

// PersistentCard reports whether a command opens a card that follows the user.
func PersistentCard(c Command) bool {
	switch c {
	case CmdShowRamp, CmdAskCompletion, CmdAskDay, CmdShowNewEra, CmdShowTask, CmdWelcomeBack, CmdShowTreasure, CmdOfferIdea:
		return true
	}
	return false
}

// Account credits the interval the previous observation covered.
func (c *OpenCard) Account(last LastObservation, credit time.Duration) {
	if credit > 0 && last.Focused && last.Activity == ActivityActive && last.Kind == PageWeb {
		c.ActiveMs += credit.Milliseconds()
	}
}

// Ignored: open for CardIgnore of active use without an answer.
func (c *OpenCard) Ignored(cfg Settings) bool {
	return c.ActiveMs >= cfg.CardIgnore().Milliseconds()
}

// AppliesTo reports whether the card is shown on this page: the return
// screen only on blacklisted sites, every other card on any web page.
func (c *OpenCard) AppliesTo(p Page) bool {
	if p.Kind != PageWeb {
		return false
	}
	if c.Command == CmdShowRamp {
		return p.Blacklisted
	}
	return true
}
