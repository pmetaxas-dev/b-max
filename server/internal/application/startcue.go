package application

type StartCueInput struct {
	StepID string `json:"stepId"`
}

// StartCueResult is a transient response, never a plan entity.
type StartCueResult struct {
	StepID         string `json:"stepId"`
	Text           string `json:"text"`
	ProgressWeight int    `json:"progressWeight"`
	// SearchQueries: "where do I start?" searches for the step (phrases, never links).
	SearchQueries []string `json:"searchQueries,omitempty"`
	Lang          string   `json:"lang"`
}

// StartCue reads only: no accounting, persistence, completion or AI calls.
func (a *App) StartCue(in StartCueInput) (StartCueResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	step := a.st.CurrentStep()
	if step == nil || step.ID != in.StepID {
		return StartCueResult{}, ErrNoPending
	}
	return StartCueResult{
		StepID: step.ID, Text: "Read just the current step once.", ProgressWeight: 0,
		SearchQueries: a.st.SearchQueries(step), Lang: a.st.Language(),
	}, nil
}
