package application

import (
	"context"
	"errors"
	"log"
)

// ErrSttUnavailable: the recording could not be turned into text.
var ErrSttUnavailable = errors.New("speech to text is unavailable")

// Transcriber turns a recording of the user's voice into text. Implemented by
// groq.Transcriber; faked in tests.
type Transcriber interface {
	Transcribe(ctx context.Context, audio []byte, contentType, lang string) (string, error)
}

// SetTranscriber wires speech to text.
func (a *App) SetTranscriber(t Transcriber) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.transcriber = t
}

// Transcribe is POST /stt. The recording is never stored.
func (a *App) Transcribe(ctx context.Context, audio []byte, contentType, lang string) (string, error) {
	a.mu.Lock()
	t := a.transcriber
	a.mu.Unlock()
	if t == nil || len(audio) == 0 {
		return "", ErrSttUnavailable
	}
	text, err := t.Transcribe(ctx, audio, contentType, lang)
	if err != nil {
		log.Printf("speech to text failed: %v", err)
		return "", ErrSttUnavailable
	}
	return text, nil
}
