import assert from 'node:assert/strict';
import test from 'node:test';
import { confirmAndRegenerate, confirmAndReset, PROFILE_CONFIRM_WORD } from '../dashboard/dev-tools.js';
import { t } from '../shared/i18n.js';

function deps({ confirm = true, typed = null } = {}) {
  const calls = { resets: [], removed: [], asked: [] };
  return {
    calls,
    api: { devReset: async (scope) => { calls.resets.push(scope); } },
    confirm: (message) => { calls.asked.push(message); return confirm; },
    prompt: (message) => { calls.asked.push(message); return typed; },
    storage: { remove: async (keys) => { calls.removed.push(...keys); } },
    text: t('el'),
  };
}

test('day and progress resets ask first and do nothing when cancelled', async () => {
  for (const scope of ['day', 'progress']) {
    const no = deps({ confirm: false });
    assert.equal(await confirmAndReset(scope, no), false);
    assert.deepEqual(no.calls.resets, []);
    const yes = deps({ confirm: true });
    assert.equal(await confirmAndReset(scope, yes), true);
    assert.deepEqual(yes.calls.resets, [scope]);
    assert.deepEqual(yes.calls.removed, [], 'only a profile reset clears extension storage');
  }
});

test('deleting the profile needs the confirmation word typed', async () => {
  for (const typed of [null, '', 'yes', 'delet']) {
    const d = deps({ typed });
    assert.equal(await confirmAndReset('profile', d), false, `typed ${typed}`);
    assert.deepEqual(d.calls.resets, []);
  }
  const d = deps({ typed: ` ${PROFILE_CONFIRM_WORD.toLowerCase()} ` });
  assert.equal(await confirmAndReset('profile', d), true);
  assert.deepEqual(d.calls.resets, ['profile']);
  assert.deepEqual(d.calls.removed, ['planetState'], "the planet's cached era goes; Max's position stays");
  assert.match(d.calls.asked[0], /DELETE/);
});

test('new steps for the current milestone are asked for only after confirmation', async () => {
  let calls = 0;
  const api = { devRegenerateSteps: async () => { calls++; } };
  assert.equal(await confirmAndRegenerate({ api, confirm: () => false, text: t('en') }), false);
  assert.equal(calls, 0);
  assert.equal(await confirmAndRegenerate({ api, confirm: () => true, text: t('en') }), true);
  assert.equal(calls, 1);
});

test('a storage failure does not undo a profile reset', async () => {
  const d = deps({ typed: PROFILE_CONFIRM_WORD });
  d.storage = { remove: async () => { throw new Error('gone'); } };
  assert.equal(await confirmAndReset('profile', d), true);
});
