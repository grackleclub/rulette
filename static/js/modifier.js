(function () {
  // re-query each time because OOB swaps replace the DOM element
  function getDialog() {
    return document.getElementById("modifier-dialog");
  }
  function getContent() {
    return document.getElementById("modifier-content");
  }
  function getData() {
    return document.getElementById("modifier-data");
  }

  // after each htmx settle, check if modifier content was populated
  document.body.addEventListener("htmx:afterSettle", function () {
    var dialog = getDialog();
    var data = getData();
    if (!data || !dialog) return;
    // dialog has content and a turn player set
    var turnPlayer = data.dataset.turnPlayer;
    if (!turnPlayer || !data.children.length) {
      if (dialog.open) dialog.close();
      return;
    }
    // only open for the turn player
    var self = document.getElementById("self");
    if (!self || self.textContent.trim() !== turnPlayer) return;
    if (!dialog.open) dialog.showModal();
  });

  // show modal when a modifier is shredded (no rule cards to target)
  function showShredNotice(e) {
    var dialog = getDialog();
    if (!dialog) return;
    var effect = e.detail && e.detail.value ? e.detail.value : "";
    var gameId = dialog.dataset.gameId;
    var safe = document.createElement("span");
    safe.textContent = effect;
    var escaped = safe.innerHTML;
    dialog.innerHTML =
      '<div class="dialog-body stack stack-centered">' +
      "<h2>No cards to " + escaped + "!</h2>" +
      "<p>You drew a <span class='script'>" + escaped + "</span> modifier but have no cards to target.</p>" +
      '<button class="button-teal" ' +
      'hx-post="/' + gameId + '/action/spin" hx-swap="none" ' +
      'hx-on::after-request="this.closest(\'dialog\').close()">spin again</button>' +
      "</div>";
    htmx.process(dialog);
    dialog.addEventListener("close", function () {
      dialog.innerHTML = '<div id="modifier-content" class="dialog-body"></div>';
    }, { once: true });
    if (!dialog.open) dialog.showModal();
  }
  // listen for both camelCase and kebab-case (htmx dispatches both)
  document.body.addEventListener("modifierShredded", showShredNotice);
  document.body.addEventListener("modifier-shredded", showShredNotice);

  // handle card button clicks
  document.body.addEventListener("click", function (e) {
    var btn = e.target.closest(".modifier-card-btn");
    if (!btn) return;
    e.preventDefault();

    var dialog = getDialog();
    var data = getData();
    var action = btn.dataset.action;
    var cardId = btn.dataset.gameCardId;
    var url = action + "?game_card_id=" + cardId;

    // for clone/transfer, include target player
    if (data) {
      var effect = data.dataset.effect;
      if (effect === "clone" || effect === "transfer") {
        var radio = data.querySelector(
          'input[name="target_player_id"]:checked'
        );
        if (!radio) {
          alert("Select a target player first.");
          return;
        }
        url += "&target_player_id=" + radio.value;
      }
    }

    fetch(url, { method: "POST" }).then(function (res) {
      if (res.ok) {
        dialog.close();
        document.body.dispatchEvent(new Event("refreshTable"));
      }
    });
  });
})();
