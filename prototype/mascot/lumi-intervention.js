/*
  Lumi intervention layer: a small conversational CARD that appears alongside
  the existing full-body emergence, in one of several reusable compositions
  ("variants"). Separate module: does not modify lumi.svg, lumi-body.svg,
  lumi.css or lumi.js. It only DRIVES the existing, unmodified
  emergeLumi()/returnLumi()/startLumiTalking()/stopLumiTalking() (lumi.js)
  and the existing .lumi-anchor > .lumi-body + .lumi structure (lumi.css),
  and adds two new siblings, .lumi-card and .lumi-hands, inserted between
  the body and the lamp (lumi-intervention.css explains why that DOM order
  alone -- no z-index -- is what keeps the lamp always on top and the card
  always in front of the (hidden) body).

  ARCHITECTURE (see Part 4 of the pose/lifecycle pass this file implements):
  there is exactly ONE authoritative piece of state per anchor -- the
  generation counter in `STATE` below -- and everything else (the card's
  own open/closed attribute, the hands layer's contents, every WAAPI
  animation this file starts) is a CONSEQUENCE of it, torn down by
  forceCleanup() the instant that generation is superseded OR the body
  leaves "active"/"emerging" for any reason. Concretely:

  - showLumiIntervention()/closeLumiIntervention() each bump the counter
    and capture their own token; every deferred step (a setTimeout, a
    Promise .then) re-checks that token before touching the DOM, so a
    call that gets superseded (closed before it opened, or replaced by a
    newer open) becomes a safe no-op instead of resurrecting stale state.
  - A MutationObserver on the anchor's data-lumi-body attribute (the
    UNMODIFIED lumi.js/lumi.css state machine -- the actual single source
    of truth for the body) is the safety net for the case this pass was
    written to fix: something OUTSIDE this file (lumi.js's own returnLumi,
    e.g. a plain "Return to lamp" button that bypasses
    closeLumiIntervention entirely) retracts the body while an
    intervention is showing. The moment data-lumi-body leaves
    active/emerging while data-lumi-variant is still set, the observer
    force-cleans the card/hands immediately, so that combination can never
    be seen on screen, no matter which code path caused the return.

  Usage:
    var i = showLumiIntervention({ variant: 'peek-side' });   // anchor defaults
    i.close();                              // or: closeLumiIntervention(anchor)
    i.ready.then(...)                       // resolves once body + card have settled
    pickLumiVariant();                      // random id, avoids repeating the last one
    LUMI_INTERVENTION_VARIANTS              // ['peek-side', 'peek-top', ...]

  Card content (opts.title / opts.lines / opts.actions) is a placeholder for
  this pass -- no task/idea system, no AI, nothing persisted or connected to
  distraction detection. Both default action buttons just close the
  intervention; opts.onAction(id) is called first if given, so a caller can
  hook real behaviour in later without touching this file.
*/
(function (global) {
  'use strict';

  var SVG_NS = 'http://www.w3.org/2000/svg';
  var VARIANTS = ['peek-side', 'peek-top', 'full-behind', 'full-lean', 'full-entrance'];

  // ---- geometry: card position/occlusion (UNCHANGED from the prior pass) --
  //
  // How the CARD sits relative to the anchor and how much of the body it
  // occludes. Two modes:
  //   centered: true   the card is centred horizontally on the anchor. The
  //                     card (232px) is far wider than the body (~38px), so
  //                     centring it covers her FULL WIDTH from `dy` down --
  //                     used for the variants that hide most/all of the
  //                     body and leave only the head (and maybe a shoulder
  //                     sliver) above the card's top edge.
  //   dx (no centered)  the card's LEFT edge, as an offset from the anchor's
  //                     x=0 (the body's own horizontal centre). A SMALL or
  //                     NEGATIVE dx puts that edge AT or LEFT OF centre, so
  //                     more than half the body -- not just a sliver -- ends
  //                     up covered; only what is left of that line (one
  //                     shoulder/arm, part of the torso) stays visible.
  // dy is the card's top edge, below the anchor (below:true) or its bottom
  // edge, above the anchor (below:false, used if flipped near an edge).
  //
  // Measured against the real rendered body at 1x (45px bulb), anchor-
  // relative, in px: torso x[-11,11] y[23,52], legs+shoes x[-19,19]
  // y[50,75], each arm+glove roughly x[+-7..18] y[32..62] (see README.md,
  // "Intervention layer" for the full table, and the "Composition guides"
  // demo toggle to see the live overlap while tuning these).
  var GEOMETRY = {
    'peek-side': { dx: -8, dy: 24, side: 'right', below: true },
    'peek-top': { centered: true, dy: 25, side: 'right', below: true },
    'full-behind': { centered: true, dy: 42, side: 'right', below: true },
    'full-lean': { dx: 3, dy: 29, side: 'right', below: true, lean: true },
    // FULL POSE (internal id kept as 'full-entrance' for API stability):
    // NOT centred and offset well clear of her (dx=22 > her ~19-wide span),
    // so the card sits BESIDE her at chest height with ~zero occlusion from
    // the card alone -- "almost the entire body visible next to the card".
    'full-entrance': { dx: 22, dy: 16, side: 'right', below: true }
  };

  // ---- geometry: connecting arm -> hand -> target, with NO gap ----------
  //
  // lumi-body.svg's arm (#lumib-arm-shape) is a fixed-length curved path,
  // pivoting with its glove at the SAME shoulder point (106,292 view-box
  // units -- lumi.css, unconditional). Left alone, both always end up at
  // the SAME natural wrist point relative to that shoulder. To make a hand
  // reach a point FARTHER than the arm's own natural reach -- true for
  // every card target below -- WITHOUT ever detaching it from the
  // shoulder, the arm and its glove are transformed together with a
  // single rigid sequence that always pivots at the shoulder:
  //   1. rotate so the arm's own NATURAL direction lines up with the
  //      target's direction (the origin never moves under a rotation);
  //   2. scale ALONG that direction only, by (distance to target) / (the
  //      arm's own natural reach), so the wrist lands EXACTLY on the
  //      target -- the arm's WIDTH (perpendicular to its own length) is
  //      untouched, so it reads as reaching further, not stretched sideways.
  // Both steps share the shoulder as their transform-origin, so the
  // shoulder end can never separate from the torso, and the glove
  // (attached, unmodified, to the far end of this same rigid transform)
  // can never float free of the arm -- replacing an earlier version that
  // moved the glove alone by an arbitrary translate, which is what
  // produced the "floating detached hand" look this pass fixes.
  //
  // All geometry here is expressed in the PRE-MIRROR ("left") local frame
  // lumi-body.svg itself uses for its right arm/glove (see cloneLimb): a
  // real, on-screen target for the RIGHT side is mirrored (x negated) INTO
  // this frame before the angle/scale math, so one formula serves both
  // sides, applied to the pre-mirror element either way.
  var SHOULDER_LOCAL = { x: -9.9, y: 31.95 }; // view-box (106,292) -> anchor-relative px at 1x
  var ARM_NATURAL_LOCAL = { x: -12.6, y: 54.2 }; // the natural (untransformed) wrist/glove centre
  var ARM_REACH = Math.sqrt(Math.pow(ARM_NATURAL_LOCAL.x - SHOULDER_LOCAL.x, 2) + Math.pow(ARM_NATURAL_LOCAL.y - SHOULDER_LOCAL.y, 2));
  var ARM_BASE_ANGLE = (Math.atan2(ARM_NATURAL_LOCAL.y - SHOULDER_LOCAL.y, ARM_NATURAL_LOCAL.x - SHOULDER_LOCAL.x) * 180) / Math.PI;

  // targetReal: an on-screen (post-mirror), anchor-relative {x,y} the hand
  // should end up at (a point on the card's edge for a hold; a point
  // clear of it for a raise). Returns a CSS transform string to apply to
  // the PRE-MIRROR arm/glove elements (see cloneLimb), pivoting at their
  // existing transform-origin (106px 292px, unconditional, from lumi.css).
  function armPoseTransform(targetReal, side) {
    var t = side === 'left' ? targetReal : { x: -targetReal.x, y: targetReal.y };
    var dx = t.x - SHOULDER_LOCAL.x,
      dy = t.y - SHOULDER_LOCAL.y;
    var dist = Math.sqrt(dx * dx + dy * dy) || 0.001;
    var angle = (Math.atan2(dy, dx) * 180) / Math.PI;
    var k = dist / ARM_REACH;
    return 'rotate(' + angle.toFixed(2) + 'deg) scale(' + k.toFixed(3) + ', 1) rotate(' + (-ARM_BASE_ANGLE).toFixed(2) + 'deg)';
  }

  // ---- pose: which hand(s) touch the card, and which are simply raised --
  //
  //   hold   the arm+glove pair is CLONED into a new front layer
  //          (.lumi-hands, painted above the card, below the lamp) and
  //          posed (armPoseTransform) so the glove lands ON a point on the
  //          card's edge (a fraction of its width/height, plus a small
  //          overlap so the fingers visibly cross the line). Cloning is
  //          required because the target deliberately overlaps the card's
  //          rect, and the ORIGINAL pair (inside .lumi-body, behind the
  //          card) would otherwise be hidden by it. side: 'auto' =
  //          whichever side is not covered by the card right now;
  //          'left'/'right' pins one side (the centred variants, where
  //          both hands hold the same top edge).
  //
  //   raise  the SAME pose treatment on the EXISTING (back-layer) arm+
  //          glove -- no cloning, because the target sits clear of the
  //          card's top edge, already visible without needing a front
  //          layer. outX/ty describe an absolute anchor-relative target
  //          (outX to the side the hand ends up on, ty usually negative =
  //          above the head), used for variants 4/5's "second" hand
  //          (thumbs-up / pointing approximations -- see the note below).
  //
  // Approximation note: lumi-body.svg's glove is a single rounded mitten
  // with no separate, poseable fingers or thumb (see its <defs>,
  // #lumib-glove-shape). A literal thumbs-up or one-finger point is not
  // achievable without NEW artwork, which this pass must not add. "raise"
  // instead lifts and angles the EXISTING, fully-connected arm+mitten
  // toward head height, the closest read achievable without redrawing --
  // a raised, gesturing hand, not an anatomically distinct gesture.
  var POSE = {
    'peek-side': {
      hold: [{ side: 'auto', xFrac: 0, yFrac: 0.3, overlap: 6 }]
    },
    // xFrac ~0.36/0.64 (not spread to the card's outer edges): close enough
    // to a CENTRED card's own centre to roughly line up with where each
    // side's shoulder actually is, but spaced apart enough (~60px) that
    // the two held hands render as two distinct hands, not one merged blob.
    'peek-top': {
      hold: [
        { side: 'left', xFrac: 0.36, yFrac: 0, overlap: 6 },
        { side: 'right', xFrac: 0.64, yFrac: 0, overlap: 6 }
      ]
    },
    'full-behind': {
      hold: [
        { side: 'left', xFrac: 0.34, yFrac: 0, overlap: 6 },
        { side: 'right', xFrac: 0.66, yFrac: 0, overlap: 6 }
      ]
    },
    // raise targets are picked to keep the resulting stretch (see
    // armPoseTransform's k) around 1.3-1.5 -- a modest, still-plausible
    // "reaching" elongation. Raising a hand ABOVE this character's own
    // (very large, ~45px) head is a MUCH longer reach than her short arms'
    // natural length for ANY target near/above the bulb: the vertical
    // distance alone from the shoulder to head height already exceeds the
    // arm's whole reach, so hitting it exactly would need k > 3, which
    // looks like a thin, broken-looking line, not a raised arm (this was
    // visually caught and is why these numbers land the hand beside the
    // head/at cheek height, gesturing upward and outward, rather than
    // literally above the bulb -- the closest natural-looking read this
    // character's own proportions allow without new artwork).
    'full-lean': {
      hold: [{ side: 'auto', xFrac: 0, yFrac: 0.28, overlap: 6 }],
      raise: [{ side: 'opposite', outX: 25, ty: 7 }] // "thumbs-up" approximation, chest/shoulder height
    },
    'full-entrance': {
      hold: [{ side: 'auto', xFrac: 0, yFrac: 0.26, overlap: 6 }],
      raise: [{ side: 'opposite', outX: 22, ty: 0 }] // "pointing up" approximation, raised beside the head
    }
  };

  var CARD_MARGIN = 10; // px kept clear of the clamp box on every side
  var CARD_WIDTH = 232; // compact, fixed so every variant reads as one system
  var CARD_FALLBACK_MS = 500; // safety net if a card transition can't be observed

  // ---- variant picking ----------------------------------------------------
  function mulberry32(seed) {
    return function () {
      seed |= 0;
      seed = (seed + 0x6d2b79f5) | 0;
      var t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
      t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
  }

  var lastVariant = typeof WeakMap === 'function' ? new WeakMap() : null;

  // Deterministic if `seed` is a number (same seed -> same pick), random
  // otherwise. Avoids repeating the immediately-previous variant shown on
  // this anchor, so "just pick something" never feels stuck on one pose.
  function pickLumiVariant(seed, anchor) {
    var rng = seed === undefined || seed === null ? Math.random : mulberry32(seed);
    var pick = VARIANTS[Math.floor(rng() * VARIANTS.length)];
    var last = lastVariant && anchor && lastVariant.get(anchor);
    if (pick === last && VARIANTS.length > 1) {
      var i = (VARIANTS.indexOf(pick) + 1 + Math.floor(rng() * (VARIANTS.length - 1))) % VARIANTS.length;
      pick = VARIANTS[i];
    }
    if (lastVariant && anchor) lastVariant.set(anchor, pick);
    return pick;
  }

  // ---- card positioning (UNCHANGED from the prior pass) ------------------
  // The rect every edge of the card must stay inside: the viewport, narrowed
  // by every scrollable/clipping ancestor between the anchor and <body> (so
  // the demo's own .stage, which has overflow:hidden, is respected for
  // free; a production overlay with a position:fixed anchor and no such
  // ancestor just gets the viewport).
  function clampBox(anchor) {
    var vw = document.documentElement.clientWidth,
      vh = document.documentElement.clientHeight;
    var box = { left: 0, top: 0, right: vw, bottom: vh };
    var node = anchor.parentElement;
    while (node && node !== document.body && node.nodeType === 1) {
      var cs = global.getComputedStyle(node);
      if (/(auto|hidden|scroll|clip)/.test(cs.overflow + cs.overflowX + cs.overflowY)) {
        var r = node.getBoundingClientRect();
        box = {
          left: Math.max(box.left, r.left),
          top: Math.max(box.top, r.top),
          right: Math.min(box.right, r.right),
          bottom: Math.min(box.bottom, r.bottom)
        };
      }
      node = node.parentElement;
    }
    return box;
  }

  // How much an ancestor is currently scaling Lumi (e.g. the demo's zoom
  // control), found generically by comparing the lamp's rendered size to
  // its own CSS size -- nothing here is demo-specific.
  function ancestorScale(anchor) {
    var lamp = anchor.querySelector(':scope > .lumi');
    if (!lamp) return 1;
    var cssW = parseFloat(global.getComputedStyle(lamp).width) || 1;
    var renderedW = lamp.getBoundingClientRect().width;
    return renderedW / cssW || 1;
  }

  // Computes and applies the card's position for `variant`. For a centred
  // composition, only the vertical direction can flip (the card is already
  // symmetric on the body); otherwise picks the side with more room too.
  // Always hard-clamps inside the box as a last resort so the card is never
  // actually cut off. Returns the resolved { side, below }.
  function positionCard(anchor, card, variant) {
    var g = GEOMETRY[variant];
    var scale = ancestorScale(anchor);
    var a = anchor.getBoundingClientRect(); // zero-size point: a.left/a.top is Lumi's centre
    var box = clampBox(anchor);
    var w = card.offsetWidth || CARD_WIDTH;
    var h = card.offsetHeight || 150;

    var side = g.side;
    var dyPage = g.dy * scale;
    var below = g.below;
    var pageLeft;

    if (g.centered) {
      pageLeft = a.left - w / 2;
    } else {
      var dxPage = g.dx * scale;
      var roomRight = box.right - a.left,
        roomLeft = a.left - box.left;
      if (side === 'right' && dxPage + w + CARD_MARGIN > roomRight && roomLeft > roomRight) side = 'left';
      if (side === 'left' && dxPage + w + CARD_MARGIN > roomLeft && roomRight > roomLeft) side = 'right';
      pageLeft = side === 'right' ? a.left + dxPage : a.left - dxPage - w;
    }

    var roomBelow = box.bottom - a.top,
      roomAbove = a.top - box.top;
    if (below && dyPage + h + CARD_MARGIN > roomBelow && roomAbove > roomBelow) below = false;
    if (!below && dyPage + h + CARD_MARGIN > roomAbove && roomBelow > roomAbove) below = true;

    var pageTop = below ? a.top + dyPage : a.top - dyPage - h;
    pageLeft = Math.min(Math.max(pageLeft, box.left + CARD_MARGIN), box.right - w - CARD_MARGIN);
    pageTop = Math.min(Math.max(pageTop, box.top + CARD_MARGIN), box.bottom - h - CARD_MARGIN);

    // Convert back to the anchor's own (pre-ancestor-scale) local px, since
    // the card's transform is applied INSIDE the same scaled ancestor.
    var localX = (pageLeft - a.left) / scale;
    var localY = (pageTop - a.top) / scale;
    card.style.setProperty('--lumi-card-tx', localX.toFixed(1) + 'px');
    card.style.setProperty('--lumi-card-ty', localY.toFixed(1) + 'px');
    card.setAttribute('data-lumi-card-side', side);
    card.setAttribute('data-lumi-card-vside', below ? 'below' : 'above');
    anchor.setAttribute('data-lumi-card-side', side); // read by the lumi-lean CSS (mirrors the rotation)
    return { side: side, below: below };
  }

  // ---- card element --------------------------------------------------
  function buildCard() {
    var card = document.createElement('div');
    card.className = 'lumi-card';
    card.innerHTML =
      '<p class="lumi-card-line lumi-card-line--title" data-lumi-card-title></p>' +
      '<div data-lumi-card-lines></div>' +
      '<div class="lumi-card-actions" data-lumi-card-actions></div>';
    return card;
  }

  function fillCard(card, opts, anchor) {
    var title = opts.title !== undefined ? opts.title : 'Hey!';
    var lines = opts.lines || ["Looks like you’re distracted.", 'Want to get back on track?'];
    var actions = opts.actions || [
      { id: 'accept', label: "Let’s do it", primary: true },
      { id: 'dismiss', label: 'Not now' }
    ];
    var titleEl = card.querySelector('[data-lumi-card-title]');
    titleEl.textContent = title || '';
    titleEl.style.display = title ? '' : 'none';
    var linesEl = card.querySelector('[data-lumi-card-lines]');
    linesEl.innerHTML = '';
    lines.forEach(function (line) {
      var p = document.createElement('p');
      p.className = 'lumi-card-line';
      p.textContent = line;
      linesEl.appendChild(p);
    });
    var actionsEl = card.querySelector('[data-lumi-card-actions]');
    actionsEl.innerHTML = '';
    actions.forEach(function (action) {
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'lumi-card-btn' + (action.primary ? ' lumi-card-btn--primary' : '');
      btn.textContent = action.label;
      btn.onclick = function () {
        if (typeof opts.onAction === 'function') opts.onAction(action.id, card);
        // keepOpen: for an action whose result is more card content (e.g. an
        // "Undo" follow-up), not the end of the interaction -- skips the
        // auto-close so the caller can re-render in place with
        // showLumiIntervention() instead of a jarring retract+re-emerge.
        // Every existing caller omits it, so this is additive only.
        if (!action.keepOpen) closeLumiIntervention(anchor);
      };
      actionsEl.appendChild(btn);
    });
  }

  function ensureCard(anchor) {
    var card = anchor.querySelector(':scope > .lumi-card');
    if (card) return card;
    card = buildCard();
    var lamp = anchor.querySelector(':scope > .lumi');
    if (!lamp) throw new Error('showLumiIntervention: no .lumi inside this .lumi-anchor.');
    anchor.insertBefore(card, lamp); // between .lumi-body and .lumi -- see lumi-intervention.css
    return card;
  }

  // ---- hands layer: hands that hold the card -----------------------------
  //
  // A NEW sibling, inserted between .lumi-card and .lumi, so it paints
  // ABOVE the card but still below the lamp (same DOM-order technique as
  // .lumi-card itself -- see lumi-intervention.css, no z-index anywhere
  // here either). It holds CLONES of the existing .lumib-arm/.lumib-glove
  // groups (cloneNode -- the SAME markup, not redrawn), one pair per
  // "hold" pose. The clones' <use href="#lumib-arm-shape"/"#lumib-glove-
  // shape"> resolve against the ORIGINAL <defs> still in .lumi-body's own
  // svg (a bare #id reference resolves against the whole document, not
  // just the containing <svg> -- verified; see also README.md's existing
  // note on reusing ids across two inlined copies). Nothing here
  // duplicates any path data.
  function ensureHandsLayer(anchor) {
    var layer = anchor.querySelector(':scope > .lumi-hands');
    if (layer) return layer;
    layer = document.createElement('div');
    layer.className = 'lumi-hands';
    layer.setAttribute('aria-hidden', 'true');
    layer.innerHTML = '<svg viewBox="0 0 300 300" overflow="visible"></svg>';
    var lamp = anchor.querySelector(':scope > .lumi');
    anchor.insertBefore(layer, lamp);
    return layer;
  }

  // Clones the LIVE arm+glove pair for `side` ('left'/'right') out of the
  // body layer, both wrapped in ONE new group (.lumi-limb-pose) so a single
  // armPoseTransform() moves them together, rigidly connected, and (for the
  // right side) that whole group sits inside a mirror wrapper identical to
  // the one lumi-body.svg itself uses for its right arm/glove -- so the
  // clone's local coordinates and shoulder-based transform-origin (106px
  // 292px, from lumi.css, unconditional -- reused, not redeclared) match
  // the original exactly, whichever side it is.
  function cloneLimb(anchor, side) {
    var suffix = side === 'left' ? '' : ' > g';
    var armOriginal = anchor.querySelector('.lumi-body #lumib-arms' + suffix + ' > .lumib-arm');
    var gloveOriginal = anchor.querySelector('.lumi-body #lumib-gloves' + suffix + ' > .lumib-glove');
    if (!armOriginal || !gloveOriginal) return null;
    var pose = document.createElementNS(SVG_NS, 'g');
    pose.setAttribute('class', 'lumi-limb-pose');
    pose.appendChild(armOriginal.cloneNode(true));
    pose.appendChild(gloveOriginal.cloneNode(true));
    if (side === 'left') return { container: pose, poseEl: pose };
    var mirror = document.createElementNS(SVG_NS, 'g');
    mirror.setAttribute('transform', 'translate(300 0) scale(-1 1)');
    mirror.appendChild(pose);
    return { container: mirror, poseEl: pose };
  }

  // Per-anchor set of currently-running hand WAAPI animations, so they can
  // be cancelled cleanly on close/variant-swap (mirrors how lumi.js tracks
  // and cancels the talking sway's loops). Not persisted beyond that.
  var handAnims = typeof WeakMap === 'function' ? new WeakMap() : null;
  function trackHandAnim(anchor, anim) {
    if (!handAnims) return;
    if (!handAnims.has(anchor)) handAnims.set(anchor, []);
    handAnims.get(anchor).push(anim);
  }
  function cancelHandAnims(anchor) {
    if (!handAnims || !handAnims.has(anchor)) return;
    handAnims.get(anchor).forEach(function (a) {
      try {
        a.cancel();
      } catch (e) {}
    });
    handAnims.set(anchor, []);
  }

  // Animates `el`'s `transform` as a small, slow wiggle AROUND a fixed base
  // pose (base = armPoseTransform's rigid rotate/scale/rotate string, which
  // already puts the hand exactly on the card or exactly where it should
  // be raised to). The wiggle is just one more `rotate()` appended to that
  // SAME string, around the SAME shoulder origin, so a tiny amount of
  // "maintaining contact" motion is added without ever separating the
  // glove from the arm or the arm from the shoulder (every keyframe is a
  // small variation of the one connected pose, never a different one).
  // Skipped entirely under reduced motion -- the element is left at the
  // plain base pose, static, matching every other reduced-motion path in
  // this codebase (emergeLumi/returnLumi, the idle sway all go static-and-
  // instant, never mid-motion).
  function wiggle(anchor, el, base, amplitude, durationMs) {
    if (global.matchMedia('(prefers-reduced-motion: reduce)').matches || typeof el.animate !== 'function') {
      el.style.transform = base;
      return;
    }
    var kf = [-1, 0.6, 0, -0.6, 1, 0].map(function (mult) {
      return { transform: base + ' rotate(' + (mult * amplitude).toFixed(2) + 'deg)' };
    });
    var anim = el.animate(kf, { duration: durationMs, iterations: Infinity, easing: 'ease-in-out' });
    trackHandAnim(anchor, anim);
  }

  // A single "hold" spec -> the front clone for that side, posed onto the
  // card's edge with a tiny perpetual wiggle so it reads as "maintaining
  // contact" rather than a frozen prop. Appends the clone into `svg` and
  // returns which side it resolved to (for the hidden-back-limb /
  // attribute bookkeeping).
  function applyHold(anchor, svg, cardGeom, cardSide, spec) {
    var side = spec.side === 'auto' ? (cardSide === 'right' ? 'left' : 'right') : spec.side;
    // For an 'auto' hold, the card's edge FACING this hand flips along with
    // which side the card itself actually resolved to (it can flip near a
    // viewport edge -- see positionCard): xFrac 0 (the card's LEFT edge)
    // when the card sits to her right and the near/left hand reaches for
    // it; xFrac 1 (its RIGHT edge) in the mirrored case. A fixed-side spec
    // (peek-top/full-behind's left+right pair, both on the same, `centered`
    // card's top edge, which never flips sides) keeps its own declared
    // xFrac untouched.
    var xFrac = spec.side === 'auto' ? (side === 'left' ? 0 : 1) : spec.xFrac;
    var targetReal = {
      x: cardGeom.x + cardGeom.w * xFrac + (side === 'left' ? spec.overlap : -spec.overlap),
      y: cardGeom.y + cardGeom.h * spec.yFrac + (spec.yFrac === 0 ? spec.overlap : 0)
    };
    var limb = cloneLimb(anchor, side);
    if (!limb) return null;
    svg.appendChild(limb.container);
    wiggle(anchor, limb.poseEl, armPoseTransform(targetReal, side), 1.2, 3400); // small, slow: "maintains contact", never lets go
    return side;
  }

  // A "raise" spec: which side it lands on (its own, or whichever side the
  // sole 'auto' hold did NOT take), then the SAME pose treatment on the
  // EXISTING (back-layer) arm+glove -- no cloning, because the target sits
  // clear of the card's edge, already visible without needing a front
  // layer. Returns the resolved side.
  function applyRaise(anchor, pose, cardSide, spec) {
    var side = spec.side === 'opposite' ? (cardSide === 'right' ? 'right' : 'left') : spec.side;
    if (pose.hold && pose.hold.length === 1 && pose.hold[0].side === 'auto') {
      side = cardSide === 'right' ? 'right' : 'left';
    }
    var suffix = side === 'left' ? '' : ' > g';
    var arm = anchor.querySelector('.lumi-body #lumib-arms' + suffix + ' > .lumib-arm');
    var glove = anchor.querySelector('.lumi-body #lumib-gloves' + suffix + ' > .lumib-glove');
    if (!arm || !glove) return null;
    // outX is a magnitude (always positive); the raised hand goes to
    // whichever side it actually resolved to.
    var targetReal = { x: (side === 'left' ? -1 : 1) * spec.outX, y: spec.ty };
    var base = armPoseTransform(targetReal, side);
    wiggle(anchor, arm, base, 1.4, 3000);
    wiggle(anchor, glove, base, 1.8, 3000);
    return side;
  }

  // Builds/updates the front "hold" clones for `variant` and repositions the
  // existing (back-layer) "raise" hand(s), all relative to the CARD's
  // ACTUAL resolved position (its --lumi-card-tx/-ty, already computed by
  // positionCard, including any edge-flip) -- so this never needs to know
  // about, or duplicate, the edge-avoidance logic itself.
  function positionHands(anchor, card, variant, cardSide) {
    clearHands(anchor); // reset first: cancels any previous variant's wiggle anims and raised-limb transforms
    var pose = POSE[variant] || {};
    var layer = ensureHandsLayer(anchor);
    var svg = layer.querySelector('svg');
    var cardGeom = {
      w: card.offsetWidth || CARD_WIDTH,
      h: card.offsetHeight || 150,
      x: parseFloat(card.style.getPropertyValue('--lumi-card-tx')) || 0,
      y: parseFloat(card.style.getPropertyValue('--lumi-card-ty')) || 0
    };

    var heldSides = (pose.hold || []).map(function (spec) {
      return applyHold(anchor, svg, cardGeom, cardSide, spec);
    });
    anchor.setAttribute('data-lumi-hold-left', heldSides.indexOf('left') !== -1 ? 'true' : 'false');
    anchor.setAttribute('data-lumi-hold-right', heldSides.indexOf('right') !== -1 ? 'true' : 'false');

    (pose.raise || []).forEach(function (spec) {
      var side = applyRaise(anchor, pose, cardSide, spec);
      if (side) anchor.setAttribute('data-lumi-raise-' + side, 'true');
    });
  }

  // Removes the front hold clones and restores any raised arm/glove to its
  // plain lumi.css resting transform, cancelling every WAAPI wiggle for this
  // anchor first (an in-flight infinite-iteration animation.cancel()s
  // cleanly; letting the elements just get destroyed/style-reset under it
  // would leave a dangling Animation object, harmless but untidy). This is
  // the ONE function that fully undoes positionHands()'s visible effects --
  // both the normal close path and the lifecycle safety net below call it.
  function clearHands(anchor) {
    cancelHandAnims(anchor);
    var layer = anchor.querySelector(':scope > .lumi-hands');
    if (layer) layer.querySelector('svg').innerHTML = '';
    ['left', 'right'].forEach(function (side) {
      var suffix = side === 'left' ? '' : ' > g';
      var arm = anchor.querySelector('.lumi-body #lumib-arms' + suffix + ' > .lumib-arm');
      var glove = anchor.querySelector('.lumi-body #lumib-gloves' + suffix + ' > .lumib-glove');
      if (arm) arm.style.transform = '';
      if (glove) glove.style.transform = '';
      anchor.removeAttribute('data-lumi-raise-' + side);
    });
    anchor.removeAttribute('data-lumi-hold-left');
    anchor.removeAttribute('data-lumi-hold-right');
  }

  // ---- lifecycle controller ----------------------------------------------
  //
  // ONE authoritative piece of state per anchor: a generation counter.
  // showLumiIntervention()/closeLumiIntervention() each bump it and keep
  // their own token; every deferred continuation (a setTimeout, a Promise
  // .then) compares its captured token against the CURRENT value before
  // touching the DOM, so being superseded (closed before finishing, or
  // replaced by a newer open) makes it a safe no-op rather than a race that
  // could resurrect a card/hands after something else already tore them
  // down. This is what makes rapid open/close/open, rapid variant
  // switching, and "closed mid-emergence" all converge on the correct
  // final state regardless of exact timing.
  var STATE = typeof WeakMap === 'function' ? new WeakMap() : null;
  function stateFor(anchor) {
    if (!STATE) return { generation: 0 };
    var s = STATE.get(anchor);
    if (!s) {
      s = { generation: 0 };
      STATE.set(anchor, s);
    }
    return s;
  }
  function bumpGeneration(anchor) {
    var s = stateFor(anchor);
    s.generation++;
    return s.generation;
  }
  function isCurrent(anchor, gen) {
    return stateFor(anchor).generation === gen;
  }

  // Hard, synchronous, idempotent teardown of every visible trace of an
  // intervention: cancels hand animations/clones (clearHands), force-closes
  // the card (including cancelling any in-flight open/close animation on
  // it, so a pending settleCard() callback cannot re-open it afterwards --
  // see the generation check inside showLumiIntervention/
  // closeLumiIntervention for the other half of that guard), and clears
  // the state attributes. Does NOT touch data-lumi-body or call
  // emergeLumi/returnLumi itself -- this is what runs IN RESPONSE to that
  // state having already changed (or about to be forced), never the
  // trigger for it.
  function forceCleanup(anchor) {
    clearHands(anchor);
    var card = anchor.querySelector(':scope > .lumi-card');
    if (card) {
      card.setAttribute('data-lumi-card-open', 'false');
      if (typeof card.getAnimations === 'function') {
        card.getAnimations().forEach(function (a) {
          try {
            a.cancel();
          } catch (e) {}
        });
      }
    }
    if (openCards) openCards.delete(anchor);
    anchor.removeAttribute('data-lumi-variant');
    anchor.removeAttribute('data-lumi-card-side');
  }

  // The safety net (Part 2 of this pass): watches the anchor's
  // data-lumi-body attribute -- lumi.js/lumi.css's own, unmodified single
  // source of truth for the body -- and force-cleans the instant it leaves
  // active/emerging WHILE an intervention is still marked as showing
  // (data-lumi-variant present). This is what makes it impossible to see a
  // card/hands next to a retracted lamp, however the retraction happened:
  // through closeLumiIntervention (which already clears data-lumi-variant
  // BEFORE calling returnLumi, so this never double-fires for the normal
  // path -- see closeLumiIntervention), or through returnLumi() called
  // directly (e.g. a plain "Return to lamp" button that never knew an
  // intervention was open), which is exactly the lifecycle bug this pass
  // fixes. Set up lazily, once per anchor, the first time an intervention
  // is shown on it.
  var guarded = typeof WeakSet === 'function' ? new WeakSet() : null;
  function ensureLifecycleGuard(anchor) {
    if (guarded && guarded.has(anchor)) return;
    if (guarded) guarded.add(anchor);
    if (typeof MutationObserver !== 'function') return; // no graceful degrade needed: bumpGeneration()+isCurrent() still stop the normal races
    var mo = new MutationObserver(function () {
      var bodyState = anchor.getAttribute('data-lumi-body') || 'hidden';
      if (anchor.hasAttribute('data-lumi-variant') && bodyState !== 'active' && bodyState !== 'emerging') {
        bumpGeneration(anchor); // invalidate any in-flight open/close belonging to the call this bypassed
        forceCleanup(anchor);
      }
    });
    mo.observe(anchor, { attributes: true, attributeFilter: ['data-lumi-body'] });
  }

  // ---- open / close -----------------------------------------------------
  function resolveAnchor(x) {
    if (x && x.nodeType === 1) return x;
    if (x && x.anchor) return resolveAnchor(x.anchor);
    return document.querySelector('.lumi-anchor');
  }

  function settleCard(card, open, done) {
    var reduce = global.matchMedia('(prefers-reduced-motion: reduce)').matches;
    card.setAttribute('data-lumi-card-open', open ? 'true' : 'false');
    if (reduce) {
      done();
      return;
    }
    var anims = typeof card.getAnimations === 'function' ? card.getAnimations() : [];
    if (!anims.length) {
      done();
      return;
    }
    var timer = setTimeout(done, CARD_FALLBACK_MS);
    Promise.all(
      anims.map(function (a) {
        return a.finished.catch(function () {});
      })
    ).then(function () {
      clearTimeout(timer);
      done();
    });
  }

  // Currently-open cards, so a resize can keep them clear of the viewport
  // edges without any dragging support existing yet.
  var openCards = typeof Set === 'function' ? new Set() : null;
  var resizeTimer = null;
  function onResize() {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(function () {
      if (!openCards) return;
      openCards.forEach(function (anchor) {
        var card = anchor.querySelector(':scope > .lumi-card');
        var variant = anchor.getAttribute('data-lumi-variant');
        if (card && variant && card.getAttribute('data-lumi-card-open') === 'true') {
          var pos = positionCard(anchor, card, variant);
          positionHands(anchor, card, variant, pos.side); // keep hands attached to the card's NEW position too
        }
      });
    }, 120);
  }
  if (global.addEventListener) global.addEventListener('resize', onResize);

  function showLumiIntervention(opts) {
    opts = opts || {};
    var anchor = resolveAnchor(opts.anchor);
    if (!anchor) throw new Error('showLumiIntervention: no .lumi-anchor found.');
    var variant = opts.variant || pickLumiVariant(opts.seed, anchor);
    if (VARIANTS.indexOf(variant) === -1) throw new Error('Unknown Lumi intervention variant: ' + variant);
    var g = GEOMETRY[variant];

    ensureLifecycleGuard(anchor);
    var myGen = bumpGeneration(anchor); // supersedes any in-flight open/close on this anchor

    var card = ensureCard(anchor);
    var alreadyOpen = card.getAttribute('data-lumi-card-open') === 'true';
    fillCard(card, opts, anchor);

    anchor.setAttribute('data-lumi-variant', variant);
    var pos = positionCard(anchor, card, variant);
    if (openCards) openCards.add(anchor);

    var bodyReady = emergeLumi(anchor); // existing, unmodified; a no-op promise if already active
    var reduce = global.matchMedia('(prefers-reduced-motion: reduce)').matches;

    var cardReady = new Promise(function (resolve) {
      if (alreadyOpen) {
        settleCard(card, true, resolve); // just reposition/recolour in place, no re-entrance
        return;
      }
      var open = function () {
        if (!isCurrent(anchor, myGen)) {
          resolve(false); // superseded by a newer open/close before this fired
          return;
        }
        settleCard(card, true, resolve);
      };
      // The staggers below are a deliberate PACING choice (part of the
      // "quick, smooth, friendly" entrance), not something reduced motion
      // should still wait through: with no transitions to sequence, open
      // immediately, exactly like emergeLumi()/returnLumi() already do.
      if (reduce) open();
      else global.setTimeout(open, g.quick ? 60 : 220);
    });

    var settled = Promise.all([bodyReady, cardReady]).then(function (results) {
      return isCurrent(anchor, myGen) && results[0] !== false && results[1] !== false;
    });
    // Hold/raise hand poses go on top of the body only once it has actually
    // reached "active" (bodyReady resolved TRUE): each sets an inline
    // transform on top of lumi.css's own hidden<->active transition, which
    // is only safe once that transition has already finished -- doing it
    // any earlier would freeze the emergence at whatever value this wrote,
    // well before her body has actually come out. Skipped if bodyReady
    // resolved false (the emergence was itself interrupted by a return) or
    // if a newer open/close has superseded this one in the meantime.
    bodyReady.then(function (ok) {
      if (ok && isCurrent(anchor, myGen)) positionHands(anchor, card, variant, pos.side);
    });

    return {
      variant: variant,
      side: pos.side,
      ready: settled,
      close: function () {
        return closeLumiIntervention(anchor);
      }
    };
  }

  function closeLumiIntervention(target) {
    var anchor = resolveAnchor(target);
    if (!anchor) return Promise.resolve(false);
    ensureLifecycleGuard(anchor);
    var myGen = bumpGeneration(anchor); // supersedes any in-flight open belonging to a call this close interrupts
    if (openCards) openCards.delete(anchor);
    clearHands(anchor); // release any held/raised pose before the body retracts
    var card = anchor.querySelector(':scope > .lumi-card');
    if (!card || card.getAttribute('data-lumi-card-open') !== 'true') {
      anchor.removeAttribute('data-lumi-variant');
      anchor.removeAttribute('data-lumi-card-side');
      return returnLumi(anchor); // existing, unmodified
    }
    return new Promise(function (resolve) {
      settleCard(card, false, function () {
        if (!isCurrent(anchor, myGen)) {
          resolve(false); // a newer open/close took over while the card was closing
          return;
        }
        anchor.removeAttribute('data-lumi-variant'); // relaxes any lean before the body retracts
        anchor.removeAttribute('data-lumi-card-side');
        returnLumi(anchor).then(resolve); // existing, unmodified
      });
    });
  }

  global.showLumiIntervention = showLumiIntervention;
  global.closeLumiIntervention = closeLumiIntervention;
  global.pickLumiVariant = pickLumiVariant;
  global.LUMI_INTERVENTION_VARIANTS = VARIANTS.slice();
})(window);
