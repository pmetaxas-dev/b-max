package application

import (
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"focuscompanion/internal/domain"
)

// Page metadata (MVP §4, "metadata as a shield"): on blacklisted pages the
// content script may read the channel and description (a "Lesson 3" video
// from a Python channel), so relevance sees more than the title. It is kept
// in memory only, briefly, per URL: never in state.json, never sent to the AI.

const (
	maxMetadataPages = 50
	metadataTTL      = 30 * time.Minute
	maxChannelRunes  = 200
	maxMetadataRunes = 500
)

type MetadataInput struct {
	URL         string `json:"url"`
	Channel     string `json:"channel"`
	Description string `json:"description"`
}

type pageMetadata struct {
	text string
	at   time.Time
}

// StoreMetadata keeps a blacklisted page's channel/description for the next
// events on that URL. Anything else is refused: only the pages the server
// asked about (Payload.ReadMetadata) are ever read.
func (a *App) StoreMetadata(in MetadataInput) error {
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ErrInvalid
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if p := domain.ClassifyPage(in.URL, a.st.Blacklist); !p.Blacklisted || p.DeterministicDistraction {
		return ErrInvalid
	}
	now := a.now()
	if a.metadata == nil {
		a.metadata = map[string]pageMetadata{}
	}
	for k, m := range a.metadata { // drop stale entries; keep the cache small
		if now.Sub(m.at) > metadataTTL || len(a.metadata) >= maxMetadataPages {
			delete(a.metadata, k)
		}
	}
	text := strings.TrimSpace(clipText(in.Channel, maxChannelRunes) + " " + clipText(in.Description, maxMetadataRunes))
	if text == "" {
		delete(a.metadata, in.URL)
		return nil
	}
	a.metadata[in.URL] = pageMetadata{text: text, at: now}
	return nil
}

// metadataFor returns the fresh metadata text for url, or "". Mutex held.
func (a *App) metadataFor(rawURL string, now time.Time) string {
	m, ok := a.metadata[rawURL]
	if !ok || now.Sub(m.at) > metadataTTL {
		return ""
	}
	return m.text
}

func clipText(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > max {
		return string([]rune(s)[:max])
	}
	return s
}
