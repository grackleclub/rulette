(function () {
  function getDialog() {
    return document.getElementById("modifier-dialog");
  }
  function getData() {
    return document.getElementById("modifier-data");
  }

  // a modifier action the player committed to but that the server deferred
  // because a challenge is in progress. it completes automatically once the
  // challenge resolves and the game returns to the pending state.
  var pending = null; // { url, effect }
  var inFlight = false;

  // show that a committed action is waiting on a challenge to clear. disables
  // the choices so the player can't double-submit while we retry for them.
  function showWaiting(effect) {
    var data = getData();
    if (!data) return;
    data.querySelectorAll(
      ".modifier-card-btn, input[name='target_player_id']"
    ).forEach(function (el) {
      el.disabled = true;
    });
    var status = document.getElementById("modifier-status");
    if (!status) {
      status = document.createElement("p");
      status.id = "modifier-status";
      data.appendChild(status);
    }
    status.textContent =
      "A challenge is in progress — your " + (effect || "modifier") +
      " will complete automatically once it's resolved.";
  }

  // post a modifier action. on success the dialog closes; while a challenge
  // blocks it (423) the action is queued and retried on the next table poll;
  // any other failure is surfaced and the dialog is dismissed.
  function attempt(url, effect) {
    if (inFlight) return;
    inFlight = true;
    fetch(url, { method: "POST" }).then(function (res) {
      inFlight = false;
      if (res.ok) {
        pending = null;
        var d = getDialog();
        if (d) d.close();
        document.body.dispatchEvent(new Event("refreshTable"));
        return;
      }
      if (res.status === 423) {
        pending = { url: url, effect: effect };
        showWaiting(effect);
        return;
      }
      pending = null;
      var dialog = getDialog();
      if (dialog) dialog.close();
      document.body.dispatchEvent(new Event("refreshTable"));
      res.text().then(function (msg) {
        alert(msg.trim() || "Could not complete that action.");
      });
    }).catch(function () {
      inFlight = false;
      // keep a queued action queued; a transient blip shouldn't drop it.
      if (!pending) alert("Network error. Please try again.");
    });
  }

  document.body.addEventListener("htmx:afterSettle", function (e) {
    if (!e.detail || !e.detail.elt) return;
    // every table poll is a chance to complete a queued modifier action once
    // the blocking challenge has resolved.
    if (e.detail.elt.id === "table" && pending && !inFlight) {
      attempt(pending.url, pending.effect);
      return;
    }
    // after modifier content is fetched, check if we should open the dialog
    if (e.detail.elt.id !== "modifier-content") return;
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
  // htmx dispatches a server trigger under both its given name and the
  // kebab-cased form, so listen to just one or this fires twice.
  document.body.addEventListener("modifierShredded", showShredNotice);

  // handle card button clicks
  document.body.addEventListener("click", function (e) {
    var btn = e.target.closest(".modifier-card-btn");
    if (!btn) return;
    e.preventDefault();

    var data = getData();
    var action = btn.dataset.action;
    var cardId = btn.dataset.gameCardId;
    var url = action + "?game_card_id=" + cardId;
    var effect = data ? data.dataset.effect : "";

    // for clone/transfer, include target player
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

    attempt(url, effect);
  });
})();
