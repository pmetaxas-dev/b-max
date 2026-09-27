// The chat with Max: a conversation like any AI chat, with dictation and an
// optional spoken voice. The component only shows and collects; the caller's
// onSend(text) talks to the server and resolves with Max's reply.
// This runs in an extension page, so setTimeout is fine (not the worker).

import { announce, button, clear, el } from './a11y.js';
import {
  browserLang, createListener, createSpeaker, detectLang, readSetting, writeSetting,
} from './voice-io.js';

const TEXT = {
  en: {
    region: 'Chat with Max', placeholder: 'Write to Max…', send: 'Send', mic: 'Speak', micStop: 'Stop',
    ideas: '💡 Ideas', ideasLabel: 'Suggested replies', listening: 'Listening… I send it after a short pause, or press Stop.', transcribing: 'Writing down what I heard…', approx: 'Heard roughly. Please check it, then press Send.',
    sendingIn: (n) => `Sending in ${n}s. Click the text to fix it.`, noSpeech: 'I did not hear anything. Try again.', sttFailed: 'I could not write that down. Try again or type.', thinking: 'Max is thinking…', voiceOn: 'Max’s voice: on', voiceOff: 'Max’s voice: off',
    langLabel: 'Language', micDenied: 'Microphone access was not granted.', micUnsupported: 'Dictation is not available in this browser.',
    busy: (n) => `Max needs a short break (the AI is busy). Try again in ${n} seconds.`,
    micFailed: 'Could not hear you. Try again.', failed: 'Max could not answer. Send your message again.',
    offline: 'Server unavailable. Start the local server and try again.', you: 'You', max: 'Max', log: 'Conversation',
  },
  el: {
    region: 'Συζήτηση με τον Max', placeholder: 'Γράψε στον Max…', send: 'Στείλε', mic: 'Μίλα', micStop: 'Στοπ',
    ideas: '💡 Ιδέες', ideasLabel: 'Προτεινόμενες απαντήσεις', listening: 'Ακούω… Το στέλνω μετά από μια μικρή παύση, ή πάτα Στοπ.', transcribing: 'Γράφω αυτό που άκουσα…', approx: 'Το άκουσα κατά προσέγγιση. Έλεγξέ το και πάτα Στείλε.',
    sendingIn: (n) => `Στέλνω σε ${n}″. Κάνε κλικ στο κείμενο για να το διορθώσεις.`, noSpeech: 'Δεν άκουσα κάτι. Δοκίμασε ξανά.', sttFailed: 'Δεν μπόρεσα να το γράψω. Δοκίμασε ξανά ή πληκτρολόγησε.', thinking: 'Ο Max σκέφτεται…', voiceOn: 'Φωνή του Max: ναι', voiceOff: 'Φωνή του Max: όχι',
    langLabel: 'Γλώσσα', micDenied: 'Δεν δόθηκε πρόσβαση στο μικρόφωνο.', micUnsupported: 'Η υπαγόρευση δεν υποστηρίζεται σε αυτόν τον browser.',
    busy: (n) => `Ο Max χρειάζεται μια μικρή ανάσα (το AI έχει φόρτο). Δοκίμασε ξανά σε ${n} δευτερόλεπτα.`,
    micFailed: 'Δεν σε άκουσα. Δοκίμασε ξανά.', failed: 'Ο Max δεν μπόρεσε να απαντήσει. Στείλε ξανά το μήνυμά σου.',
    offline: 'Ο server δεν είναι διαθέσιμος. Ξεκίνα τον τοπικό server και δοκίμασε ξανά.', you: 'Εσύ', max: 'Max', log: 'Συζήτηση',
  },
};

export const chatText = (lang) => TEXT[lang] ?? TEXT.en;

const MAX_SUGGESTIONS = 2;
const SEND_DELAY_S = 3; // after dictation the text waits this long, so it can be fixed
const CHARS_PER_TICK = 2;
const TICK_MS = 16;

/**
 * options.onSend(text) -> Promise<string|null>: Max's reply. It may throw an
 * Error whose `offline` is true when the server cannot be reached.
 * options.onTalking(bool): Max's mouth moves while he types or speaks.
 * options.live: a polite live region for screen readers.
 */
