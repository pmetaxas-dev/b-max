// A calm sound behind the work, for focus (helpful with ADHD). Made with the
// browser's own audio, so there are no files and no licences: noise shaped
// into "deep calm" (brown noise), rain, or slow waves. Optional and off by
// default; it plays only while this page is open. Page-only (never the
// service worker), and the browser only lets audio start after a click.

import { readSetting, writeSetting } from './voice-io.js';

export const SOUND_KINDS = ['brown', 'rain', 'waves'];

const LOOP_SECONDS = 4;

function noise(ctx, kind) {
  const length = ctx.sampleRate * LOOP_SECONDS;
  const buffer = ctx.createBuffer(1, length, ctx.sampleRate);
  const data = buffer.getChannelData(0);
  let last = 0;
  let b0 = 0, b1 = 0, b2 = 0;
  for (let i = 0; i < length; i++) {
    const white = Math.random() * 2 - 1;
    if (kind === 'pink') {
      b0 = 0.99765 * b0 + white * 0.099046;
      b1 = 0.963 * b1 + white * 0.2965164;
      b2 = 0.57 * b2 + white * 1.0526913;
      data[i] = (b0 + b1 + b2 + white * 0.1848) * 0.2;
    } else {
      last = (last + 0.02 * white) / 1.02; // brown: integrated white noise
      data[i] = last * 3.5;
    }
  }
  return buffer;
}

export function createFocusSound() {
  let ctx = null;
  let master = null;
  let stopNodes = [];
  let kind = readSetting('bmax.focusKind', 'off');
  let volume = Number(readSetting('bmax.focusVolume', '0.4'));
  if (!SOUND_KINDS.includes(kind)) kind = 'off';
  if (!(volume >= 0 && volume <= 1)) volume = 0.4;
  // The choice is remembered, but sound never starts by itself: it waits for a click.
  const remembered = kind;
  kind = 'off';

  function halt() {
    for (const stop of stopNodes) {
      try { stop(); } catch { /* already stopped */ }
    }
    stopNodes = [];
  }

  function source(buffer) {
    const node = ctx.createBufferSource();
    node.buffer = buffer;
    node.loop = true;
    node.start();
    stopNodes.push(() => node.stop());
    return node;
  }

  async function play(next) {
    halt();
    kind = SOUND_KINDS.includes(next) ? next : 'off';
    writeSetting('bmax.focusKind', kind);
    if (kind === 'off') return;
    const Audio = globalThis.AudioContext || globalThis.webkitAudioContext;
    if (!Audio) {
      kind = 'off';
      return;
    }
    ctx ??= new Audio();
    await ctx.resume();
    master ??= ctx.createGain();
    master.gain.value = volume;
    master.connect(ctx.destination);

    if (kind === 'brown') {
      const low = ctx.createBiquadFilter();
      low.type = 'lowpass';
      low.frequency.value = 900;
      source(noise(ctx, 'brown')).connect(low).connect(master);
    } else if (kind === 'rain') {
      const high = ctx.createBiquadFilter();
      high.type = 'highpass';
      high.frequency.value = 600;
      const low = ctx.createBiquadFilter();
      low.type = 'lowpass';
      low.frequency.value = 7000;
      const soft = ctx.createGain();
      soft.gain.value = 0.55;
      source(noise(ctx, 'pink')).connect(high).connect(low).connect(soft).connect(master);
    } else {
      // Slow waves: brown noise whose loudness swells and fades.
      const low = ctx.createBiquadFilter();
      low.type = 'lowpass';
      low.frequency.value = 700;
      const swell = ctx.createGain();
      swell.gain.value = 0.6;
      const lfo = ctx.createOscillator();
      lfo.frequency.value = 0.09;
      const depth = ctx.createGain();
      depth.gain.value = 0.4;
      lfo.connect(depth).connect(swell.gain);
      lfo.start();
      stopNodes.push(() => lfo.stop());
      source(noise(ctx, 'brown')).connect(low).connect(swell).connect(master);
    }
  }

  return {
    play,
    stop: () => play('off'),
    get kind() { return kind; },
    get remembered() { return remembered; },
    get volume() { return volume; },
    setVolume(next) {
      volume = Math.min(1, Math.max(0, Number(next)));
      writeSetting('bmax.focusVolume', String(volume));
      if (master) master.gain.value = volume;
    },
  };
}
