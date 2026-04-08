(function () {
  // open a dialog: data-open-dialog="dialog-id"
  document.body.addEventListener("click", function (e) {
    var btn = e.target.closest("[data-open-dialog]");
    if (btn) {
      var dialogId = btn.dataset.openDialog;
      var fetchEvent = btn.dataset.fetchEvent;
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
    var dialogId = e.detail.elt.getAttribute("data-close-on-success");
    if (dialogId) {
      document.getElementById(dialogId).close();
    }
  });

  // affirm: close decide-dialog, reset points forms, open points-dialog
  document.body.addEventListener("click", function (e) {
    if (!e.target.closest("[data-affirm]")) return;
    document.getElementById("decide-dialog").close();
    document.querySelectorAll("#points-dialog form").forEach(function (f) {
      f.reset();
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
