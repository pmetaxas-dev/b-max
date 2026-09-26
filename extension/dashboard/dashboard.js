// B-MAX dashboard: one calm page, no scrolling. Max and his name, the one task
// to do NOW, the rest of today, and a chat that opens only when you ask. While
// getting to know each other the chat is the whole page. The page refreshes
// itself, so tasks made from the lamp on any website show up here.

import * as api from '../shared/api.js';
import { announce, button, clear, el } from '../shared/a11y.js';
import { createPlanetView } from '../shared/planet-placeholder.js';
import { createChat } from '../shared/chat.js';
import { createFocusSound, SOUND_KINDS } from '../shared/focus-sound.js';
import { searchUrl } from '../shared/search.js';
import { ERAS } from '../shared/planet-state.js';
import { browserLang, createSpeaker, readSetting, writeSetting } from '../shared/voice-io.js';
import { MSG } from '../shared/messaging.js';
import { t } from '../shared/i18n.js';
import { confirmAndRegenerate, confirmAndReset } from './dev-tools.js';

const app = document.getElementById('app');
const live = document.getElementById('live');
const planetSlot = document.getElementById('planet-slot');
const planet = createPlanetView('full');
planetSlot.append(planet.element);
const sound = createFocusSound();
const voice = createSpeaker(); // "Max speaks": one setting for the chat, the pop-ups and this page

const DONE_VISIBLE_MS = 3 * 60_000; // a finished task stays ticked this long, then goes
const TICK_MS = 30_000; // re-draw (times, finished tasks); every second tick asks the server
const DRAWER_IDLE_MS = 2 * 60_000; // the chat closes itself after this much quiet
const SAY_MS = 10_000; // how long a message from Max stays under his name
const LATER_SHOWN = 5;
const CONFIRM_MS = 8000; // an unanswered "did you finish?" goes away by itself
const WAVE_MS = 2700;
const LEAVE_ONBOARDING_MS = 4000; // time to read his last words before the page becomes the dashboard

const UI = {
  en: {
    role: 'your companion', now: 'NOW', next: 'Next', talk: 'Talk to Max', close: 'Close', chat: 'Chat with Max',
    goal: 'My goal', ideas: 'Saved ideas', noIdeas: 'No ideas saved yet.',
    noTasks: 'Nothing on your list yet. Tell Max what you need to do.', allDone: 'All done for now. Well done!',
    finish: '✓ Done', pause: '⏸ Pause', resume: '▶ Continue', undo: '↩ Undo', goThere: '🔍 Go there',
    sure: (t) => `Did you really finish “${t}”?`, yes: 'Yes, I finished', no: 'Not yet',
    ago: (m) => `started ${m}′ ago`, worked: (m) => `${m}′ on related pages`, leftOff: (n) => `You left off: ${n}`,
    soundTitle: 'Focus sound', soundHint: 'A calm sound in the background. It plays while this page is open.',
    off: 'Off', brown: 'Deep calm', rain: 'Rain', waves: 'Waves', volume: 'Volume',
    more: (n) => `+${n} more`, howLong: '⏱ how long?', doneAria: 'Done', newFromMax: 'New message from Max',
    chest: 'Idea chest', chestHint: 'Throw in anything that comes to mind. It is kept safe, and Max will remind you of it when the time is right.',
    chestPlaceholder: 'A thought, an idea…', chestAdd: 'Keep it', chestKept: 'Kept safe.', chestEmpty: 'The chest is empty for now.',
    toTask: 'Do it today', del: 'Delete', delSure: (t) => `Delete “${t}” for good?`, delYes: 'Yes, delete', delNo: 'Keep it',
    a11yTitle: 'Accessibility', maxSpeaks: 'Max speaks (answers, pop-ups on every tab)', readStatus: '🔊 Tell me where I am',
    largeText: 'Large text', contrast: 'High contrast', on: 'on', off2: 'off', shortcuts: 'Shortcuts',
    keyStatus: 'Max says where you are, on any tab', keyMax: 'Open Max on the current page', keyDictate: 'Speak to Max, or answer a pop-up with your voice', keyCard: 'Reach the buttons of a pop-up', keyEsc: 'Close the chat or a panel',
  },
  el: {
    role: 'ο βοηθός σου', now: 'ΤΩΡΑ', next: 'Μετά', talk: 'Μίλα στον Max', close: 'Κλείσιμο', chat: 'Συζήτηση με τον Max',
    goal: 'Ο στόχος μου', ideas: 'Αποθηκευμένες ιδέες', noIdeas: 'Δεν έχεις αποθηκεύσει ιδέες ακόμα.',
    noTasks: 'Η λίστα σου είναι άδεια. Πες στον Max τι πρέπει να κάνεις.', allDone: 'Όλα έτοιμα προς το παρόν. Μπράβο!',
    finish: '✓ Τελείωσα', pause: '⏸ Παύση', resume: '▶ Συνέχεια', undo: '↩ Αναίρεση', goThere: '🔍 Πάμε εκεί',
    sure: (t) => `Τελείωσες πραγματικά το «${t}»;`, yes: 'Ναι, τελείωσα', no: 'Όχι ακόμα',
    ago: (m) => `ξεκίνησες πριν ${m}′`, worked: (m) => `${m}′ σε σχετικές σελίδες`, leftOff: (n) => `Είχες μείνει: ${n}`,
    soundTitle: 'Ήχος εστίασης', soundHint: 'Ένας ήρεμος ήχος στο παρασκήνιο. Παίζει όσο είναι ανοιχτή αυτή η σελίδα.',
    off: 'Κλειστό', brown: 'Βαθιά ηρεμία', rain: 'Βροχή', waves: 'Κύματα', volume: 'Ένταση',
    more: (n) => `+${n} ακόμα`, howLong: '⏱ πόση ώρα;', doneAria: 'Ολοκληρώθηκε', newFromMax: 'Νέο μήνυμα από τον Max',
    chest: 'Σεντούκι ιδεών', chestHint: 'Πέτα εδώ ό,τι σου έρθει στο μυαλό. Κρατιέται ασφαλές και ο Max θα σου το θυμίσει στην κατάλληλη στιγμή.',
    chestPlaceholder: 'Μια σκέψη, μια ιδέα…', chestAdd: 'Κράτα το', chestKept: 'Κρατήθηκε.', chestEmpty: 'Το σεντούκι είναι άδειο για την ώρα.',
    toTask: 'Κάν’ το σημερινό', del: 'Διαγραφή', delSure: (t) => `Να διαγραφεί οριστικά το «${t}»;`, delYes: 'Ναι, διαγραφή', delNo: 'Κράτα το',
    a11yTitle: 'Προσβασιμότητα', maxSpeaks: 'Ο Max μιλάει (απαντήσεις, pop-ups σε κάθε tab)', readStatus: '🔊 Πες μου πού βρίσκομαι',
    largeText: 'Μεγάλο κείμενο', contrast: 'Υψηλή αντίθεση', on: 'ναι', off2: 'όχι', shortcuts: 'Συντομεύσεις',
    keyStatus: 'Ο Max λέει πού βρίσκεσαι, σε οποιοδήποτε tab', keyMax: 'Άνοιγμα του Max στην τρέχουσα σελίδα', keyDictate: 'Μίλα στον Max ή απάντησε σε pop-up με τη φωνή σου', keyCard: 'Πρόσβαση στα κουμπιά ενός pop-up', keyEsc: 'Κλείσιμο του chat ή ενός panel',
  },
};

