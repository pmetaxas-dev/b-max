import assert from 'node:assert/strict';
import test from 'node:test';
import { DEFAULT_PLANET_STATE, ERAS, planetQuery, resolvePlanetState, validPlanetState } from '../shared/planet-state.js';

// In-memory stand-in for chrome.storage.local (promise API).
function memoryStorage(initial = {}) {
  const data = { ...initial };
  return {
    data,
    async get(key) { return key in data ? { [key]: data[key] } : {}; },
    async set(items) { Object.assign(data, items); },
  };
}

test('every contract era and weather is accepted', () => {
  for (const era of ERAS) {
    for (const weather of ['clear', 'storm']) {
      assert.deepEqual(validPlanetState({ era, weather, progress: 0.5 }), { era, weather });
    }
  }
});

test('invalid or old-vocabulary values are rejected, not passed through', () => {
  for (const bad of [
    null, undefined, {}, { era: 'space' }, { weather: 'clear' },
    { era: 'Cyberpunk Future', weather: 'clear' }, { era: 'Bronze Age', weather: 'clear' },
    { era: 'Space', weather: 'clear' }, { era: 'space', weather: 'rain' }, { era: 4, weather: 'clear' },
  ]) {
    assert.equal(validPlanetState(bad), null, JSON.stringify(bad));
  }
});

test('a valid server state is used and cached', async () => {
  const storage = memoryStorage();
  const got = await resolvePlanetState({ era: 'medieval', weather: 'clear' }, storage);
  assert.deepEqual(got, { era: 'medieval', weather: 'clear', source: 'server' });
  assert.deepEqual(storage.data.planetState, { era: 'medieval', weather: 'clear' });
});

test('Go unavailable restores the last valid state', async () => {
  const storage = memoryStorage({ planetState: { era: 'industrial', weather: 'clear' } });
  assert.deepEqual(await resolvePlanetState(null, storage), { era: 'industrial', weather: 'clear', source: 'cache' });
});

test('invalid server values fall back to the cache and do not overwrite it', async () => {
  const storage = memoryStorage({ planetState: { era: 'copper', weather: 'clear' } });
  const got = await resolvePlanetState({ era: 'atlantis', weather: 'clear' }, storage);
  assert.deepEqual(got, { era: 'copper', weather: 'clear', source: 'cache' });
  assert.deepEqual(storage.data.planetState, { era: 'copper', weather: 'clear' });
});

test('first run with no backend and no cache is prehistoric and clear', async () => {
  assert.deepEqual(await resolvePlanetState(null, memoryStorage()), { ...DEFAULT_PLANET_STATE, source: 'default' });
  // A corrupt cache is ignored the same way, and storage failures never throw.
  assert.deepEqual(await resolvePlanetState(null, memoryStorage({ planetState: { era: 'x' } })), { ...DEFAULT_PLANET_STATE, source: 'default' });
  const broken = { get: async () => { throw new Error('boom'); }, set: async () => { throw new Error('boom'); } };
  assert.equal((await resolvePlanetState(null, broken)).source, 'default');
  assert.equal((await resolvePlanetState({ era: 'space', weather: 'clear' }, broken)).source, 'server');
});

test('the planet URL always carries all three parameters', () => {
  assert.equal(planetQuery({ era: 'medieval', weather: 'clear' }, 'popup'), 'era=medieval&weather=clear&view=popup');
  assert.equal(planetQuery({ era: 'space', weather: 'storm' }, 'full'), 'era=space&weather=storm&view=full');
  assert.throws(() => planetQuery({ era: 'space', weather: 'clear' }, 'sidebar'));
});
