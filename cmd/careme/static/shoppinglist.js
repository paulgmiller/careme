(() => {
  const currentScript = document.currentScript;
  const shareHash = currentScript?.dataset.shareHash ?? "";

  const setRecipeDetailsVisible = (hash, isVisible) => {
    const panel = document.getElementById("details-" + hash);
    if (!panel) {
      return;
    }
    panel.classList.toggle("hidden", !isVisible);
    const button = document.getElementById("details-" + hash + "-button");
    if (button) {
      button.setAttribute("aria-expanded", isVisible ? "true" : "false");
      button.textContent = isVisible ? "Hide details" : "Show details";
    }
  };

  const updateFinalizeButton = () => {
    const form = document.getElementById("regenerateForm");
    const finalizeButton = document.getElementById("finalizeButton");
    if (!form || !finalizeButton) {
      return;
    }
    const savedInputs = form.querySelectorAll("input[name='saved']");
    const hasSavedRecipes = Array.from(savedInputs).some(
      (input) => input.value.trim() !== "",
    );
    finalizeButton.disabled = !hasSavedRecipes;
  };

  document.querySelectorAll("[data-recipe-choice]").forEach((input) => {
    input.addEventListener("change", () => {
      const hash = input.dataset.recipeHash;
      const choice = input.dataset.choice;
      if (!hash || !choice) {
        return;
      }
      const savedInput = document.getElementById("saved-" + hash);
      const dismissedInput = document.getElementById("dismissed-" + hash);
      if (!savedInput || !dismissedInput) {
        return;
      }
      if (choice === "save") {
        savedInput.value = hash;
        dismissedInput.value = "";
      } else {
        dismissedInput.value = hash;
        savedInput.value = "";
      }
      setRecipeDetailsVisible(hash, false);
      updateFinalizeButton();
    });
  });

  document.querySelectorAll("[data-toggle-details]").forEach((button) => {
    button.addEventListener("click", () => {
      const hash = button.dataset.recipeHash;
      if (!hash) {
        return;
      }
      const panel = document.getElementById("details-" + hash);
      if (!panel) {
        return;
      }
      const isOpen = panel.classList.contains("hidden");
      setRecipeDetailsVisible(hash, isOpen);
    });
  });

  const shoppingListToggle = document.getElementById("shoppingListToggle");
  const shoppingListPanel = document.getElementById("shoppingListPanel");
  if (shoppingListToggle && shoppingListPanel) {
    shoppingListToggle.addEventListener("click", () => {
      shoppingListPanel.classList.toggle("hidden");
      const isOpen = !shoppingListPanel.classList.contains("hidden");
      shoppingListToggle.textContent = isOpen ? "Hide" : "Show";
      shoppingListToggle.setAttribute("aria-expanded", isOpen ? "true" : "false");
    });
  }

  const shareButton = document.querySelector("[data-share-recipes]");
  if (shareButton) {
    shareButton.addEventListener("click", async () => {
      if (!shareHash) {
        return;
      }
      const shareUrl = window.location.origin + "/recipes?h=" + encodeURIComponent(shareHash);
      try {
        await navigator.clipboard.writeText(shareUrl);
        const feedback = document.getElementById("shareFeedback");
        if (feedback) {
          feedback.classList.remove("hidden");
          window.setTimeout(() => {
            feedback.classList.add("hidden");
          }, 2000);
        }
      } catch (error) {
        alert("Failed to copy link: " + error);
      }
    });
  }

  updateFinalizeButton();
})();
