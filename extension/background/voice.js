// Max's voice on every tab. The worker's chrome.tts can speak on any page
// without a click, which a page's own speech cannot; the user's choice ("Max
// speaks") is one flag in chrome.storage.local shared by every part of the
// extension. No timers here (this runs in the service worker).

import * as api from '../shared/api.js';
import { call } from './server-client.js';

const VOICE_KEY = 'bmaxVoice';

const BCP47 = { el: 'el-GR', en: 'en-US' };

export async function voiceEnabled() {
  try {
    return (await chrome.storage.local.get(VOICE_KEY))[VOICE_KEY] === true;
  } catch {
    return false;
  }
}

export function speak(text, lang) {
  if (!text || !chrome.tts) return;
  chrome.tts.stop();
  chrome.tts.speak(String(text).slice(0, 1500), { lang: BCP47[lang] ?? BCP47.en, rate: 1.0 });
}

/** A card or a reply: spoken only if the user asked Max to speak. */
export async function speakIfEnabled(text, lang) {
  if (await voiceEnabled()) speak(text, lang);
}

/** "Where am I?": always spoken, because the user asked (a shortcut or a button). */
export async function speakStatus() {
  const res = await call(api.getStatus);
  if (res.ok) speak(res.data.text, res.data.lang);
  else speak('Server unavailable. Start the local server.', 'en');
  return res;
}
