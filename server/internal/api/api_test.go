package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"focuscompanion/internal/api"
	"focuscompanion/internal/application"
	"focuscompanion/internal/clock"
	"focuscompanion/internal/groq"
	"focuscompanion/internal/storage"
)

const validPlan = `{"targetHorizon":"2027-06","milestones":[
  {"title":"Draft","weight":3,"steps":[{"text":"write the intro","estimatedMinutes":10,"scheduledFor":"2026-09-18"}]},
  {"title":"Polish","weight":1,"steps":[]}]}`

// fakeGroq answers chat completions with the given contents in order, repeating the last.
func fakeGroq(t *testing.T, contents ...string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" || r.URL.Path != "/openai/v1/chat/completions" {
			http.Error(w, "bad request", http.StatusUnauthorized)
			return
		}
		i := int(n.Add(1)) - 1
		if i >= len(contents) {
			i = len(contents) - 1
		}
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": contents[i]}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

type manualClock struct{ t time.Time }

func (c *manualClock) Now() time.Time { return c.t }

func newServer(t *testing.T, groqURL, key string) (*httptest.Server, string) {
	t.Helper()
	return newServerClock(t, groqURL, key, clock.Real{})
}

func newServerClock(t *testing.T, groqURL, key string, clk clock.Clock) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	repo, err := storage.NewJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	client := groq.NewClient(groqURL, key, "test-model")
	app, err := application.New(repo, clk, groq.NewPlanner(client), application.NoShield{})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.New(app))
	t.Cleanup(srv.Close)
	return srv, dir
}

func post(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestOnboardingRepairsOnceThenSucceeds(t *testing.T) {
	g, calls := fakeGroq(t, `{"milestones":[]}`, validPlan)
	srv, _ := newServer(t, g.URL, "test-key")
	code, out := post(t, srv.URL+"/api/v1/onboarding", `{"goal":"finish my thesis","preferences":{"workBlock":"25"}}`)
	if code != 200 {
		t.Fatalf("status %d: %v", code, out)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected one repair request, got %d calls", calls.Load())
	}
	if out["onboarded"] != true || out["currentStep"].(map[string]any)["text"] != "write the intro" {
		t.Fatalf("summary %v", out)
	}
}

func TestOnboardingGivesUpAfterOneRepair(t *testing.T) {
	g, calls := fakeGroq(t, `not json`, `{"milestones":[{"title":"","weight":1}]}`)
	srv, _ := newServer(t, g.URL, "test-key")
	code, out := post(t, srv.URL+"/api/v1/onboarding", `{"goal":"finish my thesis"}`)
	if code != http.StatusBadGateway || out["error"] != "plan_unavailable" {
		t.Fatalf("got %d %v", code, out)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected exactly one repair, got %d calls", calls.Load())
	}
	resp, _ := http.Get(srv.URL + "/api/v1/state/summary")
	var s map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&s)
	if s["onboarded"] != false {
		t.Fatal("a malformed plan must never be stored")
	}
}

func TestOnboardingWithGroqUnreachableOrUnkeyed(t *testing.T) {
	for name, tc := range map[string]struct{ url, key string }{
		"unreachable": {"http://127.0.0.1:1", "test-key"},
		"no key":      {"http://127.0.0.1:1", ""},
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := newServer(t, tc.url, tc.key)
			code, out := post(t, srv.URL+"/api/v1/onboarding", `{"goal":"finish my thesis"}`)
			if code != http.StatusBadGateway || out["error"] != "plan_unavailable" {
				t.Fatalf("got %d %v", code, out)
			}
		})
	}
}