let lastFull;
let state = null; // the last ChatState from the server
let chat = null;
let maxAnchor = null;
let known = 0; // messages already shown
let shownEra = null; // the planet's era the user last saw
let ticks = 0;
let tickTimer = null;
let idleTimer = null;
let sayTimer = null;
let leaving = false; // the last words of getting-to-know-each-other are being read
let confirmId = null; // the task whose "did you finish?" is showing
let confirmTimer = null;
let peekAnchor = null; // the second Max, who peeks out from behind the chat
let refs = {}; // the elements render() fills

const ui = () => UI[chat?.lang === 'el' ? 'el' : 'en'];

// --- popovers: goal, sound, test tools -----------------------------------------
const popovers = [];
function closePopovers() {
  for (const { pop, btn } of popovers) {
    pop.hidden = true;
    btn.setAttribute('aria-expanded', 'false');
  }
}
function bindPopover(btn, pop, render, { below } = {}) {
  popovers.push({ pop, btn });
  btn.addEventListener('click', async (event) => {
    event.stopPropagation();
    const opening = pop.hidden;
    closePopovers();
    if (!opening) return;
    await render();
    if (below) {
      const r = btn.getBoundingClientRect();
      pop.style.left = `${Math.max(12, r.left)}px`;
      pop.style.top = `${r.bottom + 8}px`;
    }
    pop.hidden = false;
    btn.setAttribute('aria-expanded', 'true');
  });
  pop.addEventListener('click', (event) => event.stopPropagation());
}
document.addEventListener('click', closePopovers);
document.addEventListener('keydown', (event) => {
  if (event.key !== 'Escape') return;
  const open = popovers.find(({ pop }) => !pop.hidden);
  closePopovers();
  open?.btn.focus(); // a keyboard user lands where they were
});

// --- ⚙️ test tools (hackathon build only; see dev-tools.js) --------------------
const devPanel = document.getElementById('dev-panel');
const gear = document.getElementById('dev-gear');

