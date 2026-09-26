/*
  Lumi idle controller.

  CSS (lumi.css) does the float and the aura pulse on its own. The only thing
  CSS cannot do is blink at random intervals, so this file schedules blinks
  by toggling `.lumi-blink` on #lumi-eyes.

  Usage (svg must be inline in the DOM):
    const idle = startLumiIdle(svgElement);
    idle.blink();  // force one blink (handy for testing)
    idle.stop();   // remove idle motion entirely

  Talking (a layer on top of idle; the lamp itself speaks, no text/voice):
    idle.startTalking();   // mouth syllables + tiny aura breath + tiny head sway
    idle.stopTalking();    // finishes the current syllable, then back to idle
    idle.isTalking();
    // or, if you only hold the <svg>:  startLumiTalking(svg) / stopLumiTalking(svg)
  Reduced motion: no animation; a static half-open mouth is the indicator.

  Full-body emergence (see lumi.css): the lamp is permanent and never changes
  size. The full-body layer (.lumi-body, a sibling of .lumi inside .lumi-anchor)
  comes out of it and goes back in:
    emergeLumi(anchor);   // hidden -> emerging -> active     (returns a Promise)
    returnLumi(anchor);   // active -> returning -> hidden    (returns a Promise)
    lumiBodyState(anchor) // 'hidden' | 'emerging' | 'active' | 'returning'
  `anchor` is the .lumi-anchor element. These only flip data-lumi-body on it;
  CSS does the motion. They never touch position or the lamp. The promise
  resolves true when the pose is reached, false if a later call took over first.
*/

