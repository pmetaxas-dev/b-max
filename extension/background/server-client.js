// Availability-aware wrapper around the API client.
//
// When the server is unreachable, tracking stops: nothing is sent, nothing is
// stored, no interventions (§15). There is no retry timer; the one-minute
// chrome.alarms heartbeat is the bounded backoff and probes /health.

import * as api from '../shared/api.js';
import { getSession, setSession } from '../shared/state.js';

const KEY = 'serverAvailable';

export const isAvailable = () => getSession(KEY, true);

/** Calls an API function and records whether the server answered at all. */
export async function call(fn) {
  try {
    const data = await fn();
    await setSession(KEY, true);
    return { ok: true, data };
  } catch (err) {
    if (err instanceof api.ServerUnavailableError) {
      await setSession(KEY, false);
      return { ok: false, unavailable: true, error: 'Server unavailable' };
    }
    // The server is up but refused the request; availability is unchanged.
    await setSession(KEY, true);
    return { ok: false, unavailable: false, error: err.code || String(err), message: err.message };
  }
}

/** Heartbeat probe while unavailable. Returns true if the server is back. */
export async function probe() {
  const res = await call(api.health);
  return res.ok;
}
