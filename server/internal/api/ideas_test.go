package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"focuscompanion/internal/domain"
	"focuscompanion/internal/storage"
)

func TestIdeasCaptureOverHTTP(t *testing.T) {
	srv, dir := newServer(t, "http://127.0.0.1:1", "")
	for _, kind := range []string{"idea", "task"} {
		code, out := post(t, srv.URL+"/api/v1/ideas", `{"type":"`+kind+`","text":"Save for later"}`)
		if code != http.StatusCreated || out["type"] != kind || out["text"] != "Save for later" || out["id"] == "" {
			t.Fatalf("%d: %v", code, out)
		}
	}
	for _, body := range []string{`{"type":"task","text":" "}`, `{"type":"step","text":"later"}`, `{`} {
		if code, _ := post(t, srv.URL+"/api/v1/ideas", body); code != http.StatusBadRequest {
			t.Fatalf("invalid capture status %d", code)
		}
	}
	resp, err := http.Get(srv.URL + "/api/v1/ideas")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var items []domain.Idea
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || len(items) != 2 {
		t.Fatalf("bad parked list: %v", items)
	}
	repo, err := storage.NewJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	state, err := repo.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Ideas) != 2 || state.Goal != nil || len(state.Steps) != 0 {
		t.Fatal("capture did not stay parked")
	}
}
