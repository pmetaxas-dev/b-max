# NOTES — Phase 2 implementation

Scope: `architecture-plan-v2.md` (v2.1) §21 Phase 2 — onboarding (four questions, goal, voice recording), plan generation, browser events, deterministic session state, blacklist, grace, return screen, step confirmation, return anchor capture. Nothing from Phase 3+ was built. Where I thought the design was wrong I left it as specified and recorded it here.

## What was and was not verified

**Verified (run in this session):**
- `go vet` + `go test ./...` — domain rules, application flow, and HTTP handlers (real sockets via `httptest`, real Groq client against a fake Groq server, including one repair request, Groq unreachable, and no API key).
- `node --test extension/tests/anchor.test.mjs` — anchor tier logic and the sensitive-field rule.
- `scripts/check-extension.mjs` — every extension module parses as an ES module, every path in `manifest.json` exists, every relative import resolves.
- `grep` — no `setTimeout(`/`setInterval(` in the service worker or anything it imports; no `<div>`/`<span>` used as a control.

**Not verified — be sceptical of these:**
- **The extension has never run in Chrome.** No browser is installed here. Content-script injection through the dynamic-import loader, Shadow DOM rendering, MV3 worker wake-up, `chrome.alarms`, `chrome.action.openPopup`, `MediaRecorder`, and cross-origin `fetch` from the worker are all untested. Expect first-load bugs.
- **The keyboard-only flow and screen-reader announcements** (§12 test, acceptance criterion 7) were not exercised. The markup follows the rules; nobody has tabbed through it.
- **Real Groq was never called** (no key). The prompts in `server/internal/groq/planner.go` are drafts checked only against a fake.
- **The built server binary was not started.** The shell allowlist blocked running it, so `main.go` (loopback bind, env handling) is only compile-checked.
- **`scripts/check.sh` was not run as a script** (`bash` is not on the allowlist here); its individual commands were run one by one. Run `bash scripts/check.sh` yourself.

## Scope notes

- **Phase 1 did not exist.** The repo had only the two documents. I built the minimum Phase 2 needs to run: `manifest.json`, a Go server with `GET /health`, the worker wiring, and the "Server unavailable" state.
- **The documents live at the repo root** (`architecture-plan-v2.md`, `mvp(1).md`), not under `docs/` as §18 and the task text say. I did not move them.
- **`lib/` was not created.** Phase 2 has no third-party dependency; Three.js (the only vendored library) is Phase 3.
- **No extension icon.** §4.3 says the toolbar icon is the planet; that is Phase 3.
- **Not built (later phases):** Relevance/Groq shield (an interface with a `NoShield` that answers `UNKNOWN` exists), strong work context, lamp counting and idea capture, treasure, playback of voice, recovery and difficult-day, day summary, App Clock (only a `Clock` interface), demo shortcut, eras/outfits/Three.js, origin allowlist (§20 puts it in Phase 5), blacklist editing (see "Impractical" 6).
- I did not commit anything.

## Every place I had to guess, and what I chose

### Contract and transport
1. **Event payload.** Each event carries a full snapshot, not just a signal: `{type, tabId, url, title, focused}`. §14 says "one event per browser signal" and "raw signals", but a bare `window_focus` cannot tell Go which tab is active. `type` is validated and used only for the heartbeat step-refill; `tab_removed` is treated as an ordinary snapshot.
2. **Server port** `8787` (env `PORT`). The extension hardcodes it in `extension/shared/api.js`; `host_permissions` allows any port on `127.0.0.1`.
3. **No origin check and no CORS headers**, per §20. I assumed extension `host_permissions` bypasses CORS; not verified in Chrome.
4. **Incognito windows** are reported to the server as an empty page, so nothing from them is sent.
5. **`captureAnchor` flag.** §9 says never capture on blacklisted pages, but the content script must not know the blacklist ("business rules never in JavaScript"). I added a boolean `payload.captureAnchor` to every event response; the worker relays it to the tab, and revokes it when a tab starts loading. Go re-checks every rule when the anchor arrives.
6. **Undo** is sent as a fourth `choice` (`undo`) on `POST /ramp/respond`. §14 lists only Continue / Browse / Not today for that route. For "I want to browse", undo is effectively a no-op because the day's record already exists and an unanswered screen is not re-armed.
7. **Continue** returns `lastWorkTabId`, `lastWorkUrl`, `anchorUrl` from the server and the worker performs the tab switch. It does not check that the old tab id still shows a work page (tab ids can be stale after a browser restart).