function renderDevPanel() {
  const tx = t(lastFull?.lang);
  const status = el('p', { class: 'muted', role: 'status', 'aria-live': 'polite' });
  const reset = (scope) => async () => {
    try {
      const done = await confirmAndReset(scope, {
        api, text: tx, storage: chrome.storage?.local,
        confirm: (message) => window.confirm(message), prompt: (message) => window.prompt(message),
      });
      if (!done) return;
      status.textContent = tx.devDone;
      await load();
    } catch (err) {
      status.textContent = err.message;
    }
  };
  const regenerate = async () => {
    try {
      status.textContent = tx.devWorking;
      if (!(await confirmAndRegenerate({ api, text: tx, confirm: (message) => window.confirm(message) }))) {
        status.textContent = '';
        return;
      }
      status.textContent = tx.devDone;
      await load();
    } catch (err) {
      status.textContent = err.message;
    }
  };
  const fast = el('input', {
    type: 'checkbox', id: 'dev-fast', checked: !!lastFull?.fastTimings,
    onchange: async (event) => {
      try {
        await api.devTimings(event.target.checked);
        status.textContent = tx.devDone;
      } catch (err) {
        event.target.checked = !event.target.checked;
        status.textContent = err.message;
      }
    },
  });
  // App Clock (Phase 5): days pass for the demo without waiting for them.
  const clock = (body) => async () => {
    try {
      await api.devClock(body);
      status.textContent = tx.devDone;
      await load();
    } catch (err) {
      status.textContent = err.message;
    }
  };
  clear(devPanel);
  devPanel.append(
    el('h2', { text: tx.devTitle }),
    el('div', { class: 'row' },
      button(tx.devDay, reset('day'), { class: 'secondary' }),
      button(tx.devRegenerate, regenerate, { class: 'secondary' }),
      button(tx.devProgress, reset('progress'), { class: 'secondary' }),
      button(tx.devProfile, reset('profile'), { class: 'secondary' }),
    ),
    el('p', { class: 'muted', text: tx.devClockLabel(lastFull?.clockOffsetDays ?? 0) }),
    el('div', { class: 'row' },
      button(tx.devNextDay, clock({ addDays: 1, visited: true }), { class: 'secondary' }),
      button(tx.devAway, clock({ addDays: 5 }), { class: 'secondary' }),
      button(tx.devClockReset, clock({ reset: true }), { class: 'secondary' }),
    ),
    el('label', { for: 'dev-fast', class: 'dev-fast' }, fast, tx.devFast),
    status,
  );
}
bindPopover(gear, devPanel, renderDevPanel);

// --- focus sound ----------------------------------------------------------------
const soundBtn = document.getElementById('sound-btn');
const soundPop = document.getElementById('sound-pop');

function renderSoundPanel() {
  const tx = ui();
  const choices = ['off', ...SOUND_KINDS].map((kind) =>
    button(tx[kind], async () => {
      await sound.play(kind);
      renderSoundPanel();
      soundBtn.textContent = sound.kind === 'off' ? '🎧' : '🎵';
    }, { class: 'secondary', 'aria-pressed': String(sound.kind === kind) }));
  const volume = el('input', {
    type: 'range', min: '0', max: '1', step: '0.05', value: String(sound.volume), id: 'sound-volume',
    oninput: (event) => sound.setVolume(event.target.value),
  });
  clear(soundPop);
  soundPop.append(
    el('h2', { text: tx.soundTitle }),
    el('p', { class: 'muted', text: tx.soundHint }),
    el('div', { class: 'choices' }, ...choices),
    el('label', { for: 'sound-volume', text: tx.volume }),
    volume,
  );
}
bindPopover(soundBtn, soundPop, renderSoundPanel);

// --- accessibility: Max's voice, where am I, larger text, strong contrast --------------------
const a11yBtn = document.getElementById('a11y-btn');
const a11yPop = document.getElementById('a11y-pop');

function applyDisplayPrefs() {
  document.documentElement.classList.toggle('large-text', readSetting('bmax.large', '0') === '1');
  document.documentElement.classList.toggle('high-contrast', readSetting('bmax.contrast', '0') === '1');
}
applyDisplayPrefs();

async function speakStatusNow(target) {
  try {
    const res = await chrome.runtime.sendMessage({ type: MSG.STATUS_SPOKEN });
    target.textContent = res?.ok ? res.data.text : '';
  } catch {
    target.textContent = '';
  }
}

function renderA11yPanel() {
  const tx = ui();
  const status = el('p', { class: 'a11y-status', role: 'status', 'aria-live': 'polite', hidden: true });
  const toggle = (label, isOn, flip) => button(`${label}: ${isOn ? tx.on : tx.off2}`, () => {
    flip();
    renderA11yPanel();
  }, { class: 'secondary', 'aria-pressed': String(isOn) });
  const pref = (key, value) => () => {
    writeSetting(key, readSetting(key, '0') === '1' ? '0' : '1');
    applyDisplayPrefs();
  };
  clear(a11yPop);
  a11yPop.append(
    el('h2', { text: tx.a11yTitle }),
    el('div', { class: 'choices' },
      toggle(tx.maxSpeaks, voice.enabled, () => voice.setEnabled(!voice.enabled)),
      button(tx.readStatus, async () => {
        status.hidden = false;
        await speakStatusNow(status);
      }, { class: 'secondary' }),
      toggle(tx.largeText, readSetting('bmax.large', '0') === '1', pref('bmax.large')),
      toggle(tx.contrast, readSetting('bmax.contrast', '0') === '1', pref('bmax.contrast'))),
    status,
    el('h2', { text: tx.shortcuts }),
    el('ul', { class: 'a11y-keys' },
      el('li', {}, el('kbd', { text: 'Alt+Shift+S' }), ` ${tx.keyStatus}`),
      el('li', {}, el('kbd', { text: 'Alt+Shift+M' }), ` ${tx.keyMax}`),
      el('li', {}, el('kbd', { text: 'Alt+Shift+V' }), ` ${tx.keyDictate}`),
      el('li', {}, el('kbd', { text: 'Alt+Shift+K' }), ` ${tx.keyCard}`),
      el('li', {}, el('kbd', { text: 'Esc' }), ` ${tx.keyEsc}`)),
  );
}
voice.onChange(() => { if (!a11yPop.hidden) renderA11yPanel(); });
bindPopover(a11yBtn, a11yPop, renderA11yPanel);

