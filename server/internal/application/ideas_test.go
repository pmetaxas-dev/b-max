package application_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"focuscompanion/internal/application"
	"focuscompanion/internal/domain"
	"focuscompanion/internal/storage"
)

func TestCapturePreservesWorkflowAndPersists(t *testing.T) {
	for _, kind := range []string{"idea", "task"} {
		t.Run(kind, func(t *testing.T) {
			r := newRig(t)
			r.onboard()
			before, err := r.repo.Load()
			if err != nil {
				t.Fatal(err)
			}
			before.Steps[0].Status = domain.StatusDone
			before.RecomputeProgress()
			before.Progress.Era = "seeded-era"
			before.WorkContext = domain.WorkContext{LastWorkTabID: 7, LastWorkURL: work, FocusedMsOnStep: 42000}
			before.Session.State = domain.StateWorking
			before.Session.ContextStrength = "strong"
			before.Session.SessionFocusedMs = 42000
			stepID := before.CurrentStep().ID
			before.Anchors[stepID] = domain.ReturnAnchor{StepID: stepID, Tier: 1, URL: work, Snippet: "keep this place", CapturedAt: r.clk.Now()}
			before.Pending = &domain.PendingPrompt{Type: domain.PromptCompletion, StepID: stepID}
			before.Ideas = []domain.Idea{{ID: "existing", Type: "idea", Text: "already parked", Priority: 2, Reviewed: true, CreatedAt: r.clk.Now()}}
			if err := r.repo.Save(before); err != nil {
				t.Fatal(err)
			}
			r.app, err = application.New(r.repo, r.clk, r.pl, application.NoShield{})
			if err != nil {
				t.Fatal(err)
			}
			view := r.app.Full()
			calls := r.pl.calls
			r.pl.down = true
			r.clk.add(time.Hour)
			item, err := r.app.Capture(application.CaptureInput{Text: "  later thought  ", Type: kind})
			if err != nil {
				t.Fatal(err)
			}
			if item.ID == "" || item.Type != kind || item.Text != "later thought" || item.Reviewed || !item.CreatedAt.Equal(r.clk.Now()) {
				t.Fatalf("bad capture: %+v", item)
			}
			if r.pl.calls != calls {
				t.Fatal("capture called AI")
			}
			after, err := r.repo.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after.CurrentStep(), before.CurrentStep()) {
				t.Fatal("Current Step changed")
			}
			if !reflect.DeepEqual(after.Goal, before.Goal) {
				t.Fatal("active goal changed")
			}
			if !reflect.DeepEqual(after.WorkContext, before.WorkContext) || !reflect.DeepEqual(after.Anchors, before.Anchors) {
				t.Fatal("work context changed")
			}
			if after.Progress != before.Progress {
				t.Fatal("progress/era changed")
			}
			if !reflect.DeepEqual(r.app.Full(), view) {
				t.Fatal("plan or world view changed")
			}
			want := append(append([]domain.Idea{}, before.Ideas...), item)
			if !reflect.DeepEqual(after.Ideas, want) {
				t.Fatal("parked items not persisted")
			}
			after.Ideas = before.Ideas
			if !reflect.DeepEqual(after, before) {
				t.Fatal("capture changed state outside parked items")
			}
			restarted, err := application.New(r.repo, r.clk, r.pl, application.NoShield{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(restarted.Ideas(), want) {
				t.Fatal("items lost on restart")
			}
			items := restarted.Ideas()
			items[0].Text = "mutated caller copy"
			if !reflect.DeepEqual(restarted.Ideas(), want) {
				t.Fatal("list exposed mutable state")
			}
		})
	}
}

type failingCaptureRepo struct{ storage.Repository }

func (r failingCaptureRepo) Save(*domain.State) error { return errors.New("disk unavailable") }

func TestCaptureFailureDoesNotParkOrChangeWorkflow(t *testing.T) {
	r := newRig(t)
	r.onboard()
	app, err := application.New(failingCaptureRepo{r.repo}, r.clk, r.pl, application.NoShield{})
	if err != nil {
		t.Fatal(err)
	}
	before := app.Full()
	if _, err := app.Capture(application.CaptureInput{Text: "later", Type: "task"}); err == nil {
		t.Fatal("false success")
	}
	if len(app.Ideas()) != 0 || !reflect.DeepEqual(app.Full(), before) {
		t.Fatal("failed save changed state")
	}
	stored, err := r.repo.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Ideas) != 0 {
		t.Fatal("failed capture persisted")
	}
}

func TestCaptureValidationAndNoGoal(t *testing.T) {
	r := newRig(t)
	for _, in := range []application.CaptureInput{{Text: "  ", Type: "idea"}, {Text: "later", Type: "step"}, {Text: "later"}} {
		if _, err := r.app.Capture(in); !errors.Is(err, application.ErrInvalid) {
			t.Fatalf("expected invalid: %v", err)
		}
	}
	if len(r.app.Ideas()) != 0 {
		t.Fatal("invalid captures parked")
	}
	if _, err := r.app.Capture(application.CaptureInput{Text: "later", Type: "task"}); err != nil {
		t.Fatal(err)
	}
	if r.app.Summary().CurrentStep != nil || r.app.Summary().Goal != "" {
		t.Fatal("capture created a plan")
	}
}
