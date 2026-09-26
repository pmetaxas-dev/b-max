import { button, el } from './a11y.js';

// The pending "did you finish?" in the popup and the full tab. Max asks on
// the page (and asks again after more work); here it is a standing offer,
// not a second interrogation: no "Not yet" (which did nothing visible), just
// the way to say "done" whenever it is true.
export const DONE_PROMPT_TITLE = 'Tell me when you finish, so we can move on';

export function donePrompt(step, onDone, { headingId } = {}) {
  return el('section', headingId ? { 'aria-labelledby': headingId } : {},
    el('h2', { id: headingId, text: DONE_PROMPT_TITLE }),
    el('p', { class: 'step-current', text: step.text }),
    el('div', { class: 'row' }, button("Yes, I'm done", onDone)),
  );
}
