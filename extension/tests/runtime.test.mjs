import assert from 'node:assert/strict';
import test, { beforeEach } from 'node:test';
import { signal, revokeCapture } from '../background/events.js';
import { handleMessage, searchUrl } from '../background/actions.js';
import { MSG } from '../shared/messaging.js';
import fs from 'node:fs';

const work = 'https://example.com/work';
const youtube = 'https://www.youtube.com/watch?v=1';
let state;

beforeEach(() => {
  state = {
    activeId: 7, windowId: 1, focused: true, activity: 'active', storage: {},
    tabs: new Map([[7, { id: 7, windowId: 1, url: youtube, title: 'Video', status: 'complete', active: true }]]),
    documentToken: 'document-1', messages: [], requests: [], updates: [], windows: [], created: [],
    decision: { command: 'SHOW_RAMP', reason: 'grace_elapsed', payload: { deliveryId: 'delivery-1', site: 'youtube.com', step: { id: 's1', text: 'Work' } } },
    rampResult: { lastWorkTabId: 7, lastWorkUrl: work, anchorUrl: work },
  };
  globalThis.chrome = {
    idle: { queryState: async (seconds) => { assert.equal(seconds, 60); return state.activity; } },
    storage: { session: {
      get: async (key) => ({ [key]: state.storage[key] }),
      set: async (values) => Object.assign(state.storage, values),
      setAccessLevel: async () => {},
    } },
    tabs: {
      query: async (query) => query.active ? [state.tabs.get(state.activeId)] : [...state.tabs.values()],
      get: async (id) => { if (!state.tabs.has(id)) throw new Error('closed'); return state.tabs.get(id); },
      update: async (id, values) => { state.updates.push({ id, ...values }); return state.tabs.get(id); },
      create: async (values) => { state.created.push(values); return values; },
      sendMessage: async (id, message, options) => {
        if (state.noReceiver) throw new Error('no receiver');
        if (message.type === MSG.PAGE_CONTEXT) return { documentToken: state.documentToken, url: state.tabs.get(id).url, visible: state.focused && state.activeId === id };
        state.messages.push({ id, message, options });
      },
    },
    windows: {
      getLastFocused: async () => ({ id: state.windowId, focused: state.focused }),
      get: async (id) => ({ id, focused: state.focused && id === state.windowId }),
      update: async (id, values) => state.windows.push({ id, ...values }),
    },
    runtime: { getURL: (path) => 'chrome-extension://test/' + path },
  };
  // get() must omit nonexistent keys, just like chrome.storage.session.
  chrome.storage.session.get = async (key) => key in state.storage ? { [key]: state.storage[key] } : {};
  globalThis.fetch = async (url, init) => {
    const body = init.body ? JSON.parse(init.body) : undefined;
    state.requests.push({ url, body });
    if (url.endsWith('/browser/event')) {
      await state.onEvent?.();
      return Response.json(state.decision);
    }
    if (url.endsWith('/ramp/respond')) {
      await state.onRamp?.();
      return Response.json(state.rampResult);
    }
    if (url.endsWith('/browser/ack')) return Response.json({ acknowledged: true });
    throw new Error('unexpected request: ' + url);
  };
});

test('browser snapshots carry each Chrome activity state', async () => {
  state.decision = { command: 'DO_NOTHING', payload: { captureAnchor: false } };
  for (const activity of ['active', 'idle', 'locked']) {
    state.activity = activity;
    await signal('heartbeat');
    assert.equal(state.requests.at(-1).body.activity, activity);
  }
});

test('valid command targets top frame with document identity, without worker-side ack', async () => {
  await signal('heartbeat');
  const command = state.messages.find(({ message }) => message.type === MSG.COMMAND);
  assert.equal(command.id, 7);
  assert.equal(command.message.documentToken, 'document-1');
  assert.equal(command.message.url, youtube);
  assert.deepEqual(command.options, { frameId: 0 });
  assert.equal(state.requests.filter(({ url }) => url.endsWith('/browser/ack')).length, 0);
});

for (const site of [undefined, 'youtube.com']) {
  test(`Go dismissal scope ${site ?? 'global'} is relayed unchanged to all receiving tabs`, async () => {
    state.tabs.set(8, { ...state.tabs.get(7), id: 8, url: work });
    const scope = { commands: ['SHOW_RAMP', 'ASK_COMPLETION', 'ASK_DAY'], ...(site ? { site } : {}) };
    state.rampResult.dismissUnsolicited = scope;
    await handleMessage({ type: MSG.RAMP_RESPOND, site: 'youtube.com', choice: 'browse' }, {});
    assert.deepEqual(state.messages.map(({ id, message }) => ({ id, message })), [7, 8].map(id => ({ id, message: { type: MSG.DISMISS_UNSOLICITED, ...scope } })));
  });
}

