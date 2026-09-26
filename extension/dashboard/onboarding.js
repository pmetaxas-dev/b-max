// First open: four questions (buttons, with a manual answer), one goal, ten
// seconds of voice, then the plan is built and shown ready (mvp §1).

import * as api from '../shared/api.js';
import { button, clear, el } from '../shared/a11y.js';
import { langOf, t } from '../shared/i18n.js';
import { createVoiceStep } from './voice.js';

// "about 3 hours of work" from the server's goal_too_small message, in the
// goal's language. Only the estimate is in that message, never the goal.
export function smallGoalDetail(message, lang) {
  const hours = /about (\d+) hours/.exec(message ?? '')?.[1];
  if (!hours) return lang === 'el' ? 'λίγες ώρες δουλειάς' : 'a few hours of work';
  return lang === 'el' ? `περίπου ${hours} ώρες δουλειάς` : `about ${hours} hours of work`;
}

const QUESTIONS = [
  {
    key: 'workBlock',
    legend: 'How long do you work before you need a break?',
    options: [
      ['10′', '10 minutes'],
      ['25′', '25 minutes'],
      ['45′', '45 minutes'],
    ],
    own: 'Your own (minutes)',
  },
  {
    key: 'bestTime',
    legend: 'When do you work best?',
    options: [['Morning', 'morning'], ['Afternoon', 'afternoon'], ['Evening', 'evening']],
    own: 'Your own',
  },
  {
    key: 'distraction',
    legend: 'What pulls you away most often?',
    options: [['Social', 'social'], ['YouTube', 'YouTube'], ['Gaming', 'gaming']],
    own: 'Your own',
  },
  {
    key: 'returnCue',
    legend: 'What makes you come back?',
    options: [['Sound', 'sound'], ['Something discreet', 'discreet'], ['Nothing', 'nothing']],
  },
];

const GOAL_EXAMPLES = [
  'Become a developer by 2028',
  'Finish my thesis',
  'Lose 5 kg in a month',
];

function questionGroup(q) {
  let value = '';
  let usingOwn = false;
  const buttons = [];
  const ownInput = el('input', { type: 'text', id: `own-${q.key}`, maxlength: 100, hidden: true });
  const ownLabel = el('label', { for: `own-${q.key}`, hidden: true, text: `${q.own}:` });

  function refresh() {
    buttons.forEach((b) => b.setAttribute('aria-pressed', b.dataset.value === value && !usingOwn ? 'true' : 'false'));
    ownBtn?.setAttribute('aria-pressed', usingOwn ? 'true' : 'false');
    ownInput.hidden = ownLabel.hidden = !usingOwn;
  }

  for (const [label, val] of q.options) {
    const b = button(label, () => {
      usingOwn = false;
      value = val;
      refresh();
    }, { 'aria-pressed': 'false' });
    b.dataset.value = val;
    buttons.push(b);
  }
  const ownBtn = q.own
    ? button('Your own', () => {
        usingOwn = true;
        refresh();
        ownInput.focus();
      }, { class: 'secondary', 'aria-pressed': 'false' })
    : null;

  const node = el('fieldset', {}, el('legend', { text: q.legend }), el('div', { class: 'row' }, ...buttons, ownBtn), ownLabel, ownInput);
  return { node, get: () => (usingOwn ? ownInput.value.trim() : value) };
}

