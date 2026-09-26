import assert from 'node:assert/strict';
import test from 'node:test';
import { readPageMetadata } from '../content/page-metadata.js';

// A document double answering querySelector from a selector -> node map.
function doc(nodes) {
  return { querySelector: (selector) => nodes[selector] ?? null };
}
const textNode = (textContent) => ({ textContent });
const metaNode = (content) => ({ getAttribute: (name) => (name === 'content' ? content : null) });

test('YouTube: channel and description from the on-screen elements', () => {
  const d = doc({
    'ytd-watch-metadata ytd-channel-name a': textNode('  Corey   Schafer '),
    'ytd-watch-metadata #description-inline-expander': textNode('Python Tutorial for Beginners\n\nvariables, loops'),
    'meta[name="description"]': metaNode('STALE: the first video watched in this tab'),
  });
  assert.deepEqual(readPageMetadata(d, 'https://www.youtube.com/watch?v=1'), {
    channel: 'Corey Schafer',
    description: 'Python Tutorial for Beginners variables, loops',
  });
});

test("YouTube never falls back to the <head> meta tags (they belong to the tab's first video)", () => {
  const d = doc({ 'meta[name="description"]': metaNode('Cat compilation') });
  assert.deepEqual(readPageMetadata(d, 'https://m.youtube.com/watch?v=2'), { channel: '', description: '' });
});

test('elsewhere: the page description, clipped', () => {
  const long = 'python '.repeat(200);
  const read = readPageMetadata(doc({ 'meta[property="og:description"]': metaNode(long) }), 'https://www.reddit.com/r/learnpython/');
  assert.equal(read.channel, '');
  assert.equal(read.description.length, 500);
  assert.deepEqual(readPageMetadata(doc({}), 'https://x.com/home'), { channel: '', description: '' });
});

test('an unreadable URL reads nothing', () => {
  assert.deepEqual(readPageMetadata(doc({}), 'not a url'), { channel: '', description: '' });
});