for (const command of ['SHOW_RAMP', 'ASK_COMPLETION', 'ASK_DAY']) {
  for (const timing of ['pending', 'resolved']) {
    test(`${command} drops ${timing} silence race and evaluates subsequent events freshly`, async () => {
      state.decision.command = command;
      let releaseEvent, eventStarted;
      const started = new Promise(resolve => { eventStarted = resolve; });
      state.onEvent = () => { eventStarted(); return new Promise(resolve => { releaseEvent = resolve; }); };
      const event = signal('heartbeat');
      await started;
      let releaseRamp, rampStarted;
      const rampReady = new Promise(resolve => { rampStarted = resolve; });
      state.onRamp = () => { rampStarted(); return new Promise(resolve => { releaseRamp = resolve; }); };
      const action = handleMessage({ type: MSG.RAMP_RESPOND, site: 'youtube.com', choice: 'not_today' }, {});
      await rampReady;
      if (timing === 'resolved') { releaseRamp(); await action; }
      releaseEvent();
      await event;
      assert.equal(state.messages.filter(({ message }) => message.type === MSG.COMMAND).length, 0);
      assert.equal(state.requests.filter(({ url }) => url.endsWith('/browser/ack')).length, 0);
      if (timing === 'pending') { releaseRamp(); await action; }
      state.onEvent = undefined;
      await signal('heartbeat');
      assert.equal(state.messages.filter(({ message }) => message.type === MSG.COMMAND).length, 1);
    });
  }
}

for (const change of ['tab', 'window', 'blur', 'navigation', 'loading', 'reload', 'idle', 'locked', 'title']) {
  test(`stale command dropped after ${change} during HTTP request`, async () => {
    state.onEvent = async () => {
      switch (change) {
        case 'tab':
          state.tabs.set(8, { ...state.tabs.get(7), id: 8 }); state.activeId = 8; break;
        case 'window': state.windowId = 2; break;
        case 'blur': state.focused = false; break;
        case 'navigation': state.tabs.get(7).url = work; break;
        case 'loading': state.tabs.get(7).pendingUrl = work; break;
        case 'reload': await revokeCapture(7); state.documentToken = 'document-2'; break;
        case 'idle': state.activity = 'idle'; break;
        case 'locked': state.activity = 'locked'; break;
        case 'title': state.tabs.get(7).title = 'Different video'; break;
      }
    };
    await signal('heartbeat');
    assert.equal(state.messages.filter(({ message }) => message.type === MSG.COMMAND).length, 0);
    assert.equal(state.requests.filter(({ url }) => url.endsWith('/browser/ack')).length, 0);
  });
}

test('missing content-script receiver does not acknowledge delivery', async () => {
  state.noReceiver = true;
  await signal('heartbeat');
  assert.equal(state.requests.length, 1);
  assert.equal(state.messages.length, 0);
});

test('render acknowledgement forwards sender tab and issued ID to Go', async () => {
  const res = await handleMessage({ type: MSG.DELIVERY_ACK, deliveryId: 'delivery-1', documentToken: 'document-1', url: youtube },
    { tab: { id: 7 }, frameId: 0, documentId: 'chrome-document-1' });
  assert.equal(res.ok, true);
  assert.deepEqual(state.requests[0].body, { deliveryId: 'delivery-1', tabId: 7, url: youtube });
});

test('ack from a background or replaced document is not forwarded', async () => {
  const message = { type: MSG.DELIVERY_ACK, deliveryId: 'delivery-1', documentToken: 'document-1', url: youtube };
  const sender = { tab: { id: 7 }, frameId: 0, documentId: 'chrome-document-1' };
  state.tabs.get(7).active = false;
  assert.equal((await handleMessage(message, sender)).ok, false);
  state.tabs.get(7).active = true;
  state.documentToken = 'document-2';
  assert.equal((await handleMessage(message, sender)).ok, false);
  assert.equal(state.requests.length, 0);
});

test('Continue preserves activation and window focus for matching work URL', async () => {
  state.tabs.get(7).url = work;
  await handleMessage({ type: MSG.RAMP_RESPOND, site: 'youtube.com', choice: 'continue' }, {});
  assert.deepEqual(state.updates, [{ id: 7, active: true }]);
  assert.deepEqual(state.windows, [{ id: 1, focused: true }]);
  assert.deepEqual(state.created, []);
});

