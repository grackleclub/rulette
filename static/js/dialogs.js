(function () {
  // open a dialog: data-open-dialog="dialog-id"
  document.body.addEventListener("click", function (e) {
    var btn = e.target.closest("[data-open-dialog]");
    if (btn) {
      var dialogId = btn.dataset.openDialog;
      var fetchEvent = btn.dataset.fetchEvent;
      var inviteLink = btn.dataset.inviteLink;
      var qrSrc = btn.dataset.qrSrc;
      if (inviteLink || qrSrc) {
        var dialog = document.getElementById(dialogId);
        if (!dialog) {
          console.error("dialog not found:", dialogId);
          return;
        }
        if (qrSrc) {
          var img = dialog.querySelector("#invite-qr");
          if (img) img.src = new URL(qrSrc, location.href).href;
        }
        var absLink;
        if (inviteLink) {
          absLink = new URL(inviteLink, location.href).href;
          var linkEl = dialog.querySelector("#invite-link");
          if (linkEl) {
            linkEl.textContent = absLink;
            linkEl.href = absLink;
          }
        }
        dialog.showModal();
        if (absLink) {
          var successEl = dialog.querySelector("#invite-copy-success");
          var failureEl = dialog.querySelector("#invite-copy-failure");
          try {
            navigator.clipboard.writeText(absLink).then(function () {
              if (successEl) successEl.hidden = false;
              if (failureEl) failureEl.hidden = true;
            }).catch(function (err) {
              console.error("clipboard write failed:", err);
              if (failureEl) failureEl.hidden = false;
              if (successEl) successEl.hidden = true;
            });
          } catch (err) {
            console.error("clipboard unavailable:", err);
            if (failureEl) failureEl.hidden = false;
            if (successEl) successEl.hidden = true;
          }
        }
        return;
      }
      if (fetchEvent) {
        // dispatch event to trigger htmx fetch, then open after load
        var dialog = document.getElementById(dialogId);
        dialog.addEventListener("htmx:afterSettle", function once() {
          dialog.removeEventListener("htmx:afterSettle", once);
          dialog.showModal();
        });
        document.body.dispatchEvent(new Event(fetchEvent));
      } else {
        document.getElementById(dialogId).showModal();
      }
    }
  });

  // generic server-driven notice: HX-Trigger {"notice":"message"}
  function showNotice(e) {
    var dialog = document.getElementById("notice-dialog");
    var message = document.getElementById("notice-message");
    if (!dialog || !message) return;
    message.textContent = e.detail && e.detail.value ? e.detail.value : "";
    if (!dialog.open) dialog.showModal();
  }
  document.body.addEventListener("notice", showNotice);

  // host tried to start with fewer than the recommended players: confirm first.
  // HX-Trigger {"confirmStart":""} -> the dialog's "continue" reposts start with
  // ?confirm=1 so the server proceeds.
  function showConfirmStart() {
    var dialog = document.getElementById("confirm-start-dialog");
    if (dialog && !dialog.open) dialog.showModal();
  }
  document.body.addEventListener("confirmStart", showConfirmStart);

  // a drawn rule card opens its own dialog; its "got it" button posts
  // /action/acknowledge, which advances the turn. it stays silent (the spin
  // event already dings the spinner) -- see sound.js dingNotice.
  function showNewCard(e) {
    var dialog = document.getElementById("newcard-dialog");
    var content = document.getElementById("newcard-content");
    if (!dialog || !content) return;
    content.textContent = e.detail && e.detail.value ? e.detail.value : "";
    if (!dialog.open) dialog.showModal();
  }
  document.body.addEventListener("newCard", showNewCard);

  // close a dialog: data-close-dialog="dialog-id"
  document.body.addEventListener("click", function (e) {
    var btn = e.target.closest("[data-close-dialog]");
    if (btn) {
      document.getElementById(btn.dataset.closeDialog).close();
    }
  });

  // close dialog on successful htmx submit: data-close-on-success="dialog-id"
  document.body.addEventListener("htmx:afterRequest", function (e) {
    if (!e.detail.successful) return;
    // walk up to find the attribute (e.detail.elt may be the submit button,
    // not the form that carries data-close-on-success)
    var src = e.detail.elt.closest("[data-close-on-success]");
    if (src) {
      document.getElementById(src.getAttribute("data-close-on-success")).close();
    }
  });

  // points stepper: +/- buttons update hidden input and display
  document.body.addEventListener("click", function (e) {
    var btn = e.target.closest(".points-step");
    if (!btn) return;
    var target = btn.dataset.target || "points";
    var input = document.getElementById(target + "-amount");
    var display = document.getElementById(target + "-display");
    if (!input || !display) return;
    var min = btn.dataset.min !== undefined ? parseInt(btn.dataset.min, 10) : -99;
    var val = parseInt(input.value, 10) + parseInt(btn.dataset.step, 10);
    if (val < min) val = min;
    if (val > 99) val = 99;
    input.value = val;
    display.textContent = val;
  });

  // keep the full game log pinned to the newest entry, but only when the
  // reader is already at the bottom -- don't yank them while they scroll back.
  var logAtBottom = true;
  document.body.addEventListener("htmx:beforeSwap", function (e) {
    if (e.target && e.target.id === "event-log-full") {
      var el = e.target;
      logAtBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 8;
    }
  });
  document.body.addEventListener("htmx:afterSettle", function (e) {
    if (e.target && e.target.id === "event-log-full" && logAtBottom) {
      e.target.scrollTop = e.target.scrollHeight;
    }
  });

  // the full log only loads on open (loadEventLog); refresh it while its dialog
  // is open by piggybacking on the live feed's poll, so closed dialogs don't
  // poll the history endpoint in the background.
  document.body.addEventListener("htmx:afterSettle", function (e) {
    if (!e.target || e.target.id !== "event-log") return;
    var dialog = document.getElementById("event-log-dialog");
    if (dialog && dialog.open) {
      document.body.dispatchEvent(new Event("loadEventLog"));
    }
  });
})();
