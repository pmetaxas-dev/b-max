import assert from 'node:assert/strict';
import test from 'node:test';

import { chooseAnchor, collect, startAnchorCapture, isSensitiveField, MAX_SNIPPET } from '../content/anchor.js';
import { MSG } from '../shared/messaging.js';

const base = { fieldText: '', selectionText: '', heading: '', scrollPercent: 0, title: '' };

test('tier order: field > selection > heading > title', () => {
  const all = { fieldText: 'typed', selectionText: 'selected', heading: 'Head', title: 'Title' };
  assert.equal(chooseAnchor({ ...base, ...all }).tier, 1);
  assert.equal(chooseAnchor({ ...base, ...all, fieldText: '' }).tier, 2);
  assert.equal(chooseAnchor({ ...base, ...all, fieldText: '', selectionText: '' }).tier, 3);
  assert.equal(chooseAnchor({ ...base, title: 'Title' }).tier, 4);
  assert.equal(chooseAnchor(base), null);
});

test('tier 1 keeps the last ~120 characters before the caret', () => {
  const text = 'a'.repeat(300) + 'THE END';
  const a = chooseAnchor({ ...base, fieldText: text });
  assert.equal(a.snippet.length, MAX_SNIPPET);
  assert.ok(a.snippet.endsWith('THE END'));
});

test('other tiers keep the first 120 characters', () => {
  const a = chooseAnchor({ ...base, heading: 'H' + 'x'.repeat(300) });
  assert.equal(a.snippet.length, MAX_SNIPPET);
  assert.ok(a.snippet.startsWith('H'));
});

test('snippet is plain, single-line text', () => {
  const a = chooseAnchor({ ...base, fieldText: '  line one\n\n\tline   two ' });
  assert.equal(a.snippet, 'line one line two');
});

test('whitespace-only field falls through to the next tier', () => {
  assert.equal(chooseAnchor({ ...base, fieldText: ' \n ', heading: 'Head' }).tier, 3);
});

test('scroll percentage is clamped and kept', () => {
  assert.equal(chooseAnchor({ ...base, title: 't', scrollPercent: 250 }).scrollPercent, 100);
  assert.equal(chooseAnchor({ ...base, title: 't', scrollPercent: -5 }).scrollPercent, 0);
  assert.equal(chooseAnchor({ ...base, title: 't', scrollPercent: 42.4 }).scrollPercent, 42);
});

test('credential and payment fields are sensitive', () => {
  assert.ok(isSensitiveField({ type: 'password' }));
  assert.ok(isSensitiveField({ type: 'PASSWORD' }));
  for (const ac of ['current-password', 'new-password', 'one-time-code', 'username', 'cc-number', 'cc-csc', 'section-x cc-exp']) {
    assert.ok(isSensitiveField({ type: 'text', autocomplete: ac }), ac);
  }
  assert.ok(!isSensitiveField({ type: 'text', autocomplete: 'off' }));
  assert.ok(!isSensitiveField({ type: 'text' }));
});

test('lamp focus never becomes a work anchor on tick, hide or unload', async (t) => {
  const originals = Object.fromEntries(['window', 'document', 'chrome', 'setInterval'].map((key) => [key, globalThis[key]]));
  t.after(() => { for (const [key, value] of Object.entries(originals)) globalThis[key] = value; });
  let tick;
  const handlers = {}, messages = [];
  globalThis.document = {
    activeElement: { tagName: 'FOCUS-COMPANION' }, visibilityState: 'visible',
    addEventListener(type, fn) { handlers[type] = fn; },
  };
  globalThis.window = { addEventListener(type, fn) { handlers[type] = fn; } };
  globalThis.setInterval = (fn) => { tick = fn; return 0; };
  globalThis.chrome = { runtime: { sendMessage: async (msg) => {
    messages.push(msg); return { ok: true, data: true };
  } } };
  assert.equal(collect(), null);
  startAnchorCapture().setAllowed(true);
  await tick();
  document.visibilityState = 'hidden';
  handlers.visibilitychange();
  handlers.beforeunload();
  assert.ok(messages.some((msg) => msg.type === MSG.CAN_CAPTURE));
  assert.ok(messages.every((msg) => msg.type !== MSG.ANCHOR));
});
