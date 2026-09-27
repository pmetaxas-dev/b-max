# B-MAX

A calm companion for people with ADHD. **Max**, a friendly lamp with a body, lives in a Chrome extension:
you talk to him (typing or speaking, in Greek or English), tell him your life goal and today's tasks, and he
prioritises them, follows your work, brings you back when you drift, and grows a planet that follows your goal.
It is built to be usable without looking at the screen (spoken pop-ups, spoken status, keyboard shortcuts).

Two parts: a local **Go server** (all the rules, and the AI calls to Groq) and a **Chrome extension** (the interface).

## Run

```bash
cd 'E:/2026_projects!/b-max/server'          # the Go module is in server/, not the repository root
export GROQ_API_KEY="your-groq-api-key"      # chat and microphone need it
export GROQ_MODEL="openai/gpt-oss-120b"      # optional
go run ./cmd/server                           # listens on 127.0.0.1:8787
```

1. Open `chrome://extensions`, enable **Developer mode**, click **Load unpacked**, select the `extension` folder.
2. The first open starts a chat with Max. Use ⚙️ in the dashboard for test tools (reset, fast timings, next day).

Optional variables: `GROQ_STT_MODEL` (default `whisper-large-v3`), `PORT`, `DATA_DIR`.
Check the server: <http://127.0.0.1:8787/api/v1/health>.

## Check

```bash
bash scripts/check.sh    # Go vet and tests, extension tests, code rules; must end with ALL CHECKS PASSED
```

## More

- `docs/HANDOFF.md`: what is built, where the UI lives, what not to break, a demo script (Greek).
- `architecture-plan-v2.md`, `NOTES.md`, `docs/architecture.md`: design and decisions.
