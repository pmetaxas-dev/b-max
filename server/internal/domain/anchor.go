package domain

import (
	"strings"
	"time"
	"unicode"
)

const (
	MaxSnippetRunes = 120 // §9: snippet truncated to 120 characters
	maxTitleRunes   = 200
	maxURLRunes     = 2000
)

// AnchorInput is what the content script sends to POST /anchor.
type AnchorInput struct {
	Tier          int    `json:"tier"`
	URL           string `json:"url"`
	Title         string `json:"title"`
	Snippet       string `json:"snippet"`
	ScrollPercent int    `json:"scrollPercent"`
}

func plainText(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > max {
		r = r[:max]
	}
	return string(r)
}

// NewAnchor sanitizes an anchor: plain text, truncated, scroll clamped.
func NewAnchor(stepID string, in AnchorInput, now time.Time) (ReturnAnchor, bool) {
	if in.Tier < 1 || in.Tier > 4 {
		return ReturnAnchor{}, false
	}
	sp := in.ScrollPercent
	if sp < 0 {
		sp = 0
	}
	if sp > 100 {
		sp = 100
	}
	a := ReturnAnchor{
		StepID: stepID, Tier: in.Tier,
		URL:           plainText(in.URL, maxURLRunes),
		Title:         plainText(in.Title, maxTitleRunes),
		Snippet:       plainText(in.Snippet, MaxSnippetRunes),
		ScrollPercent: sp, CapturedAt: now,
	}
	return a, a.URL != ""
}

// ReplacesAnchor implements bestAnchor (§9): a new anchor replaces the stored
// one only if its tier is equal or higher. Tier 1 is highest, so a lower tier
// number wins; any tier replaces an older anchor of the same tier.
func ReplacesAnchor(stored ReturnAnchor, hasStored bool, next ReturnAnchor) bool {
	return !hasStored || next.Tier <= stored.Tier
}
