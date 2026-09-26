# Lumi head SVG

`lumi.svg` is the head-only Lumi (bulb + collar): the floating lamp, which is Lumi's permanent idle form. It is displayed with a ~45px visible bulb and never changes size (see "Lamp size"). It is drawn from `lumi-design.png`, the approved reference. The default face is the reference's "Happy" expression. `lumi-body.svg` is the separate neck-down body that comes out of the lamp during an intervention (see "Full-body layer"); the lamp is its head.

- Pure vector: paths, ellipses, gradients. No raster data, no scripts. The only filter is one small `feGaussianBlur` (`lumi-eye-blur`) used for the eye glow.
- `viewBox="0 0 300 300"` with no `width`/`height`, so it scales to whatever box you give it. Bulb centre is (150,150), radius 100. The margin around the bulb holds the aura.
- All ids are prefixed `lumi-` so they are safe to target from CSS/JS.

## Group map (back to front)

| Group id | What it is | Children |
|---|---|---|
| `lumi-aura` | Everything glow-related. Sits behind the bulb. | `lumi-aura-halo` (radial-gradient circle), `lumi-aura-rays` (5 strokes: `lumi-ray-top`, `-tr`, `-tl`, `-r`, `-l`) |
| `lumi-bulb` | The glass. | `lumi-bulb-glass`, `lumi-bulb-filament`, `lumi-bulb-highlights`, `lumi-bulb-rim` |
| `lumi-face` | Everything on the face. | see below |
| `lumi-face-plate` | Dark rounded plate. | `lumi-face-plate-shape`, `lumi-face-plate-sheen` |
| `lumi-eyebrows` | Small brown arcs on the glass above the plate. | `lumi-brow-left`, `lumi-brow-right` |
| `lumi-eyes` | Glowing pale-yellow eyes. | `lumi-eye-left`, `lumi-eye-right`. Each holds a `.glow` path (wide, faint) and a `.core` path (thin, bright). |
| `lumi-mouth` | Empty by default (Happy has no mouth). | `lumi-mouth-smile`, `lumi-mouth-open`, both `display="none"` |
| `lumi-collar` | Metal screw base, drawn in front of the bulb neck. | `lumi-collar-ring`, `-body`, `-ridges`, `-highlight`, `-tip` |

`lumi-root` wraps everything if you need to move or scale the whole mascot.

Paint order matters: aura, then bulb, then face, then collar. Face groups do not overlap the bulb rim, so `lumi-face` can be transformed on its own without clipping.

## Idle animation

Files: `lumi.svg` (lamp artwork, never edited for animation), `lumi-body.svg` (full-body character), `lumi.css` (static look, idle motion, lamp size, body layer), `lumi.js` (blink timing, talking timing, emerge/return), `demo.html` (visual check).

| Target | Motion |
|---|---|
| `#lumi-root` | Slow float: up 8 SVG units and back, `ease-in-out`, 5.2s loop. The aura, bulb, face and collar move together. 8 units is about 1.8px at the 67.5px lamp box, and scales with size. |
| `#lumi-aura-halo` | Breathing: scale 1 to 1.05 and opacity 0.8 to 1, 4.6s loop. It sits behind the glass, so the bulb never changes size. |
| `#lumi-aura-rays` | Opacity 0.65 to 1, in sync with the halo. |
| `#lumi-eyes` | Blink: Y scale to 0.1 about the eye baseline (y=181) and back, 190ms, one shot. `lumi.js` triggers it at random 3.5-8s intervals, with an occasional quick double blink. The eyes return to the normal Happy shape. |

Nothing else is animated: bulb, face plate, brows, mouth and collar are untouched.

Use it (SVG must be inline):

```html
<link rel="stylesheet" href="lumi.css">
<script src="lumi.js"></script>
<script>
  // `host` is any element you injected the lumi.svg markup into
  const idle = startLumiIdle(host.querySelector('svg'));
  // idle.blink()  force a blink;  idle.stop()  remove idle motion
</script>
```

Tuning: override `--lumi-float-distance`, `--lumi-float-duration`, `--lumi-aura-duration` and `--lumi-blink-duration` on the `<svg>` (they are set on `.lumi-idle`).

