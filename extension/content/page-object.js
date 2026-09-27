// The page object: exactly one element per page, inside a Shadow DOM so site CSS
// cannot break it (architecture-plan-v2 §4.2). Resting state is the small Max
// lamp; it grows a body + intervention card only when Go sends a command.
//
// Max herself (idle/talking/emerge/intervention-card/drag) is
// extension/shared/max/*, synced verbatim from prototype/mascot -- that is
// the canonical source; update there first, then re-copy. Those files are
// plain global-attaching scripts (not ES modules), loaded here via dynamic
// import() for their side effect of setting window.startMaxIdle etc.;
// content scripts run in an isolated JS world, so this never touches the
// HOST PAGE's own `window`.
//
// Cards: return screen, completion prompt, day question, new era. There is
// no greeting card: the day's first card says hello (payload.greeting).
// Capture parks ideas/tasks inline in a separate small form, deliberately
// NOT folded into Max's card (which only knows title/lines/buttons, not an
// input form).

import { announce, button, el } from '../shared/a11y.js';
import { isAuthPage } from '../shared/auth-pages.js';
import { t } from '../shared/i18n.js';
import { MSG, send } from '../shared/messaging.js';
import { startAnchorCapture } from './anchor.js';
import { readPageMetadata } from './page-metadata.js';
import { matchSpokenAction } from './voice-answer.js';

const UNDO_WINDOW_MS = 5000; // §8: undo window is 5 seconds
const CAPTURE_CONFIRMATION_MS = 4000;
const THUMBS_UP_MS = 2500;
// Cards whose rendering is acknowledged to the server (issued deliveries).
const ACKNOWLEDGED = new Set(['SHOW_RAMP', 'ASK_DAY', 'SHOW_NEW_ERA', 'SHOW_TASK', 'SHOW_WELCOME_BACK', 'SHOW_TREASURE', 'SHOW_OLD_IDEA']);

// Words for a voice: no icons or symbols (a voice would read their names).
export function plainText(text) {
  return String(text ?? '').replace(/[\u{1F300}-\u{1FAFF}\u{2600}-\u{27BF}\u{2B00}-\u{2BFF}\u{23E9}-\u{23FF}\u2713\u2714]/gu, '').replace(/\s+/g, ' ').trim();
}

// "2026-10-20" in the user's language, e.g. "20 October".
export function formatDay(key, lang) {
  const [y, m, d] = String(key).split('-').map(Number);
  if (!y || !m || !d) return key;
  return new Date(y, m - 1, d).toLocaleDateString(lang === 'el' ? 'el-GR' : 'en-GB', { day: 'numeric', month: 'long' });
}

// The lamp's state on Max: brightness by data attribute, and the count as
// text beside her and in the button's accessible name.
export function applyLampState(anchor, lampButton, { unseen = 0, level = 'off', lang } = {}) {
  anchor.setAttribute('data-lamp', level);
  let badge = anchor.querySelector('.max-count');
  if (unseen > 0) {
    if (!badge) {
      badge = el('span', { class: 'max-count', 'aria-hidden': 'true' });
      anchor.append(badge);
    }
    badge.textContent = String(unseen);
  } else {
    badge?.remove();
  }
  lampButton?.setAttribute('aria-label', `B-MAX — ${t(lang).lampLabel(unseen)}`);
}

const CSS = `
/* !important: the host element lives in the page's light DOM, where a normal
   :host rule loses to any page rule matching it (e.g. "* { opacity: .9 }" or
   "body > * { filter: ... }"), which would make Max look faded. */
:host { all: initial !important; }
.wrap { position: fixed; top: 64px; right: 12px; z-index: 2147483646;
        font: 16px/1.45 system-ui, sans-serif; display: flex; flex-direction: column;
        align-items: flex-end; gap: 8px; }
.bubble { width: min(340px, calc(100vw - 24px)); padding: 16px; border-radius: 12px;
          background: #1f2937; color: #ffffff; border: 2px solid #ffffff; box-shadow: 0 4px 16px rgba(0,0,0,.35); }
.bubble[hidden] { display: none; }
.capture label { display: block; margin-bottom: 8px; }
.capture input, .capture select { box-sizing: border-box; width: 100%; min-height: 44px; font: inherit; }
.msg { margin: 0 0 8px; }
.actions { display: flex; flex-wrap: wrap; gap: 16px; margin-top: 12px; } /* spaced apart, on purpose */
.actions button { min-height: 44px; min-width: 44px; padding: 8px 14px; font: inherit; border-radius: 8px;
                  cursor: pointer; color: #111827; background: #ffffff; border: 2px solid #ffffff; }
.actions button.quiet { color: #ffffff; background: transparent; }
:focus-visible { outline: 3px solid #fbbf24; outline-offset: 2px; }
.sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
/* Viewport-anchored. ":host" raises specificity above max.css's own
   ".max-anchor { position: absolute }", which loads LATER in this shadow
   root and would otherwise win on source order: Max then scrolled away
   with the document and her card was clamped far from her. */
:host .max-anchor { position: fixed; z-index: 2147483647; }
/* Hover/focus feedback: a glow on the lamp only, no movement, so it never
   shifts position or the card's anchor. */
.max-anchor > .max > svg { transition: filter 160ms ease-out; }
/* The lamp's brightness: unseen ideas (off / medium / full). The number is
   also written beside her (.max-count), so light is never the only signal.
   Both stay hidden off a resting Max -- an idea count sitting on her head is
   not something to see at a glance while working -- and reveal only on
   hover/focus of the anchor, same as any other on-demand detail. */
.max-anchor:hover[data-lamp="off"] > .max > svg,
.max-anchor:focus-within[data-lamp="off"] > .max > svg { filter: saturate(0.7) brightness(0.85); }
.max-anchor:hover[data-lamp="medium"] > .max > svg,
.max-anchor:focus-within[data-lamp="medium"] > .max > svg { filter: drop-shadow(0 0 4px rgba(255, 213, 74, 0.6)); }
.max-anchor:hover[data-lamp="full"] > .max > svg,
.max-anchor:focus-within[data-lamp="full"] > .max > svg { filter: drop-shadow(0 0 10px rgba(255, 213, 74, 0.95)) brightness(1.1); }
.max-count { position: absolute; right: -6px; bottom: -4px; min-width: 20px; height: 20px; padding: 0 5px;
              box-sizing: border-box; border-radius: 10px; background: #1f2937; color: #ffffff; border: 2px solid #ffd54a;
              font: 700 12px/16px system-ui, sans-serif; text-align: center; pointer-events: none;
              opacity: 0; transition: opacity 120ms ease-out; }
.max-anchor:hover > .max-count,
.max-anchor:focus-within > .max-count { opacity: 1; }
.max-anchor > .max:hover > svg,
.max-anchor > .max:focus-visible > svg { filter: drop-shadow(0 0 6px rgba(255, 213, 74, 0.9)) brightness(1.06); }
/* .max is a real <button> (a11y.js only allows on* handlers on real
   interactive elements) wearing Max's own look from max.css (position,
   size, cursor); this just strips the default button chrome so nothing
   shows through the SVG. Higher specificity than max.css's own plain
   ".max" rule, so source order here doesn't matter. */
.max-anchor > .max { appearance: none; background: none; border: none; padding: 0; margin: 0; font: inherit; }
/* Quiet thumbs-up on a goal page: beside the lamp, never a card, never
   focus, never clickable. Reduced motion: shown still, then removed. */
.max-thumb { position: absolute; left: -46px; top: 14px; font: 22px/1 system-ui, sans-serif;
              pointer-events: none; animation: max-thumb ${THUMBS_UP_MS}ms ease-out forwards; }
@keyframes max-thumb { 0% { opacity: 0; transform: translateY(6px); } 15%, 80% { opacity: 1; transform: none; } 100% { opacity: 0; } }
/* Max's chat, opened from the lamp: a small conversation beside him. */
.chat-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin: 0 0 8px; font-weight: 700; }
.chat-log { max-height: 190px; overflow-y: auto; display: flex; flex-direction: column; gap: 6px; margin: 0 0 8px; }
.chat-log .m { margin: 0; padding: 6px 10px; border-radius: 10px; overflow-wrap: anywhere; }
.chat-log .m-max { align-self: flex-start; background: #374151; }
.chat-log .m-user { align-self: flex-end; background: #0b3d63; }
.chat-form { display: flex; flex-wrap: wrap; gap: 8px; }
.chat-form input { box-sizing: border-box; flex: 1; min-width: 0; min-height: 44px; padding: 0 10px; font: inherit;
                   color: #ffffff; background: #111827; border: 2px solid #ffffff; border-radius: 8px; }
/* Its own full-width row under input/mic/send, not squeezed beside them --
   this is the one button that must never be missed for lack of room. */
.chat-form .chat-return { flex: 1 1 100%; }
.chat button { min-height: 44px; min-width: 44px; padding: 8px 14px; font: inherit; border-radius: 8px; cursor: pointer;
               color: #111827; background: #ffffff; border: 2px solid #ffffff; }
.chat button:disabled { opacity: .6; cursor: not-allowed; }
.chat .chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
.chat .chips button { min-height: 36px; padding: 4px 12px; color: #ffffff; background: transparent; border-radius: 999px; }
.chat .chat-status { margin: 6px 0 0; font-size: 14px; color: #d1d5db; }
@media (prefers-reduced-motion: reduce) { * { animation: none !important; transition: none !important; } }
`;

