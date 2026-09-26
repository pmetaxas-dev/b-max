package application_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"focuscompanion/internal/application"
	"focuscompanion/internal/domain"
)

// Metadata as a shield (MVP §4): a video with a generic title ("Lesson 3")
// from a channel about the goal is recognised once its channel/description
// is read from the page.

const lesson = "https://www.youtube.com/watch?v=lesson3"

func TestAGenericVideoFromAGoalChannelIsShielded(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, unrelated, true)
	d := r.evTitle(time.Minute, lesson, "Lesson 3 - YouTube")
	if !d.Payload.ReadMetadata {
		t.Fatal("reading metadata must be allowed on a blacklisted page")
	}
	if err := r.app.StoreMetadata(application.MetadataInput{URL: lesson, Channel: "Corey Schafer", Description: "Python Tutorial for Beginners: variables and loops"}); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if d := r.evTitle(time.Minute, lesson, "Lesson 3 - YouTube"); d.Command == domain.CmdShowRamp {
			t.Fatal("return screen on a Python lesson recognised by its description")
		}
	}
	if s := r.app.Summary(); s.Day.RelevantMinutes < 9 {
		t.Fatalf("the lesson did not count toward the goal: %d minutes", s.Day.RelevantMinutes)
	}
}

func TestWithoutMetadataAGenericVideoIsNotShielded(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, unrelated, true)
	r.evTitle(time.Minute, lesson, "Lesson 3 - YouTube")
	if err := r.app.StoreMetadata(application.MetadataInput{URL: lesson, Channel: "Cooking Daily", Description: "Lesson 3: knife skills"}); err != nil {
		t.Fatal(err)
	}
	expect(t, r.evTitle(6*time.Minute, lesson, "Lesson 3 - YouTube"), domain.CmdShowRamp, "")
}

func TestMetadataIsOnlyForBlacklistedPagesAndNeverPersisted(t *testing.T) {
	r := dayRig(t)
	for _, u := range []string{unrelated, "https://www.youtube.com/shorts/abc", "not a url", "chrome://settings"} {
		if err := r.app.StoreMetadata(application.MetadataInput{URL: u, Description: "python"}); !errors.Is(err, application.ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", u, err)
		}
	}
	if d := r.ev(0, domain.EvTabActivated, unrelated, true); d.Payload.ReadMetadata {
		t.Fatal("reading metadata allowed on a work page")
	}
	if d := r.ev(time.Minute, domain.EvTabActivated, "https://www.youtube.com/shorts/abc", true); d.Payload.ReadMetadata {
		t.Fatal("reading metadata allowed on shorts")
	}
	secret := "UniqueDescriptionText123"
	if err := r.app.StoreMetadata(application.MetadataInput{URL: lesson, Description: secret}); err != nil {
		t.Fatal(err)
	}
	r.evTitle(time.Minute, lesson, "Lesson 3")
	b, err := os.ReadFile(filepath.Join(r.repo.Dir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) {
		t.Fatal("page metadata was written to state.json")
	}
}

func TestMetadataExpires(t *testing.T) {
	r := dayRig(t)
	r.ev(0, domain.EvTabActivated, unrelated, true)
	if err := r.app.StoreMetadata(application.MetadataInput{URL: lesson, Description: "Python loops"}); err != nil {
		t.Fatal(err)
	}
	r.clk.add(31 * time.Minute)
	r.ev(0, domain.EvTabActivated, unrelated, true)
	r.evTitle(time.Minute, lesson, "Lesson 3")
	expect(t, r.evTitle(6*time.Minute, lesson, "Lesson 3"), domain.CmdShowRamp, "")
}
