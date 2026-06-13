(function () {
  // Submit the bug and rule-suggestion forms to the database instead of
  // navigating away. The footer (and these forms) render on every page,
  // including the index and join screens where htmx isn't loaded, so this
  // uses a plain fetch and swaps in a thank-you message on success.

  // prefill the bug report's game URL with the current page
  document.querySelectorAll("[data-feedback-url]").forEach(function (input) {
    if (!input.value) input.value = location.href;
  });

  document.querySelectorAll("[data-feedback-form]").forEach(function (form) {
    form.addEventListener("submit", function (e) {
      e.preventDefault();
      var endpoint = form.dataset.feedbackEndpoint;
      var submit = form.querySelector("[type=submit]");
      if (submit) submit.disabled = true;

      fetch(endpoint, {
        method: "POST",
        body: new URLSearchParams(new FormData(form)),
      })
        .then(function (res) {
          if (!res.ok) throw new Error("status " + res.status);
          form.hidden = true;
          var thanks = form.parentElement.querySelector("[data-feedback-thanks]");
          if (thanks) thanks.hidden = false;
        })
        .catch(function (err) {
          console.error("feedback submit failed:", err);
          if (submit) submit.disabled = false;
          alert("Sorry, something went wrong. Please try again.");
        });
    });
  });
})();