const ANCHOR_LABEL = {
  1: 'You had written:',
  2: 'You had selected:',
  3: 'You were reading:',
  4: 'You were on:',
};

// Sign-in popups ("Continue with Google" etc.) open as a window of type
// "popup"; only the worker can see a window's type. Asked in parallel with
// loading Max, so it never delays the message listener (see start()).
function inPopupWindow() {
  return send({ type: MSG.WINDOW_KIND }).then((res) => res?.ok === true && res.data === 'popup', () => false);
}

async function loadMax(root, onOpenCapture, popupCheck, onMoved) {
  // Lazy (not module-top-level): chrome.runtime.getURL only needs to exist
  // by the time this actually runs, not at import time.
  const MAX_BASE = chrome.runtime.getURL('shared/max/');
  // Side-effecting global scripts, not ES modules (see file header): skip if
  // something already installed them (a test double, or a second page
  // object racing this one) rather than loading/running them twice.
  if (typeof window.showMaxIntervention !== 'function') {
    root.append(
      el('link', { rel: 'stylesheet', href: MAX_BASE + 'max.css' }),
      el('link', { rel: 'stylesheet', href: MAX_BASE + 'max-intervention.css' }),
    );
    // Order matches demo.html's own <script> order; both must finish before
    // any Max API below is called.
    await import(MAX_BASE + 'max.js');
    await import(MAX_BASE + 'max-intervention.js');
  }

  const [lampSvg, bodySvg] = await Promise.all(
    [MAX_BASE + 'max.svg', MAX_BASE + 'max-body.svg'].map((url) => fetch(url).then((r) => r.text())),
  );

  const maxAnchor = el('div', { class: 'max-anchor', id: 'focus-companion-max' });
  const maxBody = el('div', { class: 'max-body' });
  maxBody.innerHTML = bodySvg;
  // A real <button> (not a div+role, which a11y.js's `el` refuses an on*
  // handler on anyway -- see its header comment): Enter/Space activation is
  // then native, nothing to hand-roll. `enableMaxDrag` below attaches its
  // OWN pointerdown/move/up directly (not through `el`, same as the
  // pre-existing pointerdown listener further down), so dragging and this
  // click both work on the same element without conflict -- a real drag
  // swallows the one spurious click it would otherwise leave behind (see
  // max.js's onPointerUp).
  const lamp = el('button', {
    type: 'button',
    class: 'max',
    onclick: onOpenCapture,
    'aria-label': 'B-MAX — drag to move, press to save an idea or task',
  });
  lamp.innerHTML = lampSvg;
  maxAnchor.append(maxBody, lamp);
  if (await popupCheck) return null; // a sign-in popup: no Max at all
  root.append(maxAnchor);

  // Default resting spot roughly matches the previous plain lamp button
  // (top:12px; right:12px; 44px). The saved position overrides it below.
  maxAnchor.style.setProperty('--max-x', (window.innerWidth - 34) + 'px');
  maxAnchor.style.setProperty('--max-y', '34px');

  const idle = window.startMaxIdle(lamp.querySelector('svg'));
  // One extension-wide position (viewport px) in chrome.storage.local: the
  // same in every tab, kept across navigation, reinjection and restarts.
  // restore: never read the host page's own sessionStorage.
  window.enableMaxDrag(maxAnchor, {
    persist: (pos) => { try { chrome.storage.local.set({ [MAX_POS_KEY]: pos }); } catch { /* extension reloaded: no persistence */ } },
    restore: () => null,
  });
  try {
    applyMaxPosition(maxAnchor, (await chrome.storage.local.get(MAX_POS_KEY))[MAX_POS_KEY]);
  } catch { /* storage unavailable: keep the default spot */ }
  // A drag in another tab moves Max here too, so switching back shows the
  // same spot. Never while this tab is mid-drag.
  chrome.storage.onChanged?.addListener((changes, area) => {
    if (area !== 'local' || !changes[MAX_POS_KEY]) return;
    // This tab's own drag also lands here (its drop is saved): only another
    // tab's move repositions Max, but anything beside her follows either way.
    if (!maxAnchor.getAttribute('data-max-dragging')) applyMaxPosition(maxAnchor, changes[MAX_POS_KEY].newValue);
    onMoved?.();
  });

  return { maxAnchor, lamp, idle };
}

const MAX_POS_KEY = 'maxPosition';
// Keeps a saved position inside this tab's viewport, which may be smaller
// than the one it was saved in: half the lamp box (max.css --max-size
// 67.5px) on every side and the emerged body's reach (--max-body-reach-down,
// 1.15x the lamp box) at the bottom -- the same bounds max.js's drag uses.
const LAMP_HALF = 34;
const BODY_REACH_DOWN = 78;
export function clampMaxPosition({ x, y }, width, height) {
  return {
    x: Math.min(Math.max(x, LAMP_HALF), Math.max(LAMP_HALF, width - LAMP_HALF)),
    y: Math.min(Math.max(y, LAMP_HALF), Math.max(LAMP_HALF, height - BODY_REACH_DOWN)),
  };
}
function applyMaxPosition(anchor, pos) {
  if (!Number.isFinite(pos?.x) || !Number.isFinite(pos?.y)) return;
  const { x, y } = clampMaxPosition(pos, window.innerWidth, window.innerHeight);
  anchor.style.setProperty('--max-x', `${x}px`);
  anchor.style.setProperty('--max-y', `${y}px`);
}

