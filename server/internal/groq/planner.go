package groq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"focuscompanion/internal/domain"
)

// Planner generates plans with Groq: all milestones (with weights) and steps for
// the current milestone only (architecture-plan-v2 §9b). Prompts here are
// drafts; the pre-hackathon prompt work (§20 item 4) should replace them.
type Planner struct {
	client *Client
}

func NewPlanner(c *Client) *Planner { return &Planner{client: c} }

// languageRule is filled with the goal's language (see language.go). JSON
// keys stay in English; only the values the user reads are translated.
const languageRule = `Write every milestone "title" and step "text" in %[1]s only, the language the user wrote the goal in.
Never mix languages. Names such as Python or Excel may stay as they are.`

const outlineSystem = `You are a planning assistant inside a focus app for people with ADHD.
Reply with ONE JSON object and nothing else.
` + languageRule + `
Schema (the JSON itself must contain no comments):
{
  "targetHorizon": string,
  "milestones": [ { "title": string, "weight": number, "steps": [ ` + stepSchema + ` ] } ],
  "keywords": [string],
  "estimatedHours": number
}
Rules:
- "targetHorizon": if the goal contains a date use it, otherwise propose a realistic one.
- "estimatedHours": your honest estimate of the total hours of focused work the whole goal needs.
- Match the size of the plan to the goal: under 8 hours of work: 1 or 2 milestones; a few weeks: 3 or 4 milestones; months or years: 4 to 6 milestones. Never pad a small goal with extra milestones or steps.
- "weight": a number > 0, the milestone's relative effort.
- Milestones are in order and do not overlap.
- Only the FIRST milestone has steps (2 to 7 small, concrete actions, each doable in 5 to 60 minutes). Every other milestone has "steps": [].
- Every step belongs to its own milestone's title: a planning milestone has no writing or finishing steps, those belong to later milestones.
- "scheduledFor" is optional; if used, start from today's date.
- "keywords": specific subject terms, tools and well-known site domains for this goal (e.g. "python", "docs.python.org", "list comprehension"). Include the English technical terms even for a goal in another language, since most pages are in English. No generic words such as "video", "learn" or "tutorial".
` + queryRule

const stepsSystem = `You are a planning assistant inside a focus app for people with ADHD.
Reply with ONE JSON object and nothing else.
` + languageRule + `
Schema: { "steps": [ ` + stepSchema + ` ] }
Rules:
- 2 to 7 small, concrete actions for "currentMilestone" only, each doable in 5 to 60 minutes. "scheduledFor" is optional; if used, start from today's date.
- You are given every milestone in order and the steps already completed. Never repeat or rephrase a completed step, and never include work that belongs to another milestone: continue from where the completed steps left off.
` + queryRule

// keywordsSystem asks, once, for keywords for a goal whose plan was made
// before keywords existed (application BackfillKeywords).
const keywordsSystem = `You are a planning assistant inside a focus app for people with ADHD.
Reply with ONE JSON object and nothing else.
Schema: { "keywords": [string] }
Rules: 15 to 30 specific subject terms, tools and well-known site domains that identify web pages about this goal (e.g. "python", "docs.python.org", "list comprehension"). Include the English technical terms even for a goal in another language. No generic words such as "video", "learn", "tutorial", "data" or "using".`

const stepSchema = `{ "text": string, "estimatedMinutes": integer, "scheduledFor": "YYYY-MM-DD", "searchQueries": [string, string, string] }`

// queryRule: the app turns each query into a search-engine link itself, so
// the model never writes a URL.
const queryRule = `- "searchQueries": exactly 3 short web-search phrases (2 to 6 words) that would help someone start THIS step, in %[1]s. Search words only, never URLs.`

