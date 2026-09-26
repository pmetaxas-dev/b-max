// Package storage isolates JSON persistence behind one interface so storage can
// change later (architecture-plan-v2 §13). No lock manager, no backup rotation,
// no migrations: write to a temp file and rename.
package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"focuscompanion/internal/domain"
)

type Repository interface {
	Load() (*domain.State, error)
	Save(*domain.State) error
	// SaveVoice stores the onboarding recording under voice/ and returns its file name.
	SaveVoice(ext string, r io.Reader, maxBytes int64) (string, error)
	// DeleteVoice removes the onboarding recording, if any.
	DeleteVoice() error
	// OpenVoice opens the recording `name` and returns its media type.
	OpenVoice(name string) (io.ReadCloser, string, error)
}

type JSONRepo struct {
	dir string
}

func NewJSON(dir string) (*JSONRepo, error) {
	if err := os.MkdirAll(filepath.Join(dir, "voice"), 0o700); err != nil {
		return nil, err
	}
	return &JSONRepo{dir: dir}, nil
}

func (r *JSONRepo) statePath() string { return filepath.Join(r.dir, "state.json") }

// Dir is the data directory (tests check what is and is not written there).
func (r *JSONRepo) Dir() string { return r.dir }

func (r *JSONRepo) Load() (*domain.State, error) {
	b, err := os.ReadFile(r.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return domain.NewState(), nil
	}
	if err != nil {
		return nil, err
	}
	st := &domain.State{}
	if err := json.Unmarshal(b, st); err != nil {
		return nil, fmt.Errorf("state.json is unreadable: %w", err)
	}
	st.Normalize()
	if len(st.Blacklist) == 0 {
		st.Blacklist = domain.DefaultBlacklist()
	}
	return st, nil
}

func (r *JSONRepo) Save(st *domain.State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(r.statePath(), func(w io.Writer) error { _, err := w.Write(b); return err })
}

func (r *JSONRepo) SaveVoice(ext string, src io.Reader, maxBytes int64) (string, error) {
	name := "goal-voice" + ext
	dir := filepath.Join(r.dir, "voice")
	// One recording only: drop any earlier one, whatever its extension.
	old, _ := filepath.Glob(filepath.Join(dir, "goal-voice.*"))
	err := writeAtomic(filepath.Join(dir, name), func(w io.Writer) error {
		n, err := io.Copy(w, io.LimitReader(src, maxBytes+1))
		if err != nil {
			return err
		}
		if n > maxBytes {
			return errors.New("recording too large")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	for _, f := range old {
		if filepath.Base(f) != name {
			_ = os.Remove(f)
		}
	}
	return name, nil
}

func (r *JSONRepo) DeleteVoice() error {
	old, _ := filepath.Glob(filepath.Join(r.dir, "voice", "goal-voice.*"))
	for _, f := range old {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// voiceTypes are the recording formats the onboarding recorder produces.
var voiceTypes = map[string]string{".webm": "audio/webm", ".ogg": "audio/ogg", ".mp4": "audio/mp4", ".m4a": "audio/mp4", ".wav": "audio/wav", ".mp3": "audio/mpeg"}

func (r *JSONRepo) OpenVoice(name string) (io.ReadCloser, string, error) {
	// Only the one recording SaveVoice writes, never another path.
	if filepath.Base(name) != name || len(name) < len("goal-voice.") || name[:len("goal-voice.")] != "goal-voice." {
		return nil, "", os.ErrNotExist
	}
	f, err := os.Open(filepath.Join(r.dir, "voice", name))
	if err != nil {
		return nil, "", err
	}
	typ := voiceTypes[filepath.Ext(name)]
	if typ == "" {
		typ = "application/octet-stream"
	}
	return f, typ, nil
}

func writeAtomic(path string, write func(io.Writer) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := write(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
