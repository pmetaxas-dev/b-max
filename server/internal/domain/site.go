package domain

import (
	"net/url"
	"strings"
)

// DefaultBlacklist is shipped enabled (architecture-plan-v2 §7). Adult sites are
// deliberately absent.
func DefaultBlacklist() []BlacklistEntry {
	domains := []string{
		"youtube.com", "instagram.com", "tiktok.com", "x.com", "twitter.com",
		"facebook.com", "reddit.com", "twitch.tv", "netflix.com",
	}
	out := make([]BlacklistEntry, 0, len(domains))
	for _, d := range domains {
		out = append(out, BlacklistEntry{Domain: d, Enabled: true, Source: "default"})
	}
	return out
}

// aliases merges sites that are one product; they share one once-per-day key (§7).
var aliases = map[string]string{
	"twitter.com": "x.com",
}

// deterministicPaths are distraction by definition and never reach the shield (§7).
var deterministicPaths = []struct{ host, prefix string }{
	{"youtube.com", "/shorts/"},
	{"instagram.com", "/reels/"},
	{"facebook.com", "/reel/"},
}

// Page is the deterministic reading of a URL.
type Page struct {
	Kind        PageKind
	Domain      string // host, lower case
	PageKey     string // host + path, no query or fragment
	Blacklisted bool
	Site        string // alias-resolved blacklist domain; the once-per-day key
	// DeterministicDistraction is true for paths that skip the shield.
	DeterministicDistraction bool
	// Relevant: the page is about the goal (relevance.go). Set by the caller,
	// which knows the goal's keywords; ClassifyPage itself never sets it.
	Relevant bool
	// NotWork: a web page that is neither work nor distraction (a search
	// that is not about the goal). Cards still show on it. Set by the caller.
	NotWork bool
}

// Sign-in pages are neutral, like chrome:// pages: no Max, no card, no
// time. Someone logging in is neither working nor distracted. KEEP IN SYNC
// with extension/shared/auth-pages.js (both have a table test).
var authHosts = []string{
	"accounts.google.com", "appleid.apple.com", "idmsa.apple.com",
	"login.microsoftonline.com", "login.live.com", "login.yahoo.com",
	"auth0.com", "okta.com",
}

// authSegments are whole path segments ("/accounts/login/", "/login.php"),
// never substrings, so "/loginhelp" or "/docs/authentication" stay normal.
var authSegments = map[string]bool{
	"oauth": true, "oauth2": true, "authorize": true, "login": true, "logon": true,
	"signin": true, "sign-in": true, "two_step_verification": true, "2fa": true, "checkpoint": true,
}

// IsAuthPage reports whether a URL is a sign-in / authentication page.
func IsAuthPage(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	for _, h := range authHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	for _, seg := range strings.Split(strings.ToLower(u.Path), "/") {
		if dot := strings.IndexByte(seg, '.'); dot > 0 {
			seg = seg[:dot] // login.php
		}
		if authSegments[seg] {
			return true
		}
	}
	return false
}

// IsSearchResults reports whether a URL is a web-search results page. The
// caller treats one as neutral unless its query is about the goal: searching
// "reddit" is a way to get somewhere, not work.
func IsSearchResults(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	switch {
	case strings.HasPrefix(host, "google.") && u.Path == "/search":
	case (host == "bing.com" || host == "search.yahoo.com" || host == "ecosia.org" || host == "search.brave.com") && u.Path == "/search":
	case strings.HasPrefix(host, "yandex.") && strings.HasPrefix(u.Path, "/search"):
	case host == "duckduckgo.com" && u.Query().Get("q") != "":
	default:
		return false
	}
	return true
}

// ClassifyPage reads a URL against the blacklist. It is pure and needs no network.
func ClassifyPage(raw string, blacklist []BlacklistEntry) Page {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || IsAuthPage(u) {
		return Page{Kind: PageNeutral}
	}
	host := strings.ToLower(u.Hostname())
	p := Page{Kind: PageWeb, Domain: host, PageKey: host + u.Path}
	for _, e := range blacklist {
		if !e.Enabled {
			continue
		}
		d := strings.ToLower(e.Domain)
		if host == d || strings.HasSuffix(host, "."+d) {
			p.Blacklisted = true
			p.Site = d
			if a, ok := aliases[d]; ok {
				p.Site = a
			}
			break
		}
	}
	if p.Blacklisted {
		for _, dp := range deterministicPaths {
			if (host == dp.host || strings.HasSuffix(host, "."+dp.host)) && strings.HasPrefix(u.Path, dp.prefix) {
				p.DeterministicDistraction = true
			}
		}
	}
	return p
}
