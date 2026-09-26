package domain

import (
	"net/url"
	"testing"
)

func TestClassifyPage(t *testing.T) {
	bl := DefaultBlacklist()
	cases := []struct {
		url           string
		kind          PageKind
		blacklisted   bool
		site          string
		deterministic bool
	}{
		{"https://www.youtube.com/watch?v=abc", PageWeb, true, "youtube.com", false},
		{"https://m.youtube.com/watch?v=abc", PageWeb, true, "youtube.com", false},
		{"https://old.reddit.com/r/golang", PageWeb, true, "reddit.com", false},
		{"https://twitter.com/home", PageWeb, true, "x.com", false}, // alias shares one key
		{"https://x.com/home", PageWeb, true, "x.com", false},
		{"https://notyoutube.com/", PageWeb, false, "", false}, // suffix must respect the dot
		{"https://youtube.com.evil.example/", PageWeb, false, "", false},
		{"https://docs.google.com/document/d/1", PageWeb, false, "", false},
		{"https://www.youtube.com/shorts/xyz", PageWeb, true, "youtube.com", true},
		{"https://www.instagram.com/reels/abc/", PageWeb, true, "instagram.com", true},
		{"https://www.facebook.com/reel/123", PageWeb, true, "facebook.com", true},
		{"https://www.instagram.com/p/abc/", PageWeb, true, "instagram.com", false},
		{"chrome://newtab/", PageNeutral, false, "", false},
		{"chrome-extension://abc/summary.html", PageNeutral, false, "", false},
		{"", PageNeutral, false, "", false},
	}
	for _, c := range cases {
		p := ClassifyPage(c.url, bl)
		if p.Kind != c.kind || p.Blacklisted != c.blacklisted || p.Site != c.site || p.DeterministicDistraction != c.deterministic {
			t.Errorf("%q: got %+v", c.url, p)
		}
	}
}

// Sign-in pages are neutral: no Max, no card, no time. KEEP IN SYNC with
// extension/tests/auth-pages.test.mjs.
func TestSignInPagesAreNeutral(t *testing.T) {
	bl := DefaultBlacklist()
	for u, auth := range map[string]bool{
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=x":                    true,
		"https://appleid.apple.com/auth/authorize":                                    true,
		"https://login.microsoftonline.com/common/oauth2/authorize":                   true,
		"https://www.instagram.com/accounts/login/":                                   true,
		"https://www.instagram.com/accounts/login/two_step_verification?x=1":          true,
		"https://www.facebook.com/login.php":                                          true,
		"https://www.facebook.com/dialog/oauth?client_id=1":                           true,
		"https://www.facebook.com/checkpoint/1501092823525282/":                       true,
		"https://github.com/login/oauth/authorize?client_id=1":                        true,
		"https://x.com/i/flow/login":                                                  true,
		"https://dev-123.okta.com/app/x/sso/saml":                                     true,
		"https://www.instagram.com/p/abc/":                                            false,
		"https://example.com/loginhelp":                                               false,
		"https://docs.python.org/3/library/getpass.html":                              false,
		"https://auth0.example.com/docs":                                              false, // not the auth0.com domain
		"https://developer.mozilla.org/en-US/docs/Web/HTTP/Authentication":            false,
		"https://www.google.com/search?q=login":                                       false,
		"https://notaccounts.google.com.example.org/":                                 false,
		"https://www.reddit.com/r/programming/comments/1/how_i_built_a_login_system/": false,
	} {
		parsed, _ := url.Parse(u)
		if got := IsAuthPage(parsed); got != auth {
			t.Errorf("IsAuthPage(%q) = %v, want %v", u, got, auth)
		}
		if p := ClassifyPage(u, bl); (p.Kind == PageNeutral) != auth {
			t.Errorf("ClassifyPage(%q).Kind = %s", u, p.Kind)
		}
	}
}

func TestIsSearchResults(t *testing.T) {
	for u, want := range map[string]bool{
		"https://www.google.com/search?q=reddit":      true,
		"https://www.google.gr/search?q=python":       true,
		"https://www.bing.com/search?q=x":             true,
		"https://duckduckgo.com/?q=x":                 true,
		"https://search.brave.com/search?q=x":         true,
		"https://yandex.ru/search/?text=x":            true,
		"https://www.google.com/":                     false,
		"https://docs.google.com/document/d/1":        false,
		"https://www.google.com/maps/search/cafe":     false,
		"https://duckduckgo.com/":                     false,
		"https://www.reddit.com/search/?q=python":     false, // a site's own search is that site
		"https://googleblog.example.com/search?q=abc": false,
	} {
		if got := IsSearchResults(u); got != want {
			t.Errorf("IsSearchResults(%q) = %v, want %v", u, got, want)
		}
	}
}

func TestDisabledEntryIsNotBlacklisted(t *testing.T) {
	bl := DefaultBlacklist()
	for i := range bl {
		if bl[i].Domain == "reddit.com" {
			bl[i].Enabled = false
		}
	}
	if ClassifyPage("https://reddit.com/", bl).Blacklisted {
		t.Fatal("disabled entry matched")
	}
}

func TestAdultSitesNotInDefaults(t *testing.T) {
	for _, e := range DefaultBlacklist() {
		if e.Source != "default" || !e.Enabled {
			t.Errorf("unexpected default entry %+v", e)
		}
	}
	// §7 lists eight sites; "x.com / twitter.com" is two domains, so nine entries.
	if len(DefaultBlacklist()) != 9 {
		t.Fatalf("expected 9 default entries, got %d", len(DefaultBlacklist()))
	}
}
