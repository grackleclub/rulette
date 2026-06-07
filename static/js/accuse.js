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
  var currentInfraction = null;

  function openDecideDialog(data) {
    currentInfraction = data;
    document.querySelectorAll('.infraction-id-input').forEach(function(el) {
      el.value = data.id;
    });
    var d = document.getElementById('decide-dialog');
    var infoName = d.querySelector('.decide-info-name');
    var infoRule = d.querySelector('.decide-info-rule');
    if (infoName) infoName.textContent = 'did ' + data.accused + ' break the rule?';
    if (infoRule) infoRule.textContent = data.rule;
    if (!d.open) d.showModal();
  }

  // open decide-dialog when polling finds a pending infraction (host only)
  window.handleInfraction = function(e) {
    if (e.detail.xhr.status !== 200) return;
    var data;
    try {
      data = JSON.parse(e.detail.xhr.responseText);
    } catch (err) {
      return;
    }
    if (String(data.id) === lastDecidedId) return;
    openDecideDialog(data);
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

  // affirm: close decide-dialog, populate points-dialog with context, open it
  document.body.addEventListener("click", function(e) {
    if (!e.target.closest("[data-affirm]")) return;
    var input = document.querySelector("#decide-dialog .infraction-id-input");
    if (input && input.value) lastDecidedId = input.value;

    document.getElementById("decide-dialog").close();
    document.querySelectorAll("#points-dialog form").forEach(function(f) {
      f.reset();
    });
    var display = document.getElementById("points-display");
    if (display) display.textContent = "0";
    document.querySelectorAll("#points-dialog .infraction-id-input").forEach(function(el) {
      el.value = input ? input.value : "";
    });
    var pointsInfo = document.querySelector("#points-dialog .points-info");
    if (pointsInfo && currentInfraction) {
      pointsInfo.textContent = currentInfraction.accused + ' broke: ' + currentInfraction.rule;
    }
    document.getElementById("points-dialog").showModal();
  });

  // nevermind = deny the accusation
  document.body.addEventListener("click", function(e) {
    var btn = e.target.closest("[data-nevermind]");
    if (!btn) return;
    var dialog = document.getElementById("points-dialog");
    var form = dialog.querySelector("form");
    var input = form.querySelector(".infraction-id-input");
    if (input && input.value) {
      var gameId = dialog.dataset.gameId;
      var formData = new FormData();
      formData.append("verdict", "absolve");
      formData.append("infraction_id", input.value);
      fetch("/" + gameId + "/action/decide", { method: "POST", body: formData }).then(function(res) {
        if (res.ok) {
          lastDecidedId = input.value;
          dialog.close();
        }
      });
    }
  });

  // cancel accuse panel: data-cancel-accuse
  document.body.addEventListener("click", function(e) {
    if (!e.target.closest("[data-cancel-accuse]")) return;
    var panel = e.target.closest(".accuse-panel");
    if (panel) panel.remove();
  });
})();
