(function () {
  function getDialog() {
    return document.getElementById("modifier-dialog");
  }
  function getData() {
    return document.getElementById("modifier-data");
  }

  // after modifier content is fetched, check if we should open the dialog
  document.body.addEventListener("htmx:afterSettle", function (e) {
    if (!e.detail || !e.detail.elt || e.detail.elt.id !== "modifier-content") return;
    var dialog = getDialog();
    var data = getData();
    if (!data || !dialog) return;
    var turnPlayer = data.dataset.turnPlayer;
    if (!turnPlayer || !data.children.length) return;
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

    var wrapper = document.createElement("div");
    wrapper.className = "dialog-body stack stack-centered";

    var h2 = document.createElement("h2");
    h2.textContent = "No cards to " + effect + "!";
    wrapper.appendChild(h2);

    var p = document.createElement("p");
    p.textContent = "You drew a ";
    var span = document.createElement("span");
    span.className = "script";
    span.textContent = effect;
    p.appendChild(span);
    p.appendChild(document.createTextNode(" modifier but have no cards to target."));
    wrapper.appendChild(p);

    var btn = document.createElement("button");
    btn.className = "button-teal";
    btn.textContent = "spin again";
    btn.addEventListener("click", function () {
      htmx.ajax("POST", "/" + gameId + "/action/spin", { swap: "none" });
      dialog.close();
    });
    wrapper.appendChild(btn);

    dialog.replaceChildren(wrapper);
    dialog.addEventListener("close", function () {
      var content = document.getElementById("modifier-content");
      if (!content) {
        dialog.innerHTML = '<div id="modifier-content" class="dialog-body"' +
          ' hx-get="/' + gameId + '/data/modifier"' +
          ' hx-trigger="loadModifier from:body"' +
          ' hx-swap="innerHTML"></div>';
        htmx.process(dialog);
      } else {
        content.innerHTML = "";
      }
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
