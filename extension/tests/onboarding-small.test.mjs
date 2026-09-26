import assert from 'node:assert/strict';
import test from 'node:test';
import { smallGoalDetail } from '../dashboard/onboarding.js';
import { langOf } from '../shared/i18n.js';

test('the small-goal estimate is shown in the goal language', () => {
  const message = 'this goal looks like a task: about 2 hours of work';
  assert.equal(smallGoalDetail(message, 'el'), 'περίπου 2 ώρες δουλειάς');
  assert.equal(smallGoalDetail(message, 'en'), 'about 2 hours of work');
  assert.equal(smallGoalDetail('', 'en'), 'a few hours of work');
});

test('langOf follows the server rule', () => {
  assert.equal(langOf('Να γράψω μία παράγραφο'), 'el');
  assert.equal(langOf('Να μάθω Python'), 'el');
  assert.equal(langOf('write one paragraph'), 'en');
  assert.equal(langOf('the μ-law algorithm explained'), 'en');
});
