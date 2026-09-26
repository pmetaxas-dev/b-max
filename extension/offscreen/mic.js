// The hidden page that records Max's microphone for every website. A web page
// cannot ask for the microphone without asking the user again on each site;
// this page belongs to the extension, so the permission is given once. The
// worker starts it (background/mic.js) and passes what it hears to the page.

import { createListener } from '../shared/voice-io.js';
import { MSG } from '../shared/messaging.js';

const listener = createListener();

const emit = (payload) => chrome.runtime.sendMessage({ type: MSG.MIC_EVENT, ...payload }).catch(() => {});

chrome.runtime.onMessage.addListener((message) => {
  if (message?.target !== 'offscreen') return;
  if (message.type === MSG.MIC_START) {
    listener.start(message.lang, {
      preview: false, // the browser's quick guess needs its own permission prompt: not here
      onState: (state) => emit({ event: 'state', state }),
      onText: (text) => emit({ event: 'text', text }),
      onError: (code) => emit({ event: 'error', code }),
      onEnd: () => emit({ event: 'end' }),
    });
  } else if (message.type === MSG.MIC_STOP) {
    listener.stop();
  } else if (message.type === MSG.MIC_ABORT) {
    listener.abort();
  }
});

emit({ event: 'ready' }); // the worker waits for this before it starts a recording
