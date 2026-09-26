// Return anchor capture (architecture-plan-v2 §9).
//
// The page can be hidden before anyone asks, so capture is continuous: every
// 30 seconds while visible, on visibilitychange, and on beforeunload. The worker
// heartbeat never triggers capture.
//
// Capture is allowed only when Go says so (payload.captureAnchor, relayed by the
// worker): never on blacklisted pages, non-web pages or outside a work context.
// Go re-checks every rule when the anchor arrives.

import { MSG, send } from '../shared/messaging.js';

export const MAX_SNIPPET = 120;
const TEXT_INPUT_TYPES = new Set(['text', 'search', 'url']);

const normalize = (s) => String(s ?? '').replace(/\s+/g, ' ').trim();
const clampPercent = (n) => Math.max(0, Math.min(100, Math.round(Number(n) || 0)));

/** Credentials and payment fields are never read (§9 hard rules). */
export function isSensitiveField({ type = '', autocomplete = '' }) {
  if (String(type).toLowerCase() === 'password') return true;
  return String(autocomplete)
    .toLowerCase()
    .split(/\s+/)
    .some((t) => t === 'current-password' || t === 'new-password' || t === 'one-time-code' || t === 'username' || t.startsWith('cc-'));
}

/**
 * Picks the best available tier from already-extracted candidates. Pure, so it
 * can be tested without a DOM.
 *   1 focused field: last ~120 chars before the caret
 *   2 current text selection
 *   3 nearest heading (plus scroll percentage)
 *   4 page title
 */
export function chooseAnchor(c) {
  const scrollPercent = clampPercent(c.scrollPercent);
  const field = normalize(c.fieldText);
  if (field) return { tier: 1, snippet: field.slice(-MAX_SNIPPET), scrollPercent };
  const selection = normalize(c.selectionText);
  if (selection) return { tier: 2, snippet: selection.slice(0, MAX_SNIPPET), scrollPercent };
  const heading = normalize(c.heading);
  if (heading) return { tier: 3, snippet: heading.slice(0, MAX_SNIPPET), scrollPercent };
  const title = normalize(c.title);
  if (title) return { tier: 4, snippet: title.slice(0, MAX_SNIPPET), scrollPercent };
  return null;
}

// --- DOM extraction -----------------------------------------------------------

function deepActiveElement(doc) {
  let node = doc.activeElement;
  while (node && node.shadowRoot && node.shadowRoot.activeElement) node = node.shadowRoot.activeElement;
  return node;
}

const hiddenFromAssistiveTech = (node) => !!node?.closest?.('[aria-hidden="true"]');

function textBeforeCaret(el, doc, win) {
  const tag = el.tagName;
  if (tag === 'TEXTAREA' || (tag === 'INPUT' && TEXT_INPUT_TYPES.has((el.type || 'text').toLowerCase()))) {
    const pos = el.selectionStart ?? el.value.length;
    return el.value.slice(Math.max(0, pos - MAX_SNIPPET * 2), pos);
  }
  if (el.isContentEditable) {
    const sel = win.getSelection();
    if (sel && sel.rangeCount && sel.isCollapsed && el.contains(sel.anchorNode)) {
      const range = doc.createRange();
      range.selectNodeContents(el);
      range.setEnd(sel.anchorNode, sel.anchorOffset);
      return range.toString();
    }
  }
  return '';
}

function nearestHeading(doc) {
  let best = null;
  let bestDistance = Infinity;
  const headings = doc.querySelectorAll('h1,h2,h3,h4,h5,h6');
  for (let i = 0; i < headings.length && i < 500; i++) {
    const h = headings[i];
    if (hiddenFromAssistiveTech(h)) continue;
    const text = normalize(h.textContent);
    if (!text) continue;
    const distance = Math.abs(h.getBoundingClientRect().top);
    if (distance < bestDistance) {
      best = text;
      bestDistance = distance;
    }
  }
  return best;
}

/** Reads the page. Only ever called when capture is allowed. */
export function collect(doc = document, win = window) {
  // The lamp is UI, never the user's work. Keep the previous anchor while typing there.
  if (doc.activeElement?.tagName === 'FOCUS-COMPANION') return null;
  const active = deepActiveElement(doc);
  const sensitive =
    !!active &&
    isSensitiveField({ type: active.type, autocomplete: active.getAttribute?.('autocomplete') });
  const readable = !!active && !sensitive && !hiddenFromAssistiveTech(active);

  let selectionText = '';
  if (!sensitive) {
    const sel = win.getSelection();
    if (sel && !sel.isCollapsed && !hiddenFromAssistiveTech(sel.anchorNode?.parentElement)) {
      selectionText = sel.toString();
    }
  }

  const scrollable = doc.documentElement.scrollHeight - win.innerHeight;
  return {
    fieldText: readable ? textBeforeCaret(active, doc, win) : '',
    selectionText,
    heading: nearestHeading(doc),
    scrollPercent: scrollable > 0 ? (win.scrollY / scrollable) * 100 : 0,
    title: doc.title,
  };
}

// --- scheduling ---------------------------------------------------------------

/** Starts capture. Returns a handle the page object uses to relay permission. */
export function startAnchorCapture() {
  let allowed = false;

  function capture() {
    if (!allowed) return;
    const candidates = collect();
    if (!candidates) return;
    const anchor = chooseAnchor(candidates);
    if (!anchor) return;
    send({ type: MSG.ANCHOR, anchor: { ...anchor, url: location.href, title: document.title } });
  }

  async function refreshPermission() {
    const res = await send({ type: MSG.CAN_CAPTURE });
    allowed = !!(res && res.ok && res.data === true);
  }

  refreshPermission();
  setInterval(async () => {
    if (document.visibilityState !== 'visible') return;
    await refreshPermission();
    capture();
  }, 30_000);
  // Both of these fire before the page hides, so the DOM is still readable.
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') capture();
  });
  window.addEventListener('beforeunload', capture);

  return {
    setAllowed(value) {
      allowed = !!value;
    },
  };
}
