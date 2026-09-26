// Ten seconds of voice recording for onboarding: "Why do you want this?".
// Recording happens locally in this page; the file goes only to the local Go
// server, never to Groq. Denied permission or an unsupported browser simply
// means onboarding continues without voice (§17).
// This runs in an extension page, not the service worker, so setTimeout is fine.

import { button, el } from '../shared/a11y.js';

const MAX_MS = 10_000;

export function createVoiceStep() {
  let blob = null;
  let recorder = null;
  let stream = null;
  let stopTimer = null;

  const status = el('p', { role: 'status', 'aria-live': 'polite', class: 'muted', text: 'Optional. You can skip this.' });
  const recordBtn = button('Record (10 seconds)', startRecording);
  const stopBtn = button('Stop', stopRecording, { class: 'secondary', disabled: true });

  function releaseMic() {
    stream?.getTracks().forEach((t) => t.stop());
    stream = null;
  }

  async function startRecording() {
    if (!navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === 'undefined') {
      status.textContent = 'Recording is not available here. Continuing without voice is fine.';
      return;
    }
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch {
      status.textContent = 'Microphone access was not granted. Continuing without voice is fine.';
      return;
    }
    const chunks = [];
    recorder = new MediaRecorder(stream);
    recorder.ondataavailable = (e) => e.data.size && chunks.push(e.data);
    recorder.onstop = () => {
      clearTimeout(stopTimer);
      releaseMic();
      blob = new Blob(chunks, { type: recorder.mimeType || 'audio/webm' });
      status.textContent = 'Recorded. It stays on this computer. You can record again.';
      recordBtn.disabled = false;
      stopBtn.disabled = true;
    };
    blob = null;
    recorder.start();
    recordBtn.disabled = true;
    stopBtn.disabled = false;
    status.textContent = 'Recording… up to 10 seconds.';
    stopTimer = setTimeout(stopRecording, MAX_MS);
  }

  function stopRecording() {
    if (recorder && recorder.state === 'recording') recorder.stop();
  }

  const skipHint = el('p', { class: 'muted', text: 'To skip, just leave this and press “Build my plan”.' });

  const node = el(
    'fieldset',
    {},
    el('legend', { text: 'Why do you want this?' }),
    el('p', { text: 'This one line is the key of the app. It never leaves your computer.' }),
    el('div', { class: 'row' }, recordBtn, stopBtn),
    status,
    skipHint,
  );

  return { node, getBlob: () => blob, cancel: () => { stopRecording(); releaseMic(); } };
}
