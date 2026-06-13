(function () {
  // ---- spinner side: the challenge popup with a local countdown ----

  var spinnerTimer = null;
  var spinnerActive = false; // a challenge of mine is open this session
  var lastOutcomeId = null; // event id of the last result I've shown

  function clearSpinnerTimer() {
    if (spinnerTimer) {
      clearInterval(spinnerTimer);
      spinnerTimer = null;
    }
  }

  // newPrompt fires on the spinner's own spin response: open the popup and run
  // a local countdown from this moment, the length set by the server's window.
  // Per the rules the clock starts when the popup appears, so this is purely
  // the spinner's local time.
  function showPrompt(e) {
    var dialog = document.getElementById("newprompt-dialog");
    var content = document.getElementById("newprompt-content");
    var countdown = document.getElementById("newprompt-countdown");
    var waiting = document.getElementById("newprompt-waiting");
    var title = document.getElementById("newprompt-title");
    var outcome = document.getElementById("newprompt-outcome");
    var dismiss = document.getElementById("newprompt-dismiss");
    if (!dialog || !content || !countdown) return;
    var detail = e.detail || {};
    content.textContent = detail.prompt || "";

    // a fresh challenge: show the countdown view, clear any prior outcome.
    spinnerActive = true;
    if (title) title.textContent = "prompt challenge!";
    if (waiting) waiting.hidden = true;
    if (outcome) outcome.hidden = true;
    if (dismiss) dismiss.hidden = true;
    countdown.hidden = false;

    var windowSecs = detail.window || 60;
    var deadline = Date.now() + windowSecs * 1000;
    clearSpinnerTimer();
    function tick() {
      var remaining = Math.ceil((deadline - Date.now()) / 1000);
      if (remaining > 0) {
        countdown.textContent = remaining;
        countdown.classList.remove("prompt-countdown-done");
      } else {
        countdown.textContent = "0";
        countdown.classList.add("prompt-countdown-done");
        if (waiting) waiting.hidden = false;
        clearSpinnerTimer();
      }
    }
    tick();
    spinnerTimer = setInterval(tick, 250);
    if (!dialog.open) dialog.showModal();
  }
  document.body.addEventListener("newPrompt", showPrompt);

  // once the host rules, the result lands in the event feed as a "prompt" event
  // targeting me: swap the popup from the countdown to the outcome and wait for
  // me to dismiss it. whichever poll (feed or table) settles first triggers
  // this; the event-id guard makes the other a no-op.
  function maybeShowOutcome() {
    if (!spinnerActive) return; // no challenge of mine in flight this session
    var dialog = document.getElementById("newprompt-dialog");
    var self = document.getElementById("self");
    if (!dialog || !self) return;
    var myName = self.textContent.trim();
    if (!myName) return;

    var items = document.querySelectorAll(
      '#event-log .event[data-event-type="prompt"]'
    );
    var ev = null;
    for (var i = items.length - 1; i >= 0; i--) {
      if (items[i].getAttribute("data-target") === myName) {
        ev = items[i];
        break;
      }
    }
    if (!ev) return;
    var id = ev.getAttribute("data-event-id");
    if (id === lastOutcomeId) return; // already shown this result
    lastOutcomeId = id;
    spinnerActive = false;
    clearSpinnerTimer();

    // a delta means points were awarded (success); its absence means a fail.
    var deltaAttr = ev.getAttribute("data-delta");
    var delta = deltaAttr === null ? NaN : parseInt(deltaAttr, 10);
    var title = document.getElementById("newprompt-title");
    var countdown = document.getElementById("newprompt-countdown");
    var waiting = document.getElementById("newprompt-waiting");
    var outcome = document.getElementById("newprompt-outcome");
    var dismiss = document.getElementById("newprompt-dismiss");
    if (countdown) countdown.hidden = true;
    if (waiting) waiting.hidden = true;
    if (!isNaN(delta) && delta > 0) {
      if (title) title.textContent = "prompt complete";
      if (outcome) {
        outcome.textContent =
          "You earned " + delta + " points (1 + " + (delta - 1) + " cards).";
        outcome.classList.remove("prompt-outcome-fail");
      }
      if (dismiss) dismiss.textContent = "ok";
    } else {
      if (title) title.textContent = "prompt failed";
      if (outcome) {
        outcome.textContent = "No points awarded.";
        outcome.classList.add("prompt-outcome-fail");
      }
      if (dismiss) dismiss.textContent = "ok";
    }
    if (outcome) outcome.hidden = false;
    if (dismiss) dismiss.hidden = false;
    if (!dialog.open) dialog.showModal();
  }

  document.body.addEventListener("click", function (e) {
    if (!e.target.closest("#newprompt-dismiss")) return;
    e.preventDefault();
    var dialog = document.getElementById("newprompt-dialog");
    if (dialog && dialog.open) dialog.close();
  });

  document.body.addEventListener("htmx:afterSettle", function (e) {
    if (!e.target) return;
    if (e.target.id === "event-log" || e.target.id === "table") {
      maybeShowOutcome();
    }
  });

  // ---- host side: rule on the challenge (polled) ----

  var hostTimer = null;
  var hostSpinId = null; // spin currently shown in the decide dialog
  var lastRuledSpinId = null; // spin we've already ruled on; don't reopen it
  var ruling = false; // a ruling POST is in flight; blocks double-submits

  function clearHostTimer() {
    if (hostTimer) {
      clearInterval(hostTimer);
      hostTimer = null;
    }
  }

  function rule(action) {
    if (ruling) return; // a ruling is already in flight; ignore extra taps
    var dialog = document.getElementById("prompt-decide-dialog");
    if (!dialog) return;
    ruling = true;
    var gameId = dialog.dataset.gameId;
    fetch("/" + gameId + "/action/" + action, { method: "POST" }).then(
      function (res) {
        ruling = false;
        if (res.ok) {
          lastRuledSpinId = hostSpinId;
          hostSpinId = null;
          clearHostTimer();
          dialog.close();
          document.body.dispatchEvent(new Event("refreshTable"));
        } else if (res.status === 425) {
          // too early: the server's grace window is a touch longer than the
          // local countdown, so a fail can land in the gap. tell the host and
          // leave the dialog open; the button re-enables on the next tick.
          document.body.dispatchEvent(new CustomEvent("notice", {
            detail: { value: "Player's time is not up yet." },
          }));
        } else {
          // any other non-OK: surface it so the host can retry on purpose.
          document.body.dispatchEvent(new CustomEvent("notice", {
            detail: { value: "Could not record that ruling. Try again." },
          }));
        }
      }
    ).catch(function () {
      ruling = false;
      document.body.dispatchEvent(new CustomEvent("notice", {
        detail: { value: "Network error. Please try again." },
      }));
    });
  }

  document.body.addEventListener("click", function (e) {
    if (e.target.closest("#prompt-succeed-btn")) {
      e.preventDefault();
      rule("succeed");
    } else if (e.target.closest("#prompt-fail-btn")) {
      e.preventDefault();
      rule("fail");
    }
  });

  // handlePrompt runs after every host prompt-poll. 200 = a live challenge to
  // rule on; 204 = none (close any stale dialog).
  window.handlePrompt = function (e) {
    var dialog = document.getElementById("prompt-decide-dialog");
    if (!dialog) return;
    if (e.detail.xhr.status !== 200) {
      if (dialog.open && hostSpinId !== null) {
        clearHostTimer();
        dialog.close();
        hostSpinId = null;
      }
      return;
    }
    var data;
    try {
      data = JSON.parse(e.detail.xhr.responseText);
    } catch (err) {
      return;
    }
    if (String(data.spin_id) === String(lastRuledSpinId)) return; // already ruled
    if (String(data.spin_id) === String(hostSpinId)) return; // already showing

    // a new challenge: populate and open. anchor the countdown to the spin
    // time the server reported (now minus the elapsed seconds) so it stays
    // accurate however late this host opened the popup.
    hostSpinId = data.spin_id;
    var spinnerEl = document.getElementById("prompt-decide-spinner");
    var contentEl = document.getElementById("prompt-decide-content");
    if (spinnerEl) spinnerEl.textContent = data.spinner + "'s challenge:";
    if (contentEl) contentEl.textContent = data.prompt;

    var window_ = data.window || 60;
    var anchor = Date.now() - (data.elapsed || 0) * 1000;
    var countdown = document.getElementById("prompt-decide-countdown");
    var failBtn = document.getElementById("prompt-fail-btn");
    if (failBtn) failBtn.disabled = true;

    clearHostTimer();
    function tick() {
      var remaining = Math.ceil((window_ * 1000 - (Date.now() - anchor)) / 1000);
      if (remaining > 0) {
        if (countdown) {
          countdown.textContent = remaining;
          countdown.classList.remove("prompt-countdown-done");
        }
        if (failBtn) failBtn.disabled = true;
      } else {
        if (countdown) {
          countdown.textContent = "0";
          countdown.classList.add("prompt-countdown-done");
        }
        if (failBtn) failBtn.disabled = false;
        clearHostTimer();
      }
    }
    tick();
    hostTimer = setInterval(tick, 250);
    if (!dialog.open) dialog.showModal();
  };
})();
