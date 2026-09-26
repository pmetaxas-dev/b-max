package groq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
)

// Transcriber turns a recording of the user's voice into text with Groq's
// Whisper. Unlike the browser's own dictation it handles a language mixed
// with English words ("το app έχει bug"), keeping the English in Latin
// letters. The audio is sent to Groq and is not stored anywhere.
type Transcriber struct {
	client *Client
	model  string
}

func NewTranscriber(c *Client, model string) *Transcriber {
	return &Transcriber{client: c, model: model}
}

// vocabulary hints keep technical English words in Latin letters inside a
// Greek sentence.
var vocabulary = map[string]string{
	"el": "Ελληνικά με αγγλικούς όρους, όπως: hackathon, app, email, task, deadline, meeting, bug, code, pitch, Max, B-MAX.",
	"en": "",
}

const maxAudioBytes = 10 << 20

var audioTypes = map[string]string{
	"audio/webm": "audio.webm",
	"audio/ogg":  "audio.ogg",
	"audio/mp4":  "audio.m4a",
	"audio/mpeg": "audio.mp3",
	"audio/wav":  "audio.wav",
}

// Transcribe returns the text of an audio recording. lang is "el" or "en".
func (t *Transcriber) Transcribe(ctx context.Context, audio []byte, contentType, lang string) (string, error) {
	c := t.client
	if c.apiKey == "" {
		return "", ErrNotConfigured
	}
	name, ok := audioTypes[strings.ToLower(strings.SplitN(contentType, ";", 2)[0])]
	if !ok || len(audio) == 0 || len(audio) > maxAudioBytes {
		return "", fmt.Errorf("stt: unsupported recording")
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {fmt.Sprintf(`form-data; name="file"; filename=%q`, name)},
		"Content-Type":        {strings.SplitN(contentType, ";", 2)[0]},
	})
	if err != nil {
		return "", err
	}
	_, _ = part.Write(audio)
	fields := map[string]string{"model": t.model, "response_format": "json", "temperature": "0"}
	if lang == "el" || lang == "en" {
		fields["language"] = lang
	}
	if hint := vocabulary[lang]; hint != "" {
		fields["prompt"] = hint
	}
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/openai/v1/audio/transcriptions", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("groq unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		// Never log the body: it may echo what the user said.
		return "", fmt.Errorf("groq transcription returned status %d", resp.StatusCode)
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("groq transcription: unreadable response")
	}
	return strings.TrimSpace(out.Text), nil
}