for (const invalid of ['same-tab navigation', 'closed', 'pending navigation', 'missing recorded URL']) {
  test(`Continue uses anchor fallback for ${invalid}`, async () => {
    if (invalid === 'closed') state.tabs.delete(7);
    if (invalid === 'pending navigation') {
      state.tabs.get(7).url = work;
      state.tabs.get(7).pendingUrl = youtube;
    }
    if (invalid === 'missing recorded URL') state.rampResult.lastWorkUrl = '';
    await handleMessage({ type: MSG.RAMP_RESPOND, site: 'youtube.com', choice: 'continue' }, {});
    assert.deepEqual(state.created, [{ url: work }]);
    assert.deepEqual(state.updates, []);
    assert.deepEqual(state.windows, []);
  });
}

// Regression: the work tab was closed and the step's "best" anchor was an
// older capture on another site (a search box on wikipedia.org). Continue
// must reopen the page the user was actually working on, not the anchor.
test('Continue reopens the last work page before an older anchor when the tab is gone', async () => {
  state.tabs.delete(7);
  state.rampResult = { lastWorkTabId: 7, lastWorkUrl: 'https://docs.python.org/3/tutorial/index.html', anchorUrl: 'https://www.wikipedia.org/' };
  await handleMessage({ type: MSG.RAMP_RESPOND, site: 'reddit.com', choice: 'continue' }, {});
  assert.deepEqual(state.created, [{ url: 'https://docs.python.org/3/tutorial/index.html' }]);
  assert.deepEqual(state.updates, []);
});

test('Continue reopens the last work page when the tab navigated away and no anchor exists', async () => {
  state.rampResult.anchorUrl = '';
  await handleMessage({ type: MSG.RAMP_RESPOND, site: 'youtube.com', choice: 'continue' }, {});
  assert.deepEqual(state.created, [{ url: work }]);
  assert.deepEqual(state.updates, []);
});

// Regression (manual test): with no page about the goal today, Continue went
// to the last open page (a Google search for "reddit"). It now searches for
// the current step instead.
test('Continue searches for the current step when there is no page about it today', async () => {
  state.rampResult = { lastWorkTabId: 0, lastWorkUrl: '', anchorUrl: '', searchQuery: 'python basics introductory video' };
  await handleMessage({ type: MSG.RAMP_RESPOND, site: 'reddit.com', choice: 'continue' }, {});
  assert.deepEqual(state.created, [{ url: 'https://www.google.com/search?q=python%20basics%20introductory%20video' }]);
});

test('a page about the goal and the anchor still come before the search', async () => {
  state.tabs.delete(7);
  state.rampResult = { lastWorkTabId: 7, lastWorkUrl: 'https://docs.python.org/3/', anchorUrl: 'https://www.w3schools.com/python/', searchQuery: 'python basics' };
  await handleMessage({ type: MSG.RAMP_RESPOND, site: 'reddit.com', choice: 'continue' }, {});
  assert.deepEqual(state.created, [{ url: 'https://docs.python.org/3/' }]);
  state.created = [];
  state.rampResult = { lastWorkTabId: 0, lastWorkUrl: '', anchorUrl: 'https://www.w3schools.com/python/', searchQuery: 'python basics' };
  await handleMessage({ type: MSG.RAMP_RESPOND, site: 'reddit.com', choice: 'continue' }, {});
  assert.deepEqual(state.created, [{ url: 'https://www.w3schools.com/python/' }]);
});

test('Continue uses the dashboard only when there is no work page and no anchor', async () => {
  state.rampResult = { lastWorkTabId: 0, lastWorkUrl: '', anchorUrl: '' };
  await handleMessage({ type: MSG.RAMP_RESPOND, site: 'youtube.com', choice: 'continue' }, {});
  assert.deepEqual(state.created, [{ url: 'chrome-extension://test/dashboard/dashboard.html' }]);
  assert.deepEqual(state.updates, []);
});

test('search links are built from phrases only, with Greek encoded', () => {
  assert.equal(searchUrl('python για αρχάριους'), 'https://www.google.com/search?q=python%20%CE%B3%CE%B9%CE%B1%20%CE%B1%CF%81%CF%87%CE%AC%CF%81%CE%B9%CE%BF%CF%85%CF%82');
  assert.equal(searchUrl('a&b=c#d'), 'https://www.google.com/search?q=a%26b%3Dc%23d');
  for (const bad of ['', '   ', null, 42, { q: 'x' }, 'x'.repeat(201)]) assert.equal(searchUrl(bad), null);
});

