package domain

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// RawStep, RawMilestone and RawPlan are what the planner returns before Go
// validates and normalizes them.
type RawStep struct {
	Text             string   `json:"text"`
	EstimatedMinutes int      `json:"estimatedMinutes"`
	ScheduledFor     string   `json:"scheduledFor"`
	SearchQueries    []string `json:"searchQueries"`
}

type RawMilestone struct {
	Title  string    `json:"title"`
	Weight float64   `json:"weight"`
	Steps  []RawStep `json:"steps"`
}

type RawPlan struct {
	TargetHorizon string         `json:"targetHorizon"`
	Milestones    []RawMilestone `json:"milestones"`
	Keywords      []string       `json:"keywords"`
	// EstimatedHours: the planner's estimate of focused work for the whole
	// goal; a very small goal is really a task (application.ErrGoalTooSmall).
	EstimatedHours float64 `json:"estimatedHours"`
}

var ErrInvalidPlan = errors.New("invalid plan")

// Validate implements §9b "Invalid plan": no milestones, a milestone with no
// title, or weights that cannot be normalized. It adds one rule the spec does
// not list: the first milestone must carry at least one step, otherwise there
// is no current step to show.
func (p RawPlan) Validate() error {
	if len(p.Milestones) == 0 {
		return fmt.Errorf("%w: no milestones", ErrInvalidPlan)
	}
	sum := 0.0
	for i, m := range p.Milestones {
		if strings.TrimSpace(m.Title) == "" {
			return fmt.Errorf("%w: milestone %d has no title", ErrInvalidPlan, i+1)
		}
		if math.IsNaN(m.Weight) || math.IsInf(m.Weight, 0) || m.Weight < 0 {
			return fmt.Errorf("%w: milestone %d has an unusable weight", ErrInvalidPlan, i+1)
		}
		sum += m.Weight
	}
	if sum <= 0 {
		return fmt.Errorf("%w: weights cannot be normalized", ErrInvalidPlan)
	}
	if err := validateSteps(p.Milestones[0].Steps); err != nil {
		return fmt.Errorf("%w: first milestone: %v", ErrInvalidPlan, err)
	}
	return nil
}

func validateSteps(steps []RawStep) error {
	n := 0
	for _, s := range steps {
		if strings.TrimSpace(s.Text) != "" {
			n++
		}
	}
	if n == 0 {
		return errors.New("no steps with text")
	}
	return nil
}

func ValidateSteps(steps []RawStep) error {
	if err := validateSteps(steps); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPlan, err)
	}
	return nil
}

func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

func validDate(s string) string {
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return ""
	}
	return s
}

// BuildSteps turns raw steps into Steps. Step weights are the milestone weight
// divided evenly (§9b).
func BuildSteps(m Milestone, raw []RawStep) []Step {
	var kept []RawStep
	for _, r := range raw {
		if strings.TrimSpace(r.Text) != "" {
			kept = append(kept, r)
		}
	}
	out := make([]Step, 0, len(kept))
	for i, r := range kept {
		est := r.EstimatedMinutes
		if est <= 0 {
			est = 10
		}
		if est > 480 {
			est = 480
		}
		out = append(out, Step{
			ID: NewID("step"), MilestoneID: m.ID, Order: i + 1,
			Text: clip(r.Text, 200), EstimatedMinutes: est,
			Weight: m.Weight / float64(len(kept)), Status: StatusPending,
			ScheduledFor:  validDate(r.ScheduledFor),
			SearchQueries: CleanQueries(r.SearchQueries),
		})
	}
	return out
}

// BuildPlan normalizes milestone weights to sum to 1 and materializes steps for
// the first milestone only.
func BuildPlan(goalID string, raw RawPlan) ([]Milestone, []Step) {
	sum := 0.0
	for _, m := range raw.Milestones {
		sum += m.Weight
	}
	milestones := make([]Milestone, 0, len(raw.Milestones))
	for i, r := range raw.Milestones {
		milestones = append(milestones, Milestone{
			ID: NewID("ms"), GoalID: goalID, Title: clip(r.Title, 200),
			Order: i + 1, Weight: r.Weight / sum, Status: StatusPending,
		})
	}
	steps := BuildSteps(milestones[0], raw.Milestones[0].Steps)
	milestones[0].StepsGenerated = true
	return milestones, steps
}
