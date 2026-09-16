// Generation replaces shopping-content as recipes arrive. Preserve only each
// recipe's Details state so server-rendered content and controls can still change.
const detailsByRequest = new WeakMap();

document.addEventListener("htmx:beforeSwap", (event) => {
  if (event.detail.target.id !== "shopping-content" || !event.detail.shouldSwap) return;

  const states = new Map();
  for (const card of event.detail.target.querySelectorAll(".shopping-recipe-card")) {
    const details = card.querySelector("details");
    if (details) states.set(card.id, details.open);
  }
  detailsByRequest.set(event.detail.xhr, states);
});

document.addEventListener("htmx:afterSwap", (event) => {
  const states = detailsByRequest.get(event.detail.xhr);
  if (!states) return;
  detailsByRequest.delete(event.detail.xhr);

  for (const card of document.querySelectorAll("#shopping-content .shopping-recipe-card")) {
    const details = card.querySelector("details");
    if (details && states.has(card.id)) details.open = states.get(card.id);
  }
});
