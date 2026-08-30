function renderStars(selectedStars) {
  const selected = Number(selectedStars) || 0;
  const buttons = document.querySelectorAll("#star-rating [data-star]");
  buttons.forEach((button) => {
    const value = Number(button.dataset.star || 0);
    const filled = value <= selected;
    button.textContent = filled ? "★" : "☆";
    button.classList.toggle("text-amber-500", filled);
    button.classList.toggle("text-gray-300", !filled);
  });
}

function initializeStarRating() {
  const rating = document.getElementById("star-rating");
  const input = document.getElementById("stars-input");
  if (!rating || !input) return;

  const initial = Number(rating.dataset.initialStars || input.value || 0);
  if (initial > 0) input.value = String(initial);
  renderStars(initial);

  rating.querySelectorAll("[data-star]").forEach((button) => {
    button.addEventListener("click", () => {
      const stars = Number(button.dataset.star || 0);
      input.value = stars > 0 ? String(stars) : "";
      renderStars(stars);
    });
  });
}

function initializeRecipeSteps() {
  const root = document.querySelector("[data-recipe-steps]");
  if (!root) return;

  const steps = Array.from(root.querySelectorAll("[data-recipe-step]"));
  const undoButton = root.querySelector("[data-recipe-step-undo]");
  if (!steps.length || !undoButton) return;

  const completedSteps = [];

  root.querySelector("[data-recipe-steps-hint]").hidden = false;

  function restoreStep(step) {
    step.hidden = false;
    step.removeAttribute("data-recipe-step-completed");
  }

  function completeStep(step) {
    if (step.hasAttribute("data-recipe-step-completed")) return;
    step.setAttribute("data-recipe-step-completed", "true");
    step.hidden = true;
    completedSteps.push(step);
    undoButton.classList.remove("hidden");
  }

  undoButton.addEventListener("click", () => {
    const step = completedSteps.pop();
    if (!step) return;
    restoreStep(step);
    undoButton.classList.toggle("hidden", completedSteps.length === 0);
  });
  steps.forEach((step) => {
    const doneButton = step.querySelector("[data-recipe-step-done]");
    if (doneButton) doneButton.addEventListener("click", () => completeStep(step));
  });
}

document.addEventListener("DOMContentLoaded", () => {
  initializeStarRating();
  initializeRecipeSteps();
});
