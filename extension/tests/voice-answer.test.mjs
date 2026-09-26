import assert from 'node:assert/strict';
import test from 'node:test';
import { matchSpokenAction } from '../content/voice-answer.js';

const checkin = [
  { id: 'yes', label: '✓ Ναι, τελείωσα' },
  { id: 'not_yet', label: 'Όχι ακόμα' },
  { id: 'later', label: 'Όχι τώρα' },
];

const pick = (speech, actions = checkin) => matchSpokenAction(speech, actions)?.id ?? null;

test('Greek answers to a check-in', () => {
  assert.equal(pick('ναι'), 'yes');
  assert.equal(pick('Ναι, το τελείωσα!'), 'yes');
  assert.equal(pick('όχι ακόμα'), 'not_yet');
  assert.equal(pick('όχι τώρα'), 'later');
  assert.equal(pick('Οχι τωρα'), 'later', 'accents are ignored');
});

test('English answers to the same buttons', () => {
  const en = [
    { id: 'yes', label: '✓ Yes, done' },
    { id: 'not_yet', label: 'Not yet' },
    { id: 'later', label: 'Not now' },
  ];
  assert.equal(pick('yes I finished', en), 'yes');
  assert.equal(pick('not yet', en), 'not_yet');
  assert.equal(pick('not now', en), 'later');
});

test('the way back, help and the switch of tasks', () => {
  const resume = [
    { id: 'resume', label: 'Πάμε εκεί' },
    { id: 'yes', label: '✓ Ναι, τελείωσα' },
    { id: 'later', label: 'Όχι τώρα' },
  ];
  assert.equal(pick('πάμε πίσω', resume), 'resume');
  assert.equal(pick('take me back', [{ id: 'resume', label: 'Take me back' }, { id: 'later', label: 'Not now' }]), 'resume');
  const urgent = [
    { id: 'switch', label: 'Πρώτα το «η φόρμα»' },
    { id: 'keep', label: 'Συνεχίζω το «δοκίμιο»' },
  ];
  assert.equal(pick('πρώτα η φόρμα', urgent), 'switch');
  assert.equal(pick('συνεχίζω', urgent), 'keep');
  const remind = [{ id: 'done', label: '✓ Έγινε' }, { id: 'help', label: 'Βοήθησέ με να ξεκινήσω' }, { id: 'later', label: 'Αργότερα' }];
  assert.equal(pick('βοήθησέ με', remind), 'help');
  assert.equal(pick('αργότερα', remind), 'later');
});

test('a bare no means not yet, and nothing is guessed from silence or noise', () => {
  assert.equal(pick(''), null);
  assert.equal(pick('   '), null);
  assert.equal(pick('τι καιρό κάνει σήμερα'), null);
  assert.equal(pick('όχι'), 'not_yet');
  assert.equal(pick('ναι όχι ακόμα'), 'not_yet', 'the longer, more specific answer wins');
});

test('the words of the button itself also count', () => {
  const custom = [{ id: 'x', label: 'Ανοίγω το τετράδιο' }, { id: 'y', label: 'Διαβάζω βιβλίο' }];
  assert.equal(pick('ανοίγω το τετράδιο', custom), 'x');
  assert.equal(pick('διαβάζω', custom), 'y');
});