Reduced motion: all keyframes live inside `@media (prefers-reduced-motion: no-preference)`, and `lumi.js` schedules no blinks when the user prefers reduced motion (it reacts if the setting changes). Lumi is then completely static.

To view the demo, serve the folder (`fetch()` does not work from `file://`):

```
cd prototype/mascot && python3 -m http.server 8000   # open http://localhost:8000/demo.html
```

Why JS at all: CSS keyframes can only loop at a fixed interval, and a blink should be irregular. Float and pulse are pure CSS.

## Talking state

The lamp itself speaks (talking is not wired to the full-body layer). Talking is a state layered on top of idle: it never replaces the idle float, aura pulse or blink, and it needs no artwork changes.

```js
const idle = startLumiIdle(svg);
idle.startTalking();   // or startLumiTalking(svg)
idle.stopTalking();    // or stopLumiTalking(svg)
idle.isTalking();
```

| Part | While talking |
|---|---|
| `#lumi-mouth-open` (existing hidden variant) | CSS reveals it only while `.lumi-talking` is on the svg. Closed is invisible (the approved Happy face has no mouth). Each syllable plays closed -> slightly open -> open -> slightly open -> closed, scaling in Y from the mouth's top edge. Syllables are 380-520ms with varying openness (55-90% of the full shape), grouped into words of 2-5 syllables with 220-420ms closed pauses. |
| `#lumi-eyes`, brows | Unchanged (Happy). Random blinks continue. |
| `#lumi-aura` | One extra slow breath on top of the idle pulse (scale 1 -> 1.03, 1.9s). Halo and rays keep their own idle animations. |
| `#lumi-root` | A tiny sway of +/-0.6deg about the collar over 3.2s, using the separate `rotate` property so it never fights the idle float (which uses `transform`). |

Timing lives in `lumi.js` (Web Animations), because a natural rhythm needs per-syllable variation that CSS keyframes cannot express. CSS (`lumi.css`, "Talking" section) only holds the static look. All numbers are in the `TALK` object at the top of that section.

Start/stop: effects ease in from rest. `stopTalking()` lets the current syllable finish (so the mouth closes naturally, within about half a second), eases the sway and aura back over 400ms, then removes `.lumi-talking`. The mouth ends closed, the Happy face is back, and idle motion never stopped. If stopped between words it finishes immediately. `startTalking()` during a stop restarts cleanly. Calling either repeatedly is safe.

Reduced motion: nothing animates. `startTalking()` only adds the class, and CSS shows a static half-open mouth as the "talking" indicator. `stopTalking()` removes it. If the preference changes while talking, it switches between the two.

Not included: speech bubbles, text, text-to-speech/voice, and lip-sync to real audio.

## Lamp size (~45px visible bulb)

Lumi is always the floating lamp/head. In the final product it is the only thing visible in the normal state, it stays put, and it never grows or hides, not even while the full-body character is out. (An earlier prototype had an 80px idle and a 120px "intervention" size; that is gone.)

| | Value |
|---|---|
| Visible bulb | 45px |
| SVG box (`--lumi-size`, set on `.lumi-anchor`) | 67.5px |

The visible bulb is 2/3 of the SVG box (its circle is 200 of the 300 viewBox units; the rest is the aura), so box = bulb x 1.5.

Position and size are deliberately separate:

```html
<div class="lumi-anchor" style="--lumi-x: 72%; --lumi-y: 48px">   <!-- POSITION: a zero-size point = Lumi's centre -->
  <div class="lumi-body"><svg>...</svg></div>                       <!-- full-body layer (lumi-body.svg), behind, hidden -->
  <div class="lumi"><svg>...</svg></div>                            <!-- the lamp (lumi.svg), centred on the anchor -->
</div>
```

- Dragging and persistence should only write the anchor's position (`--lumi-x/--lumi-y`, or `left/top`) -- see "Dragging" below. Use `position: fixed` on the anchor for the real page overlay; its parent must otherwise be positioned.
- Idle motion (float, aura breathing, blink) and talking are unchanged; their distances are in viewBox units, so they scale with the smaller lamp.