test('OPEN_SEARCH opens one search tab; an invalid query opens nothing', async () => {
  assert.deepEqual(await handleMessage({ type: MSG.OPEN_SEARCH, query: 'python basics' }, {}), { ok: true });
  assert.deepEqual(state.created, [{ url: 'https://www.google.com/search?q=python%20basics' }]);
  assert.equal((await handleMessage({ type: MSG.OPEN_SEARCH, query: '' }, {})).ok, false);
  assert.equal(state.created.length, 1);
});

test('DAY_RESPOND forwards the choice and "not today" dismisses cards in every tab', async () => {
  globalThis.fetch = async (url, init) => {
    state.requests.push({ url, body: JSON.parse(init.body) });
    return Response.json({ mode: 'bad', dismissUnsolicited: { commands: ['SHOW_RAMP', 'ASK_DAY'] } });
  };
  const res = await handleMessage({ type: MSG.DAY_RESPOND, choice: 'wont_work' }, {});
  assert.equal(res.ok, true);
  assert.equal(state.requests.at(-1).url, 'http://127.0.0.1:8787/api/v1/day/respond');
  assert.deepEqual(state.requests.at(-1).body, { choice: 'wont_work' });
  assert.deepEqual(state.messages.map((m) => m.message), [{ type: MSG.DISMISS_UNSOLICITED, commands: ['SHOW_RAMP', 'ASK_DAY'] }]);
});

test('with the server\'s permission, the page metadata is forwarded after the command', async () => {
  state.decision.payload.readMetadata = true;
  const sendMessage = chrome.tabs.sendMessage;
  chrome.tabs.sendMessage = async (id, message, options) => {
    if (message.type === MSG.PAGE_METADATA) {
      state.messages.push({ id, message, options });
      return { channel: 'Corey Schafer', description: 'Python loops' };
    }
    return sendMessage(id, message, options);
  };
  const fetchEvent = globalThis.fetch;
  globalThis.fetch = async (url, init) => {
    if (url.endsWith('/browser/metadata')) {
      state.requests.push({ url, body: JSON.parse(init.body) });
      return Response.json({ stored: true });
    }
    return fetchEvent(url, init);
  };
  await signal('heartbeat');
  const types = state.messages.map(({ message }) => message.type);
  assert.ok(types.indexOf(MSG.COMMAND) < types.indexOf(MSG.PAGE_METADATA), 'the card first, then the metadata');
  const posted = state.requests.find(({ url }) => url.endsWith('/browser/metadata'));
  assert.deepEqual(posted.body, { url: youtube, channel: 'Corey Schafer', description: 'Python loops' });
});

test('without the permission no page metadata is read', async () => {
  await signal('heartbeat');
  assert.equal(state.messages.filter(({ message }) => message.type === MSG.PAGE_METADATA).length, 0);
  assert.equal(state.requests.filter(({ url }) => url.endsWith('/browser/metadata')).length, 0);
});

test('WINDOW_KIND tells a page whether it is in a popup window', async () => {
  chrome.windows.get = async (id) => ({ id, type: id === 9 ? 'popup' : 'normal' });
  assert.deepEqual(await handleMessage({ type: MSG.WINDOW_KIND }, { tab: { id: 3, windowId: 9 } }), { ok: true, data: 'popup' });
  assert.deepEqual(await handleMessage({ type: MSG.WINDOW_KIND }, { tab: { id: 3, windowId: 1 } }), { ok: true, data: 'normal' });
  assert.deepEqual(await handleMessage({ type: MSG.WINDOW_KIND }, {}), { ok: true, data: 'normal' });
  chrome.windows.get = async () => { throw new Error('gone'); };
  assert.deepEqual(await handleMessage({ type: MSG.WINDOW_KIND }, { tab: { id: 3, windowId: 9 } }), { ok: true, data: 'normal' });
});

test('CARD_CLOSE forwards the card id and reason to the server', async () => {
  globalThis.fetch = async (url, init) => {
    state.requests.push({ url, body: JSON.parse(init.body) });
    return Response.json({ closed: true });
  };
  const res = await handleMessage({ type: MSG.CARD_CLOSE, deliveryId: 'delivery-7', reason: 'ignored' }, {});
  assert.equal(res.ok, true);
  assert.equal(state.requests.at(-1).url, 'http://127.0.0.1:8787/api/v1/card/close');
  assert.deepEqual(state.requests.at(-1).body, { deliveryId: 'delivery-7', reason: 'ignored' });
});