export function createChat({ onSend, onTalking, onThinking, onTurnDone, live }) {
  const speaker = createSpeaker();
  speaker.onChange(() => refreshLabels()); // another page changed "Max speaks"
  const listener = createListener();
  let lang = readSetting('bmax.lang', browserLang());
  let busy = false;
  // True for the whole turn, including the time Max spends typing/speaking his
  // reply after it has already arrived -- longer than `busy`, which the input's
  // disabled state uses and which turns off as soon as the network call
  // settles. A caller that polls the server for fresh messages while the
  // drawer is open (dashboard.js's fetchState) must wait for THIS, or it can
  // re-render/re-speak the very reply that is still being typed out here.
  let turnBusy = false;
  let listening = false;
  let typing = null; // { finish() } while a reply is being typed

  const reduceMotion = globalThis.matchMedia?.('(prefers-reduced-motion: reduce)').matches;

  const log = el('div', { class: 'chat-log', role: 'log', 'aria-live': 'off', tabindex: 0 });
  // A roomy box of fixed size (it scrolls inside), so writing never moves the rest
  // of the page. Enter sends, Shift+Enter starts a new line.
  const input = el('textarea', { id: 'chat-input', rows: 4, maxlength: 2000, autocomplete: 'off' });
  const sendBtn = el('button', { type: 'submit' });
  const micBtn = button('', toggleMic, { class: 'secondary mic', 'aria-pressed': 'false' });
  const voiceBtn = button('', () => {
    speaker.setEnabled(!speaker.enabled);
    refreshLabels();
  }, { class: 'secondary small', 'aria-pressed': 'false' });
  const status = el('p', { class: 'chat-status', role: 'status', 'aria-live': 'polite' });
  // Suggested replies: two at most, behind one button, in a small menu that floats
  // over the conversation; they never take room from it.
  const ideasBtn = button('', () => toggleMenu(), { class: 'secondary small', 'aria-expanded': 'false', hidden: true });
  const menu = el('div', { class: 'suggest-menu', role: 'group', hidden: true });
  const form = el('form', { class: 'chat-form', onsubmit: onSubmit, novalidate: true },
    input, el('div', { class: 'chat-actions' }, micBtn, sendBtn));
  const tools = el('div', { class: 'chat-tools' }, ideasBtn, speaker.supported ? voiceBtn : null, status);
  const element = el('section', { class: 'chat', 'aria-labelledby': 'chat-h' },
    el('h2', { id: 'chat-h', class: 'sr-only' }), log, el('div', { class: 'chat-compose' }, menu, form), tools);
  // Touching the text means "I want to fix it": a running recording is dropped
  // (what was heard so far stays in the box) and nothing is sent by itself.
  const takeOver = () => {
    if (listening) listener.abort();
    cancelSend();
  };
  input.addEventListener('pointerdown', takeOver);
  input.addEventListener('keydown', (event) => {
    if (event.key !== 'Enter') takeOver();
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      form.requestSubmit();
    } else if (event.key === 'Escape' && !menu.hidden) {
      event.stopPropagation();
      toggleMenu(false);
    }
  });

  function setLang(next) {
    lang = next;
    writeSetting('bmax.lang', lang);
    refreshLabels();
  }

  function refreshLabels() {
    const t = chatText(lang);
    element.querySelector('#chat-h').textContent = t.region;
    log.setAttribute('aria-label', t.log);
    input.placeholder = t.placeholder;
    input.setAttribute('aria-label', t.placeholder);
    sendBtn.textContent = t.send;
    ideasBtn.textContent = t.ideas;
    ideasBtn.setAttribute('aria-label', t.ideasLabel);
    micBtn.textContent = listening ? `■ ${t.micStop}` : `🎤 ${t.mic}`;
    micBtn.setAttribute('aria-pressed', String(listening));
    micBtn.hidden = !listener.supported;
    voiceBtn.textContent = `🔊 ${speaker.enabled ? t.voiceOn : t.voiceOff}`;
    voiceBtn.setAttribute('aria-pressed', String(speaker.enabled));
  }

  function scrollDown() {
    log.scrollTop = log.scrollHeight;
  }

  function bubble(who, text) {
    const t = chatText(lang);
    const node = el('p', { class: `msg msg-${who}` },
      el('span', { class: 'sr-only', text: `${who === 'user' ? t.you : t.max}: ` }),
      el('span', { class: 'msg-text', text }));
    log.append(node);
    scrollDown();
    return node;
  }

  function addUser(text) {
    bubble('user', text);
  }

  /** Shows Max's reply, typed out (and read aloud when the voice is on). */
  function addMax(text, { animate = true } = {}) {
    typing?.finish();
    const node = bubble('max', animate && !reduceMotion ? '' : text);
    const textNode = node.querySelector('.msg-text');
    const done = () => {
      textNode.textContent = text;
      typing = null;
      onTalking?.(speaking);
      scrollDown();
      announce(live, text);
    };
    let speaking = false;
    if (animate) {
      speaking = speaker.speak(text, detectLang(text), {
        onstart: () => onTalking?.(true),
        onend: () => onTalking?.(false),
      });
    }
    if (!animate || reduceMotion) {
      if (!animate) node.querySelector('.msg-text').textContent = text;
      else done();
      return Promise.resolve();
    }
    return new Promise((resolve) => {
      let shown = 0;
      let timer = null;
      const finish = () => {
        clearTimeout(timer);
        done();
        resolve();
      };
      typing = { finish };
      onTalking?.(true);
      const tick = () => {
        shown = Math.min(text.length, shown + CHARS_PER_TICK);
        textNode.textContent = text.slice(0, shown);
        scrollDown();
        if (shown >= text.length) return finish();
        timer = setTimeout(tick, TICK_MS);
      };
      tick();
    });
  }

  function setMessages(messages) {
    clear(log);
    for (const m of messages) bubble(m.role === 'user' ? 'user' : 'max', m.text);
  }

  function toggleMenu(open = menu.hidden) {
    menu.hidden = !open;
    ideasBtn.setAttribute('aria-expanded', String(open));
  }

  /** Up to two replies for Max's last message; choosing one sends it like typed text. */
  function setSuggestions(list = []) {
    clear(menu);
    const shown = list.slice(0, MAX_SUGGESTIONS);
    for (const text of shown) {
      menu.append(button(text, () => { toggleMenu(false); submit(text); }, { class: 'secondary chip-reply' }));
    }
    ideasBtn.hidden = !shown.length;
    toggleMenu(false);
  }

  function setBusy(value) {
    busy = value;
    input.disabled = sendBtn.disabled = value;
    element.setAttribute('aria-busy', String(value));
  }

  function showStatus(text) {
    status.textContent = text ?? '';
  }

  async function submit(text) {
    cancelSend();
    text = text.trim();
    if (!text || busy) return;
    turnBusy = true;
    typing?.finish();
    setSuggestions([]);
    addUser(text);
    input.value = '';
    setBusy(true);
    onThinking?.(true);
    showStatus(chatText(lang).thinking);
    let reply = null;
    try {
      reply = await onSend(text);
      onThinking?.(false);
    } catch (err) {
      onThinking?.(false);
      showStatus(err?.offline ? chatText(lang).offline : err?.code === 'rate_limited' ? chatText(lang).busy(err.seconds ?? 20) : chatText(lang).failed);
      input.value = text;
      setBusy(false);
      turnBusy = false;
      input.focus();
      return;
    }
    showStatus('');
    setBusy(false);
    for (const line of [reply ?? []].flat()) await addMax(line);
    turnBusy = false;
    input.focus();
    onTurnDone?.();
  }

  function onSubmit(event) {
    event.preventDefault();
    submit(input.value);
  }

  // Dictated text is not sent at once: it waits a few seconds, and a click or a
  // key in the box, or the microphone, cancels it.
  let sendTimer = null;
  function cancelSend() {
    if (!sendTimer) return;
    clearInterval(sendTimer);
    sendTimer = null;
    showStatus('');
  }

  function scheduleSend() {
    cancelSend();
    let left = SEND_DELAY_S;
    showStatus(chatText(lang).sendingIn(left));
    sendTimer = setInterval(() => {
      left -= 1;
      if (left <= 0) {
        cancelSend();
        submit(input.value);
        return;
      }
      showStatus(chatText(lang).sendingIn(left));
    }, 1000);
  }

  async function toggleMic() {
    const t = chatText(lang);
    cancelSend();
    if (listening) return listener.stop();
    speaker.stop();
    listening = true;
    refreshLabels();
    let heard = '';
    let approximate = false;
    let failed = false;
    const started = await listener.start(lang, {
      onInterim: (text) => {
        input.value = text; // the words appear while they are said
      },
      onLevel: (level) => micBtn.style.setProperty('--level', String(Math.min(1, level * 8))),
      onState: (phase) => {
        showStatus(phase === 'transcribing' ? t.transcribing : t.listening);
        micBtn.disabled = phase === 'transcribing';
      },
      onText: (text) => {
        heard = text;
        input.value = text;
      },
      onEnd: () => {
        listening = false;
        micBtn.disabled = false;
        micBtn.style.removeProperty('--level');
        refreshLabels();
        if (heard && !approximate) scheduleSend();
        else if (approximate) input.focus(); // a rough guess is never sent without a look
        else if (!failed) showStatus('');
      },
      onError: (code) => {
        if (code === 'approximate') {
          approximate = true;
          showStatus(t.approx);
          return;
        }
        failed = true;
        showStatus(code === 'not-allowed' ? t.micDenied : code === 'no-speech' ? t.noSpeech : t.sttFailed);
      },
    });
    if (!started && !listener.supported) {
      listening = false;
      refreshLabels();
      showStatus(t.micUnsupported);
    }
  }

  refreshLabels();
  return {
    // `busy` alone would go false the moment the network call settles, before
    // Max is done typing/speaking the reply -- too early for a caller like
    // dashboard.js's fetchState(), which uses this to avoid touching the log
    // (re-rendering, or speaking the same reply again) mid-turn.
    get busy() { return busy || turnBusy; },
    element, addMax, addUser, setMessages, setBusy, setSuggestions, showStatus, submit,
    focus: () => input.focus(),
    get lang() { return lang; },
    setLang,
    refreshLabels,
  };
}
