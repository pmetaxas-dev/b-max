// Speaking to Max and hearing Max. Page-only (never the service worker). Both
// are optional: without them the chat still works with the keyboard.
//  - createSpeaker: Max reads his answers aloud with the browser's own voice
//    (helps blind users). Off by default, remembered per browser.
//  - createListener: the user dictates. The browser records the voice, waits
//    for a pause, and the local server turns it into text with Groq Whisper,
//    which keeps English words inside Greek speech in Latin letters (the
//    browser's own dictation does not). The recording is not kept.

import { transcribe } from './api.js';

const BCP47 = { el: 'el-GR', en: 'en-US' };

/** 'el' when the text contains Greek letters, otherwise 'en'. */
export function detectLang(text) {
  return /[Ͱ-Ͽἀ-῿]/.test(text ?? '') ? 'el' : 'en';
}

export function browserLang() {
  return (globalThis.navigator?.language ?? 'en').toLowerCase().startsWith('el') ? 'el' : 'en';
}

function store() {
  try {
    return globalThis.localStorage;
  } catch {
    return null;
  }
}

export function readSetting(key, fallback) {
  try {
    return store()?.getItem(key) ?? fallback;
  } catch {
    return fallback;
  }
}

export function writeSetting(key, value) {
  try {
    store()?.setItem(key, value);
  } catch {
    /* private mode: the setting just is not remembered */
  }
}

// "Max speaks" is one flag in chrome.storage.local, shared by the dashboard, the
// chat and the pop-ups on every tab (the worker speaks those with chrome.tts).
const VOICE_KEY = 'bmaxVoice';

export function createSpeaker() {
  const tts = globalThis.chrome?.tts;
  const synth = globalThis.speechSynthesis;
  const supported = !!tts || (!!synth && typeof globalThis.SpeechSynthesisUtterance !== 'undefined');
  let enabled = supported && readSetting('bmax.voice', '0') === '1';
  const watchers = new Set();
  const changed = (value) => {
    enabled = supported && !!value;
    watchers.forEach((fn) => fn());
  };
  try {
    globalThis.chrome?.storage?.local?.get(VOICE_KEY).then((stored) => {
      if (typeof stored[VOICE_KEY] === 'boolean') changed(stored[VOICE_KEY]);
    });
    globalThis.chrome?.storage?.onChanged?.addListener((changes, area) => {
      if (area === 'local' && changes[VOICE_KEY]) changed(changes[VOICE_KEY].newValue === true);
    });
  } catch {
    /* no extension storage (a test): the local setting is used */
  }

  function speak(text, lang, { onstart, onend } = {}) {
    if (!supported || !enabled || !text) return false;
    if (tts) {
      tts.stop();
      tts.speak(String(text).slice(0, 1500), {
        lang: BCP47[lang] ?? BCP47.en,
        onEvent: (event) => {
          if (event.type === 'start') onstart?.();
          else if (['end', 'interrupted', 'cancelled', 'error'].includes(event.type)) onend?.();
        },
      });
      return true;
    }
    synth.cancel();
    const u = new globalThis.SpeechSynthesisUtterance(text);
    u.lang = BCP47[lang] ?? BCP47.en;
    const voice = synth.getVoices().find((v) => v.lang.toLowerCase().startsWith(lang));
    if (voice) u.voice = voice;
    u.onstart = () => onstart?.();
    u.onend = u.onerror = () => onend?.();
    synth.speak(u);
    return true;
  }

  return {
    supported,
    get enabled() {
      return enabled;
    },
    setEnabled(value) {
      enabled = supported && !!value;
      writeSetting('bmax.voice', enabled ? '1' : '0');
      try {
        globalThis.chrome?.storage?.local?.set({ [VOICE_KEY]: enabled });
      } catch {
        /* no extension storage */
      }
      if (!enabled) {
        tts?.stop();
        synth?.cancel();
      }
    },
    onChange: (fn) => watchers.add(fn),
    speak,
    stop: () => {
      tts?.stop();
      synth?.cancel();
    },
  };
}

const SILENCE_MS = 2800; // a pause this long after speech ends the recording
const NO_SPEECH_MS = 8000; // nothing said at all: stop
const MAX_MS = 90_000;
const SPEECH_LEVEL = 0.02; // RMS of the microphone signal that counts as speaking
const MIME_TYPES = ['audio/webm;codecs=opus', 'audio/webm', 'audio/ogg;codecs=opus', 'audio/mp4'];