test('Continue never reopens a non-web work URL', async () => {
  state.tabs.delete(7);
  state.rampResult = { lastWorkTabId: 7, lastWorkUrl: 'chrome://settings', anchorUrl: '' };
  await handleMessage({ type: MSG.RAMP_RESPOND, site: 'youtube.com', choice: 'continue' }, {});
  assert.deepEqual(state.created, [{ url: 'chrome-extension://test/dashboard/dashboard.html' }]);
});

test('worker configures idle threshold and sends state changes without timers', async () => {
  const event = () => ({ addListener(fn) { this.listener = fn; } });
  chrome.idle.setDetectionInterval = (seconds) => { state.threshold = seconds; };
  chrome.idle.onStateChanged = event();
  chrome.runtime.onInstalled = event();
  chrome.runtime.onStartup = event();
  chrome.runtime.onMessage = event();
  chrome.alarms = { get: async () => ({ name: 'heartbeat' }), onAlarm: event() };
  chrome.tabs.onActivated = event();
  chrome.tabs.onRemoved = event();
  chrome.tabs.onUpdated = event();
  chrome.windows.onFocusChanged = event();
  chrome.commands = { onCommand: event() };
  chrome.webNavigation = { onErrorOccurred: event() };
  await import('../background/service-worker.js');
  assert.equal(state.threshold, 60);
  state.activity = 'idle';
  await chrome.idle.onStateChanged.listener('idle');
  assert.equal(state.requests.at(-1).body.type, 'activity_changed');
  assert.equal(state.requests.at(-1).body.activity, 'idle');

  // The planet's expand navigation, blocked because it came from the sandboxed
  // page, is re-issued by the extension with the exact same URL, once per tab.
  const planet = 'chrome-extension://test/shared/planet/planet.html?era=copper&weather=clear&view=full';
  const blocked = (over) => ({ tabId: 42, frameId: 0, url: planet, error: 'net::ERR_BLOCKED_BY_CLIENT', ...over });
  state.updates = [];
  const onError = chrome.webNavigation.onErrorOccurred.listener;
  onError(blocked({ error: 'net::ERR_ABORTED' }));
  onError(blocked({ frameId: 3 }));
  onError(blocked({ url: 'https://example.com/?view=full' }));
  assert.deepEqual(state.updates, []);
  onError(blocked());
  onError(blocked());
  assert.deepEqual(state.updates, [{ id: 42, url: planet }]);
});

for (const kind of ['idea', 'task']) {
  test(`${kind} capture only forwards to Go without navigation or context changes`, async () => {
    const storage = structuredClone(state.storage);
    globalThis.fetch = async (url, init) => {
      state.requests.push({ url, body: JSON.parse(init.body), method: init.method });
      return Response.json({ id: 'parked-1', type: kind }, { status: 201 });
    };
    const result = await handleMessage({ type: MSG.CAPTURE_IDEA, text: 'later', itemType: kind }, { tab: { id: 7 } });
    assert.equal(result.ok, true);
    assert.equal(state.requests.length, 1);
    assert.ok(state.requests[0].url.endsWith('/ideas'));
    assert.equal(state.requests[0].method, 'POST');
    assert.deepEqual(state.requests[0].body, { text: 'later', type: kind });
    assert.deepEqual(state.storage, { ...storage, serverAvailable: true });
    assert.deepEqual(state.messages, []);
    assert.deepEqual(state.updates, []);
    assert.deepEqual(state.windows, []);
    assert.deepEqual(state.created, []);
  });
}

test('worker import graph contains no UI polling or JS timers', () => {
  const seen = new Set();
  function inspect(url) {
    if (seen.has(url.href)) return;
    seen.add(url.href);
    const source = fs.readFileSync(url, 'utf8').replace(/\/\/[^\n]*/g, '');
    assert.doesNotMatch(source, /\b(?:setInterval|setTimeout)\s*\(/, url.pathname);
    assert.ok(!url.pathname.endsWith('/start-cue.js'));
    for (const match of source.matchAll(/from\s+['"]([^'"]+)['"]/g)) {
      if (match[1].startsWith('.')) inspect(new URL(match[1], url));
    }
  }
  inspect(new URL('../background/service-worker.js', import.meta.url));
  assert.ok(seen.size > 3);
});
