(function () {
  // three sounds, loaded from /static/audio (Turk provides the WAVs)
  var SOUNDS = {
    alert: "/static/audio/alert.wav", // your turn / new card / a message
    happy: "/static/audio/happy.wav", // you gained points / your accusation held
    sad: "/static/audio/sad.wav", // you lost points / your accusation was tossed
  };
  var STORE_KEY = "rulette-sound";

  var ctx = null;
  var buffers = {};
  var lastSeenId = 0;
  var seeded = false; // the first poll only seeds; it never replays history

  function soundOn() {
    return localStorage.getItem(STORE_KEY) !== "off"; // default on
  }
  function self() {
    var el = document.getElementById("self");
    return el ? el.textContent.trim() : "";
  }

  // create the audio context and decode the WAVs once, on first user gesture
  function ensureAudio() {
    if (ctx) return;
    var AC = window.AudioContext || window.webkitAudioContext;
    if (!AC) return;
    ctx = new AC();
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

  function play(name) {
    if (!ctx || !buffers[name]) return;
    if (ctx.state === "suspended") ctx.resume();
    var src = ctx.createBufferSource();
    src.buffer = buffers[name];
    src.connect(ctx.destination);
    src.start(0);
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
  // with events present only seeds lastSeenId, so history doesn't replay.
  function process() {
    var list = document.getElementById("event-log");
    if (!list) return;
    var items = list.querySelectorAll(".event");
    var me = self();
    var on = soundOn();
    var maxId = lastSeenId;
    for (var i = 0; i < items.length; i++) {
      var id = parseInt(items[i].getAttribute("data-event-id"), 10);
      if (isNaN(id)) continue;
      if (id > maxId) maxId = id;
      if (!seeded) continue; // first pass only seeds; no playback
      if (id <= lastSeenId) continue; // already handled
      if (!on) continue;
      var m = soundFor(items[i]);
      if (m && m.who && m.who === me) play(m.sound);
    }
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
    localStorage.setItem(STORE_KEY, soundOn() ? "off" : "on");
    refresh(btn);
    if (inGame) ensureAudio();
  });
  document.addEventListener("DOMContentLoaded", function () {
    var btn = document.querySelector("[data-sound-toggle]");
    if (btn) refresh(btn);
  });

  if (inGame) {
    // the feed re-renders on its poll; react when it settles
    document.body.addEventListener("htmx:afterSettle", function (e) {
      if (e.target && e.target.id === "event-log") process();
    });
    document.body.addEventListener("notice", dingNotice);
    document.body.addEventListener("modifierShredded", dingNotice);
    document.body.addEventListener("modifier-shredded", dingNotice);
    // browsers block audio until a gesture; unlock on the first interaction
    document.body.addEventListener("pointerdown", ensureAudio);
  }
})();
