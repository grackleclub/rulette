(function () {
  // three sounds, loaded from /static/audio (Turk provides the WAVs)
  var SOUNDS = {
    alert: "/static/audio/alert.wav", // your turn / new card / a message
    happy: "/static/audio/happy.wav", // you gained points / your accusation held
    sad: "/static/audio/sad.wav", // you lost points / your accusation was tossed
  };
  var STORE_KEY = "rulette-sound";

  var ctx = null;
  var gain = null; // shared gain so overlapping sounds don't clip
  var buffers = {};
  var lastSeenId = 0;
  var seeded = false; // the first poll only seeds; it never replays history

  var mixSoundProximity = 250; // ms to stagger sounds that arrive together
  var maxQueuedSounds = 4; // cap stacked sounds so a burst can't pile up
  var nextAt = 0; // AudioContext time the next stacked sound may start

  // localStorage can throw (privacy modes, storage disabled); guard it and
  // fall back to an in-memory preference so muting still works for the session,
  // defaulting to sound-on when nothing has been set.
  var memPref = null;
  function storeGet() {
    try {
      return localStorage.getItem(STORE_KEY);
    } catch (e) {
      return memPref;
    }
  }
  function storeSet(v) {
    memPref = v;
    try {
      localStorage.setItem(STORE_KEY, v);
    } catch (e) {
      /* storage unavailable: preference lives in memPref for this session */
    }
  }

  function soundOn() {
    return storeGet() !== "off"; // default on
  }
  function self() {
    var el = document.getElementById("self");
    return el ? el.textContent.trim() : "";
  }

  // create the audio context and decode the WAVs once. safe to call at load:
  // decoding doesn't need a user gesture (only playback does), so doing it
  // early means the first sound isn't lost waiting on the decode.
  function preloadAudio() {
    if (ctx) return;
    var AC = window.AudioContext || window.webkitAudioContext;
    if (!AC) return;
    ctx = new AC();
    gain = ctx.createGain();
    gain.gain.value = 0.85; // headroom so overlapping sounds don't clip
    gain.connect(ctx.destination);
    Object.keys(SOUNDS).forEach(function (name) {
      fetch(SOUNDS[name])
        .then(function (r) {
          if (!r.ok) throw new Error("missing");
          return r.arrayBuffer();
        })
        .then(function (data) {
          return ctx.decodeAudioData(data);
        })
        .then(function (buf) {
          buffers[name] = buf;
        })
        .catch(function () {
          /* file absent or undecodable: that sound just stays silent */
        });
    });
  }

  var unlocked = false; // iOS needs a real source started inside the gesture

  // browsers block audio until a user gesture; resume the (preloaded) context
  // on the first one so playback is allowed. buffers are already decoded.
  // iOS is stricter than resuming: it only lifts the block once a source has
  // actually started during the gesture, so play one silent sample as well.
  function unlockAudio() {
    preloadAudio();
    if (!ctx) return;
    if (ctx.state === "suspended") ctx.resume();
    if (unlocked) return;
    try {
      var silent = ctx.createBuffer(1, 1, 22050);
      var src = ctx.createBufferSource();
      src.buffer = silent;
      src.connect(ctx.destination);
      src.start(0);
      unlocked = true;
    } catch (e) {
      /* a later gesture will try again */
    }
  }

  function play(name) {
    if (!ctx || !buffers[name]) return;
    if (ctx.state === "suspended") ctx.resume();
    var gap = mixSoundProximity / 1000; // Web Audio works in seconds
    var now = ctx.currentTime;
    // sounds that arrive together play in order, overlapping: each starts one
    // gap after the previous instead of all at the same instant. a lone sound
    // (nextAt already in the past) still starts immediately.
    var at = Math.max(now, nextAt);
    // don't let an old backlog delay the newest sound: if the queue is already
    // a full window deep, start now and overlap rather than wait.
    if (at - now > maxQueuedSounds * gap) at = now;
    var src = ctx.createBufferSource();
    src.buffer = buffers[name];
    src.connect(gain);
    src.start(at);
    nextAt = at + gap;
  }

  // an event element -> { sound, who } where "who" is the player it concerns;
  // null for events with no sound in this set.
  function soundFor(ev) {
    var actor = ev.getAttribute("data-actor") || "";
    var target = ev.getAttribute("data-target") || "";
    var deltaAttr = ev.getAttribute("data-delta");
    var delta = deltaAttr === null ? NaN : parseInt(deltaAttr, 10);
    var affirmedAttr = ev.getAttribute("data-affirmed");
    var affirmed = affirmedAttr === null ? null : affirmedAttr === "true";
    switch (ev.getAttribute("data-event-type")) {
      case "turn":
        return { sound: "alert", who: target }; // your turn
      case "spin":
        return { sound: "alert", who: actor }; // you drew a card
      case "rolled-end":
        return { sound: "alert", who: actor }; // you rolled the end of the game
      case "clone":
      case "transfer":
        return { sound: "alert", who: target }; // a card landed with you
      case "points":
        if (isNaN(delta) || delta === 0) return null; // no-op/unknown: no sound
        return { sound: delta > 0 ? "happy" : "sad", who: target };
      case "decide":
        // target is the accuser; they hear the verdict
        if (affirmed === null) return null;
        return { sound: affirmed ? "happy" : "sad", who: target };
      default:
        return null;
    }
  }

  // walk the live feed; play the newest events that concern me. the first run
  // with events present only seeds lastSeenId, so history doesn't replay. within
  // a single fetch each sound type plays at most once, so a burst of like events
  // in one poll doesn't stack; separate polls each get their own sound.
  function process() {
    var list = document.getElementById("event-log");
    if (!list) return;
    var items = list.querySelectorAll(".event");
    var me = self();
    var on = soundOn();
    var maxId = lastSeenId;
    var toPlay = [];
    var played = Object.create(null); // sound types already queued this fetch (debounce per type)
    for (var i = 0; i < items.length; i++) {
      var id = parseInt(items[i].getAttribute("data-event-id"), 10);
      if (isNaN(id)) continue;
      if (id > maxId) maxId = id;
      if (!seeded) continue; // first pass only seeds; no playback
      if (id <= lastSeenId) continue; // already handled
      if (!on) continue;
      var m = soundFor(items[i]);
      if (!m || !m.who || m.who !== me) continue;
      if (played[m.sound]) continue; // this type already queued this fetch
      played[m.sound] = true;
      toPlay.push(m.sound);
    }
    // a burst can't pile up: keep only the most recent few sounds (newest win)
    if (toPlay.length > maxQueuedSounds) {
      toPlay = toPlay.slice(toPlay.length - maxQueuedSounds);
    }
    for (var k = 0; k < toPlay.length; k++) play(toPlay[k]);
    // keep the live list small; full history is available via the dialog
    if (items.length > 50) {
      for (var j = 0; j < items.length - 50; j++) items[j].remove();
    }
    lastSeenId = maxId;
    seeded = true;
  }

  // server messages/warnings and the "no cards to ..." notice ding. these
  // events only fire on the screen of the player who triggered them.
  function dingNotice() {
    if (soundOn()) play("alert");
  }

  // the toggle lives in the shared footer, so it shows on every page; audio
  // only matters on a game page (where the event feed exists).
  var inGame = !!document.getElementById("event-log");

  // mute toggle (data-sound-toggle) — just a stored preference, works anywhere
  function refresh(btn) {
    btn.textContent = soundOn() ? "🔊 sound" : "🔇 muted";
    btn.setAttribute("aria-pressed", soundOn() ? "true" : "false");
  }
  document.body.addEventListener("click", function (e) {
    var btn = e.target.closest("[data-sound-toggle]");
    if (!btn) return;
    storeSet(soundOn() ? "off" : "on");
    refresh(btn);
    if (inGame) unlockAudio();
  });
  document.addEventListener("DOMContentLoaded", function () {
    var btn = document.querySelector("[data-sound-toggle]");
    if (btn) refresh(btn);
  });

  if (inGame) {
    // decode the WAVs now so the first sound isn't lost warming up
    preloadAudio();
    // the feed re-renders on its poll; react when it settles
    document.body.addEventListener("htmx:afterSettle", function (e) {
      if (e.target && e.target.id === "event-log") process();
    });
    document.body.addEventListener("notice", dingNotice);
    document.body.addEventListener("modifierShredded", dingNotice);
    document.body.addEventListener("modifier-shredded", dingNotice);
    // browsers block audio until a gesture; unlock on the first interaction.
    // listen for several gesture types because iOS Safari unlocks reliably on
    // touchend/click but not always on pointerdown.
    ["pointerdown", "touchend", "click"].forEach(function (evt) {
      document.body.addEventListener(evt, unlockAudio);
    });
  }
})();
