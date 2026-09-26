package domain

import "testing"

func TestEraIndexForPercentThresholds(t *testing.T) {
	cases := []struct {
		pct  float64
		want int
	}{
		{0, 0}, {1, 0}, {20, 0},
		{20.1, 1}, {21, 1}, {40, 1},
		{40.1, 2}, {41, 2}, {60, 2},
		{60.1, 3}, {61, 3}, {80, 3},
		{80.1, 4}, {81, 4}, {100, 4},
	}
	for _, c := range cases {
		if got := EraIndexForPercent(c.pct); got != c.want {
			t.Fatalf("EraIndexForPercent(%v) = %d, want %d", c.pct, got, c.want)
		}
	}
}

func TestRecomputeProgressSetsEraName(t *testing.T) {
	s := &State{
		Milestones: []Milestone{{ID: "m1", Weight: 1}},
		Steps:      []Step{{ID: "s1", MilestoneID: "m1", Weight: 1, Status: StatusPending}},
	}
	s.RecomputeProgress()
	if s.Progress.Era != "prehistoric" {
		t.Fatalf("0%% progress era = %q, want prehistoric", s.Progress.Era)
	}
	s.Steps[0].Status = StatusDone
	s.RecomputeProgress()
	if s.Progress.Era != "space" {
		t.Fatalf("100%% progress era = %q, want space", s.Progress.Era)
	}
}

func TestEraVocabularyMatchesPlanetRuntime(t *testing.T) {
	cases := []struct {
		pct        float64
		era, label string
	}{
		{0, "prehistoric", "Prehistoric"},
		{30, "copper", "Copper"},
		{50, "medieval", "Medieval"},
		{70, "industrial", "Industrial"},
		{100, "space", "Space"},
	}
	for _, c := range cases {
		if got := EraForPercent(c.pct); got != c.era {
			t.Fatalf("EraForPercent(%v) = %q, want %q", c.pct, got, c.era)
		}
		if got := EraLabelForPercent(c.pct); got != c.label {
			t.Fatalf("EraLabelForPercent(%v) = %q, want %q", c.pct, got, c.label)
		}
	}
	if w := (&State{}).Weather(); w != WeatherClear {
		t.Fatalf("Weather() = %q, want %q", w, WeatherClear)
	}
}
