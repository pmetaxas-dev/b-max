// Channel and description of a blacklisted page (MVP §4, "metadata as a
// shield"), so a "Lesson 3" video from a Python channel can count as goal
// work. Read only when the server allows it (payload.readMetadata: blacklisted
// pages only) and only what is on screen; never stored by the server.
//
// YouTube swaps videos without reloading, and leaves the <head> meta tags of
// the FIRST video in place, so on YouTube only the on-screen elements are
// read. These selectors follow YouTube's current layout and may need updating
// when YouTube changes it; the fallback is simply no metadata (title only).

const MAX_CHANNEL = 200;
const MAX_DESCRIPTION = 500;

const clip = (s, max) => (s ?? '').replace(/\s+/g, ' ').trim().slice(0, max);

export function readPageMetadata(doc = document, href = location.href) {
  let host = '';
  try {
    host = new URL(href).hostname;
  } catch {
    return { channel: '', description: '' };
  }
  const text = (selector) => doc.querySelector(selector)?.textContent ?? '';
  const meta = (selector) => doc.querySelector(selector)?.getAttribute('content') ?? '';
  if (/(^|\.)youtube\.com$/.test(host)) {
    return {
      channel: clip(text('ytd-watch-metadata ytd-channel-name a') || text('#owner ytd-channel-name a'), MAX_CHANNEL),
      description: clip(text('ytd-watch-metadata #description-inline-expander') || text('ytd-watch-metadata #description'), MAX_DESCRIPTION),
    };
  }
  // Reddit's subreddit is read from the URL by the server; elsewhere the
  // page's own description is enough.
  return {
    channel: '',
    description: clip(meta('meta[name="description"]') || meta('meta[property="og:description"]'), MAX_DESCRIPTION),
  };
}
