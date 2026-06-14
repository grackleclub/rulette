(function () {
  // the accuser's chooser after their accusation is upheld: give one of their
  // own rule cards to the accused, or skip. driven by a poll so it reaches the
  // accuser's client (a different device from the host who decided), modeled on
  // the infraction poll in accuse.js.
  var lastResolvedId = null; // infraction we've already acted on; don't reopen
  var shownId = null; // infraction currently shown in the dialog
  var inFlight = false;

  function getDialog() {
    return document.getElementById("transfer-dialog");
  }

  function post(url) {
    if (inFlight) return;
    inFlight = true;
    var dialog = getDialog();
    fetch(url, { method: "POST" }).then(function (res) {
      inFlight = false;
      if (res.ok) {
        lastResolvedId = shownId;
        shownId = null;
        if (dialog) dialog.close();
        document.body.dispatchEvent(new Event("refreshTable"));
        return;
      }
      if (dialog) dialog.close();
      shownId = null;
      document.body.dispatchEvent(new Event("refreshTable"));
      res.text().then(function (msg) {
        alert(msg.trim() || "Could not complete that action.");
      });
    }).catch(function () {
      inFlight = false;
      alert("Network error. Please try again.");
    });
  }

  // open the chooser when the poll finds a transfer owed to me. 200 = my turn
  // to give a card; 204 = nothing (close any stale dialog).
  window.handleTransfer = function (e) {
    var dialog = getDialog();
    if (!dialog) return;
    if (e.detail.xhr.status !== 200) {
      if (dialog.open && shownId !== null) {
        dialog.close();
        shownId = null;
      }
      return;
    }
    var data;
    try {
      data = JSON.parse(e.detail.xhr.responseText);
    } catch (err) {
      return;
    }
    if (String(data.infraction_id) === String(lastResolvedId)) return;
    if (String(data.infraction_id) === String(shownId)) return; // already up

    shownId = data.infraction_id;
    var gameId = dialog.dataset.gameId;
    var prompt = document.getElementById("transfer-prompt");
    if (prompt) prompt.textContent = "Give one of your rules to " + data.accused + ", or keep them all.";

    var list = document.getElementById("transfer-cards");
    if (list) {
      list.replaceChildren();
      (data.cards || []).forEach(function (c) {
        var btn = document.createElement("button");
        btn.className = "index-card index-card-accuse";
        btn.textContent = c.content;
        btn.addEventListener("click", function () {
          post("/" + gameId + "/action/accusation-transfer?game_card_id=" + c.id);
        });
        list.appendChild(btn);
      });
    }
    var skip = document.getElementById("transfer-skip-btn");
    if (skip) {
      skip.onclick = function () {
        post("/" + gameId + "/action/accusation-transfer?skip=1");
      };
    }
    if (!dialog.open) dialog.showModal();
  };
})();
