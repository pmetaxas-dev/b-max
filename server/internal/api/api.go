// Package api validates requests, calls the application layer and returns JSON.
// It contains no domain rules (architecture-plan-v2 §4.5).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"focuscompanion/internal/application"
	"focuscompanion/internal/domain"
)

const (
	APIVersion    = "v1"
	maxJSONBody   = 64 << 10
	maxVoiceBytes = 5 << 20
)

var voiceExt = map[string]string{
	"audio/webm": ".webm",
	"audio/ogg":  ".ogg",
	"audio/mp4":  ".m4a",
	"audio/mpeg": ".mp3",
}

type Server struct {
	app *application.App
}

func New(app *application.App) http.Handler {
	s := &Server{app: app}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "B-MAX server is running.\n\n"+
			"Check server health: /api/v1/health\n\n"+
			"The app interface runs in the Chrome extension.\n"+
			"Open chrome://extensions, enable Developer mode, click Load unpacked,\n"+
			"and select this project's extension folder. Then click B-MAX\n"+
			"in the browser's extensions menu to open the app.\n")
	})
	const p = "/api/v1"
	mux.HandleFunc("GET "+p+"/health", s.health)
	mux.HandleFunc("GET "+p+"/state/summary", s.summary)
	mux.HandleFunc("GET "+p+"/state/full", s.full)
	mux.HandleFunc("POST "+p+"/onboarding", s.onboarding)
	mux.HandleFunc("POST "+p+"/voice", s.voice)
	mux.HandleFunc("POST "+p+"/browser/event", s.event)
	mux.HandleFunc("POST "+p+"/browser/ack", s.deliveryAck)
	mux.HandleFunc("POST "+p+"/browser/metadata", s.metadata)
	mux.HandleFunc("POST "+p+"/ramp/respond", s.rampRespond)
	mux.HandleFunc("POST "+p+"/day/respond", s.dayRespond)
	mux.HandleFunc("POST "+p+"/card/close", s.cardClose)
	// Test tools (dashboard ⚙️); hackathon build only.
	mux.HandleFunc("POST "+p+"/dev/reset", s.devReset)
	mux.HandleFunc("POST "+p+"/dev/timings", s.devTimings)
	mux.HandleFunc("POST "+p+"/dev/storm", s.devStorm)
	mux.HandleFunc("POST "+p+"/dev/era/next", s.devNextEra)
	mux.HandleFunc("POST "+p+"/dev/regenerate-steps", s.devRegenerateSteps)
	mux.HandleFunc("POST "+p+"/steps/respond", s.stepsRespond)
	mux.HandleFunc("POST "+p+"/steps/start-cue", s.startCue)
	mux.HandleFunc("POST "+p+"/anchor", s.anchor)
	mux.HandleFunc("POST "+p+"/ideas", s.capture)
	mux.HandleFunc("GET "+p+"/ideas", s.ideas)
	mux.HandleFunc("GET "+p+"/tasks", s.tasks)
	mux.HandleFunc("POST "+p+"/tasks/done", s.taskDone)
	mux.HandleFunc("GET "+p+"/chat", s.chatState)
	mux.HandleFunc("POST "+p+"/chat", s.chat)
	mux.HandleFunc("POST "+p+"/stt", s.stt)
	mux.HandleFunc("POST "+p+"/lang", s.lang)
	mux.HandleFunc("POST "+p+"/ideas/delete", s.ideaDelete)
	mux.HandleFunc("POST "+p+"/ideas/promote", s.ideaPromote)
	mux.HandleFunc("GET "+p+"/status", s.status)
	mux.HandleFunc("POST "+p+"/tasks/help", s.taskHelp)
	mux.HandleFunc("POST "+p+"/tasks/action", s.taskAction)
	// Phase 4 (application/support.go).
	mux.HandleFunc("GET "+p+"/lamp", s.lamp)
	mux.HandleFunc("POST "+p+"/ideas/review", s.reviewIdeas)
	mux.HandleFunc("POST "+p+"/ideas/offer", s.answerOldIdea)
	mux.HandleFunc("GET "+p+"/voice", s.playVoice)
	mux.HandleFunc("POST "+p+"/voice/played", s.voicePlayed)
	mux.HandleFunc("GET "+p+"/summary/day", s.daySummary)
	// Phase 5 test tools (dashboard ⚙️, application/demo.go).
	mux.HandleFunc("POST "+p+"/dev/clock", s.devClock)
	mux.HandleFunc("POST "+p+"/dev/seed", s.devSeed)
	return mux
}

func (s *Server) lamp(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Lamp())
}

