package application

import (
	"time"

	"focuscompanion/internal/domain"
)

func newDelivery(now time.Time, obs domain.Observation) domain.Delivery {
	return domain.Delivery{ID: domain.NewID("delivery"), TabID: obs.TabID, URL: obs.URL, IssuedAt: now}
}

// DeliveryAck identifies the issued command and the page that actually rendered it.
// The worker supplies tabId from Chrome's message sender, not from page input.
type DeliveryAck struct {
	DeliveryID string `json:"deliveryId"`
	TabID      int    `json:"tabId"`
	URL        string `json:"url"`
}

func (a *App) AcknowledgeDelivery(in DeliveryAck) error {
	if in.DeliveryID == "" || in.TabID < 0 || in.URL == "" {
		return ErrInvalid
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	day := a.st.Day(now)
	matches := func(d *domain.Delivery) bool {
		return d != nil && d.ID == in.DeliveryID && d.TabID == in.TabID && d.URL == in.URL
	}
	last := a.st.Session.Last
	current := last.Focused && last.Activity == domain.ActivityActive && last.TabID == in.TabID && last.URL == in.URL
	for i := range a.st.Interventions {
		rec := &a.st.Interventions[i]
		if rec.Date != day.Date || !matches(&rec.Delivery) {
			continue
		}
		if !rec.ShownAt.IsZero() {
			return nil
		}
		if !current || a.silenceReason(day, rec.Site) != "" {
			return ErrNoPending
		}
		rec.ShownAt, rec.Status = now, "shown"
		a.persist()
		return nil
	}
	// Other open cards (day question, completion prompt, era) consume nothing
	// a stale page could steal: they stay open whether or not a page
	// acknowledged them (card.go), so their acknowledgement is just accepted.
	// The return screen keeps its strict rules above; a copy of it that
	// followed the user to another page acknowledges nothing new once the
	// screen already counts as shown.
	if c := a.st.OpenCard; c != nil && c.DeliveryID == in.DeliveryID {
		if c.Command != domain.CmdShowRamp {
			return nil
		}
		if rec := a.st.LatestIntervention(day.Date, c.Site); rec != nil && !rec.ShownAt.IsZero() {
			return nil
		}
	}
	return ErrNoPending // superseded, unknown, or from a previous day
}