// Acceptance criteria 3 and 4 over real HTTP: after onboarding, Groq is
// switched off entirely and the return screen still appears.
func TestReturnScreenOverHTTPWithGroqGone(t *testing.T) {
	g, _ := fakeGroq(t, validPlan)
	clk := &manualClock{t: time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local)}
	srv, _ := newServerClock(t, g.URL, "test-key", clk)
	if code, out := post(t, srv.URL+"/api/v1/onboarding", `{"goal":"finish my thesis"}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	g.Close() // Groq is now unreachable

	ev := func(after time.Duration, typ, url string) map[string]any {
		clk.t = clk.t.Add(after)
		_, out := post(t, srv.URL+"/api/v1/browser/event",
			`{"type":"`+typ+`","tabId":3,"url":"`+url+`","title":"x","focused":true,"activity":"active"}`)
		return out
	}
	if out := ev(0, "tab_activated", "https://docs.google.com/d/1"); out["command"] != "DO_NOTHING" {
		t.Fatalf("%v", out) // no greeting card: the first card of the day says hello
	}
	if out := ev(time.Minute, "tab_activated", "https://www.youtube.com/watch?v=1"); out["command"] != "DO_NOTHING" || out["reason"] != "grace_not_elapsed" {
		t.Fatalf("%v", out)
	}
	out := ev(5*time.Minute, "heartbeat", "https://www.youtube.com/watch?v=1")
	if out["command"] != "SHOW_RAMP" {
		t.Fatalf("return screen did not appear with Groq unreachable: %v", out)
	}
	payload := out["payload"].(map[string]any)
	if payload["step"].(map[string]any)["text"] != "write the intro" || payload["site"] != "youtube.com" {
		t.Fatalf("payload %v", payload)
	}
}

func TestEventValidationAndCommandShape(t *testing.T) {
	g, _ := fakeGroq(t, validPlan)
	srv, _ := newServer(t, g.URL, "test-key")
	post(t, srv.URL+"/api/v1/onboarding", `{"goal":"finish my thesis"}`)
	code, out := post(t, srv.URL+"/api/v1/browser/event", `{"type":"nonsense","url":"https://a.com","focused":true}`)
	if code != 400 || out["error"] != "invalid" {
		t.Fatalf("%d %v", code, out)
	}
	code, out = post(t, srv.URL+"/api/v1/browser/event", `{"type":"heartbeat","tabId":1,"url":"https://a.com","title":"a","focused":true,"activity":"active"}`)
	if code != 200 || out["command"] == nil || out["reason"] == "" {
		t.Fatalf("every response carries one command and a reason: %d %v", code, out)
	}
}

func TestDeliveryAcknowledgementOverHTTP(t *testing.T) {
	g, _ := fakeGroq(t, validPlan)
	clk := &manualClock{t: time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local)}
	srv, _ := newServerClock(t, g.URL, "test-key", clk)
	post(t, srv.URL+"/api/v1/onboarding", `{"goal":"finish my thesis"}`)
	event := func(url string) map[string]any {
		code, out := post(t, srv.URL+"/api/v1/browser/event", `{"type":"heartbeat","tabId":3,"url":"`+url+`","focused":true,"activity":"active"}`)
		if code != 200 {
			t.Fatalf("event: %d %v", code, out)
		}
		return out
	}
	ack := func(out map[string]any, url string) {
		id := out["payload"].(map[string]any)["deliveryId"].(string)
		code, result := post(t, srv.URL+"/api/v1/browser/ack", `{"deliveryId":"`+id+`","tabId":3,"url":"`+url+`"}`)
		if code != 200 || result["acknowledged"] != true {
			t.Fatalf("ack: %d %v", code, result)
		}
	}
	workURL, blacklistedURL := "https://example.com/work", "https://www.youtube.com/watch?v=1"
	event(workURL) // work context; there is no greeting card any more
	for _, command := range []string{"SHOW_RAMP"} {
		url := blacklistedURL
		event(url)
		clk.t = clk.t.Add(5 * time.Minute)
		if out := event(url); out["command"] != command {
			t.Fatalf("initial: %v", out)
		}
		out := event(url)
		if out["command"] != command {
			t.Fatalf("unacked delivery consumed: %v", out)
		}
		ack(out, url)
		next := event(url)
		if command == "SHOW_RAMP" {
			// The return screen stays open (same card) until answered or ignored.
			id := out["payload"].(map[string]any)["deliveryId"]
			if next["command"] != command || next["reason"] != "card_open" || next["payload"].(map[string]any)["deliveryId"] != id {
				t.Fatalf("open return screen not kept: %v", next)
			}
		} else if next["command"] != "DO_NOTHING" {
			t.Fatalf("ack did not suppress: %v", next)
		}
	}
	if code, _ := post(t, srv.URL+"/api/v1/browser/ack", `{}`); code != 400 {
		t.Fatalf("invalid ack status: %d", code)
	}
	if code, _ := post(t, srv.URL+"/api/v1/browser/ack", `{"deliveryId":"unknown","tabId":3,"url":"https://example.com"}`); code != 409 {
		t.Fatalf("unknown ack status: %d", code)
	}
}

func TestActivityEventOverHTTP(t *testing.T) {
	g, _ := fakeGroq(t, validPlan)
	srv, _ := newServer(t, g.URL, "test-key")
	post(t, srv.URL+"/api/v1/onboarding", `{"goal":"finish my thesis"}`)
	for _, activity := range []string{"idle", "locked"} {
		code, out := post(t, srv.URL+"/api/v1/browser/event", `{"type":"activity_changed","tabId":3,"url":"https://example.com/work","focused":true,"activity":"`+activity+`"}`)
		if code != 200 || out["command"] != "DO_NOTHING" || out["reason"] != "user_inactive" {
			t.Fatalf("%d %v", code, out)
		}
	}
	if code, _ := post(t, srv.URL+"/api/v1/browser/event", `{"type":"heartbeat","activity":"invalid"}`); code != 400 {
		t.Fatalf("invalid activity status: %d", code)
	}
}

func TestVoiceUpload(t *testing.T) {
	g, _ := fakeGroq(t, validPlan)
	srv, dir := newServer(t, g.URL, "test-key")

	resp, _ := http.Post(srv.URL+"/api/v1/voice", "audio/webm", bytes.NewReader([]byte("x")))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("voice before a goal should be refused, got %d", resp.StatusCode)
	}
	post(t, srv.URL+"/api/v1/onboarding", `{"goal":"finish my thesis"}`)

	resp, _ = http.Post(srv.URL+"/api/v1/voice", "text/plain", strings.NewReader("hi"))
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("got %d", resp.StatusCode)
	}
	resp, _ = http.Post(srv.URL+"/api/v1/voice", "audio/webm;codecs=opus", bytes.NewReader([]byte("RIFFdata")))
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	b, err := os.ReadFile(filepath.Join(dir, "voice", "goal-voice.webm"))
	if err != nil || string(b) != "RIFFdata" {
		t.Fatalf("recording not on disk: %v %q", err, b)
	}
	// The recording is stored locally. Phase 4 plays it back in the dashboard,
	// but a web page (an <audio src>, a plain fetch) still cannot get it.
	if resp, _ = http.Get(srv.URL + "/api/v1/voice"); resp.StatusCode == 200 {
		t.Fatal("voice must not be readable without the extension's header")
	}
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/voice", nil)
	req.Header.Set(api.VoiceHeader, "1")
	if resp, _ = http.DefaultClient.Do(req); resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "audio/webm" {
		t.Fatalf("playback for the extension: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if b, _ := io.ReadAll(resp.Body); string(b) != "RIFFdata" {
		t.Fatalf("played %q", b)
	}
	full, _ := http.Get(srv.URL + "/api/v1/state/full")
	var f map[string]any
	_ = json.NewDecoder(full.Body).Decode(&f)
	if f["voiceRecorded"] != true {
		t.Fatalf("voiceRecorded = %v", f["voiceRecorded"])
	}
}

func TestOversizedVoiceRejected(t *testing.T) {
	g, _ := fakeGroq(t, validPlan)
	srv, dir := newServer(t, g.URL, "test-key")
	post(t, srv.URL+"/api/v1/onboarding", `{"goal":"finish my thesis"}`)
	resp, _ := http.Post(srv.URL+"/api/v1/voice", "audio/webm", bytes.NewReader(make([]byte, 6<<20)))
	if resp.StatusCode == 200 {
		t.Fatal("6 MB recording accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "voice", "goal-voice.webm")); err == nil {
		t.Fatal("oversized recording left on disk")
	}
}

func TestRootExplainsHowToOpenApp(t *testing.T) {
	srv, _ := newServer(t, "http://127.0.0.1:1", "")
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("unexpected home response: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for _, want := range []string{"chrome://extensions", "Load unpacked", "/api/v1/health"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("home response missing %q", want)
		}
	}
}

func TestDayRespondOverHTTP(t *testing.T) {
	g, _ := fakeGroq(t, validPlan)
	srv, _ := newServer(t, g.URL, "test-key")
	if code, out := post(t, srv.URL+"/api/v1/day/respond", `{"choice":"wont_work"}`); code != http.StatusConflict || out["error"] != "no_goal" {
		t.Fatalf("before onboarding: %d %v", code, out)
	}
	if code, _ := post(t, srv.URL+"/api/v1/onboarding", `{"goal":"finish my thesis"}`); code != 200 {
		t.Fatal("onboarding failed")
	}
	if code, out := post(t, srv.URL+"/api/v1/day/respond", `{"choice":"maybe"}`); code != http.StatusBadRequest {
		t.Fatalf("invalid choice: %d %v", code, out)
	}
	code, out := post(t, srv.URL+"/api/v1/day/respond", `{"choice":"need_start"}`)
	queries, _ := out["searchQueries"].([]any)
	if code != 200 || out["mode"] != "undecided" || len(queries) == 0 || out["step"] == nil {
		t.Fatalf("need_start: %d %v", code, out)
	}
	code, out = post(t, srv.URL+"/api/v1/day/respond", `{"choice":"wont_work"}`)
	if code != 200 || out["mode"] != "bad" || out["dismissUnsolicited"] == nil {
		t.Fatalf("wont_work: %d %v", code, out)
	}
	resp, _ := http.Get(srv.URL + "/api/v1/state/summary")
	var s map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&s)
	if day, _ := s["day"].(map[string]any); day["mode"] != "bad" || s["lang"] != "en" {
		t.Fatalf("summary day %v lang %v", s["day"], s["lang"])
	}
	if code, out := post(t, srv.URL+"/api/v1/card/close", `{"deliveryId":"none","reason":"done"}`); code != http.StatusConflict {
		t.Fatalf("closing a card that is not open: %d %v", code, out)
	}
	if code, out := post(t, srv.URL+"/api/v1/card/close", `{"deliveryId":"none","reason":"x"}`); code != http.StatusBadRequest {
		t.Fatalf("invalid reason: %d %v", code, out)
	}
}

func TestDevToolsOverHTTP(t *testing.T) {
	g, _ := fakeGroq(t, validPlan)
	srv, _ := newServer(t, g.URL, "test-key")
	post(t, srv.URL+"/api/v1/onboarding", `{"goal":"finish my thesis"}`)
	if code, out := post(t, srv.URL+"/api/v1/dev/timings", `{"fast":true}`); code != 200 || out["fast"] != true || out["graceSeconds"] != float64(10) {
		t.Fatalf("timings: %d %v", code, out)
	}
	if code, out := post(t, srv.URL+"/api/v1/dev/reset", `{"scope":"day"}`); code != 200 || out["onboarded"] != true || out["fastTimings"] != true {
		t.Fatalf("reset day: %d %v", code, out)
	}
	if code, _ := post(t, srv.URL+"/api/v1/dev/reset", `{"scope":"nope"}`); code != http.StatusBadRequest {
		t.Fatalf("bad scope: %d", code)
	}
	if code, out := post(t, srv.URL+"/api/v1/dev/reset", `{"scope":"profile"}`); code != 200 || out["onboarded"] != false {
		t.Fatalf("reset profile: %d %v", code, out)
	}
}

func TestNoUnspecifiedRoutes(t *testing.T) {
	g, _ := fakeGroq(t, validPlan)
	srv, _ := newServer(t, g.URL, "test-key")
	// /summary/day was on this list until Phase 4 added it (TestPhase4Routes).
	for _, path := range []string{"/missing", "/api/v1/demo/clock", "/api/v1/blacklist"} {
		resp, _ := http.Get(srv.URL + path)
		if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s exists but is not a specified route (%d)", path, resp.StatusCode)
		}
	}
}

func TestPhase4And5Routes(t *testing.T) {
	g, _ := fakeGroq(t, validPlan)
	srv, _ := newServer(t, g.URL, "test-key")
	if code, out := post(t, srv.URL+"/api/v1/dev/seed", `{}`); code != 200 || out["goal"] != "Become a YouTuber" {
		t.Fatalf("seed: %d %v", code, out)
	}
	for path, key := range map[string]string{"/api/v1/lamp": "level", "/api/v1/summary/day": "date"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if resp.StatusCode != 200 || out[key] == nil {
			t.Fatalf("%s: %d %v", path, resp.StatusCode, out)
		}
	}
	if code, out := post(t, srv.URL+"/api/v1/ideas/review", `{}`); code != 200 || out["level"] != "off" {
		t.Fatalf("review: %d %v", code, out)
	}
	if code, out := post(t, srv.URL+"/api/v1/dev/clock", `{"addDays":5}`); code != 200 || out["offsetDays"] != float64(5) {
		t.Fatalf("clock: %d %v", code, out)
	}
	if code, _ := post(t, srv.URL+"/api/v1/dev/clock", `{"addDays":-1}`); code != http.StatusBadRequest {
		t.Fatalf("negative days: %d", code)
	}
	// No recording in the seed: 404, never another file.
	if resp, _ := http.Get(srv.URL + "/api/v1/voice"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("voice without a recording: %d", resp.StatusCode)
	}
}