// --- Max ---------------------------------------------------------------------------
function setTalking(on) {
  for (const anchor of [maxAnchor, peekAnchor]) {
    const svg = anchor?.querySelector('.max > svg');
    if (!svg || !window.startMaxTalking) continue;
    try {
      if (on) window.startMaxTalking(svg);
      else window.stopMaxTalking(svg);
    } catch {
      /* the idle controller is not running yet */
    }
  }
}

async function buildMax(stage) {
  const base = '../shared/max/';
  const [lampSvg, bodySvg] = await Promise.all(
    [base + 'max.svg', base + 'max-body.svg'].map((url) => fetch(url).then((r) => r.text())),
  );
  const anchor = el('div', { class: 'max-anchor' });
  const body = el('div', { class: 'max-body' });
  body.innerHTML = bodySvg;
  const lamp = el('div', { class: 'max' });
  lamp.innerHTML = lampSvg;
  anchor.append(body, lamp);
  stage.append(anchor);
  const svg = lamp.querySelector('svg');
  svg.setAttribute('aria-hidden', 'true');
  body.querySelector('svg')?.setAttribute('aria-hidden', 'true');
  window.startMaxIdle?.(svg);
  return anchor;
}

const onboarding = () => document.body.classList.contains('onboarding');
const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// He is thinking while the answer comes: his eyes open and close.
function setThinking(on) {
  maxAnchor?.classList.toggle('thinking', on);
  peekAnchor?.classList.toggle('thinking', on);
}

