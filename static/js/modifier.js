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

  // show notice when a modifier is shredded (no rule cards to target)
  function showShredNotice(e) {
    console.log("modifierShredded fired", e);
    var notice = document.getElementById("modifier-notice");
    if (!notice) {
      console.log("modifier-notice element not found");
      return;
    }
    var effect = e.detail && e.detail.value ? e.detail.value : "";
    notice.textContent =
      "You drew a " + effect + " modifier but have no cards to target. Spin again!";
    notice.hidden = false;
    clearTimeout(notice._timer);
    notice._timer = setTimeout(function () {
      notice.hidden = true;
    }, 15000);
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
