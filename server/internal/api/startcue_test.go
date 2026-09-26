package api_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartCueHTTP(t *testing.T) {
	g, calls := fakeGroq(t, validPlan)
	srv, dir := newServer(t, g.URL, "test-key")
	url := srv.URL + "/api/v1/steps/start-cue"
	if code, _ := post(t, url, `{}`); code != 409 {
		t.Fatalf("no current step: %d", code)
	}
	code, summary := post(t, srv.URL+"/api/v1/onboarding", `{"goal":"finish my thesis"}`)
	if code != 200 {
		t.Fatalf("onboarding: %d %+v", code, summary)
	}
	id := summary["currentStep"].(map[string]any)["id"].(string)
	before, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	n := calls.Load()
	for i := 0; i < 2; i++ {
		code, cue := post(t, url, `{"stepId":"`+id+`"}`)
		// stepId, text, progressWeight, plus the step's searches and language.
		if code != 200 || len(cue) != 5 || cue["stepId"] != id || cue["text"] != "Read just the current step once." || cue["progressWeight"] != float64(0) || cue["lang"] != "en" {
			t.Fatalf("cue: %d %+v", code, cue)
		}
		queries, _ := cue["searchQueries"].([]any)
		for _, q := range queries {
			if s, _ := q.(string); s == "" || strings.Contains(s, "://") {
				t.Fatalf("a search is empty or a link: %+v", queries)
			}
		}
		if len(queries) == 0 {
			t.Fatal("no searches for the step")
		}
	}
	for _, body := range []string{`{}`, `{"stepId":"stale"}`} {
		if code, cue := post(t, url, body); code != 409 || cue["text"] != nil {
			t.Fatalf("stale: %d %+v", code, cue)
		}
	}
	if code, _ := post(t, url, `{`); code != 400 {
		t.Fatalf("malformed: %d", code)
	}
	if calls.Load() != n {
		t.Fatal("cue made a Groq HTTP call")
	}
	after, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("cue mutated persisted state")
	}
}