## Dragging

The idle lamp is draggable (`lumi.js`, `enableLumiDrag(anchor, opts?)`); the full-body character never is (see "Full-body layer" below).

```js
var disable = enableLumiDrag(anchor); // pointerdown on .lumi starts a drag; call once per anchor
disable();                            // removes the listeners, if ever needed
```

- Moves **only** the anchor's position (`--lumi-x/--lumi-y`), exactly the two properties "Lamp size" above already reserves for this. Nothing else about Lumi changes: `data-lumi-body` is never touched, so a drag cannot turn the lamp into the full body, and cannot desync from `emergeLumi()`/`returnLumi()`/`showLumiIntervention()` -- those only ever read the anchor's CURRENT position live, so there is nothing for a drag to leave stale. When an intervention starts, it anchors at wherever the lamp currently is, dragged or not, for free.
- **Refused outright while the body is out**: a `pointerdown` (or an in-progress drag, checked on every `pointermove`) is a no-op unless `lumiBodyState(anchor) === 'hidden'`. This is the entire answer to "must not interfere with the intervention lifecycle" -- dragging and the body/card lifecycle simply never run at the same time, rather than being coordinated.
- A plain click/tap never moves anything: a drag only starts once the pointer has moved `DRAG_THRESHOLD_PX` (4px).
- **Clamped** to stay inside the anchor's `offsetParent` (or the viewport, for a `position: fixed` real-page anchor with no `offsetParent`), keeping at least half the lamp's own box and the full-body's own reach (`--lumi-body-reach-x/-down`, "Viewport safety" above) clear of every edge -- so the lamp itself never clips, and neither would a body emerging from wherever it was dropped. Re-clamped (debounced) on `resize` too, once a position has actually been chosen (restored or dropped at least once) -- an un-dragged lamp keeps whatever positioning (e.g. a responsive `%` `--lumi-x`) the host page set.
- **Session persistence**: `sessionStorage`, keyed by the anchor's `id` (`lumi-drag-pos:<id>`, or `:default`). Restored (and re-clamped) the next time `enableLumiDrag` runs in the same tab session. Pass `opts.persist(pos)`/`opts.restore()`/`opts.storageKey` to use a different store (e.g. `chrome.storage.session` in the extension, matching how the rest of this project scopes transient state to a browser session) instead of the `sessionStorage` default.
- Pointer Events (`pointerdown`/`pointermove`/`pointerup`/`pointercancel` with pointer capture), so mouse, touch and pen all work; `touch-action: none` on `.lumi` (`lumi.css`) stops a touch-drag from scrolling the page instead.
- Reduced motion is irrelevant here: dragging is a direct 1:1 pointer-follow, not an animation.

## Full-body layer (emergence prototype)

When Lumi intervenes, her body comes **out of the lamp**, stays as an active pose, then goes back in. The lamp is the origin, is Lumi's HEAD, and stays visible the whole time. This is only the visual emergence: one pose, no speech, no text, no body expressions. The lamp itself is draggable (see "Dragging" above); the body never is, and dragging is refused for as long as it is out.

```
.lumi-anchor            position only; zero-size point = Lumi's centre (stacking context)
├── .lumi-body          the neck-down body: hidden by default, painted BEHIND the lamp
└── .lumi               the lamp = the head, always present
```

```js
emergeLumi(anchor);     // Promise: resolves true when the last part has settled (~1.35s)
returnLumi(anchor);     // Promise: resolves true when everything is back inside the lamp (~1.2s)
lumiBodyState(anchor);  // 'hidden' | 'emerging' | 'active' | 'returning'
```

`anchor` is the `.lumi-anchor` element. The functions only flip `data-lumi-body` on it; CSS transitions do the motion, so calling the opposite function half way simply reverses from where the parts are. A newer call supersedes an unfinished one (the older promise resolves `false`). "Done" means every CSS transition of the change has finished (it waits on `getAnimations()`), so it stays correct if the timings in CSS change.

### Why there is no second head

There is **one** bulb, the lamp. The body asset is neck-down only, so the character reads as "Lumi coming out of her own lamp", not "another Lumi appearing under the lamp". (An earlier version had its own second bulb head, which read as a second character.)

