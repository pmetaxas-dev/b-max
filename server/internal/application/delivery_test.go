package application_test

import (
	"errors"
	"testing"
	"time"

	"focuscompanion/internal/application"
	"focuscompanion/internal/domain"
)

func TestRampNotConsumedBeforeAck(t *testing.T) {
	r := newRig(t)
	r.onboard()
	r.ev(0, domain.EvTabActivated, work, true)
	r.ev(time.Minute, domain.EvTabActivated, yt, true)
	first := r.evUnacknowledged(5*time.Minute, domain.EvHeartbeat, yt, true)
	expect(t, first, domain.CmdShowRamp, "")
	st, _ := r.repo.Load()
	if st.InterventionShown(domain.DayKey(r.clk.t), "youtube.com") || st.Interventions[0].Status != "issued" {
		t.Fatal("issuing consumed eligibility")
	}
	// Not yet rendered: the open card is re-sent, same id, never a second card.
	second := r.evUnacknowledged(time.Minute, domain.EvHeartbeat, yt, true)
	expect(t, second, domain.CmdShowRamp, "card_open")
	if first.Payload.DeliveryID != second.Payload.DeliveryID {
		t.Fatal("the open card was re-issued as a new card")
	}
	if err := r.app.AcknowledgeDelivery(application.DeliveryAck{DeliveryID: "delivery_unknown", TabID: 7, URL: yt}); !errors.Is(err, application.ErrNoPending) {
		t.Fatalf("unknown ack accepted: %v", err)
	}
	r.ack(second, yt)
	r.ack(second, yt) // duplicate is idempotent
	st, _ = r.repo.Load()
	if len(st.Interventions) != 1 || st.Interventions[0].Status != "shown" || st.Interventions[0].ShownAt.IsZero() {
		t.Fatal("ack not persisted as shown")
	}
	// Answered: closed, and not shown again today.
	if _, err := r.app.RespondRamp(application.RampInput{Site: "youtube.com", Choice: domain.ChoiceContinue}); err != nil {
		t.Fatal(err)
	}
	expect(t, r.evUnacknowledged(time.Minute, domain.EvHeartbeat, yt, true), domain.CmdDoNothing, "already_shown_today")
}

// The return screen counts as shown only when acknowledged from the page and
// day it was issued for. (This used the greeting before there was none.)
func TestAcknowledgementRequiresIssuedContextAndCurrentDay(t *testing.T) {
	for _, change := range []string{"tab", "url", "navigation", "midnight"} {
		t.Run(change, func(t *testing.T) {
			r := newRig(t)
			r.onboard()
			r.ev(0, domain.EvTabActivated, work, true)
			r.ev(time.Minute, domain.EvTabActivated, yt, true)
			d := r.evUnacknowledged(6*time.Minute, domain.EvHeartbeat, yt, true)
			expect(t, d, domain.CmdShowRamp, "")
			ack := application.DeliveryAck{DeliveryID: d.Payload.DeliveryID, TabID: 7, URL: yt}
			switch change {
			case "tab":
				ack.TabID = 8
			case "url":
				ack.URL = rd
			case "navigation":
				r.evUnacknowledged(time.Second, domain.EvTabUpdated, rd, true)
			case "midnight":
				r.clk.add(24 * time.Hour)
			}
			if err := r.app.AcknowledgeDelivery(ack); !errors.Is(err, application.ErrNoPending) {
				t.Fatalf("stale ack accepted: %v", err)
			}
			st, _ := r.repo.Load()
			if st.InterventionShown(st.Interventions[0].Date, "youtube.com") {
				t.Fatal("a stale acknowledgement consumed the day's return screen")
			}
		})
	}
}

func TestIdleAndLockedDoNotCreditStepOrRefreshWork(t *testing.T) {
	for _, activity := range []domain.ActivityState{domain.ActivityIdle, domain.ActivityLocked, ""} {
		t.Run(string(activity), func(t *testing.T) {
			r := newRig(t)
			r.onboard()
			r.ev(0, domain.EvTabActivated, work, true)
			for i := 0; i < 12; i++ {
				d, err := r.app.HandleEvent(application.Event{Type: domain.EvActivityChanged, TabID: 7, URL: work, Focused: true, Activity: activity})
				if err != nil {
					t.Fatal(err)
				}
				expect(t, d, domain.CmdDoNothing, "user_inactive")
				r.clk.add(time.Minute)
			}
			st, _ := r.repo.Load()
			if st.WorkContext.FocusedMsOnStep != 0 || st.Session.SessionFocusedMs != 0 || st.Pending != nil {
				t.Fatal("inactive time reached step accounting")
			}
			if !st.Session.LastRelevantActivityAt.Equal(r.clk.t.Add(-12 * time.Minute)) {
				t.Fatal("inactive event refreshed work")
			}
		})
	}
}
