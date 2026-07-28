// Progressive enhancement for the grid.
//
// Every cell is already a working <form> that posts and redirects. This file
// only intercepts those submissions to avoid a full page reload, and updates
// the accumulators from what the server sends back. If it fails to load, or
// throws, the grid keeps working exactly as it does with JavaScript disabled.
//
// The totals are never computed here. They come from the same SQL that the
// PDFs use, so the numbers on screen cannot drift from the printed ones.

(function () {
  "use strict";

  // Marking the body is what hides the per-cell save buttons. Doing it from
  // JS rather than in the stylesheet means a failure to load leaves them
  // visible and the no-JS path intact.
  document.body.classList.add("js");

  var table = document.querySelector("table.grid");

  function post(form) {
    // URLSearchParams, not FormData: this sends
    // application/x-www-form-urlencoded, which is byte-for-byte what the
    // plain form submit sends. One encoding on the wire means the enhanced
    // path cannot diverge from the path that must work.
    return fetch(form.action, {
      method: "POST",
      body: new URLSearchParams(new FormData(form)),
      headers: { "X-Requested-With": "XMLHttpRequest", Accept: "application/json" },
      credentials: "same-origin"
    }).then(function (res) {
      return res.json().catch(function () {
        return { ok: false, error: res.statusText };
      });
    });
  }

  function setText(selector, value) {
    if (value === undefined || value === null || value === "") return;
    var el = document.querySelector(selector);
    if (el) el.textContent = value;
  }

  function flash(cell, ok) {
    cell.classList.remove("cell-saved", "cell-error");
    // Force a reflow so the animation restarts on a repeated save.
    void cell.offsetWidth;
    cell.classList.add(ok ? "cell-saved" : "cell-error");
  }

  function attr(v) {
    return String(v).replace(/["\\]/g, "\\$&");
  }

  function submitCell(input) {
    var form = input.form;
    if (!form) return;

    // Nothing changed since the last successful save: skip the round trip.
    if (input.dataset.last === input.value) return;

    var cell = input.closest("td");
    post(form).then(function (res) {
      if (!res.ok) {
        flash(cell, false);
        if (res.error) input.title = res.error;
        return;
      }
      input.title = "";
      if (typeof res.value === "string") input.value = res.value;
      input.dataset.last = input.value;
      flash(cell, true);

      var emp = input.dataset.employee;
      var date = input.dataset.date;
      if (emp) setText('[data-employee-total="' + attr(emp) + '"]', res.employeeTotal);
      if (date) setText('[data-day-total="' + attr(date) + '"]', res.dayTotal);
      setText("[data-grand-total]", res.grandTotal);
    }).catch(function () {
      // Network failure: fall back to a real submit so the edit is not lost
      // silently. The user sees a page load, which is the honest outcome.
      form.submit();
    });
  }

  if (table) {
    table.addEventListener("submit", function (e) {
      e.preventDefault();
      var input = e.target.querySelector("input.hours, input.comment");
      if (input) submitCell(input);
    });

    table.addEventListener(
      "blur",
      function (e) {
        if (e.target.matches("input.hours, input.comment")) submitCell(e.target);
      },
      true
    );

    table.addEventListener("keydown", function (e) {
      if (!e.target.matches("input.hours, input.comment")) return;

      if (e.key === "Enter") {
        e.preventDefault();
        submitCell(e.target);
        moveFocus(e.target, 1);
        return;
      }
      if (e.key === "Escape") {
        e.target.value = e.target.dataset.last || "";
        e.target.blur();
        return;
      }
      if (e.key === "ArrowDown") { e.preventDefault(); moveFocus(e.target, 1); }
      if (e.key === "ArrowUp") { e.preventDefault(); moveFocus(e.target, -1); }
    });

    // Remember the loaded value so an untouched cell never posts.
    table.querySelectorAll("input.hours, input.comment").forEach(function (i) {
      i.dataset.last = i.value;
    });
  }

  // Move up or down the same column, which is how hours actually get entered:
  // one employee at a time, down the month.
  function moveFocus(input, delta) {
    var cell = input.closest("td");
    var row = cell.parentElement;
    var col = Array.prototype.indexOf.call(row.children, cell);
    var rows = Array.prototype.slice.call(row.parentElement.children);
    var i = rows.indexOf(row) + delta;

    while (i >= 0 && i < rows.length) {
      var target = rows[i].children[col];
      var next = target && target.querySelector("input.hours, input.comment");
      if (next) {
        next.focus();
        next.select();
        return;
      }
      i += delta; // skip locked cells
    }
  }

  // The language selector submits on change; its button is hidden by the
  // stylesheet only once this file has run, so the no-JS path keeps one.
  document.querySelectorAll("select[data-autosubmit]").forEach(function (sel) {
    sel.addEventListener("change", function () {
      if (sel.form) sel.form.submit();
    });
  });

  // Confirm destructive submits that opted in.
  document.querySelectorAll("form[data-confirm]").forEach(function (form) {
    form.addEventListener("submit", function (e) {
      if (!window.confirm(form.dataset.confirm)) e.preventDefault();
    });
  });
})();