### The asset: `lumi-body.svg`

- Pure vector, no head. It uses the **same 300-unit coordinate space as `lumi.svg`** and is displayed in the **same box as the lamp** (`.lumi-body` and `.lumi` have identical size and centre), so lamp coordinates apply directly: collar tip y=277, bulb centre (150,150).
- Drawn from the "Idle" and hero poses of `lumi-design.png`: white egg-shaped torso with a dark outline and a glowing orange/yellow chest badge, thick curved rubber-hose dark arms and legs, puffy white gloves with cuffs and thumbs, chunky white sneakers with orange soles and toe accents, soft ground shadow. Below the collar the body is about one bulb-diameter tall, as in the reference.
- Parts are separate groups so CSS can stagger them: `#lumib-core` (= `#lumib-legs` behind `#lumib-torso`, one attached piece), `.lumib-arm`, `.lumib-glove` (one per side; the right ones are the left ones mirrored by a wrapper `<g transform>`), `#lumib-shadow`. All ids are prefixed `lumib-`.
- **Head-to-body joint.** The torso's top is only as wide as the collar (x 127-173) and starts right under it, so the body continues from the collar (as in the reference) instead of standing behind the bulb; its outline never runs up beside the glass. A dark neck (`#lumib-neck`) starts behind the collar and flares into the shoulders, so the collar always sits directly on it. The lamp's own idle float is locked off while the body is out (see "Lamp float lock" below), so this joint never has to survive independent head motion.
- `lumi.svg` is unchanged and the body is not part of it.

### How the "coming out of the lamp" illusion works

