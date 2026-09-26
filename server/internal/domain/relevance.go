package domain

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Relevance: is a page about the user's goal? Decided here, locally, by
// matching the goal's keywords against the page's domain, path and title.
// No page the user visits is ever sent anywhere. Keywords come from the
// planner (Goal.Keywords); plans made before keywords existed fall back to
// words taken from the goal, milestones and steps themselves.

const (
	maxKeywords     = 40
	maxKeywordRunes = 40
	minKeywordRunes = 3
	maxQueries      = 3
	maxQueryRunes   = 80
)

// genericWords never make a page relevant on their own: they appear in most
// step texts ("watch a video", "read chapter 1") and in most page titles.
var genericWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`
		the and for with from into your you that this what how why when about over under
		watch video videos read reading write writing learn learning make create take notes note
		complete simple short first basic basics introductory introduction intro online free
		minute minutes hour hours day days week weeks month months year years chapter exercise exercises
		become goal goals step steps plan project projects work working start small new best top guide
		using use used user users data input output open source build building review reviews apply roles role
		type types read reads print prints script scripts greeting simple short long basic advanced practice
		improve understand study course courses lesson lessons tips book books time daily weekly level skills
		skill career team job jobs get make made find help home page news more other some first second third
		good great easy hard part parts full real life world people thing things way ways list lists`) {
		genericWords[normalizeText(w)] = true
	}
	for _, w := range strings.Fields(`
		και για απο από στο στη στην στον στα στις τους την τον της του των ενα ένα μια μία με σε να θα
		δες διάβασε διαβασε γράψε γραψε μάθε μαθε μάθω μαθω κάνε κανε ολοκλήρωσε ολοκληρωσε βίντεο βιντεο
		λεπτά λεπτα ώρα ωρα μέρα μερα εβδομάδα εβδομαδα μήνα μηνα στόχος στοχος βήμα βημα σημειώσεις σημειωσεις
		εισαγωγή εισαγωγη εισαγωγικό εισαγωγικο βασικά βασικα απλό απλο μικρό μικρο πρώτο πρωτο νέο νεο θέλω θελω`) {
		genericWords[normalizeText(w)] = true
	}
}

