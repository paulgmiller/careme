// Keep loading failures actionable without replacing the last displayed post.
for (const name of ["htmx:responseError", "htmx:sendError", "htmx:timeout"]) {
  document.addEventListener(name, event => {
    if (!event.detail.elt.closest("#careme-feed")) return;
    const feed = document.getElementById("careme-feed");
    let status = feed.querySelector('[role="status"]');
    if (!status) {
      status = document.createElement("p");
      status.setAttribute("role", "status");
      status.className = "mt-4 text-sm text-gray-600";
      feed.append(status);
    }
    status.textContent = "Posts couldn’t load. Try refreshing the page, or follow Careme below.";
  });
}

// Stop playback before HTMX replaces the current card.
document.addEventListener("htmx:beforeSwap", event => {
  if (event.detail.target.id !== "careme-feed") return;
  for (const video of event.detail.target.querySelectorAll("video")) video.pause();
});
