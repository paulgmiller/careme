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
  const status = root.querySelector("[data-recipe-step-status]");
  const message = root.querySelector("[data-recipe-step-message]");
  const undoButton = root.querySelector("[data-recipe-step-undo]");
  const resetButton = root.querySelector("[data-recipe-step-reset]");
  if (!steps.length || !status || !message || !undoButton || !resetButton) return;

  const completedSteps = [];
  const swipeIntentDistance = 10;

  root.querySelector("[data-recipe-steps-hint]").hidden = false;

  function restoreStepStyles(step) {
    step.removeAttribute("data-recipe-step-dragging");
    step.style.removeProperty("opacity");
    step.style.removeProperty("transform");
  }

  function updateStatus() {
    const count = completedSteps.length;
    const allDone = count === steps.length;
    status.classList.toggle("hidden", count === 0);
    status.classList.toggle("flex", count > 0);
    message.textContent = count === 0
      ? ""
      : allDone
        ? "All done, chef!"
        : `${count} ${count === 1 ? "step" : "steps"} done.`;
    resetButton.classList.toggle("hidden", !allDone);
  }

  function restoreStep(step) {
    step.hidden = false;
    step.removeAttribute("data-recipe-step-completed");
    restoreStepStyles(step);
  }

  function completeStep(step) {
    if (step.hasAttribute("data-recipe-step-completed")) return;
    step.setAttribute("data-recipe-step-completed", "true");
    restoreStepStyles(step);
    step.hidden = true;
    completedSteps.push(step);
    updateStatus();
  }

  undoButton.addEventListener("click", () => {
    const step = completedSteps.pop();
    if (!step) return;
    restoreStep(step);
    updateStatus();
  });
  resetButton.addEventListener("click", () => {
    completedSteps.splice(0).forEach(restoreStep);
    updateStatus();
  });

  steps.forEach((step) => {
    let pointerID = null;
    let startX = 0;
    let startY = 0;
    let offsetX = 0;
    let isHorizontal = false;

    function resetSwipe() {
      pointerID = null;
      offsetX = 0;
      isHorizontal = false;
      restoreStepStyles(step);
    }

    step.addEventListener("pointerdown", (event) => {
      if (event.pointerType === "mouse") event.preventDefault();
      pointerID = event.pointerId;
      startX = event.clientX;
      startY = event.clientY;
      offsetX = 0;
      isHorizontal = false;
      step.setPointerCapture(event.pointerId);
    });

    step.addEventListener("pointermove", (event) => {
      if (event.pointerId !== pointerID) return;
      const deltaX = event.clientX - startX;
      const deltaY = event.clientY - startY;

      if (!isHorizontal) {
        if (Math.abs(deltaX) < swipeIntentDistance && Math.abs(deltaY) < swipeIntentDistance) return;
        if (Math.abs(deltaY) >= Math.abs(deltaX)) {
          resetSwipe();
          return;
        }
        isHorizontal = true;
        step.setAttribute("data-recipe-step-dragging", "true");
      }

      offsetX = deltaX;
      const progress = Math.min(Math.abs(offsetX) / step.offsetWidth, 1);
      step.style.transform = `translateX(${offsetX}px)`;
      step.style.opacity = String(1 - progress * 0.65);
    });

    step.addEventListener("pointerup", (event) => {
      if (event.pointerId !== pointerID) return;
      const threshold = Math.min(120, step.offsetWidth * 0.3);
      const shouldComplete = isHorizontal && Math.abs(offsetX) >= threshold;
      resetSwipe();
      if (shouldComplete) completeStep(step);
    });

    step.addEventListener("pointercancel", resetSwipe);
  });
}

document.addEventListener("DOMContentLoaded", () => {
  initializeStarRating();
  initializeRecipeSteps();
});