### Session, grace, blacklist
8. **Neutral pages** (`chrome://`, extension pages, `about:`, empty) are neither work nor distraction: they change nothing except ending a pause. §5.2 says "non-blacklisted tab", which would make `chrome://newtab` start a work context.
9. **Any focused non-blacklisted web event refreshes `lastRelevantActivityAt`**, including heartbeats. There is no real input signal in the spec, so `WORKING → IDLE` fires only when events stop (Chrome closed, machine asleep) or after 15 minutes on blacklisted/neutral pages.
10. **Grace** = wall-clock time since the episode started, minus time Chrome was unfocused. Time on a short (<60 s) work-page return counts. An episode belongs to one blacklist site; moving to another site, or back to the first, starts a new episode (§8 table).
11. **The 60-second rule** is checked on events, so with the 1-minute heartbeat the real reset takes 60–120 s.
12. **DISTRACTED → PAUSED → WORKING**: coming back to Chrome on a work page goes straight to `WORKING` (per the diagram), though the episode is kept until 60 continuous seconds.
13. **Site key** = the matching blacklist entry's domain after alias resolution (`twitter.com` → `x.com`). §7 says "registrable domain"; Go's standard library has no public-suffix list, so I match the blacklist entry as a suffix at a dot boundary. Equivalent for the shipped entries.
14. **"If they met their time"** (§8): I read it as *focused time on non-blacklisted pages in the current session ≥ the onboarding work-block minutes*. "Confirmed" is not read as step confirmation. It suppresses only the return screen, not the completion prompt. Free-text answers use the first integer; if there is none the rule never applies.
15. **Segment credit cap of 180 s** (not in the spec). Without it a laptop that sleeps credits the whole gap as focused work.
16. **Segments** are stored (capped at 1000, not in the spec) but no rule reads them; focused time is accumulated as each interval closes.
17. **Order of the return-screen checks:** work context → grace → Not today → session-time-met → once per site per day → shield. `session-time-met` is my addition to the §8 flowchart.

### Plan, completion, progress
18. **Groq:** model `llama-3.3-70b-versatile` (env `GROQ_MODEL`), JSON mode, temperature 0.3, endpoint `/openai/v1/chat/completions`, prompts asking for 3–6 milestones and 3–7 steps (**not validated**). Only the goal sentence and today's date are sent.
19. **Invalid plan** adds one rule to §9b's list: the first milestone must have at least one step (otherwise there is no current step). One repair request, then a retryable `plan_unavailable` error; "retry" is the user pressing the button again, which repeats the whole call.
20. **Step defaults:** missing/≤0 duration → 10 min, cap 480; text clipped to 200 chars; an invalid `scheduledFor` is dropped silently. Groq is asked for `scheduledFor`; if it omits it, This Week and Today are simply empty.
21. **Week** = Monday–Sunday in the server's local time. **Today** = `scheduledFor` equals today. Current step = first pending step in milestone order, independent of both.
22. **Step counts.** Only the current milestone's steps exist (§9b), so "Step X of N" counts within the current milestone, and the announcement is `Step 2 of 5 complete.` (plus `Milestone complete: …`). Era is omitted from the announcement (Phase 3).
23. **`PlanetVisualState` in Phase 2** contains only `progress` (0–1); `era`, `parameters`, `unlockedSignatureElements` are empty. Progress = confirmed weight ÷ total weight, computed on "Yes" — Phase 3's "weighted progress" is only this one division.
24. **Completion prompt.** Created when focused time on the current step ≥ 80% of its estimate; delivered on the next event on a non-blacklisted web page. This merges §9b's "leaves the work context after the threshold" and "next heartbeat if they stay" into one rule. `PendingPrompt.shownAt` is an extra field so a heartbeat does not re-show it. "Not yet" snoozes 30 minutes, then it may show again. Expiry at end of day also resets the step's accumulated time so it is not recreated immediately. Time before a step becomes current is never credited to it.
25. **Greeting** (`SHOW_GREETING`): first event of the day on a focused non-blacklisted web page; lower priority than return screen and completion prompt; unaffected by Not today.
26. **Onboarding answers.** All four are stored; only the work-block answer is used (guess 14). A second onboarding is refused with 409 while a goal is active; it is allowed after completion and replaces the plan. There is no abandon route.
27. **Steps for the next milestone** are generated inside `POST /steps/respond` (can block up to 60 s) and retried by the heartbeat if Groq is down.

### Anchor
28. **Update rule:** a new anchor replaces the stored one if `new.tier ≤ stored.tier` (§9 "equal or higher"). `scrollPercent` is stored for every tier, not only tier 3. A new step starts with no anchor; the previous step's is not carried over.
29. **Extraction:** text-like inputs are `text`, `search`, `url` (not `email`/`tel`); textarea and contenteditable supported; shadow-DOM active element is followed; anything inside `aria-hidden` is skipped; a sensitive focused field also blocks selection capture. Only the top frame is read, which is how cross-origin iframes are excluded.
30. **Snippets:** tier 1 keeps the last 120 characters, other tiers the first 120; whitespace collapsed; Go truncates again.
31. **Capture cadence:** the 30-second tick re-asks the worker for permission before reading; `visibilitychange` and `beforeunload` use the last known permission because they must be synchronous.