export async function start() {
  if (window.__focusCompanionPageObject || !document.body) return;
  // Sign-in pages get no Max; the server treats them as neutral too.
  if (isAuthPage(location.href)) return;
  window.__focusCompanionPageObject = true;
  const popupCheck = inPopupWindow();

  const capture = startAnchorCapture();
  const documentToken = crypto.randomUUID();

  const host = document.createElement('focus-companion');
  const root = host.attachShadow({ mode: 'open' });

  // The live region and its content stay inside this same shadow root: ID
  // references do not cross the shadow boundary (§12).
  const live = el('div', { class: 'sr', role: 'status', 'aria-live': 'polite' });
  const parked = el('section', { class: 'bubble capture', 'aria-label': 'Save for later', hidden: true });
  const chatPanel = el('section', { class: 'bubble chat', 'aria-label': 'Max', hidden: true });
  const wrap = el('div', { class: 'wrap' }, parked, chatPanel);
  root.append(el('style', { text: CSS }), live, wrap);

  // The "save for later" form opens from Max, beside her, wherever she was
  // dragged (not in a fixed corner): to her right if it fits, else her left,
  // level with her and kept inside the viewport.
  function placeCaptureNearMax(panel = parked) {
    const r = maxAnchor?.getBoundingClientRect?.();
    if (!r) return;
    const vw = window.innerWidth, vh = window.innerHeight;
    const width = Math.min(340, vw - 24), gap = 44, margin = 12;
    const height = panel.offsetHeight || 220;
    const left = r.left + gap + width <= vw - margin ? r.left + gap : Math.max(margin, r.left - gap - width);
    const top = Math.min(Math.max(margin, r.top - 24), Math.max(margin, vh - margin - height));
    wrap.style.left = `${Math.round(left)}px`;
    wrap.style.top = `${Math.round(top)}px`;
    wrap.style.right = 'auto';
    wrap.style.alignItems = 'flex-start';
  }
  document.body.append(host);

  let captureOrigin;
  let captureSelection;
  let captureForm;
  let captureVersion = 0;
  let captureConfirmationTimer;
  const activeElement = () => {
    let node = document.activeElement;
    while (node?.shadowRoot?.activeElement) node = node.shadowRoot.activeElement;
    return node;
  };
  // Pointer focus reaches the lamp before click; remember the editor beforehand.
  function rememberCaptureOrigin() {
    captureOrigin = activeElement();
    const selection = window.getSelection?.();
    captureSelection = selection?.rangeCount && captureOrigin?.contains(selection.anchorNode)
      ? selection.getRangeAt(0).cloneRange() : null;
  }

  // Max (maxAnchor/lamp) loads asynchronously below (fetch + dynamic
  // import). The message listener is registered further down BEFORE that
  // finishes on purpose -- see its own comment -- so `maxReady` gates only
  // the handful of cases that actually touch maxAnchor/lamp.
  let maxAnchor, lamp;
  let markMaxReady;
  const maxReady = new Promise((resolve) => { markMaxReady = resolve; });

  function closeCapture() {
    clearTimeout(captureConfirmationTimer);
    captureVersion++;
    if (parked.contains(activeElement())) {
      const target = captureOrigin?.isConnected ? captureOrigin : lamp;
      target.focus({ preventScroll: true });
      if (target === captureOrigin && captureSelection?.startContainer.isConnected && captureSelection?.endContainer.isConnected) {
        const selection = window.getSelection();
        selection.removeAllRanges();
        selection.addRange(captureSelection);
      }
    }
    captureForm = null;
    parked.hidden = true;
    parked.replaceChildren();
    captureOrigin = null;
    captureSelection = null;
  }

  function openCapture() {
    clearTimeout(captureConfirmationTimer);
    if (captureForm) { captureForm.querySelector('input').focus(); return; }
    if (!captureOrigin) rememberCaptureOrigin();
    const version = ++captureVersion;
    const kind = el('select', {}, el('option', { value: 'idea', text: 'Idea' }), el('option', { value: 'task', text: 'Task' }));
    const text = el('input', { type: 'text', required: true });
    const status = el('p', { role: 'status', 'aria-live': 'polite' });
    let saving = false;
    const save = button('Save for later', null, { type: 'submit' });
    const form = el('form', { onsubmit: async (event) => {
      event.preventDefault();
      if (saving) return;
      saving = true;
      save.setAttribute('aria-disabled', 'true');
      form.setAttribute('aria-busy', 'true');
      const submittedText = text.value;
      const submittedKind = kind.value;
      const result = await send({ type: MSG.CAPTURE_IDEA, text: submittedText, itemType: submittedKind });
      if (version !== captureVersion) return;
      saving = false;
      save.setAttribute('aria-disabled', 'false');
      form.setAttribute('aria-busy', 'false');
      if (!result?.ok) {
        announce(status, 'Could not save. Your text is here — try again.');
        return;
      }
      updateLamp();
      const message = 'Saved for later — you stay where you were.';
      // Do not discard edits made while the request was in flight.
      if (text.value !== submittedText || kind.value !== submittedKind) {
        announce(status, message);
        return;
      }
      closeCapture();
      parked.replaceChildren(el('p', { class: 'msg', text: message }));
      parked.hidden = false;
      placeCaptureNearMax();
      announce(live, message);
      const confirmationVersion = captureVersion;
      captureConfirmationTimer = setTimeout(() => {
        if (captureVersion !== confirmationVersion || captureForm) return;
        parked.hidden = true;
        parked.replaceChildren();
        if (live.textContent === message) live.textContent = '';
      }, CAPTURE_CONFIRMATION_MS);
    } },
    el('label', {}, 'Type', kind), el('label', {}, 'Thought', text),
    el('div', { class: 'actions' }, save, button('Cancel', closeCapture)), status);
    captureForm = form;
    parked.replaceChildren(form);
    parked.hidden = false;
    placeCaptureNearMax();
    text.focus({ preventScroll: true });
  }

  let undoTimer = null;
  let renderVersion = 0;
  let renderedCommand;
  let currentVariant;
  // The server-owned card on screen (server card.go): it follows the user
  // from page to page with the same delivery id and closes only when
  // answered, ignored for 5 active minutes, or no longer relevant. Leaving
  // it with the pointer, switching tabs or a URL rewrite never closes it.
  let renderedDelivery;
  const cardOpen = () => !!maxAnchor?.querySelector('.max-card[data-max-card-open="true"]');

  function hide({ returnFocus = false } = {}) {
    renderedCommand = undefined;
    renderedDelivery = undefined;
    renderVersion++;
    currentVariant = undefined;
    lastCard = null;
    clearTimeout(undoTimer);
    window.closeMaxIntervention(maxAnchor);
    live.textContent = '';
    if (returnFocus) lamp.focus();
  }

  // Shows a message and buttons in Max's intervention card. Never moves
  // focus: stealing it would interrupt typing. The message is announced
  // through the live region instead. `actions[].onSelect` runs on click;
  // `keepOpen` skips Max's own auto-close for an action that renders more
  // card content itself (see rampChoose's Undo follow-up) rather than
  // ending the interaction.
  // hello: the day's first card opens with a greeting (there is no separate
  // greeting card); `lang` is the language the rest of the card is written in.
  function show(message, { sub, actions = [], variant, keepVariant = false, hello = false, lang = 'en' } = {}) {
    if (hello) message = `${t(lang).hello} ${message}`;
    renderedCommand = undefined;
    renderedDelivery = undefined;
    renderVersion++;
    clearTimeout(undoTimer);
    const version = renderVersion;
    if (!keepVariant || !currentVariant) currentVariant = variant || window.pickMaxVariant(undefined, maxAnchor);
    const i = window.showMaxIntervention({
      anchor: maxAnchor,
      variant: currentVariant,
      title: message,
      lines: sub ? [sub] : [],
      actions: actions.map((a) => ({ id: a.id, label: a.label, primary: a.primary, keepOpen: a.keepOpen })),
      onAction: (id) => actions.find((a) => a.id === id)?.onSelect(),
    });
    live.textContent = '';
    lastCard = { actions, lang };
    i.ready.then(() => {
      if (version === renderVersion) {
        live.textContent = sub ? `${message} ${sub}` : message;
        speakCard(message, sub, actions, lang);
      }
    });
  }

  // A card is also SPOKEN when the user asked Max to speak (the worker checks
  // the setting): what it says, then the options and how to answer them, so a
  // blind user can hear it, choose, and reach the buttons with Alt+Shift+K.
  function speakCard(message, sub, actions, lang) {
    const tx = t(lang);
    const labels = actions.map((a) => plainText(a.label)).filter(Boolean);
    const parts = [plainText(message), plainText(sub)].filter(Boolean);
    if (labels.length && tx.spokenOptions) parts.push(tx.spokenOptions(labels));
    // Each part is a sentence: a full stop only where there is none, so the voice pauses.
    const text = parts.map((part) => (/[.!?…;]$/.test(part) ? part : `${part}.`)).join(' ');
    send({ type: MSG.SPEAK, text, lang });
  }

  // --- return screen ---------------------------------------------------------

  function showRamp({ site, step, anchor: returnAnchor, greeting }) {
    const sub = returnAnchor?.snippet ? `${ANCHOR_LABEL[returnAnchor.tier] ?? ''} “${returnAnchor.snippet}”` : undefined;
    // step.door: on a difficult stretch the step is a door, never explained.
    show(step?.door || `Your current step: ${step?.text ?? ''}`, {
      sub,
      hello: greeting, // this card is still English-only, so is its hello
      actions: [
        { id: 'continue', label: 'Continue', primary: true, onSelect: () => rampChoose(site, 'continue') },
        { id: 'browse', label: 'I want to browse', onSelect: () => rampChoose(site, 'browse') },
        { id: 'not_today', label: 'Not today', onSelect: () => rampChoose(site, 'not_today') },
      ],
    });
  }

  async function rampChoose(site, choice) {
    const res = await send({ type: MSG.RAMP_RESPOND, site, choice });
    if (!res?.ok) return;
    if (choice === 'continue') return hide();
    // Undo reverts the suppression only; it lasts 5 seconds (§8). Same card,
    // same pose (keepVariant) -- she doesn't re-pose just to say "okay".
    show('Okay. No problem.', {
      keepVariant: true,
      actions: [{ id: 'undo', label: 'Undo', onSelect: () => rampUndo(site) }],
    });
    undoTimer = setTimeout(() => hide(), UNDO_WINDOW_MS);
  }

  async function rampUndo(site) {
    await send({ type: MSG.RAMP_RESPOND, site, choice: 'undo' });
    show('Undone.', { keepVariant: true });
    undoTimer = setTimeout(() => hide(), 2000);
  }

  // --- completion prompt -----------------------------------------------------

  // gentle: asked again after "not yet" and more work on the step, as a
  // small, short question (the server decides; never more than 3 a day).
  function showCompletion({ step, greeting, gentle }) {
    const message = gentle ? `✓ Done with “${step?.text ?? ''}”?` : `It looks like you finished: “${step?.text ?? ''}”. Did you?`;
    show(message, {
      hello: greeting, // this card is still English-only, so is its hello
      variant: gentle ? 'peek-side' : undefined,
      actions: [
        { id: 'yes', label: 'Yes', primary: true, keepOpen: true, onSelect: () => completionAnswer(step.id, 'yes') },
        { id: 'not_yet', label: 'Not yet', onSelect: () => completionAnswer(step.id, 'not_yet') },
      ],
    });
  }

  async function completionAnswer(stepId, answer) {
    const res = await send({ type: MSG.STEP_RESPOND, stepId, answer });
    if (answer !== 'yes' || !res?.ok) return hide();
    const { announcement, currentStep: next, searchQueries = [], goalCompleted, lang } = res.data;
    if (!next || goalCompleted || next.id === stepId) {
      // e.g. "Step 4 of 8 complete." announced politely; the card then rests.
      show(announcement || 'Done.', { keepVariant: true });
      undoTimer = setTimeout(() => hide(), 4000);
      return;
    }
    // The next step right away: carry on here, or get three searches.
    const tx = t(lang);
    show(`✅ ${announcement || 'Done.'}`, {
      keepVariant: true,
      sub: tx.nextStep(next.text),
      actions: [
        { id: 'continue_here', label: tx.continueHere, primary: true, onSelect: () => {} },
        { id: 'where_start', label: tx.whereStart, keepOpen: true, onSelect: () => showStartHere({ step: next, searchQueries }, lang) },
      ],
    });
  }

  // --- good day / bad day ----------------------------------------------------

  // "No work on your goals today?" -- three answers; "not today" makes the
  // rest of the day silent (the server enforces it), with a 5-second Undo.
  function showAskDay({ lang, greeting }) {
    const tx = t(lang);
    show(tx.dayTitle, {
      hello: greeting,
      lang,
      sub: tx.daySub,
      actions: [
        { id: 'will', label: tx.dayWill, primary: true, onSelect: () => dayAnswer('will_work', lang) },
        { id: 'need_start', label: tx.dayNeedStart, keepOpen: true, onSelect: () => dayAnswer('need_start', lang) },
        { id: 'wont', label: tx.dayWont, keepOpen: true, onSelect: () => dayAnswer('wont_work', lang) },
      ],
    });
  }

  async function dayAnswer(choice, lang) {
    const tx = t(lang);
    const res = await send({ type: MSG.DAY_RESPOND, choice });
    if (!res?.ok) return hide();
    if (choice === 'will_work') {
      announce(live, tx.dayWillDone);
      return;
    }
    if (choice === 'need_start') return showStartHere(res.data, lang);
    show(tx.dayWontDone, {
      keepVariant: true,
      actions: [{ id: 'undo', label: tx.undo, onSelect: () => dayUndo(lang) }],
    });
    undoTimer = setTimeout(() => hide(), UNDO_WINDOW_MS);
  }

  async function dayUndo(lang) {
    await send({ type: MSG.DAY_RESPOND, choice: 'undo' });
    show(t(lang).undone, { keepVariant: true });
    undoTimer = setTimeout(() => hide(), 2000);
  }

  // The current step and three searches. The server sends search phrases,
  // never links; the worker turns each into a search page (actions.js).
  function showStartHere({ step, searchQueries = [] } = {}, lang) {
    const tx = t(lang);
    show(tx.startTitle, {
      keepVariant: true,
      sub: step?.text,
      actions: [
        ...searchQueries.slice(0, 3).map((query, i) => ({
          id: `search-${i}`, label: `🔍 ${query}`, onSelect: () => send({ type: MSG.OPEN_SEARCH, query }),
        })),
        { id: 'close', label: tx.close, onSelect: () => {} },
      ],
    });
  }

  // A quiet badge beside the lamp: no card, no focus, gone by itself.
  let thumbTimer;
  function showThumbsUp({ lang }) {
    maxAnchor.querySelector('.max-thumb')?.remove();
    clearTimeout(thumbTimer);
    const badge = el('span', { class: 'max-thumb', 'aria-hidden': 'true', text: '👍' });
    maxAnchor.append(badge);
    announce(live, t(lang).thumbs);
    thumbTimer = setTimeout(() => badge.remove(), THUMBS_UP_MS);
  }

  // A new era is announced whatever the day's mode: full body, prominent.
  function showNewEra({ era, eraLabel, lang, deliveryId, greeting }) {
    const tx = t(lang);
    // Either button is the user's response: the card stops following them.
    const done = () => send({ type: MSG.CARD_CLOSE, deliveryId, reason: 'done' });
    show(`🌍 ${tx.eraTitle(tx.eras[era] ?? eraLabel)}`, {
      hello: greeting,
      lang,
      variant: 'full-entrance',
      sub: tx.eraSub,
      actions: [
        { id: 'show', label: tx.eraShow, primary: true, onSelect: () => { done(); send({ type: MSG.OPEN_FULL }); } },
        { id: 'later', label: tx.eraLater, onSelect: done },
      ],
    });
  }

  // The lamp's brightness is the number of ideas not seen yet (server:
  // support.go): off, medium, full. Never by light alone: the number is
  // written beside it and in the button's name.
  async function updateLamp() {
    if (!maxAnchor) return;
    const res = await send({ type: MSG.LAMP_STATE });
    if (!res?.ok || !res.data) return;
    lampLang = res.data.lang === 'el' ? 'el' : 'en';
    applyLampState(maxAnchor, lamp, res.data);
  }

  // Welcome back after days away: no count of days, nothing missed; the new
  // date and one small step (server: support.go).
  function showWelcomeBack({ recovery, voice, lang, deliveryId, greeting }) {
    const tx = t(lang);
    const step = recovery?.step;
    const lines = [
      recovery?.newDate ? tx.welcomeDate(formatDay(recovery.newDate, lang)) : null,
      step ? tx.welcomeStep(step.door || step.text, Math.min(step.estimatedMinutes || 10, 10)) : null,
    ].filter(Boolean);
    const done = () => send({ type: MSG.CARD_CLOSE, deliveryId, reason: 'done' });
    show(tx.welcomeTitle, {
      hello: greeting,
      lang,
      sub: lines.join(' '),
      actions: [
        { id: 'go', label: tx.welcomeGo, primary: true, onSelect: done },
        voice ? { id: 'voice', label: tx.hearVoice, onSelect: () => { done(); send({ type: MSG.OPEN_FULL, section: 'voice' }); } } : null,
      ].filter(Boolean),
    });
  }

  // Weekend treasure: once, between work.
  function showTreasure({ ideas, lang, deliveryId, greeting }) {
    const tx = t(lang);
    const done = () => send({ type: MSG.CARD_CLOSE, deliveryId, reason: 'done' });
    show(`✨ ${tx.treasureTitle(ideas)}`, {
      hello: greeting,
      lang,
      actions: [
        { id: 'show', label: tx.treasureShow, primary: true, onSelect: () => { done(); send({ type: MSG.OPEN_FULL, section: 'treasure' }); } },
        { id: 'later', label: tx.eraLater, onSelect: done },
      ],
    });
  }

  // A difficult stretch: the user's own old idea. Never says why.
  function showOldIdea({ idea, voice, lang, greeting }) {
    if (!idea) return;
    const tx = t(lang);
    show(`💡 ${tx.oldIdeaTitle(idea.daysAgo)}`, {
      hello: greeting,
      lang,
      sub: tx.oldIdeaSub(idea.text),
      actions: [
        {
          id: 'yes', label: tx.oldIdeaYes, primary: true, keepOpen: true,
          onSelect: async () => {
            const res = await send({ type: MSG.IDEA_OFFER, id: idea.id, accept: true });
            if (!res?.ok) return;
            show(tx.oldIdeaYesDone, { keepVariant: true });
            undoTimer = setTimeout(() => hide(), 2500);
          },
        },
        { id: 'no', label: tx.oldIdeaNo, onSelect: () => send({ type: MSG.IDEA_OFFER, id: idea.id, accept: false }) },
        voice ? { id: 'voice', label: tx.hearVoice, onSelect: () => send({ type: MSG.OPEN_FULL, section: 'voice' }) } : null,
      ].filter(Boolean),
    });
  }

  // The user's own task for today (server: tasks.go). "Later" sets it aside;
  // the server brings it back after more active time, at most 3 times a day.
  function showTask({ task, lang, deliveryId, greeting, taskMode = 'remind', other }) {
    if (!task) return;
    const tx = t(lang);
    if (taskMode !== 'remind') return showTaskMode({ task, lang, greeting, taskMode, other });
    const sub = task.overdue ? tx.taskSubOverdue : task.urgent ? tx.taskSubLate : tx.taskSubToday;
    show(`📝 ${tx.taskTitle(task.text)}`, {
      hello: greeting,
      lang,
      sub,
      actions: [
        {
          id: 'done', label: tx.taskDone, primary: true, keepOpen: true,
          onSelect: async () => {
            const res = await send({ type: MSG.TASK_DONE, id: task.id });
            if (!res?.ok) return;
            show(tx.taskDoneMsg, { keepVariant: true });
            undoTimer = setTimeout(() => hide(), 2000);
          },
        },
        { id: 'help', label: tx.taskHelp, keepOpen: true, onSelect: () => showTaskHelp(task, lang) },
        { id: 'later', label: tx.taskLater, onSelect: () => send({ type: MSG.CARD_CLOSE, deliveryId, reason: 'ignored' }) },
      ],
    });
  }

  // "Help me start": one tiny first action for the task, and searches when
  // they would help (the worker turns each phrase into a search page).
  // The three questions Max asks about a task in progress (tasklife.go): the
  // time has passed ("checkin"), it stayed open from yesterday ("carry"), or
  // the user drifted to a distraction ("resume": back to where they were).
  function showTaskMode({ task, lang, greeting, taskMode, other }) {
    const tx = t(lang);
    const act = (action) => send({ type: MSG.TASK_ACTION, id: task.id, action });
    const finish = async () => {
      const res = await act('yes');
      if (!res?.ok) return;
      show(tx.taskDoneMsg, { keepVariant: true });
      updateLamp();
      undoTimer = setTimeout(() => hide(), 2500);
    };
    const setAside = async () => {
      const res = await act('later');
      if (!res?.ok) return;
      show(tx.notNowDone, { keepVariant: true });
      undoTimer = setTimeout(() => hide(), 2500);
    };
    const back = async () => {
      const res = await act('resume');
      if (!res?.ok) return;
      if (res.data?.resumeUrl || res.data?.searchQuery) return hide();
      show(tx.resumeNothing, { keepVariant: true });
      undoTimer = setTimeout(() => hide(), 4000);
    };
    if (taskMode === 'urgent') {
      // Another task's deadline is close: switch to it, or keep the one in progress.
      show(tx.urgentTitle(task.text), {
        hello: greeting,
        lang,
        sub: other ? tx.urgentSub(other.text) : undefined,
        actions: [
          {
            id: 'switch', label: tx.urgentSwitch(task.text), primary: true, keepOpen: true,
            onSelect: async () => {
              await act('start');
              hide();
            },
          },
          { id: 'keep', label: other ? tx.urgentKeep(other.text) : tx.ansNotNow, onSelect: () => act('keep') },
        ],
      });
      return;
    }
    if (taskMode === 'refocus') {
      // Working on another task than the one with priority: say which comes
      // first, and offer the way back, or to swap them.
      show(tx.refocusTitle(task.text), {
        hello: greeting,
        lang,
        sub: other ? tx.refocusSub(other.text) : undefined,
        actions: [
          { id: 'back', label: tx.refocusBack, primary: true, keepOpen: true, onSelect: back },
          { id: 'yes', label: tx.refocusDone, keepOpen: true, onSelect: finish },
          other ? {
            id: 'switch', label: tx.refocusSwitch(other.text), keepOpen: true,
            onSelect: async () => {
              await send({ type: MSG.TASK_ACTION, id: other.id, action: 'start' });
              hide();
            },
          } : null,
        ].filter(Boolean),
      });
      return;
    }
    if (taskMode === 'resume') {
      const where = task.note || task.lastTitle;
      show(tx.resumeTitle(task.text), {
        hello: greeting,
        lang,
        sub: where ? tx.resumeSub(where) : undefined,
        actions: [
          { id: 'resume', label: tx.resumeGo, primary: true, keepOpen: true, onSelect: back },
          { id: 'yes', label: tx.ansYes, keepOpen: true, onSelect: finish },
          { id: 'later', label: tx.ansNotNow, keepOpen: true, onSelect: setAside },
        ],
      });
      return;
    }
    const carry = taskMode === 'carry';
    show(carry ? tx.carryTitle(task.text) : tx.checkinTitle(task.text), {
      hello: greeting,
      lang,
      sub: carry ? undefined : task.workedMin > 0 ? tx.checkinSubWorked(task.workedMin) : tx.checkinSubTime,
      actions: [
        { id: 'yes', label: tx.ansYes, primary: true, keepOpen: true, onSelect: finish },
        { id: 'not_yet', label: tx.ansNotYet, onSelect: () => act('not_yet') },
        { id: 'later', label: tx.ansNotNow, keepOpen: true, onSelect: setAside },
      ],
    });
  }

  async function showTaskHelp(task, lang) {
    const tx = t(lang);
    // The card is answered: it closes on the server, so it cannot follow the
    // user onto the next page (or a search result) and ask again.
    send({ type: MSG.TASK_ACTION, id: task.id, action: 'helped' });
    show(tx.taskHelpWorking, { keepVariant: true });
    const res = await send({ type: MSG.TASK_HELP, id: task.id });
    if (!res?.ok) return show(tx.taskHelpFailed, { keepVariant: true, actions: [{ id: 'close', label: tx.close, onSelect: () => {} }] });
    show(tx.taskHelpTitle, {
      keepVariant: true,
      sub: res.data.step,
      actions: [
        ...res.data.searchQueries.slice(0, 3).map((query, i) => ({
          id: `search-${i}`, label: `🔍 ${query}`, onSelect: () => send({ type: MSG.OPEN_SEARCH, query }),
        })),
        { id: 'close', label: tx.close, onSelect: () => {} },
      ],
    });
  }

  // --- Max's chat ------------------------------------------------------------
  // The lamp is the way to talk to Max from any page: he comes out with his
  // whole body, listens ("what do you need?"), keeps the task or idea, says
  // so, and folds back into the lamp. The same conversation as the dashboard.

  let chatOpen = false;
  let lastCard = null; // the open card's buttons, so a spoken answer can press one
  let chatBusy = false;
  let micListening = false;
  let micPurpose = 'chat'; // 'chat': into the text box; 'answer': press a button of the open card
  let micHeard = '';
  let sendTimer = null;
  let lampLang = 'en';
  let chatParts; // { log, input, send, status, chips }

  function chatLine(who, text) {
    chatParts.log.append(el('p', { class: `m m-${who}`, text }));
    chatParts.log.scrollTop = chatParts.log.scrollHeight;
  }

  function chatChips(items) {
    chatParts.chips.replaceChildren(...items.map(({ label, text, send: sendNow }) => button(label, () => {
      if (sendNow) return chatSubmit(text);
      chatParts.input.value = text;
      chatParts.input.focus();
    })));
  }

  function startChips(tx) {
    return [
      { label: tx.maxChipNow, text: tx.maxChipNow, send: true },
      { label: tx.maxChipStart, text: tx.maxChipStart, send: true },
      { label: tx.maxChipTask, text: tx.maxChipTaskPrefix },
      { label: tx.maxChipIdea, text: tx.maxChipIdeaPrefix },
    ];
  }

  function talkFor(ms) {
    const svg = lamp.querySelector('svg');
    try { window.startMaxTalking(svg); } catch { return; }
    setTimeout(() => { try { window.stopMaxTalking(svg); } catch { /* the page changed */ } }, ms);
  }

  // Dictated text waits three seconds before it is sent, so it can be fixed:
  // a click or a key in the text box, or the microphone, cancels it.
  function cancelSend() {
    clearTimeout(sendTimer);
    sendTimer = null;
  }

  function scheduleSend() {
    cancelSend();
    const tx = t(lampLang);
    let left = 3;
    const tick = () => {
      left -= 1;
      if (left <= 0) {
        sendTimer = null;
        chatSubmit(chatParts?.input.value ?? '');
        return;
      }
      if (chatParts) chatParts.status.textContent = tx.maxSendingIn(left);
      sendTimer = setTimeout(tick, 1000);
    };
    if (chatParts) chatParts.status.textContent = tx.maxSendingIn(left);
    sendTimer = setTimeout(tick, 1000);
  }

  // Microphone on any page: the hidden recording page of the extension listens
  // (offscreen/mic.js), the worker passes the words back here.
  function startDictation(purpose) {
    const tx = t(lampLang);
    micPurpose = purpose;
    micHeard = '';
    micListening = true;
    if (chatParts) {
      chatParts.mic.textContent = `■ ${tx.maxMicStop}`;
      chatParts.mic.setAttribute('aria-pressed', 'true');
    }
    send({ type: MSG.MIC_START, lang: lampLang });
    send({ type: MSG.SPEAK, text: tx.maxListening, lang: lampLang });
    announce(live, tx.maxListening);
  }

  function toggleMic() {
    cancelSend();
    if (micListening) return send({ type: MSG.MIC_STOP });
    startDictation('chat');
  }

  // What the user said in answer to a pop-up: the button they meant, pressed.
  function answerCardByVoice(text) {
    const tx = t(lampLang);
    const card = lastCard;
    const action = card && matchSpokenAction(text, card.actions);
    if (!action) {
      const labels = (card?.actions ?? []).map((a) => plainText(a.label)).filter(Boolean);
      send({ type: MSG.SPEAK, text: `${tx.maxNotUnderstood} ${labels.length ? t(card.lang).spokenOptions(labels) : ''}`, lang: lampLang });
      return;
    }
    send({ type: MSG.SPEAK, text: tx.maxHeardAnswer(plainText(action.label)), lang: lampLang });
    action.onSelect?.();
    if (!action.keepOpen) window.closeMaxIntervention?.(maxAnchor);
  }

  function onMicEvent(msg) {
    const tx = t(lampLang);
    const status = (text) => { if (chatParts) chatParts.status.textContent = text; };
    switch (msg.event) {
      case 'state':
        status(msg.state === 'transcribing' ? tx.maxTranscribing : tx.maxListening);
        break;
      case 'text':
        micHeard = msg.text;
        if (micPurpose === 'chat' && chatParts) chatParts.input.value = msg.text;
        break;
      case 'error':
        if (msg.code === 'not-allowed') status(tx.maxMicAllow);
        else status(msg.code === 'no-speech' ? tx.maxNoSpeech : tx.maxSttFailed);
        break;
      case 'end':
        micListening = false;
        if (chatParts) {
          chatParts.mic.textContent = `🎤 ${tx.maxMic}`;
          chatParts.mic.setAttribute('aria-pressed', 'false');
        }
        if (micHeard && micPurpose === 'answer') answerCardByVoice(micHeard);
        else if (micHeard && chatParts) scheduleSend();
        break;
      default:
    }
  }

  // Alt+Shift+V: with a pop-up open, answer it with the voice; otherwise open Max and speak to him.
  function dictateFromShortcut() {
    if (micListening) return send({ type: MSG.MIC_STOP });
    if (cardOpen() && !chatOpen) return startDictation('answer');
    if (!chatOpen) openChat();
    startDictation('chat');
  }

  // "Return to my work": the same resume action as the card's "Take me
  // back" (showTaskMode), just reachable from chat too, since chat has no
  // task id of its own to act on -- ask the server which task is in
  // progress right now, then resume that one.
  async function returnToWork() {
    const tx = t(lampLang);
    chatParts.status.textContent = '';
    const stateRes = await send({ type: MSG.CHAT_STATE });
    const doing = stateRes?.ok ? (stateRes.data.tasks ?? []).find((task) => task.status === 'doing') : null;
    if (!doing) {
      chatParts.status.textContent = tx.resumeNothing;
      return;
    }
    const res = await send({ type: MSG.TASK_ACTION, id: doing.id, action: 'resume' });
    if (!res?.ok || !(res.data?.resumeUrl || res.data?.searchQuery)) {
      chatParts.status.textContent = tx.resumeNothing;
    }
  }

  async function chatSubmit(text) {
    cancelSend();
    text = text.trim();
    if (!text || chatBusy) return;
    const tx = t(lampLang);
    chatBusy = true;
    chatParts.input.disabled = chatParts.send.disabled = true;
    chatParts.input.value = '';
    chatParts.chips.replaceChildren();
    chatLine('user', text);
    chatParts.status.textContent = tx.maxThinking;
    const res = await send({ type: MSG.CHAT, text, lang: navigator.language });
    if (!chatOpen) return; // closed while waiting
    chatBusy = false;
    chatParts.input.disabled = chatParts.send.disabled = false;
    chatParts.status.textContent = '';
    if (!res?.ok) {
      chatParts.input.value = text;
      chatParts.status.textContent = res?.unavailable ? tx.maxOffline : res?.error === 'rate_limited' ? tx.maxBusy(/\d+/.exec(res.message ?? '')?.[0] ?? '20') : tx.maxFailed;
      chatParts.input.focus();
      return;
    }
    const replies = res.data.newMessages ?? [];
    replies.forEach((line) => chatLine('max', line));
    announce(live, replies.join(' '));
    send({ type: MSG.SPEAK, text: replies.map(plainText).join(' '), lang: lampLang });
    talkFor(1800);
    updateLamp();
    const suggestions = res.data.suggestions ?? [];
    chatChips(suggestions.map((label) => ({ label, text: label, send: true })));
    chatParts.input.focus();
    // Stays open regardless of what he said: only the \u2715 button (closeChat)
    // or starting a new page interaction closes it now, never a timer.
    placeCaptureNearMax(chatPanel);
  }

  function toggleChat() {
    if (chatOpen) closeChat({ returnFocus: true });
    else openChat();
  }

  function openChat() {
    if (chatOpen) return;
    if (cardOpen()) hide(); // one thing at a time beside him
    if (!parked.hidden) closeCapture();
    chatOpen = true;
    chatBusy = false;
    const tx = t(lampLang);
    const log = el('div', { class: 'chat-log', role: 'log', 'aria-live': 'off' });
    const input = el('input', { type: 'text', maxlength: 1000, autocomplete: 'off', placeholder: tx.maxPlaceholder, 'aria-label': tx.maxPlaceholder });
    const sendBtn = el('button', { type: 'submit', text: tx.maxSend });
    const micBtn = button(`🎤 ${tx.maxMic}`, () => toggleMic(), { 'aria-pressed': 'false' });
    // Always present, not tied to any one reply: whatever Max just said, this
    // is the one way back to the page the user actually left (same resume
    // action as the card's "Take me back" -- tasks_life.go's DoingTask).
    const returnBtn = button(tx.maxReturnToWork, () => returnToWork(), { class: 'chat-return' });
    const status = el('p', { class: 'chat-status', role: 'status', 'aria-live': 'polite' });
    const chips = el('div', { class: 'chips' });
    chatParts = { log, input, send: sendBtn, mic: micBtn, status, chips };
    const form = el('form', { class: 'chat-form', novalidate: true, onsubmit: (event) => {
      event.preventDefault();
      chatSubmit(input.value);
    } }, input, micBtn, sendBtn, returnBtn);
    // Touching the text means "I want to fix it": nothing is sent by itself, and a running recording is dropped.
    const takeOver = () => {
      cancelSend();
      if (micListening) send({ type: MSG.MIC_ABORT });
    };
    input.addEventListener('pointerdown', takeOver);
    input.addEventListener('keydown', takeOver);
    chatPanel.setAttribute('role', 'dialog');
    chatPanel.replaceChildren(
      el('div', { class: 'chat-head' }, el('span', { text: 'Max' }), button('✕', () => closeChat({ returnFocus: true }), { 'aria-label': tx.maxClose })),
      log, form, status, chips,
    );
    chatLine('max', tx.maxHello);
    chatChips(startChips(tx));
    chatPanel.hidden = false;
    Promise.resolve(window.emergeMax?.(maxAnchor)).catch(() => {});
    placeCaptureNearMax(chatPanel);
    input.focus({ preventScroll: true });
    announce(live, tx.maxHello);
  }

  function closeChat({ returnFocus = false } = {}) {
    if (!chatOpen) return;
    cancelSend();
    if (micListening) send({ type: MSG.MIC_ABORT });
    chatOpen = false;
    chatBusy = false;
    if (returnFocus || chatPanel.contains(activeElement())) lamp.focus({ preventScroll: true });
    chatPanel.hidden = true;
    chatPanel.replaceChildren();
    Promise.resolve(window.returnMax?.(maxAnchor)).catch(() => {});
  }

  // --- wiring ----------------------------------------------------------------

  const isForeground = () => document.visibilityState === 'visible' && document.hasFocus();

  function acknowledgeRendered(msg) {
    const version = renderVersion;
    // Two frames give the connected, visible card a paint opportunity first.
    requestAnimationFrame(() => requestAnimationFrame(() => {
      if (version !== renderVersion || !host.isConnected) return;
      const card = maxAnchor.querySelector('.max-card');
      if (!card || card.getAttribute('data-max-card-open') !== 'true') return;
      // Not acknowledged, but NOT taken down either: the server keeps the card
      // open and re-sends it (card.go). Sites that rewrite their own URL
      // (Google, Reddit) used to lose the card here the instant it appeared.
      if (!isForeground() || location.href !== msg.url) return;
      if (!card.getClientRects().length) return;
      send({ type: MSG.DELIVERY_ACK, deliveryId: msg.payload.deliveryId, documentToken, url: location.href });
    }));
  }

  root.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !chatPanel.hidden) {
      e.preventDefault();
      e.stopPropagation();
      closeChat({ returnFocus: true });
      return;
    }
    if (e.key === 'Escape' && !parked.hidden) {
      e.preventDefault();
      e.stopPropagation();
      closeCapture();
      return;
    }
    // maxAnchor may not exist yet this early; nothing to close in that case.
    if (e.key === 'Escape' && maxAnchor?.querySelector('.max-card[data-max-card-open="true"]')) {
      e.stopPropagation();
      // Escape sets a server card aside ("ignored"); otherwise it would follow
      // the user straight back on the next page.
      if (renderedDelivery) send({ type: MSG.CARD_CLOSE, deliveryId: renderedDelivery, reason: 'ignored' });
      hide({ returnFocus: true });
    }
  });

  // Registered BEFORE `await loadMax()` below, not after: Max's real
  // artwork/script load (fetch + dynamic import, a few hundred ms) must
  // never delay this listener existing, because the worker's very first
  // move on any event is PAGE_CONTEXT -- asking this content script for its
  // documentToken -- and drops the whole delivery if that round-trip fails
  // (background/events.js's route(): "no receiver: still report the
  // browser event", then bails on `!documentToken`). A real distraction
  // event landing in that loading window (routine on a fresh
  // navigation/reload, which is exactly when the content script is
  // reinjected) would otherwise silently eat that day's intervention for
  // the site, without the user ever seeing it. PAGE_CONTEXT and
  // CAPTURE_PERMISSION don't touch Max at all, so they run immediately;
  // COMMAND/DISMISS_UNSOLICITED/FOCUS_BUBBLE wait on `maxReady` (the
  // staleness checks below still run immediately, so a message that
  // shouldn't render is still rejected right away either way).
  chrome.runtime.onMessage.addListener((msg, _sender, respond) => {
    switch (msg?.type) {
      case MSG.PAGE_CONTEXT:
        respond({ documentToken, url: location.href, visible: isForeground() });
        break;
      case MSG.CAPTURE_PERMISSION:
        if (msg.documentToken !== documentToken || msg.url !== location.href) break;
        capture.setAllowed(msg.allowed);
        break;
      case MSG.PAGE_METADATA:
        // Asked by the worker only when the server allowed it (blacklisted page).
        if (msg.documentToken !== documentToken || msg.url !== location.href) break;
        respond(readPageMetadata());
        break;
      case MSG.COMMAND:
        if (msg.documentToken !== documentToken || msg.url !== location.href || msg.title !== document.title || !isForeground()) break;
        maxReady.then(() => {
          const payload = msg.payload ?? {};
          // The server closed a card this page is still showing.
          if (payload.closeDeliveryId && payload.closeDeliveryId === renderedDelivery) hide();
          if (msg.command === 'CLOSE_CARD') return;
          if (msg.command === 'SHOW_THUMBS_UP') return showThumbsUp(payload); // not a card: nothing to dismiss or acknowledge
          // The same open card, re-sent as it follows the user: keep it as it is.
          if (payload.deliveryId && payload.deliveryId === renderedDelivery && cardOpen()) return;
          if (msg.command === 'SHOW_RAMP') showRamp(payload);
          else if (msg.command === 'ASK_COMPLETION') showCompletion(payload);
          else if (msg.command === 'ASK_DAY') showAskDay(payload);
          else if (msg.command === 'SHOW_NEW_ERA') showNewEra(payload);
          else if (msg.command === 'SHOW_TASK' && payload.task) showTask(payload);
          else if (msg.command === 'SHOW_WELCOME_BACK') showWelcomeBack(payload);
          else if (msg.command === 'SHOW_TREASURE') showTreasure(payload);
          else if (msg.command === 'SHOW_OLD_IDEA' && payload.idea) showOldIdea(payload);
          else return;
          renderedCommand = { command: msg.command, site: payload.site };
          renderedDelivery = payload.deliveryId;
          if (ACKNOWLEDGED.has(msg.command) && payload.deliveryId) acknowledgeRendered(msg);
        }).catch(() => {}); // matches events.js's signal(): a render failure here must not become an unhandled rejection
        break;
      case MSG.DISMISS_UNSOLICITED:
        maxReady.then(() => {
          if (renderedCommand && msg.commands?.includes(renderedCommand.command) &&
              (!msg.site || msg.site === renderedCommand.site)) hide();
        }).catch(() => {});
        break;
      case MSG.DICTATE:
        maxReady.then(dictateFromShortcut).catch(() => {});
        break;
      case MSG.MIC_EVENT:
        maxReady.then(() => onMicEvent(msg)).catch(() => {});
        break;
      case MSG.OPEN_MAX:
        // Alt+Shift+M: Max opens on this page, with the cursor in his text box.
        maxReady.then(() => {
          if (!chatOpen) openChat();
          chatParts?.input.focus();
        }).catch(() => {});
        break;
      case MSG.FOCUS_BUBBLE:
        // Reachable by shortcut because the card never takes focus on its own.
        maxReady.then(() => {
          const openCard = maxAnchor.querySelector('.max-card[data-max-card-open="true"]');
          if (!openCard || captureForm) { openCapture(); return; }
          openCard.querySelector('button')?.focus();
        }).catch(() => {});
        break;
    }
  });

  const followMax = () => {
    if (!parked.hidden) placeCaptureNearMax();
    if (!chatPanel.hidden) placeCaptureNearMax(chatPanel);
  };
  const loaded = await loadMax(root, () => toggleChat(), popupCheck, followMax);
  window.addEventListener('resize', followMax);
  if (!loaded) { host.remove(); return; } // sign-in popup window
  if (!host.isConnected) return; // page navigated away during the async load
  maxAnchor = loaded.maxAnchor;
  lamp = loaded.lamp;
  lamp.addEventListener('pointerdown', () => { if (!captureForm) rememberCaptureOrigin(); });
  markMaxReady();
  updateLamp();
  document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible') updateLamp(); });
}
