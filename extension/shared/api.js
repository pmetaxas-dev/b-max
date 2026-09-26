// Client for the local Go server. Used by the service worker and by extension
// pages. No timers: request timeouts use AbortSignal.timeout, so this module is
// safe to import from the MV3 service worker.

export const SERVER_URL = 'http://127.0.0.1:8787/api/v1';

/** The server could not be reached at all. */
export class ServerUnavailableError extends Error {}

/** The server answered with an error status. */
export class ApiError extends Error {
  constructor(status, code, message) {
    super(message || code);
    this.status = status;
    this.code = code;
  }
}

async function request(method, path, { body, contentType, timeoutMs = 5000 } = {}) {
  const init = { method, signal: AbortSignal.timeout(timeoutMs), headers: {} };
  if (body !== undefined) {
    if (body instanceof Blob) {
      init.body = body;
      init.headers['Content-Type'] = contentType || body.type;
    } else {
      init.body = JSON.stringify(body);
      init.headers['Content-Type'] = 'application/json';
    }
  }
  let res;
  try {
    res = await fetch(SERVER_URL + path, init);
  } catch (err) {
    throw new ServerUnavailableError(String(err));
  }
  let data = null;
  try {
    data = await res.json();
  } catch {
    /* empty body */
  }
  if (!res.ok) throw new ApiError(res.status, data?.error, data?.message);
  return data;
}

export const health = () => request('GET', '/health');
export const getSummary = () => request('GET', '/state/summary');
export const getFull = () => request('GET', '/state/full');
export const onboarding = (body) => request('POST', '/onboarding', { body, timeoutMs: 90000 });
export const uploadVoice = (blob) => request('POST', '/voice', { body: blob, timeoutMs: 30000 });
export const postEvent = (event) => request('POST', '/browser/event', { body: event });
export const acknowledgeDelivery = (body) => request('POST', '/browser/ack', { body });
export const postMetadata = (body) => request('POST', '/browser/metadata', { body });
export const rampRespond = (site, choice) => request('POST', '/ramp/respond', { body: { site, choice } });
export const dayRespond = (choice) => request('POST', '/day/respond', { body: { choice } });
export const cardClose = (deliveryId, reason) => request('POST', '/card/close', { body: { deliveryId, reason } });
// Test tools (dashboard ⚙️); hackathon build only.
export const devReset = (scope) => request('POST', '/dev/reset', { body: { scope } });
export const devTimings = (fast) => request('POST', '/dev/timings', { body: { fast } });
export const devRegenerateSteps = () => request('POST', '/dev/regenerate-steps', { body: {}, timeoutMs: 60000 });
export const stepsRespond = (stepId, answer) =>
  request('POST', '/steps/respond', { body: { stepId, answer }, timeoutMs: 60000 });
export const postAnchor = (anchor) => request('POST', '/anchor', { body: anchor });
export const captureIdea = (text, type) => request('POST', '/ideas', { body: { text, type } });
export const getTasks = () => request('GET', '/tasks');
export const taskDone = (id) => request('POST', '/tasks/done', { body: { id } });
export const startCue = (stepId) => request('POST', '/steps/start-cue', { body: { stepId } });
// Phase 4 (server: application/support.go).
export const getLamp = () => request('GET', '/lamp');
export const ideaDelete = (id) => request('POST', '/ideas/delete', { body: { id } });
export const ideaPromote = (id) => request('POST', '/ideas/promote', { body: { id } });
export const getIdeas = () => request('GET', '/ideas');
export const reviewIdeas = (ids = []) => request('POST', '/ideas/review', { body: { ids } });
export const answerOldIdea = (id, accept) => request('POST', '/ideas/offer', { body: { id, accept } });
export const voicePlayed = () => request('POST', '/voice/played', { body: {} });
export const daySummary = () => request('GET', '/summary/day');
// Phase 5 test tools (dashboard ⚙️).
export const devClock = (body) => request('POST', '/dev/clock', { body });
export const devSeed = () => request('POST', '/dev/seed', { body: {} });

/**
 * The user's own recording, as a Blob, or null if there is none. The header
 * is what lets the server tell the extension from a web page (api.go).
 */
export async function voiceBlob() {
  let res;
  try {
    res = await fetch(`${SERVER_URL}/voice`, { headers: { 'X-Focus-Companion': '1' }, signal: AbortSignal.timeout(10000) });
  } catch (err) {
    throw new ServerUnavailableError(String(err));
  }
  return res.ok ? res.blob() : null;
}
// The chat with Max (server: application/chat.go).
export const chatState = () => request('GET', '/chat');
export const chat = (body) => request('POST', '/chat', { body, timeoutMs: 60000 });
// start | pause | yes | not_yet | later | resume | helped (server: application/tasks_life.go).
export const taskAction = (id, action, note) => request('POST', '/tasks/action', { body: { id, action, note } });
// The user's voice to text (Groq Whisper on the server); the recording is not kept.
export const transcribe = (blob, lang) => request('POST', `/stt?lang=${lang === 'el' ? 'el' : 'en'}`, { body: blob, timeoutMs: 45000 });
// The language the user chose for the app, "el" or "en" (the server keeps it).
export const setLang = (lang) => request('POST', '/lang', { body: { lang } });
// Where the user stands, in words (server: application/status.go).
export const getStatus = () => request('GET', '/status');
export const taskHelp = (id) => request('POST', '/tasks/help', { body: { id }, timeoutMs: 45000 });
