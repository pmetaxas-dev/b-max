// Phase 4 sections of the full tab: the ideas treasure, the user's own
// recording and today's summary. Pure renderers; dashboard.js wires them.

import { button, el } from './a11y.js';
import { t } from './i18n.js';

/** The treasure: unseen ideas first, with "I've seen them"; older ones folded. */
export function treasureSection(ideas, onSeen, { lang } = {}) {
  const tx = t(lang);
  const only = ideas.filter((i) => i.type === 'idea');
  const unseen = only.filter((i) => !i.reviewed);
  const seen = only.filter((i) => i.reviewed);
  const item = (i) => el('li', { text: i.text });
  return el('section', { id: 'treasure', 'aria-labelledby': 'treasure-h', tabindex: '-1' },
    el('h2', { id: 'treasure-h', text: `💡 ${tx.treasureHeading} (${tx.ideasCount(unseen.length)})` }),
    unseen.length
      ? [el('ul', { class: 'ideas' }, ...unseen.map(item)), button(tx.treasureSeen, onSeen)]
      : el('p', { class: 'muted', text: tx.treasureEmpty }),
    seen.length
      ? el('details', {}, el('summary', { text: `${tx.treasureOlder} (${seen.length})` }), el('ul', {}, ...seen.map(item)))
      : null,
  );
}

/**
 * The user's own recording, played on demand. `loadVoice` resolves to a Blob
 * or null; `onPlayed` starts the server's cooldown (Max offers it again only
 * after 5 days). `createUrl` is URL.createObjectURL, injectable for tests.
 */
export function voiceSection({ lang, loadVoice, onPlayed, createUrl = (b) => URL.createObjectURL(b) }) {
  const tx = t(lang);
  const status = el('p', { class: 'muted', role: 'status' });
  const slot = el('div', {});
  const section = el('section', { id: 'voice', 'aria-labelledby': 'voice-h', tabindex: '-1' },
    el('h2', { id: 'voice-h', text: `🎙️ ${tx.voiceHeading}` }),
    el('p', { class: 'muted', text: tx.voiceHint }),
    slot,
    status,
  );
  const ready = (async () => {
    let blob = null;
    try {
      blob = await loadVoice();
    } catch {
      blob = null;
    }
    if (!blob) {
      status.textContent = tx.voiceMissing;
      return null;
    }
    const audio = el('audio', { controls: true, src: createUrl(blob), 'aria-label': tx.voiceHeading });
    let reported = false;
    audio.addEventListener('play', () => {
      if (!reported) { reported = true; onPlayed?.(); }
    });
    slot.append(audio);
    return audio;
  })();
  return { element: section, ready };
}

/** Today in one line, without judgement. */
export function daySummarySection(summary, { lang } = {}) {
  const tx = t(lang);
  return el('section', { 'aria-labelledby': 'today-sum-h' },
    el('h2', { id: 'today-sum-h', text: tx.todayHeading }),
    el('p', { text: tx.todayLine(summary) }),
  );
}
