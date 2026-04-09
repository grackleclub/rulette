(function () {
  // re-query each time because OOB swaps replace the DOM element
  function getDialog() {
    return document.getElementById("modifier-dialog");
  }
  function getContent() {
    return document.getElementById("modifier-content");
  }

  // after each htmx settle, check if modifier content was populated
  document.body.addEventListener("htmx:afterSettle", function () {
    var dialog = getDialog();
    var content = getContent();
    if (!content || !dialog) return;
    // dialog has content and a turn player set
    var turnPlayer = content.dataset.turnPlayer;
    if (!turnPlayer || !content.children.length) {
      if (dialog.open) dialog.close();
      return;
    }
    // only open for the turn player
    var self = document.getElementById("self");
    if (!self || self.textContent.trim() !== turnPlayer) return;
    if (!dialog.open) dialog.showModal();
  });

  // handle card button clicks
  document.body.addEventListener("click", function (e) {
    var btn = e.target.closest(".modifier-card-btn");
    if (!btn) return;
    e.preventDefault();

    var dialog = getDialog();
    var content = getContent();
    var action = btn.dataset.action;
    var cardId = btn.dataset.gameCardId;
    var url = action + "?game_card_id=" + cardId;

    // for clone/transfer, include target player
    if (content) {
      var effect = content.dataset.effect;
      if (effect === "clone" || effect === "transfer") {
        var radio = content.querySelector(
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
