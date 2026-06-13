(function () {
  // ---- spinner side: the challenge popup with a local countdown ----

  var spinnerTimer = null;
  var spinnerInitiative = null;

  function clearSpinnerTimer() {
    if (spinnerTimer) {
      clearInterval(spinnerTimer);
      spinnerTimer = null;
    }
  }

  // newPrompt fires on the spinner's own spin response: open the popup and run
  // a local 60s countdown from this moment. Per the rules the clock starts when
  // the popup appears, so this is purely the spinner's local time.
  function showPrompt(e) {
    var dialog = document.getElementById("newprompt-dialog");
    var content = document.getElementById("newprompt-content");
    var countdown = document.getElementById("newprompt-countdown");
    if (!dialog || !content || !countdown) return;
    content.textContent = e.detail && e.detail.value ? e.detail.value : "";

    // remember whose turn it is now so we can tell when the host's ruling has
    // advanced it (which is our cue to close the popup).
    var bar = document.querySelector(".table-bar");
    spinnerInitiative = bar ? bar.dataset.initiative : null;

    var deadline = Date.now() + 60 * 1000;
    clearSpinnerTimer();
    function tick() {
      var remaining = Math.ceil((deadline - Date.now()) / 1000);
      if (remaining > 0) {
        countdown.textContent = remaining + "s";
        countdown.classList.remove("prompt-countdown-done");
      } else {
        countdown.textContent = "time's up!";
        countdown.classList.add("prompt-countdown-done");
        clearSpinnerTimer();
      }
    }
    tick();
    spinnerTimer = setInterval(tick, 250);
    if (!dialog.open) dialog.showModal();
  }
  document.body.addEventListener("newPrompt", showPrompt);

  // close the spinner popup once the host has ruled: the turn advances, so the
  // table-bar's initiative changes (mirrors the new-rule card behavior).
  document.body.addEventListener("htmx:afterSettle", function (e) {
    if (!e.target || e.target.id !== "table") return;
    var dialog = document.getElementById("newprompt-dialog");
    if (!dialog || !dialog.open) return;
    var bar = document.querySelector(".table-bar");
    if (bar && spinnerInitiative !== null &&
        bar.dataset.initiative !== spinnerInitiative) {
      clearSpinnerTimer();
      dialog.close();
      spinnerInitiative = null;
    }
  });

  // ---- host side: rule on the challenge (polled) ----

  var hostTimer = null;
  var hostSpinId = null; // spin currently shown in the decide dialog
  var lastRuledSpinId = null; // spin we've already ruled on; don't reopen it

  function clearHostTimer() {
    if (hostTimer) {
      clearInterval(hostTimer);
      hostTimer = null;
    }
  }

  function rule(action) {
    var dialog = document.getElementById("prompt-decide-dialog");
    if (!dialog) return;
    var gameId = dialog.dataset.gameId;
    fetch("/" + gameId + "/action/" + action, { method: "POST" }).then(
      function (res) {
        if (res.ok) {
          lastRuledSpinId = hostSpinId;
          hostSpinId = null;
          clearHostTimer();
          dialog.close();
          document.body.dispatchEvent(new Event("refreshTable"));
        }
        // a 425 (too early) just leaves the dialog open; the "not complete"
        // button re-enables itself on the next tick once the window passes.
      }
    );
  }

  document.body.addEventListener("click", function (e) {
    if (e.target.closest("#prompt-complete-btn")) {
      e.preventDefault();
      rule("complete");
    } else if (e.target.closest("#prompt-incomplete-btn")) {
      e.preventDefault();
      rule("incomplete");
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
    var incompleteBtn = document.getElementById("prompt-incomplete-btn");
    if (incompleteBtn) incompleteBtn.disabled = true;

    clearHostTimer();
    function tick() {
      var remaining = Math.ceil((window_ * 1000 - (Date.now() - anchor)) / 1000);
      if (remaining > 0) {
        if (countdown) {
          countdown.textContent = remaining + "s — they're working on it";
          countdown.classList.remove("prompt-countdown-done");
        }
        if (incompleteBtn) incompleteBtn.disabled = true;
      } else {
        if (countdown) {
          countdown.textContent = "time's up";
          countdown.classList.add("prompt-countdown-done");
        }
        if (incompleteBtn) incompleteBtn.disabled = false;
        clearHostTimer();
      }
    }
    tick();
    hostTimer = setInterval(tick, 250);
    if (!dialog.open) dialog.showModal();
  };
})();
