package groq

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"focuscompanion/internal/domain"
)

// fakeChat answers each chat completion with the next content (repeating
// the last) and records the system prompt of every request.
func fakeChat(t *testing.T, contents ...string) (*Planner, *[]string) {
	t.Helper()
	var systems []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		systems = append(systems, req.Messages[0].Content)
		i := min(len(systems)-1, len(contents)-1)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": contents[i]}}}})
	}))
	t.Cleanup(srv.Close)
	return NewPlanner(NewClient(srv.URL, "k", "m")), &systems
}

const (
	greekPlan   = `{"targetHorizon":"2027-06","milestones":[{"title":"Βασικά της Python","weight":1,"steps":[{"text":"Δες ένα εισαγωγικό βίντεο για Python","estimatedMinutes":30}]},{"title":"Μικρό project","weight":1,"steps":[]}]}`
	englishPlan = `{"targetHorizon":"2027-06","milestones":[{"title":"Python basics","weight":1,"steps":[{"text":"Watch an intro video on Python","estimatedMinutes":30}]},{"title":"Small project","weight":1,"steps":[]}]}`
	mixedPlan   = `{"targetHorizon":"2027-06","milestones":[{"title":"Βασικά της Python","weight":1,"steps":[{"text":"Watch an intro video on Python","estimatedMinutes":30}]},{"title":"Μικρό project","weight":1,"steps":[]}]}`
)

var today = time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)

func TestGoalLanguage(t *testing.T) {
	for goal, want := range map[string]string{
		"Να μάθω Python μέχρι το 2027":                         langGreek,
		"Να μάθω Python":                                       langGreek,
		"Θέλω να μάθω React Native, TypeScript και Node.js":    langGreek,
		"θέλω να χάσω 5 κιλά":                                  langGreek,
		"Learn Python by 2027":                                 langEnglish,
		"Understand the μ-law algorithm for audio compression": langEnglish,
		"finish my thesis":                                     langEnglish,
		"2027":                                                 langEnglish,
	} {
		if got := goalLanguage(goal); got != want {
			t.Errorf("goalLanguage(%q) = %q, want %q", goal, got, want)
		}
	}
}

func TestGreekGoalGetsGreekPlan(t *testing.T) {
	p, systems := fakeChat(t, greekPlan)
	plan, err := p.Outline(context.Background(), "Να μάθω Python", today)
	if err != nil || plan.Milestones[0].Title != "Βασικά της Python" {
		t.Fatalf("plan %+v err %v", plan, err)
	}
	if len(*systems) != 1 || !strings.Contains((*systems)[0], "in Greek only") {
		t.Fatalf("system prompt does not require Greek: %q", *systems)
	}
}

func TestEnglishGoalGetsEnglishPlan(t *testing.T) {
	p, systems := fakeChat(t, englishPlan)
	if _, err := p.Outline(context.Background(), "Learn Python", today); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains((*systems)[0], "in English only") {
		t.Fatalf("system prompt does not require English: %q", (*systems)[0])
	}
}

func TestMixedLanguagePlanIsRepairedOnce(t *testing.T) {
	p, systems := fakeChat(t, mixedPlan, greekPlan)
	plan, err := p.Outline(context.Background(), "Να μάθω Python", today)
	if err != nil || len(*systems) != 2 || plan.Milestones[0].Steps[0].Text != "Δες ένα εισαγωγικό βίντεο για Python" {
		t.Fatalf("calls %d plan %+v err %v", len(*systems), plan, err)
	}
}

func TestWrongLanguageTwiceIsAnInvalidPlan(t *testing.T) {
	p, _ := fakeChat(t, englishPlan)
	if _, err := p.Outline(context.Background(), "Να μάθω Python", today); !errors.Is(err, domain.ErrInvalidPlan) {
		t.Fatalf("got %v, want ErrInvalidPlan", err)
	}
	p, _ = fakeChat(t, greekPlan)
	if _, err := p.Outline(context.Background(), "Learn Python", today); !errors.Is(err, domain.ErrInvalidPlan) {
		t.Fatalf("Greek plan for an English goal: got %v", err)
	}
}

func TestPromptsAskForKeywordsAndSearchQueries(t *testing.T) {
	p, systems := fakeChat(t, `{"targetHorizon":"2027-06","keywords":["python","docs.python.org"],"milestones":[{"title":"Βασικά της Python","weight":1,"steps":[{"text":"Δες ένα βίντεο για Python","estimatedMinutes":30,"searchQueries":["python για αρχάριους","python βίντεο","python μεταβλητές"]}]}]}`)
	plan, err := p.Outline(context.Background(), "Να μάθω Python", today)
	if err != nil {
		t.Fatal(err)
	}
	sys := (*systems)[0]
	for _, want := range []string{`"keywords"`, `"searchQueries"`, "never URLs", "in Greek"} {
		if !strings.Contains(sys, want) {
			t.Errorf("outline prompt misses %q", want)
		}
	}
	if strings.Contains(sys, "%!") {
		t.Errorf("outline prompt has a formatting error: %q", sys)
	}
	if len(plan.Keywords) != 2 || len(plan.Milestones[0].Steps[0].SearchQueries) != 3 {
		t.Fatalf("keywords/searchQueries not parsed: %+v", plan)
	}
	p, systems = fakeChat(t, `{"steps":[{"text":"Γράψε ένα script","estimatedMinutes":20}]}`)
	if _, err := p.StepsFor(context.Background(), "Να μάθω Python", domain.Milestone{Title: "Project"}, today, domain.PlanContext{}); err != nil {
		t.Fatal(err)
	}
	if s := (*systems)[0]; !strings.Contains(s, `"searchQueries"`) || strings.Contains(s, "%!") {
		t.Errorf("steps prompt: %q", s)
	}
}

