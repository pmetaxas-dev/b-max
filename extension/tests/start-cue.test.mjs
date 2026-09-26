import assert from 'node:assert/strict';
import test, { beforeEach, afterEach } from 'node:test';
import { createStartingCue } from '../shared/start-cue.js';

let tick, requests, currentId, errors, changes, controller;
const originalInterval = globalThis.setInterval;
const originalClear = globalThis.clearInterval;

beforeEach(() => {
  tick = undefined;
  requests = [];
  errors = [];
  changes = 0;
  currentId = 's1';
  globalThis.document = { createElement: (tag) => ({
    tag, textContent: '', children: [], listeners: {},
    append(child) { this.children.push(child); },
    addEventListener(type, fn) { this.listeners[type] = fn; },
    setAttribute() {},
  }) };
  globalThis.setInterval = (fn) => { tick = fn; return 1; };
  globalThis.clearInterval = () => { tick = undefined; };
  globalThis.fetch = async (url, init) => {
    requests.push({ url, init });
    if (url.endsWith('/steps/start-cue')) return Response.json({ stepId: 's1', text: 'Server-owned cue', progressWeight: 0 });
    if (url.endsWith('/state/summary')) return Response.json({ currentStep: currentId ? { id: currentId } : null });
    throw new Error('Unexpected request');
  };
  controller = createStartingCue({ onError: (err) => errors.push(err), onStepChanged: () => changes++ });
});

afterEach(() => {
  controller.clear();
  globalThis.setInterval = originalInterval;
  globalThis.clearInterval = originalClear;
  delete globalThis.document;
});

test('one inline cue uses Go text and sends only parent identity', async () => {
  const node = controller.render({ id: 's1' });
  const [action, output] = node.children;
  assert.equal(action.textContent, 'Give me a smaller start');
  await action.listeners.click();
  await action.listeners.click();
  assert.equal(node.children.length, 2);
  assert.equal(output.textContent, 'Server-owned cue');
  for (const { url, init } of requests) {
    assert.ok(url.endsWith('/steps/start-cue'));
    assert.equal(init.method, 'POST');
    assert.deepEqual(JSON.parse(init.body), { stepId: 's1' });
  }
  await tick();
  assert.equal(output.textContent, 'Server-owned cue');
  assert.equal(changes, 0);
});

for (const next of ['s2', null]) {
  test(`automatically discards cue when server current step becomes ${next}`, async () => {
    const [action, output] = controller.render({ id: 's1' }).children;
    await action.listeners.click();
    currentId = next;
    await tick();
    assert.equal(output.textContent, '');
    assert.equal(changes, 1);
    assert.equal(tick, undefined);
  });
}

test('rendering a new Current Step discards cue and stops watching', async () => {
  const [action, output] = controller.render({ id: 's1' }).children;
  await action.listeners.click();
  const next = controller.render({ id: 's2' });
  assert.equal(output.textContent, '');
  assert.equal(next.children[1].textContent, '');
  assert.equal(tick, undefined);
});

test('late cue response cannot attach to a changed Current Step', async () => {
  let resolve;
  globalThis.fetch = () => new Promise((done) => { resolve = done; });
  const [action, output] = controller.render({ id: 's1' }).children;
  const pending = action.listeners.click();
  const next = controller.render({ id: 's2' });
  resolve(Response.json({ stepId: 's1', text: 'Old cue', progressWeight: 0 }));
  await pending;
  assert.equal(output.textContent, '');
  assert.equal(next.children[1].textContent, '');
  assert.equal(tick, undefined);
});

test('stale rejection refreshes Current Step without displaying a cue', async () => {
  globalThis.fetch = async () => Response.json({ error: 'nothing_pending' }, { status: 409 });
  const [action, output] = controller.render({ id: 's1' }).children;
  await action.listeners.click();
  assert.equal(output.textContent, '');
  assert.equal(changes, 1);
  assert.equal(errors.length, 0);
});

test('the cue comes with the step\'s three searches, and they go when the step changes', async () => {
  const opened = [];
  globalThis.fetch = async (url) => {
    if (url.endsWith('/steps/start-cue')) {
      return Response.json({ stepId: 's1', text: 'Server-owned cue', progressWeight: 0, lang: 'el', searchQueries: ['python basics', 'https://evil.example', 'python loops'] });
    }
    return Response.json({ currentStep: { id: 's1' } });
  };
  controller = createStartingCue({ onError: (err) => errors.push(err), onStepChanged: () => changes++, openTab: (url) => opened.push(url) });
  const node = controller.render({ id: 's1' });
  await node.children[0].listeners.click();
  const searches = node.children[2];
  assert.ok(searches, 'searches shown under the cue');
  searches.remove = () => { node.children = node.children.filter((c) => c !== searches); };
  const [title, ...buttons] = searches.children;
  assert.equal(title.textContent, 'Από πού να ξεκινήσεις:');
  // A phrase that is itself a link is still only ever searched for.
  assert.deepEqual(buttons.map((b) => b.textContent), ['🔍 python basics', '🔍 https://evil.example', '🔍 python loops']);
  buttons[0].listeners.click();
  assert.deepEqual(opened, ['https://www.google.com/search?q=python%20basics']);
  controller.clear();
  assert.equal(node.children.length, 2, 'searches removed when the cue is cleared');
});

test('unavailable server uses existing error UI callback', async () => {
  globalThis.fetch = async () => { throw new Error('offline'); };
  const [action, output] = controller.render({ id: 's1' }).children;
  await action.listeners.click();
  assert.equal(output.textContent, '');
  assert.equal(errors.length, 1);
  assert.equal(tick, undefined);
});
