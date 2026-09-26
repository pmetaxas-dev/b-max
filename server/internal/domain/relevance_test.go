package domain

import (
	"reflect"
	"slices"
	"testing"
)

func TestCleanKeywords(t *testing.T) {
	got := CleanKeywords([]string{" Python ", "python", "www.Docs.Python.org", "go", "video", "2028", "Μεταβλητές", "list comprehension", ""})
	want := []string{"python", "docs.python.org", "μεταβλητεσ", "list comprehension"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CleanKeywords = %q, want %q", got, want)
	}
}

func TestCleanQueriesDropsURLsAndKeepsThree(t *testing.T) {
	got := CleanQueries([]string{"python για αρχάριους", "https://example.com/x", "www.site.gr", "Python  tutorial", "python tutorial", "ασκήσεις python", "extra"})
	want := []string{"python για αρχάριους", "Python tutorial", "ασκήσεις python"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CleanQueries = %q, want %q", got, want)
	}
}

func TestPageRelevant(t *testing.T) {
	kw := CleanKeywords([]string{"python", "docs.python.org", "μεταβλητές", "list comprehension"})
	for _, c := range []struct {
		url, title string
		want       bool
	}{
		{"https://docs.python.org/3/tutorial/", "3.13 Documentation", true},
		{"https://www.google.com/search?q=python+tutorial", "python tutorial - Google Search", true},
		{"https://el.wikipedia.org/wiki/Python", "Python - Βικιπαίδεια", true},
		{"https://www.youtube.com/watch?v=1", "Python for Beginners - Full Course", true},
		{"https://example.gr/a", "Οι ΜΕΤΑΒΛΗΤΕΣ στον προγραμματισμό", true}, // accents and case
		{"https://example.com/b", "A short list comprehension guide", true}, // multi-word keyword
		{"https://www.reddit.com/r/funny", "Funny pictures", false},         // unrelated
		{"https://pythonanywhere.example/", "Hosting", false},               // whole words only
		{"https://www.google.com/search?q=weather", "weather - Google Search", false},
		{"chrome://settings", "python", false}, // not a web page
	} {
		if got := PageRelevant(c.url, c.title, kw); got != c.want {
			t.Errorf("PageRelevant(%q, %q) = %v, want %v", c.url, c.title, got, c.want)
		}
	}
	// A domain keyword matches the host or a subdomain, never a lookalike.
	domainOnly := CleanKeywords([]string{"docs.python.org"})
	for u, want := range map[string]bool{
		"https://docs.python.org/3/":           true,
		"https://www.docs.python.org/":         true,
		"https://notdocs.python.org.evil.com/": false,
		"https://docs.python.org.evil.com/":    false,
	} {
		if got := PageRelevant(u, "Hosting", domainOnly); got != want {
			t.Errorf("domain keyword: PageRelevant(%q) = %v, want %v", u, got, want)
		}
	}
	if PageRelevant("https://docs.python.org/", "Python", nil) {
		t.Error("no keywords must mean nothing is relevant")
	}
}

func TestFallbackKeywordsForOlderPlans(t *testing.T) {
	s := &State{
		Goal:       &PrimaryGoal{Statement: "Become a developer by 2028"},
		Milestones: []Milestone{{ID: "m1", Title: "Learn programming fundamentals"}},
		Steps:      []Step{{ID: "s1", MilestoneID: "m1", Text: "Watch a 30-minute introductory video on Python basics"}},
	}
	kw := s.GoalKeywords()
	for _, want := range []string{"developer", "programming", "fundamentals", "python"} {
		if !contains(kw, want) {
			t.Errorf("fallback keywords %q miss %q", kw, want)
		}
	}
	for _, generic := range []string{"watch", "video", "learn", "become", "introductory", "basics", "2028"} {
		if contains(kw, generic) {
			t.Errorf("fallback keywords %q include generic %q", kw, generic)
		}
	}
	// A random YouTube video is not relevant just because it is a video.
	if PageRelevant("https://www.youtube.com/watch?v=2", "Funny cat video compilation", kw) {
		t.Error("generic words made an unrelated video relevant")
	}
	s.Goal.Keywords = []string{"golang"}
	if got := s.GoalKeywords(); !reflect.DeepEqual(got, []string{"golang"}) {
		t.Errorf("planner keywords must win over the fallback, got %q", got)
	}
}

