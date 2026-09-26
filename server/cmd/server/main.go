// Command server is the local Go HTTP server. It listens on 127.0.0.1 only.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"focuscompanion/internal/api"
	"focuscompanion/internal/application"
	"focuscompanion/internal/clock"
	"focuscompanion/internal/groq"
	"focuscompanion/internal/storage"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	repo, err := storage.NewJSON(env("DATA_DIR", "./data"))
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	// The key exists only in this process's environment (§16). With no key the
	// server still runs; onboarding reports a retryable error and everything
	// else works.
	client := groq.NewClient(
		env("GROQ_BASE_URL", "https://api.groq.com"),
		os.Getenv("GROQ_API_KEY"),
		env("GROQ_MODEL", "llama-3.3-70b-versatile"),
	)
	app, err := application.New(repo, clock.Real{}, groq.NewPlanner(client), application.NoShield{})
	if err != nil {
		log.Fatalf("state: %v", err)
	}
	// The chat can use a faster model than the planner: GROQ_CHAT_MODEL (each
	// model has its own per-minute limit on Groq, so it also spreads the load).
	chatClient := groq.NewClient(env("GROQ_BASE_URL", "https://api.groq.com"), os.Getenv("GROQ_API_KEY"), env("GROQ_CHAT_MODEL", env("GROQ_MODEL", "llama-3.3-70b-versatile")))
	app.SetChatter(groq.NewChatter(chatClient))
	app.SetTranscriber(groq.NewTranscriber(client, env("GROQ_STT_MODEL", "whisper-large-v3")))
	addr := "127.0.0.1:" + env("PORT", "8787")
	srv := &http.Server{
		Addr:              addr,
		Handler:           api.New(app),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("listening on http://%s", addr)
	log.Fatal(srv.ListenAndServe())
}
