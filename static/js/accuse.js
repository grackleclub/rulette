(function() {
  // open hey-dialog if navigated to directly via hash
  if (window.location.hash === '#hey') {
    window.location.hash = '';
    var target = document.querySelector("#accuse-content");
    if (target) {
      target.addEventListener("htmx:afterSettle", function once() {
        target.removeEventListener("htmx:afterSettle", once);
        document.getElementById('hey-dialog').showModal();
      });
      document.body.dispatchEvent(new Event("loadAccuse"));
    }
  }

  // tracks the last infraction id the host has already decided on,
  // so polls returning stale state don't reopen the dialog for it
  var lastDecidedId = null;

  function openDecideDialog(id) {
    document.querySelectorAll('.infraction-id-input').forEach(function(el) {
      el.value = id;
    });
    var d = document.getElementById('decide-dialog');
    if (!d.open) d.showModal();
  }

  // open decide-dialog when polling finds a pending infraction (host only)
  window.handleInfraction = function(e) {
    if (e.detail.xhr.status !== 200) return;
    var id = e.detail.xhr.responseText.trim();
    if (id === lastDecidedId) return;
    openDecideDialog(id);
  };

  // when a decide form submits successfully, remember the id
  document.body.addEventListener("htmx:afterRequest", function(e) {
    if (!e.detail.successful) return;
    var path = e.detail.requestConfig && e.detail.requestConfig.path;
    if (!path || path.indexOf("/action/decide") === -1) return;
    var src = e.detail.elt.closest("form");
    if (!src) return;
    var input = src.querySelector(".infraction-id-input");
    if (input && input.value) lastDecidedId = input.value;
  });

  // affirm is a client-side action (no htmx request), so capture the id on click
  document.body.addEventListener("click", function(e) {
    if (!e.target.closest("[data-affirm]")) return;
    var input = document.querySelector("#decide-dialog .infraction-id-input");
    if (input && input.value) lastDecidedId = input.value;
  });
})();