// Regression: the real plan from manual testing. Its step words ("using",
// "data", "user", "input", "open", "review"...) made unrelated pages
// relevant, marking the day good and blocking the day question.
func TestFallbackKeywordsStayNarrowOnARealPlan(t *testing.T) {
	s := &State{
		Goal: &PrimaryGoal{Statement: "Become a developer by 2028"},
		Milestones: []Milestone{
			{ID: "m1", Title: "Learn programming fundamentals"}, {ID: "m2", Title: "Build a portfolio"},
			{ID: "m3", Title: "Contribute to open source"}, {ID: "m4", Title: "Apply for developer roles"},
		},
		Steps: []Step{
			{MilestoneID: "m1", Text: "Watch a 30‑minute introductory video on Python basics"},
			{MilestoneID: "m1", Text: "Read and take notes on Chapter 1 of an online Python tutorial"},
			{MilestoneID: "m1", Text: "Complete 5 simple coding exercises on variables and data types"},
			{MilestoneID: "m1", Text: "Write a short script that reads user input and prints a greeting"},
			{MilestoneID: "m1", Text: "Review and refactor the script using functions"},
		},
	}
	kw := s.GoalKeywords()
	for _, generic := range []string{"using", "data", "user", "input", "open", "source", "review", "build", "types", "apply", "roles", "reads", "prints", "script", "greeting", "tutorial", "coding"} {
		if contains(kw, generic) {
			t.Errorf("fallback keywords %q include generic %q", kw, generic)
		}
	}
	for _, want := range []string{"python", "developer", "programming"} {
		if !contains(kw, want) {
			t.Errorf("fallback keywords %q miss %q", kw, want)
		}
	}
	for _, page := range [][2]string{
		{"https://www.reddit.com/r/all", "User reviews of open source data tools"},
		{"https://www.google.com/search?q=reddit", "reddit - Google Search"},
		{"https://news.example.com/", "Using the new input types in your build"},
	} {
		if PageRelevant(page[0], page[1], kw) {
			t.Errorf("unrelated page counted as relevant: %s %q", page[0], page[1])
		}
	}
	if !PageRelevant("https://www.google.com/search?q=python+basics", "python basics - Google Search", kw) {
		t.Error("a Python search must still be relevant")
	}
}

func TestSubredditNames(t *testing.T) {
	kw := CleanKeywords([]string{"python", "golang", "go", "docs.python.org"})
	for u, want := range map[string]bool{
		"https://www.reddit.com/r/learnpython/comments/1/help/": true,
		"https://old.reddit.com/r/Python/":                      true,
		"https://www.reddit.com/r/golang":                       true,
		"https://www.reddit.com/r/funny/":                       false,
		"https://www.reddit.com/r/gonewild/":                    false, // "go" is too short to match inside a name
		"https://www.reddit.com/user/learnpythonfan/":           false, // run-together names only for subreddits
		"https://example.com/r/learnpython":                     false, // only Reddit
	} {
		if got := PageRelevant(u, "", kw); got != want {
			t.Errorf("PageRelevant(%q) = %v, want %v", u, got, want)
		}
	}
}

func TestProperNounsFromSteps(t *testing.T) {
	if got := properNouns("Install SQL Server and learn Excel, then Git."); !reflect.DeepEqual(got, []string{"SQL", "Server", "Excel", "Git"}) {
		t.Fatalf("got %q", got)
	}
	if got := properNouns("Python basics"); len(got) != 0 {
		t.Fatalf("the first word is never a proper noun by capitalisation: %q", got)
	}
}

func TestSearchQueriesFallback(t *testing.T) {
	s := &State{
		Goal:       &PrimaryGoal{Statement: "Become a developer by 2028"},
		Milestones: []Milestone{{ID: "m1", Title: "Learn programming fundamentals"}},
	}
	step := &Step{ID: "s1", MilestoneID: "m1", Text: "Watch an intro video on Python basics"}
	want := []string{"Watch an intro video on Python basics", "Learn programming fundamentals", "Become a developer by 2028"}
	if got := s.SearchQueries(step); !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback queries = %q, want %q", got, want)
	}
	step.SearchQueries = []string{"python για αρχάριους"}
	if got := s.SearchQueries(step); !reflect.DeepEqual(got, step.SearchQueries) {
		t.Fatalf("planner queries must win, got %q", got)
	}
	if s.SearchQueries(nil) != nil {
		t.Fatal("no step, no queries")
	}
}

func TestBuildStepsKeepsCleanQueries(t *testing.T) {
	steps := BuildSteps(Milestone{ID: "m1", Weight: 1}, []RawStep{{Text: "a", SearchQueries: []string{"python basics", "http://x.y"}}})
	if !reflect.DeepEqual(steps[0].SearchQueries, []string{"python basics"}) {
		t.Fatalf("got %q", steps[0].SearchQueries)
	}
}

func contains(list []string, s string) bool { return slices.Contains(list, s) }