// Hello: he stands up, waves, and (on the dashboard) goes back into his lamp.
async function greet() {
  if (!maxAnchor || window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return;
  if (!onboarding()) await Promise.resolve(window.emergeMax?.(maxAnchor)).catch(() => {});
  else await wait(1600); // he is standing up already
  if (!onboarding()) refs.stage?.classList.add('hello');
  maxAnchor.classList.add('waving');
  await wait(WAVE_MS);
  maxAnchor.classList.remove('waving');
  refs.stage?.classList.remove('hello');
  if (!onboarding()) await Promise.resolve(window.returnMax?.(maxAnchor)).catch(() => {});
}

// While the chat is open the second Max peeks out from behind its right side.
function peekBesideChat(on) {
  refs.peek?.classList.toggle('open', on);
}

function setMode(isOnboarding) {
  const was = onboarding();
  document.body.classList.toggle('onboarding', isOnboarding);
  if (!maxAnchor || was === isOnboarding) return;
  // In the first conversation Max stands up in full; afterwards only his lamp stays.
  const move = isOnboarding ? window.emergeMax : window.returnMax;
  Promise.resolve(move?.(maxAnchor)).catch(() => {});
}

// A message from Max while the chat is closed: under his name for a while.
function say(text) {
  if (!text || !refs.say) return;
  clearTimeout(sayTimer);
  announce(live, text); // a screen reader hears it
  voice.speak(text, chat?.lang ?? 'en'); // and Max's voice, if he is set to speak
  refs.say.textContent = text;
  refs.say.hidden = false;
  refs.dot.hidden = false;
  sayTimer = setTimeout(() => { refs.say.hidden = true; }, SAY_MS);
}

// --- the chat drawer ---------------------------------------------------------------
const drawerOpen = () => refs.drawer?.classList.contains('open');

function resetIdle() {
  clearTimeout(idleTimer);
  if (onboarding() || !drawerOpen()) return;
  idleTimer = setTimeout(closeDrawer, DRAWER_IDLE_MS);
}

function openDrawer() {
  refs.drawer.classList.add('open');
  refs.fab.setAttribute('aria-expanded', 'true');
  refs.fab.hidden = true;
  refs.dot.hidden = true;
  refs.say.hidden = true;
  peekBesideChat(true);
  chat.focus();
  resetIdle();
}

function closeDrawer({ focusButton = false } = {}) {
  if (onboarding() || !drawerOpen()) return;
  clearTimeout(idleTimer);
  refs.drawer.classList.remove('open');
  peekBesideChat(false);
  refs.fab.hidden = false;
  refs.fab.setAttribute('aria-expanded', 'false');
  if (focusButton) refs.fab.focus();
}

// --- tasks -------------------------------------------------------------------------
function visibleTasks(tasks) {
  const cutoff = Date.now() - DONE_VISIBLE_MS;
  const open = tasks.filter((x) => !x.doneAt);
  const done = tasks.filter((x) => x.doneAt && Date.parse(x.doneAt) > cutoff);
  return { open, done };
}

function meta(task, tx) {
  const parts = [];
  if (task.estimateMin > 0) parts.push(`${task.estimateAssumed ? '~' : ''}${task.estimateMin}′`);
  if (task.deadline) parts.push(`⏰ ${task.deadline}`);
  return parts.join(' · ');
}

function minutesSince(iso) {
  return Math.max(0, Math.round((Date.now() - Date.parse(iso)) / 60_000));
}

// A finished task must be a deliberate one: the first press only asks.
function askDone(task) {
  clearTimeout(confirmTimer);
  confirmId = task.id;
  confirmTimer = setTimeout(() => {
    confirmId = null;
    render();
  }, CONFIRM_MS);
  render();
  document.querySelector('.confirm button')?.focus();
}

function cancelDone() {
  clearTimeout(confirmTimer);
  confirmId = null;
  render();
}

function confirmRow(task) {
  const tx = ui();
  return el('div', { class: 'confirm', role: 'group', 'aria-label': tx.sure(task.text) },
    el('p', { text: tx.sure(task.text) }),
    button(tx.yes, () => completeTask(task)),
    button(tx.no, cancelDone, { class: 'quiet' }));
}

function renderNow(container, open) {
  const tx = ui();
  clear(container);
  const task = open[0];
  if (!task) {
    container.append(
      el('p', { class: 'now-label', text: tx.now }),
      el('p', { class: 'empty', text: state?.tasks?.length ? tx.allDone : tx.noTasks }),
    );
    return;
  }
  const doing = task.status === 'doing';
  const paused = task.status === 'paused';
  const parts = [meta(task, tx)];
  if (doing && task.startedAt) parts.push(tx.ago(minutesSince(task.startedAt)));
  if (doing && task.workedMin > 0) parts.push(tx.worked(task.workedMin));
  container.append(
    el('p', { class: 'now-label', text: tx.now }),
    el('h2', { class: 'now-title', text: task.text }),
    el('p', { class: 'now-meta', text: parts.filter(Boolean).join(' · ') }),
  );
  if (paused && (task.note || task.lastTitle)) container.append(el('p', { class: 'now-note', text: tx.leftOff(task.note || task.lastTitle) }));
  if (doing && task.startedAt) {
    const planned = (task.estimateMin || 30) * 60_000;
    const pct = Math.min(100, Math.round(((Date.now() - Date.parse(task.startedAt)) / planned) * 100));
    container.append(el('div', { class: 'meter', role: 'presentation' }, el('i', { style: `width:${pct}%` })));
  }
  // There is no start button: Max tracks the first task by himself, so the
  // buttons only say when it is finished, or set it aside.
  if (confirmId === task.id) {
    container.append(confirmRow(task));
    return;
  }
  const actions = el('div', { class: 'now-actions' });
  // Max takes the user to the page where the task starts (the page they were on,
  // or a search for it): the chat guides, it does not make conversation.
  if (task.kind !== 'offline') actions.append(button(tx.goThere, () => goToTask(task)));
  if (paused) {
    actions.append(
      button(tx.resume, () => taskAction(task.id, 'start')),
      button(tx.finish, () => askDone(task), { class: 'quiet' }),
    );
  } else {
    actions.append(button(tx.finish, () => askDone(task)));
    if (doing) actions.append(button(tx.pause, () => taskAction(task.id, 'pause'), { class: 'quiet' }));
  }
  container.append(actions);
}

function renderLater(container, open, done) {
  const tx = ui();
  clear(container);
  const rest = open.slice(1);
  if (!rest.length && !done.length) {
    container.hidden = true;
    return;
  }
  container.hidden = false;
  container.append(el('h2', { text: tx.next }));
  const list = el('ul', { class: 'task-list' });
  rest.slice(0, LATER_SHOWN).forEach((task) => {
    // Never a one-click finish: the tick only asks, and stays empty until you confirm.
    const box = el('input', {
      type: 'checkbox', 'aria-label': `${tx.doneAria}: ${task.text}`,
      onchange: (event) => {
        event.target.checked = false;
        askDone(task);
      },
    });
    const metaNode = el('span', { class: 't-meta', text: meta(task, tx) });
    if (task.needsEstimate) metaNode.append(el('span', { class: 'chip-q', text: tx.howLong }));
    list.append(el('li', {},
      el('label', { class: 'task' }, box, el('span', { class: 't-text', text: task.text }), metaNode),
      confirmId === task.id ? confirmRow(task) : null));
  });
  for (const task of done) {
    list.append(el('li', {}, el('label', { class: 'task done' },
      el('input', { type: 'checkbox', checked: true, disabled: true, 'aria-label': `${tx.doneAria}: ${task.text}` }),
      el('span', { class: 't-text', text: task.text }),
      button(tx.undo, () => taskAction(task.id, 'reopen'), { class: 'secondary undo' }))));
  }
  container.append(list);
  if (rest.length > LATER_SHOWN) container.append(el('p', { class: 'muted', text: tx.more(rest.length - LATER_SHOWN) }));
}

function renderChestChip() {
  const chip = refs.chest;
  if (!chip) return;
  const tx = ui();
  chip.replaceChildren(document.createTextNode(`💎 ${tx.chest}`));
  if (state?.ideasUnseen > 0) chip.append(el('span', { class: 'badge', text: String(state.ideasUnseen), 'aria-hidden': 'true' }));
  chip.setAttribute('aria-label', `${tx.chest}${state?.ideasUnseen ? `: ${state.ideasUnseen}` : ''}`);
}

// The idea chest: throw a thought in, see what is inside, do one today, or let go.
let chestDeleteId = null; // the idea whose "delete for good?" is showing

async function renderChestPanel() {
  const tx = ui();
  const pop = document.getElementById('chest-pop');
  let ideas = [];
  try {
    ideas = (await api.getIdeas()).filter((i) => i.type === 'idea');
  } catch {
    /* the box is still there */
  }
  const status = el('p', { class: 'muted', role: 'status', 'aria-live': 'polite' });
  const box = el('textarea', { id: 'chest-input', rows: 2, maxlength: 300, placeholder: tx.chestPlaceholder, 'aria-label': tx.chestPlaceholder });
  const again = async (message) => {
    await renderChestPanel();
    document.querySelector('#chest-pop .muted[role="status"]').textContent = message ?? '';
    fetchState();
  };
  const add = async () => {
    const text = box.value.trim();
    if (!text) return;
    try {
      await api.captureIdea(text, 'idea');
      await again(tx.chestKept);
      document.getElementById('chest-input').focus();
    } catch {
      status.textContent = '';
    }
  };
  box.addEventListener('keydown', (event) => {
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      add();
    }
  });
  clear(pop);
  pop.append(
    el('h2', { text: `💎 ${tx.chest}` }),
    el('p', { class: 'muted', text: tx.chestHint }),
    box,
    el('div', { class: 'row', style: 'margin-top:8px' }, button(tx.chestAdd, add)),
    status,
    ideas.length
      // An idea stays in the chest, for later. It only goes when the user deletes it
      // (after a question): it is never lost by accident.
      ? el('ul', { class: 'chest-list' }, ...ideas.slice().reverse().map((idea) => (chestDeleteId === idea.id
        ? el('li', { class: 'chest-item confirm', role: 'group', 'aria-label': tx.delSure(idea.text) },
          el('span', { text: tx.delSure(idea.text) }),
          button(tx.delYes, async () => {
            chestDeleteId = null;
            await api.ideaDelete(idea.id).catch(() => {});
            await again('');
          }),
          button(tx.delNo, async () => {
            chestDeleteId = null;
            await again('');
          }, { class: 'secondary' }))
        : el('li', { class: 'chest-item' },
          el('span', { text: idea.text }),
          button(tx.toTask, async () => {
            await api.ideaPromote(idea.id).catch(() => {});
            await again('');
          }, { class: 'secondary' }),
          button(`🗑 ${tx.del}`, async () => {
            chestDeleteId = idea.id;
            await again('');
          }, { class: 'secondary', 'aria-label': `${tx.del}: ${idea.text}` })))))
      : el('p', { class: 'muted', text: tx.chestEmpty }),
  );
  api.reviewIdeas().then(() => fetchState(), () => {}); // the lamp dims: the chest was looked at
}

