package storage

import (
	"io"
	"strings"
	"testing"
)

func TestOpenVoiceOnlyOpensTheRecording(t *testing.T) {
	r, err := NewJSON(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name, err := r.SaveVoice(".webm", strings.NewReader("sound"), 100)
	if err != nil {
		t.Fatal(err)
	}
	f, typ, err := r.OpenVoice(name)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "sound" || typ != "audio/webm" {
		t.Fatalf("got %q %s", b, typ)
	}
	for _, bad := range []string{"../state.json", "state.json", "voice/goal-voice.webm", "goal-voice.webm/../../state.json", ""} {
		if _, _, err := r.OpenVoice(bad); err == nil {
			t.Fatalf("opened %q", bad)
		}
	}
}