func (s *Server) reviewIdeas(w http.ResponseWriter, r *http.Request) {
	var in application.ReviewInput
	if !decode(w, r, &in) {
		return
	}
	res, err := s.app.ReviewIdeas(in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) answerOldIdea(w http.ResponseWriter, r *http.Request) {
	var in application.IdeaAnswer
	if !decode(w, r, &in) {
		return
	}
	if err := s.app.AnswerOldIdea(in); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// voice streams the user's own recording back to the dashboard. It never
// leaves this computer: the server listens on 127.0.0.1 only.
// VoiceHeader must be on GET /voice. A web page cannot send a custom header
// to this server without a CORS preflight, which is never approved, and an
// <audio src> sends none: only the extension can fetch the recording.
const VoiceHeader = "X-Focus-Companion"

func (s *Server) playVoice(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(VoiceHeader) != "1" {
		writeErr(w, http.StatusNotFound, "no_voice", "no recording")
		return
	}
	f, typ, err := s.app.Voice()
	if err != nil {
		writeErr(w, http.StatusNotFound, "no_voice", "no recording")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, f)
}

func (s *Server) voicePlayed(w http.ResponseWriter, _ *http.Request) {
	s.app.VoicePlayed()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) daySummary(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.DaySummary())
}

func (s *Server) devClock(w http.ResponseWriter, r *http.Request) {
	var in application.ClockInput
	if !decode(w, r, &in) {
		return
	}
	res, err := s.app.SetClock(in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) devSeed(w http.ResponseWriter, _ *http.Request) {
	if err := s.app.Seed(); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.app.Full())
}

func (s *Server) tasks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Tasks())
}

func (s *Server) taskDone(w http.ResponseWriter, r *http.Request) {
	var in application.TaskInput
	if !decode(w, r, &in) {
		return
	}
	res, err := s.app.CompleteTask(in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) capture(w http.ResponseWriter, r *http.Request) {
	var in application.CaptureInput
	if !decode(w, r, &in) {
		return
	}
	item, err := s.app.Capture(in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) ideas(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Ideas())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "request body is not valid JSON")
		return false
	}
	return true
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrInvalid):
		writeErr(w, http.StatusBadRequest, "invalid", err.Error())
	case errors.Is(err, application.ErrGoalTooSmall):
		// Retryable choice for the user: keep it as a task, make it bigger, or
		// continue anyway (allowSmall). The message carries only the estimate.
		writeErr(w, http.StatusConflict, "goal_too_small", err.Error())
	case errors.Is(err, application.ErrGoalExists):
		writeErr(w, http.StatusConflict, "goal_exists", err.Error())
	case errors.Is(err, application.ErrNoPending):
		writeErr(w, http.StatusConflict, "nothing_pending", err.Error())
	case errors.Is(err, application.ErrNoGoal):
		writeErr(w, http.StatusConflict, "no_goal", err.Error())
	case errors.Is(err, application.ErrSttUnavailable):
		writeErr(w, http.StatusBadGateway, "stt_unavailable", "Max could not hear the recording. Please try again or type.")
	case errors.As(err, new(*application.RateLimitedError)):
		var limited *application.RateLimitedError
		errors.As(err, &limited)
		w.Header().Set("Retry-After", fmt.Sprint(limited.Seconds))
		writeErr(w, http.StatusTooManyRequests, "rate_limited", fmt.Sprintf("Max needs a short break: try again in %d seconds.", limited.Seconds))
	case errors.Is(err, application.ErrChatUnavailable):
		writeErr(w, http.StatusBadGateway, "chat_unavailable", "Max could not answer. Please try again.")
	case errors.Is(err, application.ErrPlanUnavailable):
		// Retryable; nothing was stored.
		writeErr(w, http.StatusBadGateway, "plan_unavailable", "The plan could not be created. Please retry.")
	default:
		writeErr(w, http.StatusInternalServerError, "internal", "unexpected error")
	}
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	persistence := "ok"
	if !s.app.Health() {
		persistence = "error"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "apiVersion": APIVersion, "persistence": persistence})
}

func (s *Server) summary(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Summary())
}

func (s *Server) full(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Full())
}

func (s *Server) onboarding(w http.ResponseWriter, r *http.Request) {
	var in application.OnboardInput
	if !decode(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	if err := s.app.Onboard(ctx, in); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.app.Summary())
}

func (s *Server) voice(w http.ResponseWriter, r *http.Request) {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
	ext, ok := voiceExt[ct]
	if !ok {
		writeErr(w, http.StatusUnsupportedMediaType, "unsupported_media", "expected an audio recording")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxVoiceBytes)
	if err := s.app.StoreVoice(ext, r.Body, maxVoiceBytes); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"stored": true})
}

