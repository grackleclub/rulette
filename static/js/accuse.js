(function() {
  // open hey-dialog if navigated to directly via hash
  if (window.location.hash === '#hey') {
    window.location.hash = '';
    document.getElementById('hey-dialog').showModal();
  }

  // populate infraction_id inputs and open decide-dialog
  // when accuse action succeeds (htmx fires infractionCreated event)
  document.body.addEventListener('infractionCreated', function(e) {
    document.querySelectorAll('.infraction-id-input').forEach(function(el) {
      el.value = e.detail.id;
    });
    document.getElementById('decide-dialog').showModal();
  });
})();