/** Renders onboarding into `container`. Calls onDone() once the plan exists. */
export function renderOnboarding(container, onDone) {
  clear(container);
  const groups = QUESTIONS.map(questionGroup);
  const voice = createVoiceStep();

  const goalInput = el('input', { type: 'text', id: 'goal', maxlength: 300, required: true, autocomplete: 'off' });
  const goalHelp = el('div', { class: 'row' }, ...GOAL_EXAMPLES.map((ex) =>
    button(ex, () => { goalInput.value = ex; goalInput.focus(); }, { class: 'secondary' })));

  const status = el('p', { role: 'status', 'aria-live': 'polite', class: 'muted' });
  const errorBox = el('p', { class: 'error', role: 'alert', hidden: true });
  const submit = el('button', { type: 'submit', text: 'Build my plan' });

  // A goal the planner estimates as a task: keep it as a task, make it
  // bigger, or continue anyway (the server reuses the plan it just made).
  const choice = el('section', { class: 'small-goal', role: 'alert', hidden: true });
  function askAboutSmallGoal(goal, message) {
    const lang = langOf(goal);
    const tx = t(lang);
    clear(choice);
    choice.append(
      el('p', { class: 'step-current', text: tx.smallTitle }),
      el('p', { class: 'muted', text: tx.smallSub(smallGoalDetail(message, lang)) }),
      el('div', { class: 'row' },
        button(tx.smallBigger, () => { choice.hidden = true; goalInput.focus(); goalInput.select(); }),
        button(tx.smallKeepTask, async () => {
          try {
            await api.captureIdea(goal, 'task');
            choice.hidden = true;
            goalInput.value = '';
            status.textContent = tx.smallKept;
            goalInput.focus();
          } catch {
            status.textContent = 'Could not save. Please try again.';
          }
        }, { class: 'secondary' }),
        button(tx.smallAnyway, () => { choice.hidden = true; build(goal, true); }, { class: 'secondary' }),
      ),
    );
    choice.hidden = false;
  }

  // The AI is unreachable: offer a simple plan made without it (Phase 5).
  function offerFallback(goal) {
    const tx = t(langOf(goal));
    clear(choice);
    choice.append(
      el('p', { class: 'muted', text: tx.fallbackHint }),
      el('div', { class: 'row' },
        button(tx.fallbackOffer, () => { choice.hidden = true; build(goal, false, true); }, { class: 'secondary' }),
      ),
    );
    choice.hidden = false;
  }

  async function onSubmit(event) {
    event.preventDefault();
    errorBox.hidden = true;
    choice.hidden = true;
    const goal = goalInput.value.trim();
    if (goal.length < 3) {
      errorBox.textContent = 'Please write your goal in one sentence.';
      errorBox.hidden = false;
      goalInput.focus();
      return;
    }
    await build(goal, false);
  }

  async function build(goal, allowSmall, fallback = false) {
    submit.disabled = true;
    status.textContent = 'Building your plan…';
    try {
      await api.onboarding({
        goal,
        allowSmall,
        fallback,
        preferences: Object.fromEntries(QUESTIONS.map((q, i) => [q.key, groups[i].get()])),
      });
    } catch (err) {
      submit.disabled = false;
      status.textContent = '';
      if (err.code === 'goal_too_small') {
        askAboutSmallGoal(goal, err.message);
        return;
      }
      errorBox.textContent =
        err instanceof api.ServerUnavailableError
          ? 'Server unavailable. Start the local server and press “Build my plan” again.'
          : err.code === 'goal_exists'
            ? 'A goal is already active.'
            : 'The plan could not be created. Please press “Build my plan” to retry.';
      errorBox.hidden = false;
      if (err.code === 'plan_unavailable') offerFallback(goal);
      return;
    }
    const recording = voice.getBlob();
    if (recording) {
      try {
        await api.uploadVoice(recording);
      } catch {
        // Voice is optional; the plan is already built.
      }
    }
    voice.cancel();
    onDone();
  }

  container.append(
    el('h1', { text: 'Welcome' }),
    el('p', { class: 'muted', text: 'Four quick questions, then one goal. About a minute.' }),
    el('form', { onsubmit: onSubmit, novalidate: true },
      ...groups.map((g) => g.node),
      el('fieldset', {},
        el('legend', { text: 'What do you want to achieve?' }),
        el('label', { for: 'goal', text: 'One sentence. This is the only time you write freely.' }),
        goalInput,
        el('p', { class: 'muted', text: 'Need an idea?' }),
        goalHelp,
      ),
      voice.node,
      errorBox,
      choice,
      el('div', { class: 'row' }, submit),
      status,
    ),
  );
  container.querySelector('button')?.focus();
}