func (s *Server) event(w http.ResponseWriter, r *http.Request) {
	var ev application.Event
	if !decode(w, r, &ev) {
		return
	}
	dec, err := s.app.HandleEvent(ev)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dec)
}

func (s *Server) deliveryAck(w http.ResponseWriter, r *http.Request) {
	var in application.DeliveryAck
	if !decode(w, r, &in) {
		return
	}
	if err := s.app.AcknowledgeDelivery(in); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"acknowledged": true})
}

func (s *Server) rampRespond(w http.ResponseWriter, r *http.Request) {
	var in application.RampInput
	if !decode(w, r, &in) {
		return
	}
	res, err := s.app.RespondRamp(in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) dayRespond(w http.ResponseWriter, r *http.Request) {
	var in application.DayInput
	if !decode(w, r, &in) {
		return
	}
	res, err := s.app.RespondDay(in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) metadata(w http.ResponseWriter, r *http.Request) {
	var in application.MetadataInput
	if !decode(w, r, &in) {
		return
	}
	if err := s.app.StoreMetadata(in); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"stored": true})
}

func (s *Server) devReset(w http.ResponseWriter, r *http.Request) {
	var in application.ResetInput
	if !decode(w, r, &in) {
		return
	}
	if err := s.app.Reset(in); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.app.Full())
}

func (s *Server) devRegenerateSteps(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := s.app.RegenerateSteps(ctx); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.app.Full())
}

func (s *Server) devTimings(w http.ResponseWriter, r *http.Request) {
	var in application.TimingsInput
	if !decode(w, r, &in) {
		return
	}
	writeJSON(w, http.StatusOK, s.app.SetTimings(in))
}

func (s *Server) devStorm(w http.ResponseWriter, r *http.Request) {
	var in application.StormInput
	if !decode(w, r, &in) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"on": s.app.SetStorm(in)})
}

func (s *Server) devNextEra(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"era": s.app.NextEra()})
}

func (s *Server) cardClose(w http.ResponseWriter, r *http.Request) {
	var in application.CardInput
	if !decode(w, r, &in) {
		return
	}
	if err := s.app.CloseCard(in); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"closed": true})
}

func (s *Server) stepsRespond(w http.ResponseWriter, r *http.Request) {
	var in application.StepInput
	if !decode(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	res, err := s.app.RespondStep(ctx, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) startCue(w http.ResponseWriter, r *http.Request) {
	var in application.StartCueInput
	if !decode(w, r, &in) {
		return
	}
	res, err := s.app.StartCue(in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) anchor(w http.ResponseWriter, r *http.Request) {
	var in domain.AnchorInput
	if !decode(w, r, &in) {
		return
	}
	res, err := s.app.StoreAnchor(in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) chatState(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.ChatState())
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	var in application.ChatInput
	if !decode(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	res, err := s.app.Chat(ctx, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) taskHelp(w http.ResponseWriter, r *http.Request) {
	var in application.TaskInput
	if !decode(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	res, err := s.app.TaskHelp(ctx, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) taskAction(w http.ResponseWriter, r *http.Request) {
	var in application.TaskActionInput
	if !decode(w, r, &in) {
		return
	}
	res, err := s.app.TaskAction(in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// stt turns a recording of the user's voice into text (Groq Whisper). The
// extension records with MediaRecorder; the audio is not kept.
func (s *Server) stt(w http.ResponseWriter, r *http.Request) {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
	if !strings.HasPrefix(ct, "audio/") {
		writeErr(w, http.StatusUnsupportedMediaType, "unsupported_media", "expected an audio recording")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxVoiceBytes*2)
	audio, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large", "the recording is too large")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	text, err := s.app.Transcribe(ctx, audio, r.Header.Get("Content-Type"), r.URL.Query().Get("lang"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

// lang stores the language the user chose for the app.
func (s *Server) lang(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Lang string `json:"lang"`
	}
	if !decode(w, r, &in) {
		return
	}
	lang, err := s.app.SetLang(in.Lang)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"lang": lang})
}

// status is where the user stands, in words, for the spoken status.
func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.SpokenStatus())
}

func (s *Server) ideaDelete(w http.ResponseWriter, r *http.Request) {
	var in application.IdeaActionInput
	if !decode(w, r, &in) {
		return
	}
	if err := s.app.DeleteIdea(in); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) ideaPromote(w http.ResponseWriter, r *http.Request) {
	var in application.IdeaActionInput
	if !decode(w, r, &in) {
		return
	}
	task, err := s.app.PromoteIdea(in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}