// normalizeText lower-cases, drops Greek accents and diaeresis, and turns
// every run of non letters/digits into one space, so "Μεταβλητές (Python)"
// and "μεταβλητες python" compare equal.
func normalizeText(s string) string {
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		r = stripGreekAccent(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

func stripGreekAccent(r rune) rune {
	switch r {
	case 'ά':
		return 'α'
	case 'έ':
		return 'ε'
	case 'ή':
		return 'η'
	case 'ί', 'ϊ', 'ΐ':
		return 'ι'
	case 'ό':
		return 'ο'
	case 'ύ', 'ϋ', 'ΰ':
		return 'υ'
	case 'ώ':
		return 'ω'
	case 'ς':
		return 'σ'
	}
	return r
}

// CleanKeywords keeps planner keywords that can be matched safely: trimmed,
// lower-cased, deduplicated, at least 3 letters, not generic, at most 40.
// Domains ("docs.python.org") are kept as domains.
func CleanKeywords(raw []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range raw {
		k = strings.ToLower(strings.TrimSpace(k))
		if isDomainKeyword(k) {
			k = strings.TrimPrefix(k, "www.")
		} else {
			k = normalizeText(k)
		}
		n := utf8.RuneCountInString(k)
		if n < minKeywordRunes || n > maxKeywordRunes || genericWords[k] || seen[k] || isNumber(k) {
			continue
		}
		seen[k] = true
		out = append(out, k)
		if len(out) == maxKeywords {
			break
		}
	}
	return out
}

func isDomainKeyword(k string) bool {
	return strings.Contains(k, ".") && !strings.ContainsAny(k, " /:")
}

func isNumber(k string) bool {
	for _, r := range k {
		if !unicode.IsDigit(r) && r != ' ' {
			return false
		}
	}
	return true
}

// CleanQueries keeps up to three search phrases. A phrase that looks like a
// URL is dropped: the planner writes search words, never links.
func CleanQueries(raw []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, q := range raw {
		q = strings.Join(strings.Fields(q), " ")
		low := strings.ToLower(q)
		if q == "" || strings.Contains(low, "://") || strings.HasPrefix(low, "www.") || seen[low] {
			continue
		}
		if utf8.RuneCountInString(q) > maxQueryRunes {
			q = string([]rune(q)[:maxQueryRunes])
		}
		seen[low] = true
		out = append(out, q)
		if len(out) == maxQueries {
			break
		}
	}
	return out
}

// FallbackKeywords derives keywords for a plan the planner gave none, until
// the background request for real ones succeeds (application BackfillKeywords).
// It must stay narrow: a generic word ("using", "data", "open") would make
// countless unrelated pages "relevant", wrongly marking the day good and
// silencing the return screen. So it keeps the specific words of the goal and
// milestone titles, and from step texts only proper nouns ("Python"), which
// name the subject; ordinary step words ("reads user input") are dropped.
func FallbackKeywords(core, steps []string) []string {
	var words []string
	for _, t := range core {
		for _, w := range strings.Fields(normalizeText(t)) {
			if utf8.RuneCountInString(w) >= 4 { // short core words are rarely specific
				words = append(words, w)
			}
		}
	}
	for _, t := range steps {
		words = append(words, properNouns(t)...) // "SQL", "Python"
	}
	return CleanKeywords(words)
}

// properNouns are the capitalised words of a sentence other than its first.
func properNouns(text string) []string {
	var out []string
	for i, w := range strings.Fields(text) {
		w = strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		if i == 0 || w == "" {
			continue
		}
		if r, _ := utf8.DecodeRuneInString(w); unicode.IsUpper(r) {
			out = append(out, w)
		}
	}
	return out
}

// GoalKeywords are the keywords relevance is matched against.
func (s *State) GoalKeywords() []string {
	if s.Goal == nil {
		return nil
	}
	if len(s.Goal.Keywords) > 0 {
		return s.Goal.Keywords
	}
	core := []string{s.Goal.Statement}
	for _, m := range s.Milestones {
		core = append(core, m.Title)
	}
	var steps []string
	for _, st := range s.Steps {
		steps = append(steps, st.Text)
	}
	return FallbackKeywords(core, steps)
}

// SearchQueries are the three "where do I start" searches for a step: the
// planner's own, or, for steps made before those existed, the step, its
// milestone and the goal.
func (s *State) SearchQueries(step *Step) []string {
	if step == nil {
		return nil
	}
	if len(step.SearchQueries) > 0 {
		return step.SearchQueries
	}
	texts := []string{step.Text}
	if m := s.MilestoneByID(step.MilestoneID); m != nil {
		texts = append(texts, m.Title)
	}
	if s.Goal != nil {
		texts = append(texts, s.Goal.Statement)
	}
	return CleanQueries(texts)
}

// subredditMatches: subreddit names run words together ("learnpython"), so
// whole-word matching misses them. Only there, a keyword of 4+ letters may
// appear anywhere in the name. ("go" never matches "golang"; "python" does
// match "learnpython" and, accepted, "montypython".)
func subredditMatches(host, path string, keywords []string) bool {
	if host != "reddit.com" && !strings.HasSuffix(host, ".reddit.com") {
		return false
	}
	rest, ok := strings.CutPrefix(strings.ToLower(path), "/r/")
	if !ok {
		return false
	}
	name, _, _ := strings.Cut(rest, "/")
	for _, k := range keywords {
		if utf8.RuneCountInString(k) >= 4 && !strings.ContainsAny(k, " .") && strings.Contains(name, k) {
			return true
		}
	}
	return false
}

// PageRelevant reports whether a web page matches any keyword: a domain
// keyword by host (or a parent domain), any other keyword as whole words in
// the host, path or title. Never true for non-web pages.
func PageRelevant(rawURL, title string, keywords []string) bool {
	if len(keywords) == 0 {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if subredditMatches(host, u.Path, keywords) {
		return true
	}
	path, _ := url.PathUnescape(u.Path)
	query := u.Query().Get("q") // search pages: the query is what the page is about
	hay := " " + normalizeText(host+" "+path+" "+query+" "+title) + " "
	for _, k := range keywords {
		if isDomainKeyword(k) {
			if host == k || strings.HasSuffix(host, "."+k) {
				return true
			}
			continue
		}
		if strings.Contains(hay, " "+k+" ") {
			return true
		}
	}
	return false
}
