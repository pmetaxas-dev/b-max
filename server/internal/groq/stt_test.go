package groq

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The recording goes to Whisper with the language and a vocabulary hint that
// keeps English words in Latin letters; the answer is the plain text.
func TestTranscribeSendsTheRecordingWithALanguageHint(t *testing.T) {
	var got struct {
		path, auth, model, lang, prompt, file string
		size                                  int
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.auth = r.URL.Path, r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("not multipart: %v", err)
		}
		got.model, got.lang, got.prompt = r.FormValue("model"), r.FormValue("language"), r.FormValue("prompt")
		f, hdr, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("no file: %v", err)
		}
		b, _ := io.ReadAll(f)
		got.file, got.size = hdr.Filename, len(b)
		_, _ = io.WriteString(w, `{"text":"  Το app έχει bug στο login.  "}`)
	}))
	defer srv.Close()
	tr := NewTranscriber(NewClient(srv.URL, "key", "chat-model"), "whisper-test")
	text, err := tr.Transcribe(context.Background(), []byte("audio-bytes"), "audio/webm;codecs=opus", "el")
	if err != nil || text != "Το app έχει bug στο login." {
		t.Fatalf("text %q err %v", text, err)
	}
	if got.path != "/openai/v1/audio/transcriptions" || got.auth != "Bearer key" || got.model != "whisper-test" ||
		got.lang != "el" || got.file != "audio.webm" || got.size != len("audio-bytes") {
		t.Fatalf("request: %+v", got)
	}
	if !strings.Contains(got.prompt, "hackathon") {
		t.Fatalf("Greek needs the English-words hint: %q", got.prompt)
	}
}

func TestTranscribeRefusesWhatItCannotSendAndNeverExposesTheAnswer(t *testing.T) {
	tr := NewTranscriber(NewClient("http://127.0.0.1:1", "", "m"), "w")
	if _, err := tr.Transcribe(context.Background(), []byte("x"), "audio/webm", "en"); err != ErrNotConfigured {
		t.Fatalf("no key: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, `private-words-spoken`)
	}))
	defer srv.Close()
	tr = NewTranscriber(NewClient(srv.URL, "key", "m"), "w")
	if _, err := tr.Transcribe(context.Background(), []byte("x"), "text/plain", "en"); err == nil {
		t.Fatal("a non-audio type must be refused")
	}
	if _, err := tr.Transcribe(context.Background(), nil, "audio/webm", "en"); err == nil {
		t.Fatal("an empty recording must be refused")
	}
	_, err := tr.Transcribe(context.Background(), []byte("x"), "audio/webm", "en")
	if err == nil || strings.Contains(err.Error(), "private-words") {
		t.Fatalf("error must not carry the response: %v", err)
	}
}