The body layer is painted behind the lamp (`z-index: -1`; the anchor's `isolation: isolate` keeps that above the page). The lamp's glass and collar are opaque, so anything inside the lamp silhouette is simply not visible. That is what "hidden inside the lamp" means here; nothing is faded in as a whole.

| | Core (torso + folded legs) | Arms / gloves / shadow |
|---|---|---|
| **Hidden** | scale 0.6, centre exactly on the lamp centre, entirely inside the glass | scale ~0 at the shoulders, opacity 0 |
| **Active** | drawn position, scale 1 | drawn position, opacity 1 |

The core scales about a point just behind the collar (150,250) and is shifted up so its centre lands on the lamp centre when hidden. Because that origin is at the top, the core's top edge stays inside the lamp for the whole motion: the body slides down out of the collar **attached**, it never floats free and there is never a gap.

### Lamp float lock

The lamp's idle float (see "Idle animation" above) animates `#lumi-root`, which moves the whole lamp SVG group up and down **inside its own box**; it never touches the box itself, so the anchor and `.lumi-body` (positioned only relative to the anchor) do not move with it. That is fine when the lamp is the whole of Lumi, but with the body out it read as the head drifting loose of the collar instead of being part of the same character.

So the float is turned off for as long as the body is not fully retracted, i.e. whenever the anchor's `data-lumi-body` is `emerging`, `active` or `returning` (not only `active`: the collar has to stay put for the whole time the body is visibly attached to it, including mid-emergence and mid-return, not just once it is fully out):

```css
.lumi-anchor[data-lumi-body="emerging"] .lumi-idle #lumi-root,
.lumi-anchor[data-lumi-body="active"] .lumi-idle #lumi-root,
.lumi-anchor[data-lumi-body="returning"] .lumi-idle #lumi-root {
  animation: none;
}
```

It resumes the moment the body is hidden again. This reuses the existing float animation and the existing `data-lumi-body` state that `lumi.js` already sets (see "Full-body layer" above); no JS changes were needed. Turning the animation off just settles `#lumi-root` at rest, a change of at most `--lumi-float-distance` (8 viewBox units, about 1.8px at the 67.5px lamp box), so there is no visible snap. Aura breathing (`#lumi-aura-halo`/`#lumi-aura-rays`), blink (`#lumi-eyes`) and talking (`#lumi-mouth-open`, the sway/aura-breath in `lumi.js`) are separate elements/animations and are untouched, so they keep running in every state.

Emerge sequence (return is the reverse, arms first, core last):

1. `0ms`: the torso slides down out of the collar and settles (800ms, soft ease-out, no overshoot).
2. `450ms`: the legs and shoes unfold downward from beneath the torso (700ms). They start folded inside the torso silhouette, so they are hidden by it.
3. `750ms`: the arms unfold from the shoulders and the gloves come with them (600ms).
4. `1000ms`: the ground shadow fades in.

Timings and easings live on `.lumi-body` in `lumi.css` (`--lumi-body-ease`, `--lumi-body-ease-back`, `--lumi-body-return-ms`). If you lengthen the return, raise `--lumi-body-return-ms` to the last part's end time (it delays the final `visibility: hidden`).

### How it is anchored

Position is only the anchor's. The body is a child of the anchor in the same box as the lamp, so it emerges from wherever Lumi is and follows the anchor for free (a future drag only moves the anchor). Nothing is in page or viewport coordinates.

### Size

The body scales with the lamp (`--lumi-size`), because it is drawn in the lamp's coordinates. With a 45px bulb the whole figure is about 100px tall (about 2.2x the bulb: 22px above the anchor, 77px below). That follows from the head being the 45px lamp and keeping the concept sheet's head-to-body proportion; the body is not inflated.

### Reduced motion

Under `prefers-reduced-motion: reduce` every transition in the body layer is off and `emergeLumi`/`returnLumi` switch straight to the final pose (the promise still resolves). Lumi's idle float stays off, as before.

### Viewport safety (structure only)

The body hangs **below** the anchor, so a lamp parked near the bottom edge would push it off-screen. Drag constraints are not implemented. The body is glued to the lamp, so clamping means keeping the **anchor** far enough from the edges; `.lumi-anchor` exposes how far the active body reaches: `--lumi-body-reach-down` (about 77px) and `--lumi-body-reach-x` (about 20px each side; the lamp's own aura is wider). The future clamp code can read those instead of hard-coding numbers. The demo anchor sits near the top of the stage so the body fits without clipping.

### Demo

`demo.html`: "Emerge Lumi" / "Return to lamp" show SMALL LAMP -> EMERGE -> FULL-BODY LUMI -> RETURN -> SMALL LAMP. "Start talking" / "Stop talking" / "Blink now" still work on the lamp. "Move anchor (test)" moves only the anchor to prove the body follows it; it is not dragging. Demo-only aids: guides (blue dashed = the 45px bulb, red dot = the anchor, red dashed line = the collar the body hangs from), **X-ray** (makes the lamp translucent and shows the body even while hidden, so you can see it inside the lamp) and **Zoom** 1x/2x/3x (scales Lumi about the anchor; 1x is the real size). The live readout is demo-only.

### Not in this prototype

Dragging, persistent position, speech bubble/text, AI, distraction detection, task capture, voice, notifications, rewards, other body poses, body expressions/animation (the body is static while active), viewport clamping.

## Intervention layer (5 reusable card compositions)

When Lumi needs to interrupt, a small conversational CARD appears alongside the existing full-body emergence, so it reads as "Lumi personally stepping into the conversation" rather than a generic notification. Files: `lumi-intervention.css`, `lumi-intervention.js` -- both new and separate from every file above. They do not modify `lumi.svg`, `lumi-body.svg`, `lumi.css` or `lumi.js`; they only DRIVE the existing, unmodified `emergeLumi()` / `returnLumi()` / `startLumiTalking()` / `stopLumiTalking()` and the existing `.lumi-anchor > .lumi-body + .lumi` structure.

```js
var i = showLumiIntervention({ variant: 'peek-side' });   // anchor defaults to the page's .lumi-anchor
i.ready.then(() => {...});    // resolves once the body AND the card have settled
i.close();                    // or: closeLumiIntervention(anchor)
pickLumiVariant();            // random id; never repeats the immediately-previous pick on that anchor
LUMI_INTERVENTION_VARIANTS    // ['peek-side', 'peek-top', 'full-behind', 'full-lean', 'full-entrance']
```

`opts`: `anchor` (defaults to `document.querySelector('.lumi-anchor')`), `variant`, `seed` (for `pickLumiVariant`), `title`/`lines`/`actions` (card text -- a placeholder for this pass, not load-bearing), `onAction(id)` (called before the card closes, so real behaviour can be wired in later without touching these files).

### Why there is still only one head

The card is inserted into the DOM **between** `.lumi-body` and `.lumi` (a new sibling, built lazily by `lumi-intervention.js`, never hand-authored):

```
.lumi-anchor
├── .lumi-body      existing body layer (lumi.css, untouched)   -- z-index: -1
├── .lumi-card      NEW -- inserted here                        -- z-index: 1 (default stacking)
└── .lumi           existing lamp (lumi.css, untouched)          -- default stacking (auto)
```

`.lumi-body` already has `z-index: -1`; `.lumi-card` and `.lumi` both have the default (`auto`), so within the anchor's stacking context they simply paint in **document order**: card first (behind), lamp last (in front). No `z-index` is set on `.lumi` and none is needed -- Lumi's head is always the topmost thing, purely because of where the card is inserted.

### The 5 variants

Not five different characters -- five compositions of the same Lumi, differing in how much of her the card ends up covering and, for one variant, timing. Geometry (`dx`/`dy`, the card's offset from the anchor) is tuned against the real rendered body: at 1x (45px bulb), anchor-relative, torso is x[-11,11] y[23,52], legs+shoes x[-19,19] y[50,75], each arm+glove roughly x[7..18] y[32..62].

| Variant | Card position | What ends up covered / visible |
|---|---|---|
| `peek-side` (1: half body, side peek) | left edge just right of centre, top at shoulder height | far arm+leg covered; near shoulder/arm + torso above stay visible |
| `peek-top` (2: half body, top peek) | nearly centred, top right at the shoulder line | only the head and shoulders clear the card |
| `full-behind` (3: full body, behind card) | top at the top of the legs | torso/arms/badge fully visible; only legs+shoes covered |
| `full-lean` (4: full body, side lean) | beside her, past the legs' width -- barely overlapping | whole figure visible, leaning toward the card (see below) |
| `full-entrance` (5: full body, small entrance) | similar overlap to `peek-side` | the real differentiator is timing: the card waits for `emergeLumi()` to fully resolve before appearing, instead of the usual small stagger, so it reads as "she arrives, THEN the card appears" rather than both at once |

### The lean (variant 4)

An additive CSS rule, not a change to the body's own art or mechanics: it only matches while `data-lumi-variant="full-lean"` (set by `lumi-intervention.js`) **and** `data-lumi-body` is `active` or `emerging` (an existing, unmodified lumi.css state) -- never during `returning` or `hidden`, so the lean relaxes to upright automatically as the body retracts, with no extra JS timing. The pivot (`50% 83.3%` = viewBox y=250 of 300) is the *same point* `#lumib-core`'s own transform-origin already uses as the body's "attached to the collar" origin, so leaning does not reopen the collar-to-torso gap (verified: the neck-to-collar overlap gets slightly larger with the lean applied, never smaller). Measured shift at 1x: about 6px at the feet for a 9deg rotation -- a real, verified rotation, kept small on purpose ("subtle and friendly"). `.lumi` (the lamp) is never touched, so the head never moves.

### Entrance / exit

`showLumiIntervention()` always calls the existing `emergeLumi(anchor)` unmodified (a harmless no-op if the body is already active). The card starts scaled down (0.92) and nudged 10px back toward Lumi, then eases to its resting position and full scale (~260-300ms, matching the body's own `cubic-bezier(0.3, 0.55, 0.25, 1)`) with a small stagger after the body starts emerging, so it reads as "she arrives, then the card grows out from her side" rather than everything popping in at once. `closeLumiIntervention()` reverses it: the card eases out first, `data-lumi-variant` is cleared (relaxing any lean), then the existing `returnLumi(anchor)` runs. Calling `showLumiIntervention()` again while a card is already open updates its content/position/variant in place (no collapse-and-reopen), so switching between variants is a smooth reposition, not a restart. Under `prefers-reduced-motion: reduce`, both the CSS transitions and the JS staggers are skipped, matching `emergeLumi`/`returnLumi`'s existing behaviour.

### Positioning: relative to the anchor, edge-aware

The card is positioned only relative to `.lumi-anchor` (never in page/viewport coordinates), so it follows the anchor wherever it is -- including a future drag, which is not implemented yet. Before placing it, `lumi-intervention.js`:

1. Finds the box the card must stay inside: the viewport, narrowed by every scrollable/clipping ancestor between the anchor and `<body>` (so this demo's own `.stage`, which has `overflow: hidden`, is respected automatically; a production overlay with a `position: fixed` anchor and no such ancestor just gets the viewport, per "Lamp size" above).
2. Picks the side (left/right) and vertical direction (below/above) with more room in that box, flipping the variant's preferred default if needed -- "Lumi near the right edge -> card opens left", etc. -- then hard-clamps inside the box as a last resort, so the card is never actually cut off even in a very narrow window.
3. Converts the result to the anchor's own local px (dividing out any ancestor scale, e.g. this demo's zoom control, found generically by comparing the lamp's rendered size to its own CSS size -- nothing here is demo-specific).
4. A debounced `resize` listener re-runs this for any currently-open card, so it stays clear of the edges if the window changes size while it is showing.

### Demo

`demo.html` adds a second controls row below the existing ones: "Variant 1" .. "Variant 5" (one button per composition above), "Random variant" (`pickLumiVariant()`), and "Close intervention". They call `showLumiIntervention`/`closeLumiIntervention` on the same `anchor` the rest of the demo uses, so "Move anchor (test)" above still applies -- move Lumi near an edge of the stage, then open a variant, to see it choose a side.

### Not in this pass

AI/distraction detection, real task/idea capture, notifications, voice, browser integration, dragging (the card is anchor-relative and ready for it, but no drag exists yet), persisted card content, more than the one lean pose.

## Other animation notes

- No transforms are baked into the groups (the few `transform` attributes are on gloss ellipses only), so CSS/WAAPI transforms on the group ids will not fight existing ones.
- For CSS transforms on SVG parts use `transform-box: fill-box; transform-origin: center;` (or `view-box` with px origins, as the blink does).
- Colors are attributes on the elements. Change `stroke`/`fill` on the group (e.g. `#lumi-aura-rays`, `#lumi-eyes .core`) or via CSS, which overrides presentation attributes.
- The aura is a gradient (no filter). The eye glow is a small blur (`lumi-eye-blur`) on each eye's `.glow` path; it scales with the viewBox, so it stays soft and consistent at any size.

## Using it in the extension

- `<img src="lumi.svg">` works for static display (CSS inside the file is not needed; nothing external is loaded).
- To animate, inline the SVG (or fetch and inject it) so page CSS/JS can reach the ids.
- If two copies are inlined on the same page (e.g. floating and expanded), the `lumi-grad-*` gradient ids, the blur filter id and all `lumi-*` group ids are duplicated. Gradients and the filter still render because the definitions are identical, and CSS id selectors (so `lumi.css`) match every copy, but `getElementById` and `url(#...)` references only resolve to the first copy. Scope JS lookups to each `<svg>` (as `lumi.js` does with `svg.querySelector`), or use one instance and scale it, or rename ids per copy. Do not put the SVG that owns the gradients under `display:none`, or the gradients in other copies stop rendering.

## Palette

| Use | Value |
|---|---|
| Warm yellow (glass mid-tone, halo, rays) | `#FFD54A` |
| Accent orange (open-mouth tongue) | `#FF8A3D` |
| Dark (face plate, collar) | `#2B2D36` |
| Rim orange | `#E88E1C` (sampled by eye; deeper than the palette orange) |
| Eye core / glow | `#FFF6D6` / `#FFCB45` (blurred) |
| Brows | `#8A5320` |

The rim orange, glass highlight tints, eye tints, brow brown and collar grays are not in the reference palette; they were matched by eye from the sheet.

## Not included yet

Other expressions, brow/eye variants beyond Happy, other body poses, and everything behavioural: thinking, celebrating, dragging, the intervention message/panel, distraction detection, task capture.
