// Answering a pop-up with the voice: the user hears the options and says one.
// This turns what was said into one of the card's buttons. It is a pure
// function (no DOM), so it is unit tested. Greek and English, accents ignored.

const strip = (text) =>
  String(text ?? '')
    .toLowerCase()
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/ς/g, 'σ')
    .replace(/[^\p{L}\p{N}\s]/gu, ' ')
    .replace(/\s+/g, ' ')
    .trim();

// What people say for each kind of button (by the button's id), already stripped.
const SAYINGS = {
  yes: ['ναι', 'τελειωσα', 'ετοιμο', 'εγινε', 'το εκανα', 'yes', 'done', 'finished', 'i finished', 'completed'],
  done: ['ναι', 'τελειωσα', 'ετοιμο', 'εγινε', 'το εκανα', 'yes', 'done', 'finished'],
  not_yet: ['ακομα', 'οχι ακομα', 'οχι', 'δεν', 'not yet', 'no'],
  later: ['τωρα', 'οχι τωρα', 'αργοτερα', 'μετα', 'later', 'not now'],
  resume: ['παμε', 'πανε', 'παω', 'πισω', 'ξαναπαμε', 'γυρνα', 'take me back', 'back', 'go back', 'continue', 'συνεχεια'],
  help: ['βοηθεια', 'βοηθησε', 'βοηθα', 'help', 'help me', 'where do i start'],
  switch: ['πρωτα', 'αλλαξε', 'ναι πρωτα', 'first', 'switch', 'change'],
  keep: ['συνεχιζω', 'κρατα', 'οχι', 'keep', 'stay', 'no'],
  close: ['κλεισε', 'κλεισιμο', 'εντάξει', 'close', 'okay'],
};

const words = (text) => strip(text).split(' ').filter((w) => w.length >= 3);

/**
 * The action the user meant, or null. actions: [{ id, label }].
 * Score = a known saying for that kind of button that appears in the speech
 * (longer sayings count more) plus words shared with the button's own label.
 */
export function matchSpokenAction(speech, actions) {
  const said = ` ${strip(speech)} `;
  if (!said.trim()) return null;
  const spokenWords = new Set(words(speech));
  let best = null;
  let bestScore = 0;
  let tie = false;
  for (const action of actions) {
    let score = 0;
    for (const saying of SAYINGS[action.id] ?? []) {
      if (said.includes(` ${saying} `)) score += 2 + saying.split(' ').length;
    }
    for (const word of words(action.label)) {
      if (spokenWords.has(word)) score += 2;
    }
    if (score > bestScore) {
      best = action;
      bestScore = score;
      tie = false;
    } else if (score === bestScore && score > 0) {
      tie = true;
    }
  }
  return tie ? null : best;
}
