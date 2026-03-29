(function() {
  var hash = window.location.hash;

  if (hash === '#hey') {
    window.location.hash = '';
    document.getElementById('hey-dialog').showModal();
  }

  var match = hash.match(/^#accuse-(\d+)$/);
  if (match) {
    window.location.hash = '';
    document.getElementById('decide-dialog').showModal();
  }
})();