function renderGoalChip() {
  const chip = refs.goal;
  if (!chip) return;
  chip.hidden = !state?.lifeGoal;
  chip.textContent = `🎯 ${state?.lifeGoal ?? ''}`;
  chip.setAttribute('aria-label', `${ui().goal}: ${state?.lifeGoal ?? ''}`);
}

async function renderGoalPanel() {
  const tx = ui();
  const pop = document.getElementById('goal-pop');
  let ideas = [];
  try {
    ideas = (await api.getIdeas()).filter((i) => i.type === 'idea');
  } catch {
    /* the goal is still shown */
  }
  clear(pop);
  pop.append(
    el('h2', { text: tx.goal }),
    el('p', { text: state?.lifeGoal ?? '' }),
    el('h2', { text: tx.ideas }),
    ideas.length ? el('ul', {}, ...ideas.slice(-8).reverse().map((i) => el('li', { text: i.text }))) : el('p', { class: 'muted', text: tx.noIdeas }),
  );
}

// Draws everything that depends on the tasks and the clock.
function render() {
  if (!state || !refs.now) return;
  const { open, done } = visibleTasks(state.tasks ?? []);
  refs.role.textContent = ui().role;
  refs.fab.replaceChildren(document.createTextNode(`💬 ${ui().talk}`), refs.dot);
  refs.drawer.querySelector('.drawer-head span').textContent = ui().chat;
  renderNow(refs.now, open);
  renderLater(refs.later, open, done);
  renderGoalChip();
  renderChestChip();
}