(function (global) {
  'use strict';

  var BLINK_MIN_MS = 3500;
  var BLINK_MAX_MS = 8000;
  var DOUBLE_BLINK_CHANCE = 0.12;
  var DOUBLE_BLINK_GAP_MS = 260;

  // ---- Talking ---------------------------------------------------------
  // CSS (lumi.css) owns the static look (.lumi-talking reveals the mouth and
  // provides the reduced-motion indicator). This section owns the timing,
  // because a natural talking rhythm needs per-syllable variation that CSS
  // keyframes cannot express. All motion is Web Animations on the EXISTING
  // groups; nothing here changes the artwork or the idle animations.
  var TALK = {
    syllableMin: 380, syllableMax: 520,   // ms per syllable
    peakMin: 0.55, peakMax: 0.9,          // how far the mouth opens (1 = the full #lumi-mouth-open)
    wordMin: 2, wordMax: 5,               // syllables per "word"
    pauseMin: 220, pauseMax: 420,         // ms of closed mouth between words
    swayDeg: 0.6, swayMs: 3200,           // head sway, degrees about the collar
    auraScale: 1.03, auraMs: 1900,        // extra aura breath on top of the idle pulse
    easeBackMs: 400
  };

  function rand(min, max) { return min + Math.random() * (max - min); }
  function randInt(min, max) { return Math.round(rand(min, max)); }

  function createTalking(svg) {
    var mouth = svg.querySelector('#lumi-mouth-open');
    var root = svg.querySelector('#lumi-root');
    var aura = svg.querySelector('#lumi-aura');
    var reduce = global.matchMedia('(prefers-reduced-motion: reduce)');
    var canAnimate = typeof Element.prototype.animate === 'function';

    var talking = false, stopping = false;
    var syllable = null, pauseTimer = null, safetyTimer = null;
    var sylCount = 0, wordLen = 0;
    var loops = [], backs = [];

    // Continuous low-amplitude loops (sway, aura breath). They start from rest
    // (first keyframe = neutral) and ease back to rest when talking stops.
    function startLoop(el, prop, neutral, peak, opposite, ms, origin) {
      if (!el) return;
      el.style.transformBox = 'view-box';
      el.style.transformOrigin = origin;
      var kf = [{}, {}, {}, {}, {}];
      var vals = [neutral, peak, neutral, opposite, neutral];
      for (var i = 0; i < 5; i++) kf[i][prop] = vals[i];
      loops.push({
        el: el, prop: prop, neutral: neutral,
        anim: el.animate(kf, { duration: ms, iterations: Infinity, easing: 'ease-in-out' })
      });
    }

    function clearOriginIfIdle(el) {
      for (var i = 0; i < loops.length; i++) if (loops[i].el === el) return;
      el.style.transformBox = '';
      el.style.transformOrigin = '';
    }

    function easeBackLoops() {
      var pending = loops; loops = [];
      pending.forEach(function (l) {
        var cur = global.getComputedStyle(l.el)[l.prop];
        l.anim.cancel();
        if (!cur || cur === 'none') cur = l.neutral;
        var kf = [{}, {}]; kf[0][l.prop] = cur; kf[1][l.prop] = l.neutral;
        var back = l.el.animate(kf, { duration: TALK.easeBackMs, easing: 'ease-out' });
        backs.push(back);
        back.onfinish = back.oncancel = function () {
          backs.splice(backs.indexOf(back), 1);
          clearOriginIfIdle(l.el);
        };
      });
    }

    function playSyllable() {
      var p = rand(TALK.peakMin, TALK.peakMax);
      var e = 'ease-in-out';
      // closed -> slightly open -> open -> slightly open -> closed
      syllable = mouth.animate([
        { opacity: 0, transform: 'scale(0.8, 0.08)',                    offset: 0,    easing: e },
        { opacity: 1, transform: 'scale(0.92, ' + (p * 0.5).toFixed(3) + ')', offset: 0.25, easing: e },
        { opacity: 1, transform: 'scale(1, ' + p.toFixed(3) + ')',      offset: 0.5,  easing: e },
        { opacity: 1, transform: 'scale(0.92, ' + (p * 0.5).toFixed(3) + ')', offset: 0.75, easing: e },
        { opacity: 0, transform: 'scale(0.8, 0.08)',                    offset: 1 }
      ], { duration: rand(TALK.syllableMin, TALK.syllableMax) });
      syllable.onfinish = onSyllableDone;
    }

    function onSyllableDone() {
      syllable = null;
      if (stopping) { finalize(); return; }
      if (!talking) return;
      if (++sylCount >= wordLen) {
        sylCount = 0; wordLen = randInt(TALK.wordMin, TALK.wordMax);
        pauseTimer = setTimeout(function () {
          pauseTimer = null;
          if (talking && !stopping) playSyllable();
        }, rand(TALK.pauseMin, TALK.pauseMax));
      } else {
        playSyllable();
      }
    }

    function beginEffects() {
      sylCount = 0; wordLen = randInt(TALK.wordMin, TALK.wordMax);
      startLoop(root, 'rotate', '0deg', TALK.swayDeg + 'deg', -TALK.swayDeg + 'deg', TALK.swayMs, '150px 250px');
      startLoop(aura, 'scale', '1', String(TALK.auraScale), '1', TALK.auraMs, '150px 150px');
      playSyllable();
    }

    // hard=true also cuts the ease-back animations (used for dispose / restart).
    function cancelEffects(hard) {
      clearTimeout(pauseTimer); pauseTimer = null;
      clearTimeout(safetyTimer); safetyTimer = null;
      if (syllable) { syllable.onfinish = null; syllable.cancel(); syllable = null; }
      loops.forEach(function (l) { l.anim.cancel(); });
      var els = loops.map(function (l) { return l.el; });
      loops = [];
      if (hard) backs.slice().forEach(function (b) { b.cancel(); });
      els.forEach(clearOriginIfIdle);
    }

    function finalize(hard) {
      cancelEffects(hard === true);
      svg.classList.remove('lumi-talking');
      talking = false; stopping = false;
    }

    function start() {
      if (talking && stopping) finalize(true);   // restart cleanly if a stop was in flight
      if (talking) return;
      talking = true;
      svg.classList.add('lumi-talking');
      if (reduce.matches || !canAnimate || !mouth) return;   // static indicator (CSS)
      beginEffects();
    }

    function stop() {
      if (!talking || stopping) return;
      if (reduce.matches || !canAnimate || !syllable) {      // static, or between words: closed already
        easeBackLoops(); finalize(); return;
      }
      stopping = true;                                       // let the current syllable finish
      easeBackLoops();
      safetyTimer = setTimeout(function () { finalize(); }, TALK.syllableMax + 300);
    }

    function onReduceChange() {
      if (!talking) return;
      cancelEffects(true); stopping = false;
      if (!reduce.matches && canAnimate && mouth) beginEffects();
    }
    if (reduce.addEventListener) reduce.addEventListener('change', onReduceChange);
    else reduce.addListener(onReduceChange);

    return {
      start: start,
      stop: stop,
      isTalking: function () { return talking; },
      dispose: function () {
        if (talking) finalize(true);
        else cancelEffects(true);
        if (reduce.removeEventListener) reduce.removeEventListener('change', onReduceChange);
        else reduce.removeListener(onReduceChange);
      }
    };
  }

  var controllers = typeof WeakMap === 'function' ? new WeakMap() : null;

  function startLumiIdle(svg) {
    var eyes = svg.querySelector('#lumi-eyes');
    var reduce = global.matchMedia('(prefers-reduced-motion: reduce)');
    var timer = null;
    var running = false;
    var talking = createTalking(svg);

    function blinkOnce() {
      if (!eyes) return;
      // Remove + force reflow so the one-shot animation can replay.
      eyes.classList.remove('lumi-blink');
      void eyes.getBoundingClientRect();
      eyes.classList.add('lumi-blink');
    }

    function onAnimationEnd(e) {
      if (e.animationName === 'lumi-blink') eyes.classList.remove('lumi-blink');
    }

    function schedule() {
      var wait = BLINK_MIN_MS + Math.random() * (BLINK_MAX_MS - BLINK_MIN_MS);
      timer = setTimeout(function () {
        blinkOnce();
        if (Math.random() < DOUBLE_BLINK_CHANCE) {
          timer = setTimeout(function () { blinkOnce(); schedule(); }, DOUBLE_BLINK_GAP_MS);
        } else {
          schedule();
        }
      }, wait);
    }

    function start() {
      if (running || reduce.matches) return;
      running = true;
      svg.classList.add('lumi-idle');
      schedule();
    }

    function pause() {
      running = false;
      clearTimeout(timer);
      timer = null;
      if (eyes) eyes.classList.remove('lumi-blink');
    }

    function onReduceChange() {
      if (reduce.matches) pause(); else start();
    }

    if (eyes) eyes.addEventListener('animationend', onAnimationEnd);
    if (reduce.addEventListener) reduce.addEventListener('change', onReduceChange);
    else reduce.addListener(onReduceChange);

    // The .lumi-idle class is always set so CSS variables apply; the CSS itself
    // only animates when reduced motion is not requested.
    svg.classList.add('lumi-idle');
    start();

    var controller = {
      blink: function () { if (!reduce.matches) blinkOnce(); },
      startTalking: talking.start,
      stopTalking: talking.stop,
      isTalking: talking.isTalking,
      stop: function () {
        talking.dispose();
        pause();
        svg.classList.remove('lumi-idle');
        if (eyes) eyes.removeEventListener('animationend', onAnimationEnd);
        if (reduce.removeEventListener) reduce.removeEventListener('change', onReduceChange);
        else reduce.removeListener(onReduceChange);
        if (controllers) controllers.delete(svg);
      }
    };
    if (controllers) controllers.set(svg, controller);
    return controller;
  }

  function lumiController(svg) {
    var c = controllers && controllers.get(svg);
    if (!c) throw new Error('Lumi idle is not running on this <svg>; call startLumiIdle(svg) first.');
    return c;
  }
  function startLumiTalking(svg) { lumiController(svg).startTalking(); }
  function stopLumiTalking(svg) { lumiController(svg).stopTalking(); }

  // Full-body emergence. State lives on the anchor; the body layer is its child.
  // The motion is a set of CSS transitions (one per body part, staggered), so
  // "done" means every transition CSS started for this change has finished.
  var BODY_FALLBACK_MS = 3000;   // safety net if the transitions cannot be observed
  var bodyPending = typeof WeakMap === 'function' ? new WeakMap() : null;

  function lumiBodyState(anchor) {
    return anchor.getAttribute('data-lumi-body') || 'hidden';
  }

  function moveLumiBody(anchor, target) {
    var layer = anchor.querySelector(':scope > .lumi-body');
    if (!layer) throw new Error('No .lumi-body layer inside this .lumi-anchor.');
    var moving = target === 'active' ? 'emerging' : 'returning';
    var state = lumiBodyState(anchor);

    // Already there, or already on the way: nothing to restart.
    if (state === target) return Promise.resolve(true);
    if (state === moving && bodyPending && bodyPending.has(anchor)) return bodyPending.get(anchor).promise;

    // A newer call supersedes an unfinished one (CSS reverses smoothly from where it is).
    var old = bodyPending && bodyPending.get(anchor);
    if (old) old.cancel();

    var settle, promise = new Promise(function (resolve) { settle = resolve; });
    var timer = null, dead = false;

    function cleanup() {
      dead = true;
      clearTimeout(timer);
      if (bodyPending) bodyPending.delete(anchor);
    }
    function finish() {
      if (dead) return;
      cleanup();
      anchor.setAttribute('data-lumi-body', target);
      settle(true);
    }

    if (bodyPending) bodyPending.set(anchor, { promise: promise, cancel: function () { cleanup(); settle(false); } });
    anchor.setAttribute('data-lumi-body', moving);

    // Force a style recalc so the transitions exist, then wait for all of them.
    // None (reduced motion, or nothing changed) means the pose is already reached.
    void layer.offsetWidth;
    var anims = typeof layer.getAnimations === 'function' ? layer.getAnimations({ subtree: true }) : null;
    if (anims && !anims.length) { finish(); return promise; }
    timer = setTimeout(finish, BODY_FALLBACK_MS);
    if (anims) {
      // A rejection means a transition was cancelled, i.e. a newer call took over.
      Promise.all(anims.map(function (a) { return a.finished; })).then(finish, function () {});
    }
    return promise;
  }

  function emergeLumi(anchor) { return moveLumiBody(anchor, 'active'); }
  function returnLumi(anchor) { return moveLumiBody(anchor, 'hidden'); }

  // ---- Dragging -----------------------------------------------------------
  // Moves ONLY the anchor's position (--lumi-x/--lumi-y, the two custom
  // properties "Lamp size" in the README already reserves for drag/
  // persistence -- see lumi.css's .lumi-anchor). Never touches
  // data-lumi-body, so a drag cannot turn the lamp into the full body and
  // cannot desync from emergeLumi()/returnLumi()/showLumiIntervention()
  // (lumi-intervention.js): those only ever read the anchor's CURRENT
  // position live (getBoundingClientRect()), so there is nothing for a
  // drag to leave stale. Dragging is simply refused while the body is not
  // 'hidden' (mid-intervention), which is what keeps it out of that
  // lifecycle entirely rather than trying to coordinate with it.
  var DRAG_THRESHOLD_PX = 4; // ignores a click/tap that wobbles a couple of px
  var dragEnabled = typeof WeakSet === 'function' ? new WeakSet() : null;

  // How far, in local (offsetParent-relative) px, the anchor may sit from
  // each edge: at least half the lamp's own box (so the bulb/aura never
  // clips) and at least the active body's own reach (so a later
  // emergeLumi()/showLumiIntervention() from this spot never clips either,
  // even though the body isn't out right now) -- reusing
  // --lumi-body-reach-down/-x exactly as lumi.css's own comment reserved
  // them for.
  function clampToLocalBounds(anchor, x, y, parentRect) {
    var cs = global.getComputedStyle(anchor);
    var half = (parseFloat(cs.getPropertyValue('--lumi-size')) || 67.5) / 2;
    var reachX = parseFloat(cs.getPropertyValue('--lumi-body-reach-x')) || 0;
    var reachDown = parseFloat(cs.getPropertyValue('--lumi-body-reach-down')) || 0;
    var side = Math.max(half, reachX);
    var bottom = Math.max(half, reachDown);
    var w = parentRect.width || global.innerWidth;
    var h = parentRect.height || global.innerHeight;
    return {
      x: Math.min(Math.max(x, side), Math.max(side, w - side)),
      y: Math.min(Math.max(y, half), Math.max(half, h - bottom))
    };
  }

  function parentRectOf(anchor) {
    var parent = anchor.offsetParent;
    // position:fixed anchors (the real page overlay, per the README) have no
    // offsetParent; client coordinates ARE local coordinates already.
    return parent ? parent.getBoundingClientRect() : { left: 0, top: 0, width: global.innerWidth, height: global.innerHeight };
  }

  // opts.storageKey: sessionStorage key (default: keyed by anchor id, so
  // multiple Lumis on one page don't collide). opts.persist/opts.restore:
  // override the storage primitive entirely (e.g. chrome.storage.session in
  // the extension) -- called as persist({x,y}) / restore() -> {x,y}|null.
  // Returns a disable() function that removes the listeners.
  function enableLumiDrag(anchor, opts) {
    opts = opts || {};
    var lamp = anchor.querySelector(':scope > .lumi');
    if (!lamp || (dragEnabled && dragEnabled.has(anchor))) return function () {};
    if (dragEnabled) dragEnabled.add(anchor);

    var storageKey = opts.storageKey || 'lumi-drag-pos:' + (anchor.id || 'default');
    var restore = opts.restore || function () {
      try {
        var raw = global.sessionStorage.getItem(storageKey);
        return raw ? JSON.parse(raw) : null;
      } catch (e) { return null; }
    };
    var persist = opts.persist || function (pos) {
      try { global.sessionStorage.setItem(storageKey, JSON.stringify(pos)); } catch (e) {}
    };

    function setLocal(x, y) {
      anchor.style.setProperty('--lumi-x', x.toFixed(1) + 'px');
      anchor.style.setProperty('--lumi-y', y.toFixed(1) + 'px');
    }

    // A position has been explicitly chosen (restored or dropped at least
    // once): from then on we keep it clamped across viewport resizes too.
    // Before that, whatever positioning the host page set (e.g. a %
    // --lumi-x meant to track viewport width) is left alone.
    var pinned = false;
    var lastLocal = null;

    var saved = restore();
    if (saved && typeof saved.x === 'number' && typeof saved.y === 'number') {
      var clampedInit = clampToLocalBounds(anchor, saved.x, saved.y, parentRectOf(anchor));
      setLocal(clampedInit.x, clampedInit.y);
      lastLocal = clampedInit;
      pinned = true;
    }

    var pointerId = null, startClientX = 0, startClientY = 0, startX = 0, startY = 0, dragging = false;

    function onPointerDown(e) {
      if (e.button !== undefined && e.button !== 0) return; // primary button/touch/pen only
      if (lumiBodyState(anchor) !== 'hidden') return; // never fight an active/emerging/returning body
      var rect = anchor.getBoundingClientRect(); // zero-size point: its own left/top IS the lamp's current centre
      var pr = parentRectOf(anchor);
      startX = rect.left - pr.left;
      startY = rect.top - pr.top;
      startClientX = e.clientX;
      startClientY = e.clientY;
      pointerId = e.pointerId;
      dragging = false; // becomes true only once the threshold is crossed, so a plain click/tap never moves anything
      lamp.setPointerCapture(pointerId);
      lamp.addEventListener('pointermove', onPointerMove);
      lamp.addEventListener('pointerup', onPointerUp);
      lamp.addEventListener('pointercancel', onPointerUp);
    }

    function onPointerMove(e) {
      if (e.pointerId !== pointerId) return;
      if (lumiBodyState(anchor) !== 'hidden') { onPointerUp(e); return; } // body started emerging mid-drag: bail cleanly, keep current position
      var dx = e.clientX - startClientX, dy = e.clientY - startClientY;
      if (!dragging) {
        if (Math.abs(dx) < DRAG_THRESHOLD_PX && Math.abs(dy) < DRAG_THRESHOLD_PX) return;
        dragging = true;
        anchor.setAttribute('data-lumi-dragging', 'true'); // CSS hook: suppress hover/transition quirks while dragging if ever needed
      }
      if (e.cancelable) e.preventDefault();
      var clamped = clampToLocalBounds(anchor, startX + dx, startY + dy, parentRectOf(anchor));
      setLocal(clamped.x, clamped.y);
      lastLocal = clamped;
    }

    function onPointerUp(e) {
      if (e.pointerId !== pointerId) return;
      lamp.removeEventListener('pointermove', onPointerMove);
      lamp.removeEventListener('pointerup', onPointerUp);
      lamp.removeEventListener('pointercancel', onPointerUp);
      try { lamp.releasePointerCapture(pointerId); } catch (err) {}
      pointerId = null;
      if (dragging) {
        anchor.removeAttribute('data-lumi-dragging');
        pinned = true;
        if (lastLocal) persist(lastLocal);
        // The lamp moved WITH the pointer, so it is typically back under the
        // cursor at release -- the browser's own click-after-mouseup
        // eligibility check (based on release coordinates, not capture)
        // would otherwise still fire a spurious click on whatever the
        // caller wired up (e.g. "open capture") right after a real drag.
        // Swallow exactly the next click, once, capture-phase so it never
        // reaches the caller's own listener.
        lamp.addEventListener('click', function swallow(ev) {
          ev.preventDefault();
          ev.stopImmediatePropagation();
        }, { capture: true, once: true });
      }
      dragging = false;
    }

    lamp.addEventListener('pointerdown', onPointerDown);

    var resizeTimer = null;
    function onResize() {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(function () {
        if (!pinned || dragging || lumiBodyState(anchor) !== 'hidden') return;
        var rect = anchor.getBoundingClientRect();
        var pr = parentRectOf(anchor);
        var clamped = clampToLocalBounds(anchor, rect.left - pr.left, rect.top - pr.top, pr);
        setLocal(clamped.x, clamped.y);
        lastLocal = clamped;
      }, 120);
    }
    global.addEventListener('resize', onResize);

    return function disable() {
      lamp.removeEventListener('pointerdown', onPointerDown);
      global.removeEventListener('resize', onResize);
      clearTimeout(resizeTimer);
      if (dragEnabled) dragEnabled.delete(anchor);
    };
  }

  global.startLumiIdle = startLumiIdle;
  global.emergeLumi = emergeLumi;
  global.returnLumi = returnLumi;
  global.lumiBodyState = lumiBodyState;
  global.startLumiTalking = startLumiTalking;
  global.stopLumiTalking = stopLumiTalking;
  global.enableLumiDrag = enableLumiDrag;
})(window);