func TestKeywordsForAnExistingGoal(t *testing.T) {
	p, systems := fakeChat(t, `{"keywords":["Python","video","docs.python.org"]}`)
	kw, err := p.Keywords(context.Background(), "Become a developer", []string{"Learn programming"})
	if err != nil || strings.Join(kw, ",") != "python,docs.python.org" {
		t.Fatalf("keywords %q err %v", kw, err)
	}
	if !strings.Contains((*systems)[0], `"keywords"`) {
		t.Fatal("prompt does not ask for keywords")
	}
	// Nothing usable twice: an invalid plan, never an empty keyword list.
	p, systems = fakeChat(t, `{"keywords":["video","learn"]}`)
	if _, err := p.Keywords(context.Background(), "x", nil); !errors.Is(err, domain.ErrInvalidPlan) || len(*systems) != 2 {
		t.Fatalf("got %v after %d calls", err, len(*systems))
	}
}

// Regression (manual test: "onboarding plan failed: groq returned status 400").
// Groq answers 400 json_validate_failed when the model's output is not valid
// JSON. That is a rejected answer like any other: ask once more.
func TestInvalidJSONFromGroqIsRetriedOnce(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"json_validate_failed","message":"private goal text"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": englishPlan}}}})
	}))
	t.Cleanup(srv.Close)
	p := NewPlanner(NewClient(srv.URL, "k", "m"))
	if _, err := p.Outline(context.Background(), "Learn Python", today); err != nil || calls != 2 {
		t.Fatalf("err %v after %d calls", err, calls)
	}
}

func TestInvalidJSONTwiceIsAnInvalidPlanAndNamesTheCodeOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"json_validate_failed","message":"private goal text"}}`))
	}))
	t.Cleanup(srv.Close)
	_, err := NewPlanner(NewClient(srv.URL, "k", "m")).Outline(context.Background(), "Learn Python", today)
	if !errors.Is(err, domain.ErrInvalidPlan) || !strings.Contains(err.Error(), "json_validate_failed") || strings.Contains(err.Error(), "private") {
		t.Fatalf("got %v", err)
	}
}

// Regression (manual test): the second milestone of "write one paragraph"
// began again with "Brainstorm ideas" and "Outline the main point".
func TestNextMilestoneGetsThePlanAndNeverRepeatsDoneSteps(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		bodies = append(bodies, req.Messages[1].Content)
		content := `{"steps":[{"text":"Brainstorm ideas for the paragraph","estimatedMinutes":10},{"text":"Write a rough first draft","estimatedMinutes":20}]}`
		if len(bodies) > 1 {
			content = `{"steps":[{"text":"Write a rough first draft","estimatedMinutes":20},{"text":"Read it aloud and fix awkward sentences","estimatedMinutes":10}]}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}}})
	}))
	t.Cleanup(srv.Close)
	plan := domain.PlanContext{
		Milestones: []string{"Plan the paragraph", "Write the first draft", "Polish"},
		DoneSteps:  []string{"Brainstorm a clear topic for the paragraph", "Create a brief outline"},
	}
	steps, err := NewPlanner(NewClient(srv.URL, "k", "m")).StepsFor(context.Background(), "Write one paragraph", domain.Milestone{Title: "Write the first draft"}, today, plan)
	if err != nil || len(bodies) != 2 || steps[0].Text != "Write a rough first draft" {
		t.Fatalf("steps %+v err %v after %d calls", steps, err, len(bodies))
	}
	for _, want := range []string{`"completedSteps"`, "Brainstorm a clear topic for the paragraph", `"milestones"`, "Polish", `"currentMilestone":"Write the first draft"`} {
		if !strings.Contains(bodies[0], want) {
			t.Errorf("request misses %s: %s", want, bodies[0])
		}
	}
}

func TestOutlineAsksForSizeMatchedToTheGoal(t *testing.T) {
	for _, want := range []string{`"estimatedHours"`, "under 8 hours", "Never pad", "belongs to its own milestone"} {
		if !strings.Contains(outlineSystem, want) {
			t.Errorf("outline prompt misses %q", want)
		}
	}
	if !strings.Contains(stepsSystem, "Never repeat or rephrase a completed step") {
		t.Error("steps prompt must forbid repeating completed steps")
	}
}

func TestPromptSchemaHasNoCommentsToCopy(t *testing.T) {
	for name, prompt := range map[string]string{"outline": outlineSystem, "steps": stepsSystem, "keywords": keywordsSystem} {
		if strings.Contains(prompt, "//") {
			t.Errorf("%s prompt contains // comments, which models copy into invalid JSON", name)
		}
	}
}

func TestNextMilestoneStepsFollowGoalLanguage(t *testing.T) {
	p, systems := fakeChat(t, `{"steps":[{"text":"Write a tiny script","estimatedMinutes":20}]}`, `{"steps":[{"text":"Γράψε ένα μικρό script","estimatedMinutes":20}]}`)
	steps, err := p.StepsFor(context.Background(), "Να μάθω Python", domain.Milestone{Title: "Μικρό project"}, today, domain.PlanContext{})
	if err != nil || len(*systems) != 2 || steps[0].Text != "Γράψε ένα μικρό script" {
		t.Fatalf("calls %d steps %+v err %v", len(*systems), steps, err)
	}
	if !strings.Contains((*systems)[0], "in Greek only") {
		t.Fatalf("steps prompt does not require Greek")
	}
}