func (p *Planner) Outline(ctx context.Context, goal string, today time.Time) (domain.RawPlan, error) {
	lang := goalLanguage(goal)
	user, _ := json.Marshal(map[string]string{"goal": goal, "today": domain.DayKey(today), "language": languageNames[lang]})
	var plan domain.RawPlan
	err := p.withRepair(ctx, fmt.Sprintf(outlineSystem, languageNames[lang]), string(user), func(content string) error {
		plan = domain.RawPlan{}
		if err := json.Unmarshal([]byte(content), &plan); err != nil {
			return fmt.Errorf("%w: not valid JSON", domain.ErrInvalidPlan)
		}
		if err := plan.Validate(); err != nil {
			return err
		}
		return checkPlanLanguage(plan, lang)
	})
	return plan, err
}

func (p *Planner) StepsFor(ctx context.Context, goal string, m domain.Milestone, today time.Time, plan domain.PlanContext) ([]domain.RawStep, error) {
	lang := goalLanguage(goal)
	user, _ := json.Marshal(map[string]any{
		"goal": goal, "milestones": plan.Milestones, "currentMilestone": m.Title,
		"completedSteps": plan.DoneSteps, "today": domain.DayKey(today), "language": languageNames[lang],
	})
	var out struct {
		Steps []domain.RawStep `json:"steps"`
	}
	err := p.withRepair(ctx, fmt.Sprintf(stepsSystem, languageNames[lang]), string(user), func(content string) error {
		out.Steps = nil
		if err := json.Unmarshal([]byte(content), &out); err != nil {
			return fmt.Errorf("%w: not valid JSON", domain.ErrInvalidPlan)
		}
		if err := domain.ValidateSteps(out.Steps); err != nil {
			return err
		}
		if err := domain.CheckNotRepeated(out.Steps, plan.DoneSteps); err != nil {
			return err
		}
		return checkStepsLanguage(out.Steps, lang)
	})
	return out.Steps, err
}

// Keywords returns cleaned keywords for an existing goal and its milestones.
func (p *Planner) Keywords(ctx context.Context, goal string, milestones []string) ([]string, error) {
	user, _ := json.Marshal(map[string]any{"goal": goal, "milestones": milestones})
	var kw []string
	err := p.withRepair(ctx, keywordsSystem, string(user), func(content string) error {
		var out struct {
			Keywords []string `json:"keywords"`
		}
		if err := json.Unmarshal([]byte(content), &out); err != nil {
			return fmt.Errorf("%w: not valid JSON", domain.ErrInvalidPlan)
		}
		if kw = domain.CleanKeywords(out.Keywords); len(kw) == 0 {
			return fmt.Errorf("%w: no usable keywords", domain.ErrInvalidPlan)
		}
		return nil
	})
	return kw, err
}

// withRepair asks once, validates, and on an invalid answer asks exactly one
// repair question. A second failure is returned for the caller to show a retry.
func (p *Planner) withRepair(ctx context.Context, system, user string, check func(string) error) error {
	msgs := []Message{{Role: "system", Content: system}, {Role: "user", Content: user}}
	content, err := p.client.CompleteJSON(ctx, msgs)
	switch {
	case errors.Is(err, ErrInvalidJSON):
		// Groq refused the answer as invalid JSON, so there is no content to
		// show back: ask again, plainly.
		msgs = append(msgs, Message{Role: "user", Content: "Your previous answer was not valid JSON. Return ONE valid JSON object only: no comments, no text around it."})
	case err != nil:
		return err
	default:
		problem := check(content)
		if problem == nil {
			return nil
		}
		msgs = append(msgs,
			Message{Role: "assistant", Content: content},
			Message{Role: "user", Content: "That JSON was rejected: " + problem.Error() + ". Return the corrected JSON object only."},
		)
	}
	content, err = p.client.CompleteJSON(ctx, msgs)
	if errors.Is(err, ErrInvalidJSON) {
		return fmt.Errorf("%w: %v", domain.ErrInvalidPlan, err) // twice: the caller shows a retry
	}
	if err != nil {
		return err
	}
	return check(content)
}
