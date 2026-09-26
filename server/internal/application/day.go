package application

import "focuscompanion/internal/domain"

// DayInput is POST /day/respond: the answer to the day question
// ("no work on your goals today?"), or undo of "not today".
type DayInput struct {
	Choice string `json:"choice"`
}

type DayResult struct {
	Mode string `json:"mode"`
	// NeedStart: the current step and three search phrases. The extension
	// builds the search links; nothing here is a URL.
	Step          *StepView `json:"step,omitempty"`
	SearchQueries []string  `json:"searchQueries,omitempty"`
	// DismissUnsolicited: "not today" closes every open card, like the
	// return screen's "not today".
	DismissUnsolicited *DismissUnsolicited `json:"dismissUnsolicited,omitempty"`
}

func (a *App) RespondDay(in DayInput) (DayResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.st.Goal == nil {
		return DayResult{}, ErrNoGoal
	}
	day := a.st.Day(a.now())
	cfg := a.st.Settings
	res := DayResult{}
	if in.Choice != domain.ChoiceUndo {
		a.closeOpen(domain.CmdAskDay)
	}
	if c := a.st.OpenCard; in.Choice == domain.DayChoiceWont && c != nil && c.Command != domain.CmdShowNewEra {
		a.st.OpenCard = nil // "not today" closes every card but a new era
	}
	switch in.Choice {
	case domain.DayChoiceWont:
		// A bad day: no card for the rest of the day, only quiet tracking
		// and thumbs-ups; a new era still shows (domain/day.go).
		day.NotToday, day.DayAnswer = true, in.Choice
		res.DismissUnsolicited = &DismissUnsolicited{Commands: []domain.Command{domain.CmdShowRamp, domain.CmdAskCompletion, domain.CmdAskDay}}
	case domain.DayChoiceWill:
		day.DayAnswer = in.Choice
		day.Reask(cfg)
	case domain.DayChoiceNeedStart:
		day.DayAnswer = in.Choice
		day.Reask(cfg)
		step := a.st.CurrentStep()
		res.Step = a.stepView(step)
		res.SearchQueries = a.st.SearchQueries(step)
	case domain.ChoiceUndo:
		if day.DayAnswer != domain.DayChoiceWont {
			return DayResult{}, ErrNoPending
		}
		day.DayAnswer = ""
		// A "not today" from the return screen still stands.
		day.NotToday = false
		for _, r := range a.st.Interventions {
			if r.Date == day.Date && r.Choice == domain.ChoiceNotToday {
				day.NotToday = true
			}
		}
		day.Reask(cfg)
	default:
		return DayResult{}, ErrInvalid
	}
	res.Mode = day.Mode()
	a.persist()
	return res, nil
}