// The app language, top right (EL / EN): Max's words, the buttons, the lamp and the cards.
function showLang(lang) {
  document.documentElement.lang = lang;
  document.querySelectorAll('.lang button').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.lang === lang)));
}

document.querySelectorAll('.lang button').forEach((b) => b.addEventListener('click', async () => {
  const lang = b.dataset.lang;
  if (!chat || chat.lang === lang) return;
  try {
    await api.setLang(lang);
  } catch {
    return;
  }
  chat.setLang(lang);
  showLang(lang);
  render();
  fetchState();
}));

// A new era is a celebration: Max says it out loud, on the page you are on.
function celebrateEra(planetState) {
  const era = planetState?.era;
  if (!ERAS.includes(era)) return;
  if (shownEra && ERAS.indexOf(era) > ERAS.indexOf(shownEra)) {
    const tx = t(chat?.lang ?? 'en');
    say(`🌍 ${tx.eraTitle(tx.eras?.[era] ?? planetState.eraLabel)} ${tx.eraSub}`);
  }
  if (!shownEra || ERAS.indexOf(era) > ERAS.indexOf(shownEra)) shownEra = era;
}

function applyState(next) {
  state = next;
  celebrateEra(next.planet);
  if (chat && next.lang && next.lang !== chat.lang) chat.setLang(next.lang);
  showLang(chat?.lang ?? next.lang ?? 'en');
  refs.motivation.textContent = next.motivation || '';
  planetSlot.hidden = false;
  planet.update(next.planet);
  if (!next.onboarded) setMode(true);
  else if (onboarding() && !leaving) {
    // Getting to know each other is done: let him finish, then show the dashboard.
    leaving = true;
    setTimeout(() => {
      setMode(false);
      leaving = false;
      render();
      openDrawer(); // the conversation goes on: he is not shut away just because the list is ready
    }, LEAVE_ONBOARDING_MS);
  }
  render();
}

async function goToTask(task) {
  try {
    const res = await api.taskAction(task.id, 'resume');
    const url = /^https?:\/\//.test(res.resumeUrl || '') ? res.resumeUrl : searchUrl(res.searchQuery);
    if (url) await chrome.tabs.create({ url });
    await fetchState();
  } catch {
    /* the button stays; the server may be unavailable */
  }
}

async function taskAction(id, action) {
  try {
    await api.taskAction(id, action);
    await fetchState();
  } catch (err) {
    if (err instanceof api.ServerUnavailableError) chat?.showStatus('');
  }
}

// A finished task: the tick, then Max celebrates in a message under his name.
async function completeTask(task) {
  cancelDone();
  try {
    await api.taskAction(task.id, 'yes');
  } catch {
    return;
  }
  try {
    const line = `✔ ${task.text}`;
    setThinking(true);
    const replies = await send({ message: line }).finally(() => setThinking(false));
    chat.addUser(line);
    for (const reply of replies) chat.addMax(reply, { animate: false });
    if (!drawerOpen()) say(replies.at(-1));
  } catch {
    await fetchState(); // Max could not answer: the tick still counts
  }
}

// One turn with the server. Returns what Max said, in order (empty when he stays quiet).
async function send(body) {
  try {
    const res = await api.chat({ lang: chat?.lang ?? browserLang(), ...body });
    known = res.messages.length;
    applyState(res);
    chat.setSuggestions(res.suggestions);
    return res.newMessages ?? [];
  } catch (err) {
    const wrapped = new Error(err.message);
    wrapped.offline = err instanceof api.ServerUnavailableError;
    wrapped.code = err.code;
    wrapped.seconds = /\d+/.exec(err.message ?? '')?.[0]; // "try again in 42 seconds"
    throw wrapped;
  }
}

// The server's view, without a turn: tasks made from the lamp on a website,
// finished tasks, and anything Max said elsewhere.
async function fetchState() {
  if (!chat) return;
  let next;
  try {
    next = await api.chatState();
  } catch {
    return; // keep what is shown; the next refresh tries again
  }
  const fresh = next.messages.length > known;
  if (fresh && !chat.busy) {
    chat.setMessages(next.messages);
    const last = next.messages.at(-1);
    if (last?.role === 'max' && !drawerOpen() && !onboarding()) say(last.text);
  }
  known = next.messages.length;
  applyState(next);
}

function tick() {
  ticks += 1;
  render();
  if (ticks % 2 === 0 && document.visibilityState === 'visible') fetchState();
}
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'visible') fetchState();
});