export function createListener() {
  const supported = !!globalThis.navigator?.mediaDevices?.getUserMedia && typeof globalThis.MediaRecorder !== 'undefined';
  let session = null;

  /**
   * onInterim(text) as the words are said (the browser's own quick guess, for
   * the eyes only); onLevel(0..) with the loudness; onState('listening' |
   * 'transcribing'); onText(text, true) once, when the words are ready;
   * onError(code): 'not-allowed', 'no-speech', 'network', 'transcribe' or
   * 'approximate' (the transcription failed and onText carries the quick
   * guess); onEnd() always, last.
   */
  async function start(lang, { onText, onEnd, onError, onState, onInterim, onLevel, preview: wantPreview = true }) {
    if (!supported) return false;
    let stream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true } });
    } catch {
      onError?.('not-allowed');
      onEnd?.();
      return false;
    }
    const mimeType = MIME_TYPES.find((type) => MediaRecorder.isTypeSupported(type));
    const recorder = new MediaRecorder(stream, mimeType ? { mimeType } : undefined);
    const chunks = [];
    recorder.ondataavailable = (event) => event.data.size && chunks.push(event.data);

    // Silence detection: the microphone's loudness, ten times a second.
    const audio = new (globalThis.AudioContext || globalThis.webkitAudioContext)();
    const analyser = audio.createAnalyser();
    audio.resume?.().catch(() => {}); // a suspended context would hear only silence
    analyser.fftSize = 1024;
    audio.createMediaStreamSource(stream).connect(analyser);
    const samples = new Uint8Array(analyser.fftSize);
    const startedAt = Date.now();
    let spoke = false;
    let lastSpeech = startedAt;

    // The browser's dictation runs at the same time, only to show the words while
    // they are said. The text that counts is Whisper's, after the pause.
    const Recognition = globalThis.SpeechRecognition || globalThis.webkitSpeechRecognition;
    let preview = '';
    let quick = null;
    if (Recognition && wantPreview) {
      try {
        quick = new Recognition();
        quick.lang = BCP47[lang] ?? BCP47.en;
        quick.interimResults = true;
        quick.continuous = true;
        quick.onresult = (event) => {
          preview = Array.from(event.results, (result) => result[0].transcript).join(' ').trim();
          onInterim?.(preview);
        };
        quick.onerror = () => {};
        quick.start();
      } catch {
        quick = null;
      }
    }

    let discard = false; // the user took over the text box: nothing is transcribed
    const stop = () => {
      if (recorder.state === 'recording') recorder.stop();
    };
    const timer = setInterval(() => {
      analyser.getByteTimeDomainData(samples);
      let sum = 0;
      for (const value of samples) sum += ((value - 128) / 128) ** 2;
      const now = Date.now();
      const level = Math.sqrt(sum / samples.length);
      onLevel?.(level);
      if (level > SPEECH_LEVEL) {
        spoke = true;
        lastSpeech = now;
      }
      const quiet = now - lastSpeech;
      if ((spoke && quiet > SILENCE_MS) || (!spoke && now - startedAt > NO_SPEECH_MS) || now - startedAt > MAX_MS) stop();
    }, 100);

    recorder.onstop = async () => {
      clearInterval(timer);
      try { quick?.stop(); } catch { /* already stopped */ }
      stream.getTracks().forEach((track) => track.stop());
      audio.close().catch(() => {});
      session = null;
      if (discard) {
        onEnd?.();
        return;
      }
      if (!spoke) {
        onError?.('no-speech');
        onEnd?.();
        return;
      }
      onState?.('transcribing');
      try {
        const { text } = await transcribe(new Blob(chunks, { type: recorder.mimeType || 'audio/webm' }), lang);
        if (text) onText?.(text, true);
        else onError?.('no-speech');
      } catch (err) {
        if (preview) {
          onText?.(preview, true); // the browser's guess: better than nothing, for the user to check
          onError?.('approximate');
        } else {
          onError?.(err?.constructor?.name === 'ServerUnavailableError' ? 'network' : 'transcribe');
        }
      }
      onEnd?.();
    };
    session = { stop, abort: () => { discard = true; stop(); } };
    recorder.start(250);
    onState?.('listening');
    return true;
  }

  return { supported, start, stop: () => session?.stop(), abort: () => session?.abort() };
}
