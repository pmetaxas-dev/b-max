package groq

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"focuscompanion/internal/domain"
)

// fakeChatServer answers each request with the next JSON object in answers and
// remembers what it was asked.
func fakeChatServer(t *testing.T, answers []string) (*Chatter, *[]string) {
	t.Helper()
	var asked []string
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		asked = append(asked, string(raw))
		content := answers[min(n, len(answers)-1)]
		n++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": content}}},
		})
	}))
	t.Cleanup(srv.Close)
	return NewChatter(NewClient(srv.URL, "key", "test-model")), &asked
}

func onboardingHistory() []domain.ChatMessage {
	return []domain.ChatMessage{
		{Role: "max", Text: "Tell me your most important goal for the next 5 months."},
		{Role: "user", Text: "Θέλω ένα αγωνιστό σε αγώνα go-kart."},
	}
}

func TestAnAnswerThatAsksForTheDreamAgainIsCorrectedOnce(t *testing.T) {
	stuck := `{"reply":"Ποιο είναι το μεγάλο όνειρό σου;"}`
	good := `{"reply":"Ωραίο! Τι θα κάνεις σήμερα;","lifeGoal":"Να αγωνιστώ σε αγώνα go-kart","goalPoints":100}`
	c, asked := fakeChatServer(t, []string{stuck, good})

	out, err := c.Chat(context.Background(), domain.ChatContext{LangHint: "el"}, onboardingHistory())
	if err != nil {
		t.Fatal(err)
	}
	if out.LifeGoal == "" || len(*asked) != 2 {
		t.Fatalf("want the corrected answer after 2 requests, got goal %q after %d", out.LifeGoal, len(*asked))
	}
	if !strings.Contains((*asked)[1], "lifeGoal") {
		t.Error("the second request must tell the model what was wrong")
	}
}

func TestACorrectionIsAskedOnlyOnceAndTheSecondAnswerIsKept(t *testing.T) {
	stuck := `{"reply":"Πες μου περισσότερα."}`
	c, asked := fakeChatServer(t, []string{stuck})

	out, err := c.Chat(context.Background(), domain.ChatContext{LangHint: "el"}, onboardingHistory())
	if err != nil || out.Reply == "" {
		t.Fatalf("the second answer must be accepted, got %v %q", err, out.Reply)
	}
	if len(*asked) != 2 {
		t.Fatalf("exactly one correction, got %d requests", len(*asked))
	}
}

func TestARepeatedMessageIsCorrected(t *testing.T) {
	h := []domain.ChatMessage{
		{Role: "max", Text: "How long will it take?"},
		{Role: "user", Text: "about an hour"},
	}
	c, asked := fakeChatServer(t, []string{`{"reply":"How long will it take?"}`, `{"reply":"Great, an hour it is."}`})

	out, err := c.Chat(context.Background(), domain.ChatContext{Onboarded: true, LangHint: "en"}, h)
	if err != nil || out.Reply != "Great, an hour it is." || len(*asked) != 2 {
		t.Fatalf("got %v %q after %d requests", err, out.Reply, len(*asked))
	}
}
