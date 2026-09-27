import assert from 'node:assert/strict';
import test, { beforeEach, afterEach } from 'node:test';
import { applyLampState, start } from '../content/page-object.js';
import { MSG } from '../shared/messaging.js';

// A small DOM boundary double: tests our rendering/ack order, not Chrome paint.
// Max herself (idle/emerge/intervention-card) is a THIRD-PARTY dependency as
// far as this file is concerned -- extension/shared/max/*, verified against
// a real browser separately (prototype/mascot's own regression pass) -- so
// she gets a small double here too, in the same spirit as `Element`: just
// enough of showMaxIntervention/closeMaxIntervention's observable contract
// (a card that shows/hides, renders actions as real buttons, honours
// `keepOpen`, resolves `ready`) for these tests to keep verifying page-object
// .js's OWN control flow (ack timing, dismissal scoping, capture form).
let frames, messages, listener, focused, failRender, fakeCard, chatAnswer, maxCalls, taskActionAnswer, chatStateAnswer;
let storedLocal = {}, storageListener, dragOptions;
let originalInterval, originalTimeout, originalClearTimeout, timers, timerClock, nextTimer;
class Element {
  constructor(tag) {
    this.tagName = tag.toUpperCase(); this.children = []; this.hidden = false;
    this.attributes = {}; this.listeners = {}; this.value = '';
    this.style = { props: {}, setProperty(k, v) { this.props[k] = v; } };
  }
  setAttribute(key, value) { this.attributes[key] = value; }
  getAttribute(key) { return key in this.attributes ? this.attributes[key] : null; }
  addEventListener(type, fn) { this.listeners[type] = fn; }
  append(...children) { for (const child of children) { this.children.push(child); if (child instanceof Element) child.parent = this; } }
  remove() { if (this.parent) this.parent.children = this.parent.children.filter((c) => c !== this); this.parent = undefined; }
  replaceChildren(...children) { if (failRender) throw new Error('render failed'); this.children = []; this.append(...children); }
  attachShadow() { this.shadowRoot = new Element('shadow-root'); this.shadowRoot.parent = this; return this.shadowRoot; }
  get isConnected() { return this === document.body || !!this.parent?.isConnected; }
  getClientRects() { return this.isConnected && !this.hidden ? [{}] : []; }
  focus(options) { document.activeElement = this; this.focusOptions = options; }
  contains(node) { return node === this || this.children.some((child) => child instanceof Element && child.contains(node)); }
  // Enough of CSS to match this file's own selectors: a bare tag name
  // ('input'), a bare class ('.max-card'), or either plus one
  // [attr="value"]/[attr] suffix ('.max-card[data-max-card-open="true"]').
  matchesSelector(sel) {
    const m = /^(?:\.([\w-]+)|([a-zA-Z][\w-]*))?(?:\[([\w-]+)(?:=(?:"([^"]*)"|'([^']*)'))?\])?$/.exec(sel);
    if (!m) throw new Error(`test double querySelector cannot parse: ${sel}`);
    const [, cls, tag, attr, dq, sq] = m;
    if (cls && !(this.attributes.class || '').split(/\s+/).includes(cls)) return false;
    if (tag && this.tagName !== tag.toUpperCase()) return false;
    if (attr) {
      const val = dq ?? sq;
      if (val === undefined ? !(attr in this.attributes) : this.attributes[attr] !== val) return false;
    }
    return true;
  }
  querySelector(sel) {
    for (const child of this.children) {
      if (!(child instanceof Element)) continue;
      if (child.matchesSelector(sel)) return child;
      const match = child.querySelector(sel);
      if (match) return match;
    }
    return null;
  }
}

beforeEach(async () => {
  frames = []; messages = []; focused = true; failRender = false; fakeCard = null;
  chatAnswer = { newMessages: ['OK, I kept it.'], suggestions: ['Thanks'] }; maxCalls = [];
  taskActionAnswer = {};
  chatStateAnswer = { tasks: [{ id: 'idea_4', status: 'doing' }] };
  storageListener = undefined; dragOptions = undefined; storedLocal = {};
  originalInterval = globalThis.setInterval;
  originalTimeout = globalThis.setTimeout;
  originalClearTimeout = globalThis.clearTimeout;
  timers = new Map(); timerClock = 0; nextTimer = 0;
  globalThis.setTimeout = (fn, delay) => {
    const id = ++nextTimer;
    timers.set(id, { fn, at: timerClock + delay });
    return id;
  };
  globalThis.clearTimeout = (id) => timers.delete(id);
  globalThis.setInterval = () => 0; // anchor capture is unrelated to these tests
  globalThis.requestAnimationFrame = (fn) => frames.push(fn);
  // Max's own side-effecting scripts set these; loadMax() (page-object.js)
  // skips loading them for real once they're already present -- see there.
  globalThis.window = {
    addEventListener() {},
    innerWidth: 1200,
    innerHeight: 800,
    pickMaxVariant: () => 'peek-side',
    startMaxIdle: () => ({}),
    emergeMax: () => { maxCalls.push('emerge'); return Promise.resolve(); },
    returnMax: () => { maxCalls.push('return'); return Promise.resolve(); },
    enableMaxDrag: (_anchor, opts) => { dragOptions = opts; return () => {}; },
    showMaxIntervention: ({ anchor, title, lines, actions, onAction }) => {
      if (!fakeCard) { fakeCard = new Element('div'); fakeCard.setAttribute('class', 'max-card'); anchor.append(fakeCard); }
      fakeCard.hidden = false;
      fakeCard.setAttribute('data-max-card-open', 'true');
      const msg = new Element('p'); msg.textContent = title;
      const kids = [msg];
      if (lines?.length) { const sub = new Element('p'); sub.textContent = lines[0]; kids.push(sub); }
      if (actions?.length) {
        const actionsEl = new Element('div');
        for (const a of actions) {
          const btn = new Element('button');
          btn.textContent = a.label;
          btn.setAttribute('type', 'button');
          btn.listeners.click = () => {
            onAction?.(a.id);
            if (!a.keepOpen) globalThis.window.closeMaxIntervention(anchor);
          };
          actionsEl.append(btn);
        }
        kids.push(actionsEl);
      }
      fakeCard.replaceChildren(...kids); // shares `failRender` with the real DOM double, on purpose
      return { ready: Promise.resolve() };
    },
    closeMaxIntervention: () => {
      if (fakeCard) { fakeCard.hidden = true; fakeCard.setAttribute('data-max-card-open', 'false'); }
    },
  };
  globalThis.fetch = async () => ({ text: async () => '<svg></svg>' });
  globalThis.location = { href: 'https://www.youtube.com/watch?v=1' };
  globalThis.document = {
    body: new Element('body'), title: 'Video', visibilityState: 'visible',
    hasFocus: () => focused, createElement: (tag) => new Element(tag), addEventListener() {},
  };
  globalThis.chrome = {
    runtime: {
      getURL: (path) => 'fake://' + path,
      onMessage: { addListener(fn) { listener = fn; } },
      sendMessage: async (message) => {
        messages.push(message);
        if (message.type === MSG.CHAT) return { ok: true, data: chatAnswer };
        if (message.type === MSG.CHAT_STATE) return { ok: true, data: chatStateAnswer };
        if (message.type === MSG.TASK_ACTION) return { ok: true, data: taskActionAnswer };
        if (message.type === MSG.TASK_HELP) return { ok: true, data: { step: 'Open a blank document.', searchQueries: ['pitch outline'] } };
        return { ok: true, data: false };
      },
    },
    storage: {
      local: {
        get: async (key) => (key in storedLocal ? { [key]: storedLocal[key] } : {}),
        set: async (items) => { Object.assign(storedLocal, items); },
      },
      onChanged: { addListener(fn) { storageListener = fn; } },
    },
  };
  await start();
});
afterEach(() => {
  globalThis.setInterval = originalInterval;
  globalThis.setTimeout = originalTimeout;
  globalThis.clearTimeout = originalClearTimeout;
});

function advanceTime(ms) {
  timerClock += ms;
  for (const [id, timer] of timers) {
    if (timer.at <= timerClock) { timers.delete(id); timer.fn(); }
  }
}

function context() { let result; listener({ type: MSG.PAGE_CONTEXT }, {}, (value) => { result = value; }); return result; }
function command(kind = 'SHOW_RAMP') {
  return {
    type: MSG.COMMAND, command: kind, documentToken: context().documentToken,
    url: location.href, title: document.title,
    payload: { deliveryId: 'issued-1', site: 'youtube.com', step: { id: 's1', text: 'Write intro' } },
  };
}
function frame() { const callbacks = frames; frames = []; callbacks.forEach((fn) => fn()); }
// page-object.js defers COMMAND/DISMISS_UNSOLICITED/FOCUS_BUBBLE handling by
// one microtask tick behind `maxReady` (see its own comment) even once
// already resolved -- a `.then()` never runs synchronously. Tests that call
// `listener(...)` for one of those and then assert on the result await this
// first.
function flush() { return Promise.resolve(); }
function acks() { return messages.filter((m) => m.type === MSG.DELIVERY_ACK); }
// Max's card (fake, see beforeEach), analogous to the old plain bubble; it
// doesn't exist until the first showMaxIntervention() call, same as "never
// shown" reads as hidden.
function bubble() { return fakeCard || { hidden: true }; }

for (const kind of ['ASK_DAY', 'SHOW_RAMP', 'ASK_COMPLETION']) {
  test(`${kind} scoped dismissal cancels pending ack and preserves Lamp draft`, async () => {
    const { parked } = captureUI();
    await openCaptureByShortcut();
    const input = parked.querySelector('input');
    input.value = 'keep my thought';
    listener(command(kind));
    await flush();
    listener({ type: MSG.DISMISS_UNSOLICITED, commands: [kind], site: 'reddit.com' });
    await flush();
    assert.equal(bubble().hidden, false);
    listener({ type: MSG.DISMISS_UNSOLICITED, commands: [kind], site: 'youtube.com' });
    await flush();
    frame(); frame();
    assert.equal(bubble().hidden, true);
    assert.equal(acks().length, 0);
    assert.equal(parked.hidden, false);
    assert.equal(captureUI().live.textContent, '');
    assert.equal(input.value, 'keep my thought');
    assert.equal(document.activeElement, input);
  });

  test(`${kind} global dismissal applies on any site`, async () => {
    listener(command(kind));
    await flush();
    listener({ type: MSG.DISMISS_UNSOLICITED, commands: ['ASK_DAY', 'SHOW_RAMP', 'ASK_COMPLETION'] });
    await flush();
    frame(); frame();
    assert.equal(bubble().hidden, true);
    assert.equal(acks().length, 0);
  });
}

for (const kind of ['SHOW_RAMP', 'ASK_DAY']) {
  test(`${kind} acknowledges only after connected UI and a paint opportunity`, async () => {
    const msg = command(kind);
    listener(msg);
    await flush();
    assert.equal(bubble().hidden, false);
    assert.equal(acks().length, 0);
    frame();
    assert.equal(acks().length, 0);
    frame();
    assert.deepEqual(acks(), [{ type: MSG.DELIVERY_ACK, deliveryId: 'issued-1', documentToken: msg.documentToken, url: msg.url }]);
  });
}

test('completion renders without joining the acknowledgement lifecycle', async () => {
  listener(command('ASK_COMPLETION'));
  await flush();
  frame(); frame();
  assert.equal(bubble().hidden, false);
  assert.equal(acks().length, 0);
});

for (const stale of ['document', 'url', 'title', 'hidden', 'unfocused']) {
  test(`recipient rejects ${stale} context before rendering`, () => {
    const msg = command();
    if (stale === 'document') msg.documentToken = 'old-document-at-same-url';
    if (stale === 'url') location.href = 'https://example.com/work';
    if (stale === 'title') document.title = 'Another video';
    if (stale === 'hidden') document.visibilityState = 'hidden';
    if (stale === 'unfocused') focused = false;
    listener(msg);
    frame(); frame();
    assert.equal(bubble().hidden, true);
    assert.equal(acks().length, 0);
  });
}

test('render failure does not acknowledge', async () => {
  failRender = true;
  listener(command());
  // The render call happens inside maxReady.then(...).catch(() => {})
  // (page-object.js) -- a failure there is swallowed the same way
  // events.js's signal() swallows one, so there is nothing to assert.throws
  // on anymore; what matters is that a failed render still never acks.
  await flush();
  frame(); frame();
  assert.equal(acks().length, 0);
});

// A site that rewrites its own URL right after load (Google, Reddit) must not
// lose the card: it is simply not acknowledged; the server keeps it open.
test('a URL change before paint drops the acknowledgement but keeps the card', async () => {
  listener(command());
  await flush();
  frame();
  location.href = 'https://www.youtube.com/watch?v=1&t=5';
  frame();
  assert.equal(acks().length, 0);
  assert.equal(bubble().hidden, false);
});

test('detached UI is not acknowledged', () => {
  listener(command());
  document.body.children[0].parent = null;
  frame(); frame();
  assert.equal(acks().length, 0);
});

test('a superseded render is not acknowledged', () => {
  listener(command());
  listener(command('ASK_COMPLETION'));
  frame(); frame();
  assert.equal(acks().length, 0);
});

// root.children: [style, live, wrap(> parked), max-anchor(> max-body, lamp)].
// (The two max.css/max-intervention.css <link>s that a real run also
// appends are skipped here: window.showMaxIntervention already exists
// (the fake, installed in beforeEach), so loadMax()'s own "already loaded"
// guard never appends them -- see page-object.js.)
function captureUI() {
  const root = document.body.children[0].shadowRoot;
  return { root, lamp: root.children[3].children[1], parked: root.children[2].children[0], live: root.children[1] };
}
function editor() {
  const node = new Element('textarea');
  node.value = 'keep writing here';
  node.selectionStart = 5;
  node.selectionEnd = 12;
  document.body.append(node);
  node.focus();
  return node;
}
async function openCaptureByShortcut() { listener({ type: MSG.FOCUS_BUBBLE }); await flush(); }
function submit(form) { return form.listeners.submit({ preventDefault() {} }); }

for (const kind of ['idea', 'task']) {
  test(`${kind} capture confirms inline and restores editor focus and selection`, async () => {
    const work = editor();
    const { parked, live } = captureUI();
    await openCaptureByShortcut();
    const form = parked.querySelector('form');
    const input = form.querySelector('input');
    assert.equal(document.activeElement, input);
    assert.equal(form.querySelector('button').attributes.type, 'submit');
    assert.equal(form.children[0].tagName, 'LABEL');
    assert.equal(form.children[1].tagName, 'LABEL');
    form.querySelector('select').value = kind;
    input.value = 'A thought for later';
    chrome.runtime.sendMessage = async (message) => { messages.push(message); return { ok: true, data: { id: 'parked-1' } }; };
    messages = [];
    await submit(form);
    frame();
    // The save, then a read of the lamp's new count (Phase 4 brightness).
    assert.deepEqual(messages, [{ type: MSG.CAPTURE_IDEA, text: 'A thought for later', itemType: kind }, { type: MSG.LAMP_STATE }]);
    assert.equal(parked.children[0].textContent, 'Saved for later — you stay where you were.');
    assert.equal(live.textContent, parked.children[0].textContent);
    assert.equal(live.attributes['aria-live'], 'polite');
    assert.equal(document.activeElement, work);
    assert.deepEqual(work.focusOptions, { preventScroll: true });
    assert.equal(work.selectionStart, 5);
    assert.equal(work.selectionEnd, 12);
    assert.equal(parked.hidden, false);
    advanceTime(3999);
    assert.equal(parked.hidden, false);
    assert.equal(live.textContent, 'Saved for later — you stay where you were.');
    advanceTime(1);
    assert.equal(parked.hidden, true);
    assert.equal(parked.children.length, 0);
    assert.equal(live.textContent, '');
    assert.equal(document.activeElement, work);
    assert.equal(work.selectionStart, 5);
    assert.equal(work.selectionEnd, 12);
    assert.equal(messages.length, 2); // expiry is UI-only: nothing sent after the save and the lamp read
  });
}

test('pointer capture remembers focus before the lamp receives it', async () => {
  const work = editor();
  const { root, lamp, parked } = captureUI();
  lamp.listeners.pointerdown();
  lamp.focus();
  await openCaptureByShortcut();
  assert.equal(document.activeElement, parked.querySelector('input'));
  let stopped = false, prevented = false;
  root.listeners.keydown({ key: 'Escape', stopPropagation() { stopped = true; }, preventDefault() { prevented = true; } });
  assert.equal(parked.hidden, true);
  assert.equal(document.activeElement, work);
  assert.ok(stopped && prevented);
});

test('keyboard activation and cancel return focus to the lamp', async () => {
  const { lamp, parked } = captureUI();
  lamp.focus();
  await openCaptureByShortcut();
  const actions = parked.querySelector('form').children[2];
  actions.children[1].focus();
  actions.children[1].listeners.click();
  assert.equal(document.activeElement, lamp);
  assert.equal(parked.hidden, true);
});

test('a failed save retains text and focus for retry and never confirms success', async () => {
  editor();
  await openCaptureByShortcut();
  const { parked } = captureUI();
  const form = parked.querySelector('form');
  const input = form.querySelector('input');
  input.value = 'Do not lose this';
  chrome.runtime.sendMessage = async () => ({ ok: false });
  await submit(form);
  frame();
  assert.equal(input.value, 'Do not lose this');
  assert.equal(document.activeElement, input);
  assert.equal(form.children[3].textContent, 'Could not save. Your text is here — try again.');
  assert.equal(parked.querySelector('form'), form);
  advanceTime(10000);
  assert.equal(parked.hidden, false);
  assert.equal(parked.querySelector('form'), form);
  assert.equal(input.value, 'Do not lose this');
  assert.equal(form.children[3].textContent, 'Could not save. Your text is here — try again.');
});

test('confirmation expiry cannot dismiss a newly opened unsaved draft', async () => {
  editor();
  await openCaptureByShortcut();
  chrome.runtime.sendMessage = async () => ({ ok: true });
  await submit(captureUI().parked.querySelector('form'));
  frame();
  advanceTime(2000);
  await openCaptureByShortcut();
  const { parked } = captureUI();
  const form = parked.querySelector('form');
  const input = form.querySelector('input');
  input.value = 'Keep this new draft';
  advanceTime(10000);
  assert.equal(parked.hidden, false);
  assert.equal(parked.querySelector('form'), form);
  assert.equal(input.value, 'Keep this new draft');
  assert.equal(document.activeElement, input);
});

test('save completion does not steal focus moved back to the page', async () => {
  editor();
  await openCaptureByShortcut();
  const form = captureUI().parked.querySelector('form');
  let resolve;
  chrome.runtime.sendMessage = () => new Promise((done) => { resolve = done; });
  const pending = submit(form);
  const other = editor();
  resolve({ ok: true });
  await pending;
  assert.equal(document.activeElement, other);
});

test('repeat submit sends once and late response cannot replace a reopened draft', async () => {
  editor();
  await openCaptureByShortcut();
  const { parked, root } = captureUI();
  const form = parked.querySelector('form');
  let resolve, calls = 0;
  chrome.runtime.sendMessage = () => { calls++; return new Promise((done) => { resolve = done; }); };
  const pending = submit(form);
  await submit(form);
  assert.equal(calls, 1);
  root.listeners.keydown({ key: 'Escape', preventDefault() {}, stopPropagation() {} });
  await openCaptureByShortcut();
  const next = parked.querySelector('form');
  next.querySelector('input').value = 'new draft';
  resolve({ ok: true });
  await pending;
  assert.equal(parked.querySelector('form'), next);
  assert.equal(next.querySelector('input').value, 'new draft');
});

test('existing prompt keyboard access and capture draft survive incoming commands', async () => {
  editor();
  await openCaptureByShortcut();
  const { parked, root, lamp } = captureUI();
  const input = parked.querySelector('input');
  input.value = 'draft';
  listener(command('ASK_COMPLETION'));
  await flush();
  assert.equal(document.activeElement, input);
  assert.equal(input.value, 'draft');
  root.listeners.keydown({ key: 'Escape', preventDefault() {}, stopPropagation() {} });
  listener({ type: MSG.FOCUS_BUBBLE });
  await flush();
  assert.equal(document.activeElement, bubble().querySelector('button'));
  root.listeners.keydown({ key: 'Escape', stopPropagation() {} });
  assert.equal(document.activeElement, lamp);
});

test('capture restores the saved contenteditable selection', async () => {
  const work = editor();
  const caret = new Element('span');
  work.append(caret);
  const range = { startContainer: caret, endContainer: caret };
  const restored = [];
  window.getSelection = () => ({
    rangeCount: 1, anchorNode: caret,
    getRangeAt: () => ({ cloneRange: () => range }),
    removeAllRanges: () => restored.push('clear'),
    addRange: (value) => restored.push(value),
  });
  await openCaptureByShortcut();
  chrome.runtime.sendMessage = async () => ({ ok: true });
  await submit(captureUI().parked.querySelector('form'));
  assert.equal(document.activeElement, work);
  assert.deepEqual(restored, ['clear', range]);
});

test('edits during a save stay in the form with current focus', async () => {
  editor();
  await openCaptureByShortcut();
  const { parked } = captureUI();
  const form = parked.querySelector('form');
  const input = form.querySelector('input');
  input.value = 'first thought';
  let resolve;
  chrome.runtime.sendMessage = () => new Promise((done) => { resolve = done; });
  const pending = submit(form);
  input.value = 'second thought';
  resolve({ ok: true });
  await pending;
  frame();
  assert.equal(parked.querySelector('form'), form);
  assert.equal(input.value, 'second thought');
  assert.equal(document.activeElement, input);
  assert.equal(form.children[3].textContent, 'Saved for later — you stay where you were.');
  advanceTime(10000);
  assert.equal(parked.hidden, false);
  assert.equal(parked.querySelector('form'), form);
  assert.equal(input.value, 'second thought');
  assert.equal(document.activeElement, input);
});

// --- Max position: one extension-wide spot in chrome.storage.local ---------
const maxPos = () => {
  const { props } = maxAnchor().style;
  return { x: props['--max-x'], y: props['--max-y'] };
};
async function reinject() {
  // A new page / tab: fresh DOM, content script injected again.
  window.__focusCompanionPageObject = false;
  document.body = new Element('body');
  await start();
}

test('the page answers a metadata request only for its own document and URL', () => {
  const ask = (over = {}) => {
    let answer;
    listener({ type: MSG.PAGE_METADATA, documentToken: context().documentToken, url: location.href, ...over }, {}, (v) => { answer = v; });
    return answer;
  };
  document.querySelector = () => null; // nothing on screen: YouTube reads nothing
  assert.deepEqual(ask(), { channel: '', description: '' });
  assert.equal(ask({ documentToken: 'someone-else' }), undefined);
  assert.equal(ask({ url: 'https://www.youtube.com/watch?v=other' }), undefined);
});

// Manual test: after dragging Max, the "save for later" form still opened in
// the top-right corner. It opens beside her now, and follows her.
test('the capture form opens beside Max, wherever she is', async () => {
  const anchor = maxAnchor();
  anchor.getBoundingClientRect = () => ({ left: 200, top: 300 });
  const { parked } = captureUI();
  await openCaptureByShortcut();
  const wrap = parked.parent;
  assert.equal(wrap.style.left, '244px', 'to her right, just past the lamp');
  assert.equal(wrap.style.top, '276px');
  assert.equal(wrap.style.right, 'auto');
  // Near the right edge it opens on her left instead.
  anchor.getBoundingClientRect = () => ({ left: 1100, top: 300 });
  storageListener({ maxPosition: { newValue: { x: 1100, y: 300 } } }, 'local');
  assert.equal(wrap.style.left, `${1100 - 44 - 340}px`);
  // Near the bottom it stays inside the viewport.
  anchor.getBoundingClientRect = () => ({ left: 200, top: 790 });
  storageListener({ maxPosition: { newValue: { x: 200, y: 790 } } }, 'local');
  assert.equal(wrap.style.top, `${800 - 12 - 220}px`);
});

test('no Max on a sign-in page', async () => {
  location.href = 'https://www.instagram.com/accounts/login/two_step_verification?x=1';
  await reinject();
  assert.equal(document.body.children.length, 0, 'no host element at all');
});

test('no Max in a sign-in popup window', async () => {
  chrome.runtime.sendMessage = async (message) => {
    messages.push(message);
    return message.type === MSG.WINDOW_KIND ? { ok: true, data: 'popup' } : { ok: true, data: false };
  };
  await reinject();
  assert.ok(messages.some((m) => m.type === MSG.WINDOW_KIND));
  assert.equal(document.body.children.length, 0, 'host removed');
});

test('an ordinary window keeps Max even if the window check fails', async () => {
  chrome.runtime.sendMessage = async () => { throw new Error('worker restarting'); };
  await reinject();
  assert.ok(maxAnchor(), 'Max is there');
});

test('a drag is saved to chrome.storage.local, never the page sessionStorage', async () => {
  assert.equal(dragOptions.restore(), null);
  dragOptions.persist({ x: 300, y: 200 });
  await flush();
  assert.deepEqual(storedLocal.maxPosition, { x: 300, y: 200 });
});

test('a new tab or reinjected page restores the saved spot', async () => {
  storedLocal.maxPosition = { x: 300, y: 200 };
  await reinject();
  assert.deepEqual(maxPos(), { x: '300px', y: '200px' });
});

test('a drag in another tab moves Max in this tab too', async () => {
  storageListener({ maxPosition: { newValue: { x: 500, y: 400 } } }, 'local');
  assert.deepEqual(maxPos(), { x: '500px', y: '400px' });
  // Unrelated keys, other areas and malformed values change nothing.
  storageListener({ other: { newValue: 1 } }, 'local');
  storageListener({ maxPosition: { newValue: { x: 1, y: 1 } } }, 'session');
  storageListener({ maxPosition: { newValue: { x: 'a' } } }, 'local');
  assert.deepEqual(maxPos(), { x: '500px', y: '400px' });
});

test('an update from another tab never fights an in-progress drag here', async () => {
  storageListener({ maxPosition: { newValue: { x: 500, y: 400 } } }, 'local');
  maxAnchor().setAttribute('data-max-dragging', 'true');
  storageListener({ maxPosition: { newValue: { x: 100, y: 100 } } }, 'local');
  assert.deepEqual(maxPos(), { x: '500px', y: '400px' });
});

test('a spot saved in a larger window is clamped into this viewport', async () => {
  storedLocal.maxPosition = { x: 5000, y: 5000 };
  await reinject();
  assert.deepEqual(maxPos(), { x: '1166px', y: '722px' }); // 1200-34, 800-78
  storageListener({ maxPosition: { newValue: { x: -50, y: 0 } } }, 'local');
  assert.deepEqual(maxPos(), { x: '34px', y: '34px' });
});

// --- the server-owned card: closes only when answered, ignored or obsolete ---
function maxAnchor() { return captureUI().root.children[3]; }
let renders;
function countRenders() {
  renders = 0;
  const original = window.showMaxIntervention;
  window.showMaxIntervention = (opts) => { renders++; return original(opts); };
}

test('the card stays however long it is left, and never listens to the pointer', async () => {
  listener(command('SHOW_RAMP'));
  await flush();
  assert.equal(maxAnchor().listeners.pointerleave, undefined, 'no close-on-leave listener');
  advanceTime(60 * 60 * 1000);
  assert.equal(bubble().hidden, false);
});

test('the same open card re-sent as it follows the user is not re-rendered', async () => {
  countRenders();
  listener(command('SHOW_RAMP'));
  await flush();
  listener(command('SHOW_RAMP')); // same deliveryId: the card following the user
  await flush();
  assert.equal(renders, 1);
  assert.equal(bubble().hidden, false);
  const other = command('SHOW_RAMP');
  other.payload.deliveryId = 'issued-2';
  listener(other);
  await flush();
  assert.equal(renders, 2, 'a different card does render');
});

test('CLOSE_CARD takes down the matching card only', async () => {
  listener(command('SHOW_RAMP'));
  await flush();
  const close = (id) => ({ ...command('CLOSE_CARD'), payload: { closeDeliveryId: id } });
  listener(close('someone-else'));
  await flush();
  assert.equal(bubble().hidden, false);
  listener(close('issued-1'));
  await flush();
  assert.equal(bubble().hidden, true);
});

test('a card closed by the server alongside another command is taken down first', async () => {
  listener(command('SHOW_RAMP'));
  await flush();
  listener({ ...command('SHOW_THUMBS_UP'), payload: { closeDeliveryId: 'issued-1', lang: 'en' } });
  await flush();
  assert.equal(bubble().hidden, true);
  assert.ok(maxAnchor().querySelector('.max-thumb'));
});

test('Escape sets a server card aside so it does not follow the user back', async () => {
  listener(command('SHOW_RAMP'));
  await flush();
  captureUI().root.listeners.keydown({ key: 'Escape', stopPropagation() {}, preventDefault() {} });
  await flush();
  assert.deepEqual(messages.filter((m) => m.type === MSG.CARD_CLOSE), [{ type: MSG.CARD_CLOSE, deliveryId: 'issued-1', reason: 'ignored' }]);
  assert.equal(bubble().hidden, true);
});

// --- good day / bad day -----------------------------------------------------
const cardTexts = () => bubble().children.filter((c) => c.tagName === 'P').map((c) => c.textContent);
const cardButtons = () => bubble().children.find((c) => c.tagName === 'DIV')?.children ?? [];
const buttonByText = (text) => cardButtons().find((b) => b.textContent.includes(text));
function dayCommand(kind, payload = {}) {
  const msg = command(kind);
  msg.payload = { deliveryId: 'issued-1', ...payload };
  return msg;
}
function respondWith(data) {
  chrome.runtime.sendMessage = async (message) => {
    messages.push(message);
    return message.type === MSG.DAY_RESPOND ? { ok: true, data } : { ok: true, data: false };
  };
}

test('ASK_DAY shows the three answers in the goal language and is acknowledged', async () => {
  listener(dayCommand('ASK_DAY', { lang: 'el', step: { id: 's1', text: 'Δες ένα βίντεο' } }));
  await flush();
  assert.deepEqual(cardTexts(), ['Δεν θα ασχοληθείς καθόλου σήμερα με τους στόχους σου;', 'Αν δεν έχεις όρεξη, δεν πειράζει.']);
  assert.deepEqual(cardButtons().map((b) => b.textContent), ['Ναι, θα ασχοληθώ', 'Θα ασχοληθώ, αλλά δεν ξέρω από πού να αρχίσω', 'Δεν θα ασχοληθώ']);
  frame(); frame();
  assert.equal(acks().length, 1);
});

test('"I won\'t today" makes the day silent with a 5-second undo', async () => {
  respondWith({ mode: 'bad' });
  listener(dayCommand('ASK_DAY', { lang: 'en' }));
  await flush();
  buttonByText("I won't today").listeners.click();
  await flush(); await flush();
  assert.deepEqual(messages.filter((m) => m.type === MSG.DAY_RESPOND).map((m) => m.choice), ['wont_work']);
  assert.equal(cardTexts()[0], "Okay. Rest today — I'll stay quiet.");
  buttonByText('Undo').listeners.click();
  await flush(); await flush();
  assert.deepEqual(messages.filter((m) => m.type === MSG.DAY_RESPOND).map((m) => m.choice), ['wont_work', 'undo']);
  advanceTime(2000);
  assert.equal(bubble().hidden, true);
});

test('"I don\'t know where to start" shows the step and three searches, never links', async () => {
  respondWith({ mode: 'undecided', step: { text: 'Watch a Python intro video' }, searchQueries: ['python basics', 'python for beginners', 'python variables'] });
  listener(dayCommand('ASK_DAY', { lang: 'en' }));
  await flush();
  buttonByText("don't know where to start").listeners.click();
  await flush(); await flush();
  assert.deepEqual(cardTexts(), ['Start from here:', 'Watch a Python intro video']);
  assert.deepEqual(cardButtons().map((b) => b.textContent), ['🔍 python basics', '🔍 python for beginners', '🔍 python variables', 'Close']);
  buttonByText('python for beginners').listeners.click();
  await flush();
  assert.deepEqual(messages.filter((m) => m.type === MSG.OPEN_SEARCH), [{ type: MSG.OPEN_SEARCH, query: 'python for beginners' }]);
});

test("the day's first card says hello in the card's own language; others do not", async () => {
  listener(dayCommand('ASK_DAY', { lang: 'el', greeting: true }));
  await flush();
  assert.equal(cardTexts()[0], '👋 Γεια σου! Δεν θα ασχοληθείς καθόλου σήμερα με τους στόχους σου;');
  const ramp = command('SHOW_RAMP');
  ramp.payload = { ...ramp.payload, deliveryId: 'issued-2', lang: 'el', greeting: true };
  listener(ramp);
  await flush();
  assert.equal(cardTexts()[0], '👋 Hi! Your current step: Write intro', 'an English-only card keeps an English hello');
  const later = command('SHOW_RAMP');
  later.payload = { ...later.payload, deliveryId: 'issued-3' };
  listener(later);
  await flush();
  assert.equal(cardTexts()[0], 'Your current step: Write intro');
});

test('after "Yes" the next step is shown with "Continue here" and "Where do I start?"', async () => {
  chrome.runtime.sendMessage = async (message) => {
    messages.push(message);
    if (message.type !== MSG.STEP_RESPOND) return { ok: true, data: false };
    return { ok: true, data: { announcement: 'Step 1 of 3 complete.', currentStep: { id: 's2', text: 'Write a rough draft' }, searchQueries: ['how to write a draft'], lang: 'el' } };
  };
  listener(command('ASK_COMPLETION'));
  await flush();
  buttonByText('Yes').listeners.click();
  await flush(); await flush();
  assert.deepEqual(cardTexts(), ['✅ Step 1 of 3 complete.', 'Επόμενο: Write a rough draft']);
  assert.deepEqual(cardButtons().map((b) => b.textContent), ['Συνέχισε εδώ', '🔍 Από πού ξεκινάω;']);
  buttonByText('Από πού ξεκινάω').listeners.click();
  await flush();
  assert.deepEqual(cardTexts(), ['Ξεκίνα από εδώ:', 'Write a rough draft']);
  assert.equal(cardButtons()[0].textContent, '🔍 how to write a draft');
});

test('a repeated "did you finish?" is the gentle, small one', async () => {
  let variant;
  const original = window.showMaxIntervention;
  window.showMaxIntervention = (opts) => { variant = opts.variant; return original(opts); };
  const msg = command('ASK_COMPLETION');
  msg.payload = { ...msg.payload, gentle: true };
  listener(msg);
  await flush();
  assert.equal(cardTexts()[0], '✓ Done with “Write intro”?');
  assert.equal(variant, 'peek-side');
  assert.deepEqual(cardButtons().map((b) => b.textContent), ['Yes', 'Not yet']);
});

test('after the last "Yes" (goal done) only the announcement shows', async () => {
  chrome.runtime.sendMessage = async (message) => {
    messages.push(message);
    return message.type === MSG.STEP_RESPOND
      ? { ok: true, data: { announcement: 'Step 3 of 3 complete.', currentStep: null, goalCompleted: true } }
      : { ok: true, data: false };
  };
  listener(command('ASK_COMPLETION'));
  await flush();
  buttonByText('Yes').listeners.click();
  await flush(); await flush();
  assert.deepEqual(cardTexts(), ['Step 3 of 3 complete.']);
  advanceTime(4000);
  assert.equal(bubble().hidden, true);
});

test('SHOW_THUMBS_UP is a quiet badge: no card, no acknowledgement, gone by itself', async () => {
  listener(dayCommand('SHOW_THUMBS_UP', { lang: 'en' }));
  await flush();
  const badge = maxAnchor().querySelector('.max-thumb');
  assert.ok(badge, 'badge beside the lamp');
  assert.equal(badge.textContent, '👍');
  assert.equal(badge.getAttribute('aria-hidden'), 'true');
  assert.equal(bubble().hidden, true, 'no card');
  frame(); frame();
  assert.equal(acks().length, 0);
  // A dismissal for cards never targets the badge's command.
  listener(command('SHOW_RAMP'));
  await flush();
  listener({ type: MSG.DISMISS_UNSOLICITED, commands: ['SHOW_RAMP'] });
  await flush();
  assert.equal(bubble().hidden, true);
  advanceTime(2500);
  assert.equal(maxAnchor().querySelector('.max-thumb'), null);
});

test('SHOW_TASK reminds of the user\'s own task; Done completes it, Later sets it aside', async () => {
  const task = { id: 'idea_1', text: 'send an email today', urgent: true };
  listener(dayCommand('SHOW_TASK', { lang: 'en', task }));
  await flush();
  assert.deepEqual(cardTexts(), ["📝 A gentle reminder: send an email today", "Still waiting for you, whenever you're ready."]);
  assert.deepEqual(cardButtons().map((b) => b.textContent), ['✓ Done', 'Help me start', 'Later']);
  frame(); frame();
  assert.equal(acks().length, 1, 'acknowledged, like every server card');
  buttonByText('Done').listeners.click();
  await flush(); await flush();
  assert.deepEqual(messages.filter((m) => m.type === MSG.TASK_DONE), [{ type: MSG.TASK_DONE, id: 'idea_1' }]);
  assert.equal(cardTexts()[0], 'Nice, one less thing. 🌱');

  listener(dayCommand('SHOW_TASK', { deliveryId: 'issued-2', lang: 'el', task: { id: 'idea_2', text: 'πλήρωσε το νοίκι' } }));
  await flush();
  assert.equal(cardTexts()[0], '📝 Μια υπενθύμιση: πλήρωσε το νοίκι');
  buttonByText('Αργότερα').listeners.click();
  await flush();
  assert.deepEqual(messages.filter((m) => m.type === MSG.CARD_CLOSE), [{ type: MSG.CARD_CLOSE, deliveryId: 'issued-2', reason: 'ignored' }]);
});

test('SHOW_WELCOME_BACK: the new date and one small step, no days counted', async () => {
  listener(dayCommand('SHOW_WELCOME_BACK', {
    lang: 'el', voice: true,
    recovery: { newDate: '2026-10-20', step: { id: 's1', text: 'γράψε το intro', estimatedMinutes: 40 } },
  }));
  await flush();
  assert.equal(cardTexts()[0], 'Καλώς ήρθες.');
  assert.match(cardTexts()[1], /^Ξαναϋπολόγισα το πλάνο — νέα ημερομηνία: 20 Οκτωβρίου\. Επόμενο βήμα: «γράψε το intro» — 10 λεπτά\.$/);
  assert.doesNotMatch(cardTexts().join(' '), /μέρες|days/);
  frame(); frame();
  assert.equal(acks().length, 1);
  buttonByText('Άκου').listeners.click();
  await flush();
  assert.ok(messages.some((m) => m.type === MSG.OPEN_FULL && m.section === 'voice'));
  assert.ok(messages.some((m) => m.type === MSG.CARD_CLOSE && m.reason === 'done'));
});

test('SHOW_WELCOME_BACK offers the voice only when the server allows it', async () => {
  listener(dayCommand('SHOW_WELCOME_BACK', { lang: 'en', recovery: { step: { text: 'write the intro', estimatedMinutes: 5 } } }));
  await flush();
  assert.deepEqual(cardTexts(), ['Welcome back.', 'Next step: “write the intro” — 5 min.']);
  assert.deepEqual(cardButtons().map((b) => b.textContent), ["Let's go"]);
});

test('SHOW_TREASURE opens the treasure in the full tab', async () => {
  listener(dayCommand('SHOW_TREASURE', { lang: 'en', ideas: 6 }));
  await flush();
  assert.equal(cardTexts()[0], '✨ You collected 6 ideas this week. Want to see them?');
  buttonByText('Show me').listeners.click();
  await flush();
  assert.ok(messages.some((m) => m.type === MSG.OPEN_FULL && m.section === 'treasure'));
});

test('SHOW_OLD_IDEA offers the user\'s own idea; yes is sent to the server', async () => {
  listener(dayCommand('SHOW_OLD_IDEA', { lang: 'en', idea: { id: 'idea_7', text: 'film my desk', daysAgo: 10 } }));
  await flush();
  assert.deepEqual(cardTexts(), ['💡 You wrote this 10 days ago.', '“film my desk” — want to work on that instead today?']);
  buttonByText('Yes').listeners.click();
  await flush(); await flush();
  assert.deepEqual(messages.filter((m) => m.type === MSG.IDEA_OFFER), [{ type: MSG.IDEA_OFFER, id: 'idea_7', accept: true }]);
  assert.equal(cardTexts()[0], 'Good. Anything you do today counts.');
});

test('the return screen shows the door, not the step, on a difficult stretch', async () => {
  listener(dayCommand('SHOW_RAMP', { site: 'youtube.com', step: { id: 's1', text: 'write the intro', door: 'Today, just this: open what you need for “write the intro”. Nothing else. 2 minutes.' } }));
  await flush();
  assert.equal(cardTexts()[0], 'Today, just this: open what you need for “write the intro”. Nothing else. 2 minutes.');
});

test('the lamp shows its brightness and writes the number beside it', async () => {
  const anchor = maxAnchor();
  const lampButton = anchor.querySelector('.max');
  applyLampState(anchor, lampButton, { unseen: 6, level: 'medium', lang: 'en' });
  assert.equal(anchor.getAttribute('data-lamp'), 'medium');
  assert.equal(anchor.querySelector('.max-count').textContent, '6');
  assert.match(lampButton.getAttribute('aria-label'), /6 ideas not seen yet/);
  applyLampState(anchor, lampButton, { unseen: 0, level: 'off', lang: 'el' });
  assert.equal(anchor.getAttribute('data-lamp'), 'off');
  assert.equal(anchor.querySelector('.max-count'), null);
  assert.match(lampButton.getAttribute('aria-label'), /Αποθήκευσε μια ιδέα/);
});

test('SHOW_NEW_ERA announces the era prominently in the goal language', async () => {
  let variant;
  const original = window.showMaxIntervention;
  window.showMaxIntervention = (opts) => { variant = opts.variant; return original(opts); };
  listener(dayCommand('SHOW_NEW_ERA', { lang: 'el', era: 'copper', eraLabel: 'Copper' }));
  await flush();
  assert.equal(variant, 'full-entrance');
  assert.deepEqual(cardTexts(), ['🌍 Νέα Εποχή: Εποχή του Χαλκού!', 'Η Γη σου μπήκε σε νέα εποχή.']);
  frame(); frame();
  assert.equal(acks().length, 1);
  buttonByText('Δες τη Γη μου').listeners.click();
  await flush();
  assert.ok(messages.some((m) => m.type === MSG.OPEN_FULL));
});

// --- Max's chat, from the lamp -------------------------------------------------

// root.children[2] is the wrap: [parked, chatPanel]. The panel is [head, log, form, status, chips].
function chatUI() {
  const panel = captureUI().root.children[2].children[1];
  const [, log, form, status, chips] = panel.children;
  return { panel, log, form, status, chips, input: form?.children[0] };
}
const ticks = async () => { for (let i = 0; i < 6; i++) await flush(); };
const texts = (parent) => parent.children.map((c) => c.textContent);

test('the lamp opens Max with his whole body and a question; a second press folds him back', async () => {
  const { lamp } = captureUI();
  lamp.listeners.click();
  const { panel, log, chips } = chatUI();
  assert.equal(panel.hidden, false);
  assert.deepEqual(texts(log), ['Tell me, what do you need?']);
  assert.deepEqual(chips.children.map((b) => b.textContent), ['What should I do now?', "I don't know where to start", 'New task', 'An idea']);
  assert.deepEqual(maxCalls, ['emerge']);
  lamp.listeners.click();
  assert.equal(chatUI().panel.hidden, true);
  assert.deepEqual(maxCalls, ['emerge', 'return']);
});

test('what the user tells Max goes to the server chat; he answers and keeps it, but stays open until the user closes him', async () => {
  const { lamp } = captureUI();
  lamp.listeners.click();
  const { form, input, log, chips, panel } = chatUI();
  input.value = 'buy milk today';
  submit(form);
  await ticks();
  assert.deepEqual(messages.filter((m) => m.type === MSG.CHAT), [{ type: MSG.CHAT, text: 'buy milk today', lang: navigator.language }]);
  assert.deepEqual(texts(log), ['Tell me, what do you need?', 'buy milk today', 'OK, I kept it.']);
  assert.deepEqual(chips.children.map((b) => b.textContent), ['Thanks'], "Max's own suggestions replace the starting ones");
  assert.equal(panel.hidden, false, 'he stays a moment');
  advanceTime(60000);
  assert.equal(panel.hidden, false, 'no timer folds him back; only the user closes him');
  assert.deepEqual(maxCalls, ['emerge']);
});

test('"Return to my work" in chat resumes whichever task is in progress, same as the card\'s "Take me back"', async () => {
  taskActionAnswer = { resumeUrl: 'https://docs.python.org/3/tutorial/' };
  const { lamp } = captureUI();
  lamp.listeners.click();
  const { form, status } = chatUI();
  const returnBtn = form.children[3];
  assert.equal(returnBtn.textContent, 'Return to my work');
  returnBtn.listeners.click();
  await ticks();
  assert.deepEqual(messages.filter((m) => m.type === MSG.CHAT_STATE), [{ type: MSG.CHAT_STATE }]);
  assert.deepEqual(messages.filter((m) => m.type === MSG.TASK_ACTION), [{ type: MSG.TASK_ACTION, id: 'idea_4', action: 'resume' }]);
  assert.equal(status.textContent, '', 'no error: the worker already opened the page');
});

test('"Return to my work" says so when nothing is in progress', async () => {
  chatStateAnswer = { tasks: [] };
  const { lamp } = captureUI();
  lamp.listeners.click();
  const { form, status } = chatUI();
  form.children[3].listeners.click();
  await ticks();
  assert.deepEqual(messages.filter((m) => m.type === MSG.TASK_ACTION), [], 'nothing to resume, so no action is sent');
  assert.equal(status.textContent, 'No page was saved. Carry on from where you were.');
});

test('when Max asks something he stays for the answer', async () => {
  chatAnswer = { newMessages: ['Until when, and how long?'], suggestions: [] };
  const { lamp } = captureUI();
  lamp.listeners.click();
  const { form, input, panel } = chatUI();
  input.value = 'send the email';
  submit(form);
  await ticks();
  advanceTime(60000);
  assert.equal(panel.hidden, false);
});

test('a failed chat keeps the text and says so', async () => {
  globalThis.chrome.runtime.sendMessage = async (m) => { messages.push(m); return { ok: false, unavailable: true }; };
  const { lamp } = captureUI();
  lamp.listeners.click();
  const { form, input, status } = chatUI();
  input.value = 'hello';
  submit(form);
  await ticks();
  assert.equal(input.value, 'hello');
  assert.match(status.textContent, /Server unavailable/);
});

test('"Help me start" on a task reminder shows a tiny first step and searches, never links', async () => {
  listener(dayCommand('SHOW_TASK', { lang: 'en', task: { id: 'idea_1', text: 'prepare the pitch' } }));
  await flush();
  buttonByText('Help me start').listeners.click();
  await ticks();
  assert.deepEqual(messages.filter((m) => m.type === MSG.TASK_HELP), [{ type: MSG.TASK_HELP, id: 'idea_1' }]);
  assert.deepEqual(cardTexts(), ['Start with this:', 'Open a blank document.']);
  buttonByText('pitch outline').listeners.click();
  assert.ok(messages.some((m) => m.type === MSG.OPEN_SEARCH && m.query === 'pitch outline'));
});

// --- A task in progress: check-in, way back, yesterday's task -----------------

const taskActions = () => messages.filter((m) => m.type === MSG.TASK_ACTION).map((m) => m.action);

test('a check-in asks whether the task is done, and says how long the user worked on it', async () => {
  const task = { id: 'idea_1', text: 'study python', workedMin: 12 };
  listener(dayCommand('SHOW_TASK', { lang: 'en', task, taskMode: 'checkin' }));
  await flush();
  assert.deepEqual(cardTexts(), ['⏱ Did you finish “study python”?', 'You spent 12 min on related pages.']);
  assert.deepEqual(cardButtons().map((b) => b.textContent), ['✓ Yes, done', 'Not yet', 'Not now']);
  buttonByText('Yes, done').listeners.click();
  await ticks();
  assert.deepEqual(taskActions(), ['yes']);
  assert.equal(cardTexts()[0], 'Nice, one less thing. 🌱');
});

test('a check-in without related pages just says the time has passed; "not now" saves where the user was', async () => {
  listener(dayCommand('SHOW_TASK', { lang: 'el', task: { id: 'idea_2', text: 'άπλωμα ρούχων', workedMin: 0 }, taskMode: 'checkin' }));
  await flush();
  assert.deepEqual(cardTexts(), ['⏱ Τελείωσες το «άπλωμα ρούχων»;', 'Η ώρα που όρισες πέρασε.']);
  buttonByText('Όχι τώρα').listeners.click();
  await ticks();
  assert.deepEqual(taskActions(), ['later']);
  assert.equal(cardTexts()[0], 'Οκ, κράτησα πού έμεινες.');
});

test('a task left open from yesterday is recapped', async () => {
  listener(dayCommand('SHOW_TASK', { lang: 'en', task: { id: 'idea_3', text: 'renew the passport' }, taskMode: 'carry' }));
  await flush();
  assert.equal(cardTexts()[0], 'Yesterday “renew the passport” stayed open. Did you do it?');
  buttonByText('Not yet').listeners.click();
  assert.deepEqual(taskActions(), ['not_yet']);
});

test('drifting away from a task in progress offers the way back to where the user was', async () => {
  taskActionAnswer = { resumeUrl: 'https://docs.python.org/3/tutorial/' };
  const task = { id: 'idea_4', text: 'study python', lastTitle: 'The Python Tutorial' };
  listener(dayCommand('SHOW_TASK', { lang: 'en', task, taskMode: 'resume' }));
  await flush();
  assert.deepEqual(cardTexts(), ['You were working on “study python”.', 'You left off: The Python Tutorial']);
  assert.deepEqual(cardButtons().map((b) => b.textContent), ['Take me back', '✓ Yes, done', 'Not now']);
  buttonByText('Take me back').listeners.click();
  await ticks();
  assert.deepEqual(taskActions(), ['resume']);
  assert.equal(bubble().hidden, true, 'the worker opens the page; the card goes away');
});

test('"Take me back" without a saved page keeps the card and says so', async () => {
  listener(dayCommand('SHOW_TASK', { lang: 'en', task: { id: 'idea_5', text: 'call the bank' }, taskMode: 'resume' }));
  await flush();
  buttonByText('Take me back').listeners.click();
  await ticks();
  assert.equal(cardTexts()[0], 'No page was saved. Carry on from where you were.');
});

test('"Help me start" closes the reminder first, so it cannot come back on the next page', async () => {
  listener(dayCommand('SHOW_TASK', { lang: 'en', task: { id: 'idea_6', text: 'prepare the pitch' } }));
  await flush();
  buttonByText('Help me start').listeners.click();
  await ticks();
  assert.deepEqual(taskActions(), ['helped']);
  assert.ok(messages.findIndex((m) => m.type === MSG.TASK_ACTION) < messages.findIndex((m) => m.type === MSG.TASK_HELP));
});

test('working on another task: Max says which comes first, and can swap them', async () => {
  taskActionAnswer = { resumeUrl: 'https://mail.google.com/mail/u/0/' };
  listener(dayCommand('SHOW_TASK', {
    lang: 'en', taskMode: 'refocus',
    task: { id: 'idea_7', text: 'send the email' }, other: { id: 'idea_8', text: 'write the text' },
  }));
  await flush();
  assert.deepEqual(cardTexts(), ['Right now “send the email” comes first.', '“write the text” comes next.']);
  assert.deepEqual(cardButtons().map((b) => b.textContent), ['Back to it', '✓ It is done', 'First “write the text”']);
  buttonByText('Back to it').listeners.click();
  await ticks();
  assert.deepEqual(messages.filter((m) => m.type === MSG.TASK_ACTION), [{ type: MSG.TASK_ACTION, id: 'idea_7', action: 'resume' }]);
  assert.equal(bubble().hidden, true, 'the worker opens the page they were on');

  listener(dayCommand('SHOW_TASK', {
    deliveryId: 'issued-9', lang: 'el', taskMode: 'refocus',
    task: { id: 'idea_7', text: 'το email' }, other: { id: 'idea_8', text: 'το κείμενο' },
  }));
  await flush();
  assert.equal(cardTexts()[0], 'Τώρα προτεραιότητα έχει το «το email».');
  buttonByText('Πρώτα το «το κείμενο»').listeners.click();
  await ticks();
  assert.deepEqual(messages.filter((m) => m.type === MSG.TASK_ACTION).at(-1), { type: MSG.TASK_ACTION, id: 'idea_8', action: 'start' });
});

// --- Max for people who cannot see the screen ---------------------------------------

const spoken = () => messages.filter((m) => m.type === MSG.SPEAK);

test('a card is spoken: what it says, then the options and how to reach them', async () => {
  listener(dayCommand('SHOW_TASK', { lang: 'en', taskMode: 'checkin', task: { id: 'idea_1', text: 'study python', workedMin: 12 } }));
  await flush();
  await ticks();
  assert.equal(spoken().length, 1);
  const { text, lang } = spoken()[0];
  assert.equal(lang, 'en');
  assert.equal(text, 'Did you finish “study python”? You spent 12 min on related pages. Options: Yes, done, Not yet, Not now. To answer, press Alt Shift V and speak, or Alt Shift K for the buttons.');
  assert.doesNotMatch(text, /[⏱✓]/, 'no icons for a voice to read out');
});

test('a Greek card is spoken in Greek', async () => {
  listener(dayCommand('SHOW_TASK', { lang: 'el', taskMode: 'carry', task: { id: 'idea_2', text: 'το διαβατήριο' } }));
  await flush();
  await ticks();
  assert.equal(spoken()[0].lang, 'el');
  assert.match(spoken()[0].text, /Επιλογές: Ναι, τελείωσα, Όχι ακόμα, Όχι τώρα\. Για να απαντήσεις πάτα Alt Shift V/);
});

test('the answers Max gives from the lamp are spoken too', async () => {
  const { lamp } = captureUI();
  lamp.listeners.click();
  const { form, input } = chatUI();
  input.value = 'buy milk';
  submit(form);
  await ticks();
  assert.ok(spoken().some((m) => m.text === 'OK, I kept it.'), JSON.stringify(spoken()));
});

test('Alt+Shift+M opens Max on the page with the cursor in his text box', async () => {
  listener({ type: MSG.OPEN_MAX });
  await ticks();
  const { panel, input } = chatUI();
  assert.equal(panel.hidden, false);
  assert.equal(document.activeElement, input);
});

test('a close deadline: Max offers to switch to the urgent task or to keep the current one', async () => {
  listener(dayCommand('SHOW_TASK', {
    lang: 'en', taskMode: 'urgent',
    task: { id: 'idea_9', text: 'hand in the form' }, other: { id: 'idea_8', text: 'write the essay' },
  }));
  await flush();
  assert.deepEqual(cardTexts(), ['⏰ The deadline of “hand in the form” is close.', 'Right now you are working on “write the essay”.']);
  assert.deepEqual(cardButtons().map((b) => b.textContent), ['First “hand in the form”', 'Keep “write the essay”']);
  buttonByText('Keep').listeners.click();
  assert.deepEqual(messages.filter((m) => m.type === MSG.TASK_ACTION).at(-1), { type: MSG.TASK_ACTION, id: 'idea_9', action: 'keep' });
  listener(dayCommand('SHOW_TASK', {
    deliveryId: 'issued-7', lang: 'el', taskMode: 'urgent',
    task: { id: 'idea_9', text: 'η φόρμα' }, other: { id: 'idea_8', text: 'το δοκίμιο' },
  }));
  await flush();
  buttonByText('Πρώτα το «η φόρμα»').listeners.click();
  await ticks();
  assert.deepEqual(messages.filter((m) => m.type === MSG.TASK_ACTION).at(-1), { type: MSG.TASK_ACTION, id: 'idea_9', action: 'start' });
});

// --- The microphone on any page (the hidden recording page does the listening) ------

const micEvent = (event) => listener({ type: MSG.MIC_EVENT, ...event });
const micButton = () => chatUI().form.children[1];
const ticksOf = async (n) => { for (let i = 0; i < n; i++) { await flush(); } };

test('the microphone in Max\'s panel: it listens, the words appear, and they are sent after three seconds', async () => {
  const { lamp } = captureUI();
  lamp.listeners.click();
  micButton().listeners.click();
  assert.deepEqual(messages.filter((m) => m.type === MSG.MIC_START), [{ type: MSG.MIC_START, lang: 'en' }]);
  micEvent({ event: 'state', state: 'listening' });
  micEvent({ event: 'text', text: 'buy milk today' });
  micEvent({ event: 'end' });
  await ticks();
  assert.equal(chatUI().input.value, 'buy milk today');
  assert.match(chatUI().status.textContent, /Sending in \d/);
  for (let i = 0; i < 3; i++) { advanceTime(1000); await ticks(); }
  assert.deepEqual(messages.filter((m) => m.type === MSG.CHAT).map((m) => m.text), ['buy milk today']);
});

test('touching the text during the countdown cancels the automatic send, so it can be fixed', async () => {
  const { lamp } = captureUI();
  lamp.listeners.click();
  micButton().listeners.click();
  micEvent({ event: 'text', text: 'buy mlk' });
  micEvent({ event: 'end' });
  await ticks();
  chatUI().input.listeners.pointerdown();
  for (let i = 0; i < 5; i++) { advanceTime(1000); await ticks(); }
  assert.equal(messages.filter((m) => m.type === MSG.CHAT).length, 0);
  assert.equal(chatUI().input.value, 'buy mlk');
});

test('the microphone says when it may not listen, and pressing it again stops', async () => {
  const { lamp } = captureUI();
  lamp.listeners.click();
  micButton().listeners.click();
  micEvent({ event: 'error', code: 'not-allowed' });
  await ticks();
  assert.match(chatUI().status.textContent, /Allow the microphone/);
  micEvent({ event: 'end' });
  await ticks();
  micButton().listeners.click();
  micEvent({ event: 'state', state: 'listening' });
  await ticks();
  micButton().listeners.click();
  assert.equal(messages.filter((m) => m.type === MSG.MIC_STOP).length, 1);
});

test('Alt+Shift+V with no pop-up opens Max and starts listening', async () => {
  listener({ type: MSG.DICTATE });
  await ticks();
  assert.equal(chatUI().panel.hidden, false);
  assert.equal(messages.filter((m) => m.type === MSG.MIC_START).length, 1);
});

test('Alt+Shift+V with a pop-up open answers it by voice: the spoken button is pressed', async () => {
  listener(dayCommand('SHOW_TASK', { lang: 'en', taskMode: 'checkin', task: { id: 'idea_1', text: 'study python', workedMin: 5 } }));
  await flush();
  await ticks();
  listener({ type: MSG.DICTATE });
  await ticks();
  assert.equal(messages.filter((m) => m.type === MSG.MIC_START).length, 1);
  assert.equal(chatUI().panel.hidden, true, 'the chat stays closed: this is an answer to the pop-up');
  micEvent({ event: 'text', text: 'yes I finished' });
  micEvent({ event: 'end' });
  await ticks();
  assert.deepEqual(messages.filter((m) => m.type === MSG.TASK_ACTION).at(-1), { type: MSG.TASK_ACTION, id: 'idea_1', action: 'yes' });
  assert.ok(spoken().some((m) => /OK: Yes, done/.test(m.text)), 'Max confirms which button he pressed');
});

test('an answer that matches no button is not guessed: Max says the options again', async () => {
  listener(dayCommand('SHOW_TASK', { lang: 'en', taskMode: 'checkin', task: { id: 'idea_1', text: 'study python' } }));
  await flush();
  await ticks();
  listener({ type: MSG.DICTATE });
  await ticks();
  micEvent({ event: 'text', text: 'what is the weather' });
  micEvent({ event: 'end' });
  await ticks();
  assert.equal(messages.filter((m) => m.type === MSG.TASK_ACTION).length, 0);
  assert.ok(spoken().some((m) => /did not catch which one.*Options: Yes, done, Not yet, Not now/.test(m.text)), JSON.stringify(spoken().map((m) => m.text)));
});

// --- the way to the task, and a busy AI ---------------------------------------------

test('"Take me back" with no saved page still takes the user somewhere: where the task starts', async () => {
  taskActionAnswer = { searchQuery: 'go-kart tracks near me' };
  listener(dayCommand('SHOW_TASK', { lang: 'en', taskMode: 'resume', task: { id: 'idea_5', text: 'search for go-kart tracks' } }));
  await flush();
  buttonByText('Take me back').listeners.click();
  await ticks();
  assert.equal(bubble().hidden, true, 'the worker opens the search; the card goes away');
});

test('a busy AI is told with the seconds to wait, and the text stays', async () => {
  globalThis.chrome.runtime.sendMessage = async (m) => {
    messages.push(m);
    return { ok: false, unavailable: false, error: 'rate_limited', message: 'Max needs a short break: try again in 42 seconds.' };
  };
  const { lamp } = captureUI();
  lamp.listeners.click();
  const { form, input, status } = chatUI();
  input.value = 'search for tracks';
  submit(form);
  await ticks();
  assert.equal(input.value, 'search for tracks');
  assert.match(status.textContent, /Try again in 42 seconds/);
});
