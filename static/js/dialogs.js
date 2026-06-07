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

})();
