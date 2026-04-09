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

  function openDecideDialog(id) {
    document.querySelectorAll('.infraction-id-input').forEach(function(el) {
      el.value = id;
    });
    var d = document.getElementById('decide-dialog');
    if (!d.open) d.showModal();
  }

  // open decide-dialog when polling finds a pending infraction (host only)
  window.handleInfraction = function(e) {
    if (e.detail.xhr.status === 200) {
      openDecideDialog(e.detail.xhr.responseText);
    }
  };
})();
