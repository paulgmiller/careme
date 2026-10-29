let pendingTransfer = null;
let waitingTimer = null;

// Delegation also handles shopping controls replaced by HTMX.
document.addEventListener("submit", (event) => {
  const form = event.target.closest("[data-cart-transfer]");
  if (!form || event.defaultPrevented) return;
  if (pendingTransfer) {
    event.preventDefault();
    return;
  }
  pendingTransfer = form;
  document.querySelectorAll("[data-cart-transfer] button[type=submit]").forEach((button) => {
    button.disabled = true;
  });
  waitingTimer = window.setTimeout(() => {
    form.querySelector("[data-cart-transfer-status]").hidden = false;
  }, 1000);
});

window.addEventListener("pageshow", () => {
  window.clearTimeout(waitingTimer);
  pendingTransfer = null;
  document.querySelectorAll("[data-cart-transfer] button[type=submit]").forEach((button) => {
    button.disabled = false;
  });
  document.querySelectorAll("[data-cart-transfer-status]").forEach((status) => {
    status.hidden = true;
  });
});