function renderUnavailable(err) {
  clear(app);
  const unavailable = err instanceof api.ServerUnavailableError;
  planet.update(null);
  app.append(el('section', { class: 'unavailable', role: 'alert' },
    el('h1', { text: unavailable ? 'Server unavailable' : 'Something went wrong' }),
    el('p', { class: 'muted', text: unavailable ? 'Start the local server and try again.' : err.message }),
    button('Try again', load)));
}

async function load() {
  clearInterval(tickTimer);
  clearTimeout(idleTimer);
  leaving = false;
  let full;
  let chatState;
  try {
    [full, chatState] = await Promise.all([api.getFull(), api.chatState()]);
  } catch (err) {
    return renderUnavailable(err);
  }
  lastFull = full;
  closePopovers();

  clear(app);
  const stage = el('div', { class: 'max-stage' });
  refs = {
    stage,
    role: el('span', { class: 'role' }),
    motivation: el('p', { id: 'motivation' }),
    say: el('button', { type: 'button', class: 'say', hidden: true, onclick: () => openDrawer() }),
    goal: el('button', { type: 'button', class: 'pill', id: 'goal-chip', 'aria-expanded': 'false', 'aria-controls': 'goal-pop', hidden: true }),
    chest: el('button', { type: 'button', class: 'pill', id: 'chest-chip', 'aria-expanded': 'false', 'aria-controls': 'chest-pop' }),
    now: el('section', { class: 'now', id: 'now-card', tabindex: '-1', 'aria-live': 'polite' }),
    later: el('section', { class: 'later', hidden: true }),
    dot: el('span', { class: 'dot', hidden: true, 'aria-hidden': 'true' }),
  };
  refs.fab = el('button', { type: 'button', class: 'fab', 'aria-expanded': 'false', 'aria-controls': 'chat-drawer', onclick: () => openDrawer() });
  refs.drawer = el('section', { class: 'drawer', id: 'chat-drawer' });

  app.append(
    el('header', { class: 'top' },
      stage,
      el('div', { class: 'who' }, el('h1', {}, 'Max', refs.role), refs.motivation)),
    refs.say,
    el('div', { class: 'row-chips' }, refs.goal, refs.chest),
    refs.now,
    refs.later,
    el('div', { class: 'spacer' }),
  );

  maxAnchor = await buildMax(stage);
  refs.peek = el('div', { class: 'max-peek', 'aria-hidden': 'true' });
  app.append(refs.peek);
  peekAnchor = await buildMax(refs.peek);
  Promise.resolve(window.emergeMax?.(peekAnchor)).catch(() => {}); // he shows his whole body from the side
  chat = createChat({
    onSend: (text) => send({ message: text }),
    onTalking: setTalking,
    onThinking: setThinking,
    onTurnDone: resetIdle,
    live,
  });
  refs.drawer.append(
    el('div', { class: 'drawer-head' }, el('span', { text: '' }),
      button('✕', () => closeDrawer({ focusButton: true }), { class: 'secondary', id: 'drawer-close' })),
    chat.element,
  );
  refs.drawer.addEventListener('pointerdown', resetIdle);
  refs.drawer.addEventListener('keydown', (event) => {
    resetIdle();
    if (event.key === 'Escape') closeDrawer({ focusButton: true });
  });
  app.append(refs.fab, refs.drawer); // inside the column: while getting to know each other the chat fills its free space

  chat.setMessages(chatState.messages);
  known = chatState.messages.length;
  chat.setSuggestions(chatState.suggestions);
  if (chatState.messages.length) chat.setLang(chatState.lang);
  document.body.classList.toggle('onboarding', !chatState.onboarded);
  if (!chatState.onboarded) Promise.resolve(window.emergeMax?.(maxAnchor)).catch(() => {});
  bindGoalChip();
  greet(); // he waves hello every time the app opens
  applyState(chatState);

  // Max opens the conversation. First time: the scripted welcome, typed out in
  // the chat. Later he only speaks when he has something to say (a task
  // without a time, or what was left from yesterday), and then quietly, under
  // his name, without opening the chat.
  chat.setBusy(true);
  try {
    const replies = await send({ kind: 'open' });
    chat.setBusy(false);
    if (onboarding()) {
      for (const line of replies) await chat.addMax(line);
    } else if (replies.length) {
      chat.setMessages(state.messages);
      say(replies.at(-1));
    }
  } catch {
    chat.setBusy(false);
  }
  if (onboarding()) chat.focus();
  tickTimer = setInterval(tick, TICK_MS);
  // Max's weekend card ("you collected some ideas") opens the chest.
  if (location.hash === '#treasure' && !onboarding()) refs.chest.click();
}

function bindGoalChip() {
  // The popover buttons are rebuilt on every load; register only the current ones.
  for (const [button, id, render] of [[refs.goal, 'goal-pop', renderGoalPanel], [refs.chest, 'chest-pop', renderChestPanel]]) {
    const existing = popovers.findIndex(({ pop }) => pop.id === id);
    if (existing >= 0) popovers.splice(existing, 1);
    bindPopover(button, document.getElementById(id), render, { below: true });
  }
}

load();
