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
        if (inviteLink) {
          var absLink = new URL(inviteLink, location.href).href;
          var linkEl = dialog.querySelector("#invite-link");
          if (linkEl) linkEl.textContent = absLink;
          try {
            navigator.clipboard.writeText(absLink).catch(function (err) {
              console.error("clipboard write failed:", err);
            });
          } catch (err) {
            console.error("clipboard unavailable:", err);
          }
        }
        dialog.showModal();
        return;
      }
      if (fetchEvent) {
        // dispatch event to trigger htmx fetch, then open after load
        var dialog = document.getElementById(dialogId);
        var target = dialog.querySelector("[hx-trigger*='" + fetchEvent + "']");
        if (target) {
          target.addEventListener("htmx:afterSettle", function once() {
            target.removeEventListener("htmx:afterSettle", once);
            dialog.showModal();
          });
        }
        document.body.dispatchEvent(new Event(fetchEvent));
      } else {
        document.getElementById(dialogId).showModal();
      }
    }
  });

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

  // affirm: close decide-dialog, reset points forms, open points-dialog
  document.body.addEventListener("click", function (e) {
    if (!e.target.closest("[data-affirm]")) return;
    var idInput = document.querySelector("#decide-dialog .infraction-id-input");
    var id = idInput ? idInput.value : "";
    document.getElementById("decide-dialog").close();
    document.querySelectorAll("#points-dialog form").forEach(function (f) {
      f.reset();
    });
    document.querySelectorAll("#points-dialog .infraction-id-input").forEach(function (el) {
      el.value = id;
    });
    document.getElementById("points-dialog").showModal();
  });

  // cancel accuse panel: data-cancel-accuse
  document.body.addEventListener("click", function (e) {
    if (!e.target.closest("[data-cancel-accuse]")) return;
    var panel = e.target.closest(".accuse-panel");
    if (panel) panel.remove();
  });
})();