### UI
32. **English only.** The MVP is written in Greek; all strings are inline English.
33. **`Alt+Shift+K`** (`chrome.commands`) moves focus to the bubble. §4.2/§12 require the bubble to be "reachable by shortcut", but Phase 2 lists no shortcut; without one a keyboard user could not reach it. It is not the capture shortcut (Phase 4).
34. **Voice** is recorded on the full tab with `MediaRecorder`, up to 10 s, and uploaded after the plan is built (a voice file needs a goal to attach to). Accepted types: webm/ogg/mp4/mpeg, max 5 MB. No playback route exists.
35. **The onboarding tab opens automatically on install.** Not specified.
36. **`focus-companion` host element and the resting button.** §4.2's resting state is the lamp; lamp brightness is Phase 4, so Phase 2 ships a plain resting button that opens the summary.

### Persistence
37. **`state.json` is written on every event** (each heartbeat and tab switch). I hold one in-memory state behind a mutex: §13 says "no lock manager", which I read as being about files, but concurrent HTTP handlers need it. A failed write is logged and `GET /health` reports `persistence: error`.

## Impractical or self-contradictory in the spec

1. **"Dropped, not queued" versus state written at issue time.** §14: a command with no receiving tab is dropped. But the intervention record is written when the screen is shown (§8), and the greeting and completion flags are set when issued. A dropped `SHOW_RAMP` therefore uses up that site's only chance for the day; a dropped greeting is lost for the day. The completion prompt survives only because the summary and full tab also show it. Fixing this needs either an acknowledgement from the extension or a definition of "shown" as "delivered".
2. **No user-activity signal.** "Inactivity timeout" and "work context" have no source of real input. A heartbeat proves only that Chrome is open. `chrome.idle` or input events from the content script would be needed for `WORKING → IDLE` to mean anything.
3. **"Step 4 of 8 complete"** (§12) cannot be produced when steps are generated one milestone at a time (§9b); the total is unknown.
4. **The DISTRACTED → WORKING 60-second rule** has 60–120 s of real resolution with a 1-minute alarm (Chrome's alarm floor).
5. **`beforeunload` capture** (§9) is asynchronous message passing in a handler that gives no time to finish, and the listener disables the back/forward cache. `visibilitychange` alone would be more reliable.
6. **Blacklist editing.** §7 lets the user add or remove entries "in the full tab", but §14 has no blacklist route and the brief forbids extra endpoints. The full tab lists entries read-only. Domain validation ("anything else is rejected at input") therefore has no input to apply to.
7. **Registrable-domain matching** (§7) needs a public-suffix list; the standard library has none and adding a Go dependency was not in scope. See guess 13.
8. **No-bundler content scripts.** Manifest content scripts cannot be modules, so a one-line classic loader dynamically imports the real module, which must be `web_accessible_resources` on every http(s) site. That lets any site detect the extension.
9. **Alarm/`AbortSignal.timeout` and "no timers."** The worker has no `setTimeout`/`setInterval`, but request timeouts use `AbortSignal.timeout`. If the intent is "no timers at all in the worker", that is one more place to check.
10. **Undo (5 s) and Not today** are server state; the spec's endpoint list has no undo. See guess 6.

## What I would need from the documents to do this correctly

- A **delivery-acknowledgement rule**: is a return screen "shown" when Go issues it or when a tab confirms display? (Impractical 1.)
- A **user-activity signal** for inactivity (`chrome.idle`, or input events), and whether "work context" should require it.
- Whether the **Relevance/shield** service is in Phase 2. Phase 2's list omits it, yet §8's flowchart and acceptance criterion 4 mention it.
- The **definition of "session"** and of "confirmed focused work" in §8's "if they met their time".
- The **exact `Today` semantics** when nothing is scheduled, and whether Groq must produce `scheduledFor` dates.
- The **Groq contract**: model, prompt, milestone/step counts, and whether those counts are validation rules.
- A **blacklist route** (or explicit permission to add one), and whether `undo` may be a choice on `/ramp/respond`.
- A decision on **step counts in announcements** when only one milestone has steps.
- **Which UI language** to ship (the MVP is Greek, the architecture English).
- What **"first tab of the day"** means when Chrome restores tabs at startup, and whether a greeting is allowed on a restored tab.
- The **keyboard shortcut** for reaching the bubble, and whether it belongs to Phase 2.
- Whether `docs/` is the intended location of the two documents.
- The pre-hackathon **prompt drafts** (§20 item 4) so the plan prompt is not invented here.

## Running it

```
cd server && GROQ_API_KEY=... go run ./cmd/server        # 127.0.0.1:8787, data in ./data (git-ignored)
# Chrome: chrome://extensions → Developer mode → Load unpacked → extension/
bash scripts/check.sh                                     # all static checks and tests
```
