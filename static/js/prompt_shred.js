(function () {
  // the spinner's bonus chooser after a succeeded prompt: shred one of their
  // own rule cards, or skip. modeled on the modifier chooser so a refresh
  // re-surfaces it and the turn can't get stuck.
  function getDialog() {
    return document.getElementById("prompt-shred-dialog");
  }

  var inFlight = false;

  // post a shred-or-skip choice. on success the dialog closes and the table
  // refreshes; any failure is surfaced and the dialog dismissed.
  function attempt(url) {
    if (inFlight) return;
    inFlight = true;
    fetch(url, { method: "POST" }).then(function (res) {
      inFlight = false;
      var d = getDialog();
      if (res.ok) {
        if (d) d.close();
        document.body.dispatchEvent(new Event("refreshTable"));
        return;
      }
      if (d) d.close();
      document.body.dispatchEvent(new Event("refreshTable"));
      res.text().then(function (msg) {
        alert(msg.trim() || "Could not complete that action.");
      });
    }).catch(function () {
      inFlight = false;
      alert("Network error. Please try again.");
    });
  }

  document.body.addEventListener("htmx:afterSettle", function (e) {
    if (!e.detail || !e.detail.elt) return;
    // every table poll is a chance to (re)open the chooser for the turn
    // player while the game waits in the prompt-shred state.
    if (e.detail.elt.id === "table") {
      var bar = document.querySelector(".table-bar");
      var dlg = getDialog();
      if (bar && bar.dataset.promptShredPending === "true" && dlg && !dlg.open) {
        document.body.dispatchEvent(new Event("loadPromptShred"));
      }
      return;
    }
    // open once the fragment lands and actually rendered the chooser (it's
    // empty for anyone who isn't the spinner).
    if (e.detail.elt.id !== "prompt-shred-content") return;
    var dialog = getDialog();
    var data = document.getElementById("prompt-shred-data");
    if (!dialog || !data) return;
    // the chooser carries the success message, so retire the generic prompt
    // outcome popup if it's still up — one modal, not two stacked.
    var outcome = document.getElementById("newprompt-dialog");
    if (outcome && outcome.open) outcome.close();
    if (!dialog.open) dialog.showModal();
  });

  document.body.addEventListener("click", function (e) {
    var card = e.target.closest(".prompt-shred-card-btn");
    if (card) {
      e.preventDefault();
      attempt(card.dataset.action + "?game_card_id=" + card.dataset.gameCardId);
      return;
    }
    var skip = e.target.closest(".prompt-shred-skip-btn");
    if (skip) {
      e.preventDefault();
      attempt(skip.dataset.action + "?skip=1");
    }
  });
})();
