function showCopied(button) {
  const status = button.querySelector("[data-share-status]");
  const label = button.dataset.shareLabel || "Share";

  button.title = "Link copied";
  button.setAttribute("aria-label", "Link copied");
  if (status) status.classList.remove("hidden");

  window.setTimeout(() => {
    button.title = label;
    button.setAttribute("aria-label", label);
    if (status) status.classList.add("hidden");
  }, 1600);
}

// Use one delegated handler because the shopping-list controls can be replaced
// by HTMX after a recipe is saved or removed.
document.addEventListener("click", async (event) => {
  const button = event.target.closest("[data-share-button]");
  if (!button) return;

  const shareURL = new URL(button.dataset.shareUrl || window.location.href, window.location.origin).toString();
  const shareTitle = button.dataset.shareTitle || document.title;

  // Mobile browsers and installed PWAs usually support the standard Web Share
  // API, which opens the native OS share sheet.
  if (navigator.share) {
    try {
      await navigator.share({ title: shareTitle, url: shareURL });
      return;
    } catch (error) {
      if (error && error.name === "AbortError") return;
    }
  }

  // Desktop browsers often do not expose a native share sheet, so copy the
  // stable URL and show visible feedback in the button.
  try {
    await navigator.clipboard.writeText(shareURL);
    showCopied(button);
  } catch {
    // Last-resort fallback for older browsers or blocked clipboard access.
    window.location.href = "mailto:?subject=" + encodeURIComponent(shareTitle) + "&body=" + encodeURIComponent(shareURL);
  }
});
