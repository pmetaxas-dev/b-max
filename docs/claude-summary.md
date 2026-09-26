# B-MAX: summary to continue the conversation (paste this into a new Claude session)

> **You are Claude, helping me (the founder of B-MAX) prepare the PITCH and finish the demo for a hackathon.**
> Answer me in **Greek**, briefly and concretely. I test things myself and paste the results back to you.
> Everything below is what we built and decided together in a long previous session. The code is in this repository
> (branch `hackathon`); start by reading `README.md` and `docs/HANDOFF.md`. Do not re-explore everything: ask me.

## 1. The project in one paragraph

**B-MAX** is a Chrome extension plus a local Go server, for people with ADHD (hackathon challenge #11, "Lost in Space, an ADHD lived experience").
**Max** (a friendly lamp character with a body, formerly called Lumi) is the user's mind put in order: the user pours out a tangle of thoughts
(typing or speaking, Greek or English), and Max turns it into **a life goal, today's tasks in one clear priority order, and an idea chest**, then
walks with the user through the day with patience. A **planet** grows toward the user's life goal. The product is **positive-only**: it never
punishes, blames or takes progress away.

The pitch sentence I want to use: *"Other ADHD apps focus on the negatives and punish; ours focuses on the positives and turns them into a weapon for the user."*
It is true for our app (see section 5), but "other apps" must be phrased carefully ("many apps"); I have not verified competitors.

## 2. What is built and working (all tested; `bash scripts/check.sh` ends with ALL CHECKS PASSED)

- **Onboarding as a chat with Max.** Scripted welcome (who Max is, what the planet is), then the life goal and today's tasks. Suggested replies sit behind a "💡 Ideas" button. Max waves hello when the app opens.
- **Dynamic prioritisation.** The AI ranks tasks on every turn (deadline, length, life goal); the server reorders by itself when a deadline gets close (Max then asks once a day "switch to it?"). **The first task starts by itself: there is no start button.** The NOW card shows one task at a time.
- **Task time is a key feature.** Max asks how long / by when, once. If the user does not know, **Max estimates from the kind of task** (an email about 10 min, studying python about 90 min) and says it as a proposal, and asks once later if the guess fits.
- **Tracking.** Max measures time on relevant pages (for browser tasks), asks "did you finish?" when the time passes, brings the user back to the page they were on when they drift to a distraction site, reminds which task has priority if they work on another, recaps yesterday's open tasks, and keeps a journal so he can answer "where was I today?".
- **Planet follows the LIFE GOAL, not the task count.** Each task has an impact 0 to 3, the goal has a size in points; a shower is worth 0. Ticking a task asks for confirmation and can be undone. New eras are celebrated. **Bad days bring clouds and lightning over the planet** (30 min on distraction sites with nothing finished, or an evening with nothing finished); it is a mood, never a penalty, and one finished task clears the sky.
- **Idea chest (💎).** A simple box to throw thoughts in (in the box, in the chat, or through the lamp). Ideas **stay** for later; each can become today's task or be deleted (after a question). Max brings an idea back when the user spends 15 min on distraction sites with nothing finished (once a day, never says why) and once a weekend between work.
- **The lamp on every website.** Click it: Max comes out with his body, listens ("what do you need?"), keeps the task/idea, and folds back. The **microphone works there too** (a hidden extension page records; the permission is asked once).
- **Voice.** Dictation (record, silence detection, Groq Whisper `whisper-large-v3`, good for Greek with English words), sent after a 3-second countdown that any click in the text cancels. Max can speak (chat and pop-ups on every tab, `chrome.tts`).
- **Accessibility for blind users and keyboard.** Spoken pop-ups (message, options, how to answer), **spoken status without AI** (`Alt+Shift+S`: task now, what follows, planet in words, weather), `Alt+Shift+M` (open Max on the page), `Alt+Shift+V` (speak to Max, or **answer a pop-up with the voice**: "yes", "not now", "take me back"), `Alt+Shift+K` (reach a pop-up's buttons), an accessibility panel (Max speaks, large text, high contrast, shortcuts), skip link, live regions, focus handling, reduced-motion support.
- **Language switch EL/EN** top right (affects Max's replies, UI, dictation, lamp, cards). **Focus sounds** (deep calm, rain, waves) made with WebAudio, dashboard only.

## 3. Architecture (for orientation)

- `server/` Go 1.24: `internal/domain` (rules, no I/O) → `application` (use cases: `chat.go`, `tasks_life.go`, `status.go`, `ideas_chest.go`) → `api` (HTTP `/api/v1/...`), `groq` (chat, planner, Whisper), `storage` (JSON file). Rules live in Go, never in JS.
- `extension/` Chrome MV3: `dashboard/` (single-page dashboard, all CSS in `dashboard.html`), `shared/chat.js`, `content/page-object.js` (the lamp on websites, Shadow DOM), `background/` (service worker: no timers allowed), `offscreen/` (hidden recording page), `shared/max/` (Max's SVG/animations), `shared/planet/planet.html` (Three.js planet, do not touch).
- AI: Groq. The chat prompt is kept short (about 1200 tokens) and only the last 10 turns are sent, because Groq rate-limits tokens per minute; `GROQ_CHAT_MODEL` lets the chat use another model than the planner; a 429 is shown to the user with the seconds to wait. Every Groq call logs its duration and token counts in the server terminal.
- AI: Groq. `GROQ_MODEL=openai/gpt-oss-120b` works (needs `reasoning_effort: low`, already in code). Run: see `README.md`. In Git Bash, paths containing `!` must be in single quotes.
- Tests: Go tests (`server`), Node tests (`extension/tests`), and I verified flows in headless Chrome with a fake Groq. **Not verified by me**: behaviour with the real Groq model in every case, audible `chrome.tts`, the keyboard shortcuts pressed in real Chrome, and any real screen reader.

## 4. Decisions and preferences I stated (respect them)

- **Minimal, calm, ADHD-friendly UI**, one focus point (the NOW card), no scrolling page. A friend will improve the visual design later, so functions come first (`docs/HANDOFF.md` explains where the UI lives and what not to break).
- **Positive only**: no punishment, streak loss, blocking, or blame. Reminders are kind; "I don't know" is fine; "not now" is fine.
- Max's name is **Max**, the app is **B-MAX**. The planet must relate to the **life goal**.
- Serious actions must be deliberate (ticking a task asks first, with undo). Ideas are never lost by accident.
- Everything important must work **by voice and keyboard for blind users**.
- I paste git commands by hand; give me `git add` / `git commit` commands, and never push for me.
- I appreciate honesty: say what is verified and what is not.

## 5. Where the pitch claim is supported (use these in the demo)

1. **The planet grows, it never dies**: finish tasks, see the new-era celebration; a rough day only brings clouds, not a lower score.
2. **The planet is the life goal**: a shower moves nothing, a real step does.
3. **"I don't know" and "not now" are welcome**: Max chooses a time himself and says "we will talk again"; "not now" saves where you were.
4. **Distractions get a way back, not a scolding**: one button takes you back to the page you were on.
5. **Max celebrates and connects each task to the dream** (the motivation line under his name, celebrations).
6. **Accessibility**: with eyes closed, `Alt+Shift+S` tells where you are and how your planet is doing; you can answer pop-ups with your voice.
7. **A mind put in order**: pour out everything; it comes back as one path, with ideas kept safe in the chest.

Suggested 3-minute demo: reset profile (⚙️) → welcome and dream by voice → two tasks (one browser, one offline) with the ranking → open YouTube and get taken back → lamp on a page: add a task with the voice → `Alt+Shift+S` with closed eyes → tick a task (confirmation) and the planet grows → toggle EL/EN. Use ⚙️ "Fast timings" to make the waits 10 times shorter for the demo.

## 6. Honest caveats to prepare answers for (likely jury questions)

- Privacy: everything is local except the AI calls to Groq (the text of chats and the recorded voice for transcription; the voice is not stored). No tracking of page content beyond keywords matching for tasks on the user's own computer.
- Evidence for ADHD claims: we did not run studies; the design follows our own research summary (`docs/MBAX_ADHD_research_summary.txt`) and lived experience. Do not claim clinical benefit.
- The AI's behaviour (tone, estimates) depends on the model; the server enforces the important rules (one question per task, no blame, progress never drops).
- The "other apps punish" statement: phrase as "many productivity apps rely on streaks, blocking and guilt"; do not name competitors unless verified.

## 7. What I want to do next with you

- Write and rehearse the **pitch** (structure, wording, the punchline above, timing, demo script, likely questions), possibly slides.
- Decide what to show live versus on a recorded fallback, in case the network or Groq fails during the demo.
- Later: my friend polishes the UI; I may ask you to review their changes with `bash scripts/check.sh`.
- One idea I left open: "teaching/lessons" (Max as a patient guide). I meant patience and step-by-step guidance, not literal lessons.

## 8. State of the repository

The code is committed on branch `hackathon` (the last work may still need `git add .` and a commit; check `git status`). `server/.go-cache/` is git-ignored; it must never be committed.
