(() => {
  const updateFavoriteUI = (favoriteID) => {
    const forms = Array.from(document.querySelectorAll("[data-favorite-form]"));
    forms.forEach((form) => {
      const input = form.querySelector("input[name='favorite_store']");
      if (!input) {
        return;
      }
      const isFavorite = input.value === favoriteID;
      const icon = form.querySelector("[data-favorite-icon]");
      const button = form.querySelector("[data-favorite-button]");
      if (icon) {
        icon.classList.toggle("text-amber-500", isFavorite);
        icon.classList.toggle("text-brand-600", !isFavorite);
        icon.textContent = isFavorite ? "★" : "☆";
      }
      if (button) {
        button.setAttribute("aria-pressed", isFavorite ? "true" : "false");
      }
    });
  };

  document.querySelectorAll("[data-instructions-button]").forEach((button) => {
    button.addEventListener("click", () => {
      const container = button.closest("li");
      if (!container) {
        return;
      }
      const panel = container.querySelector("[data-instructions-panel]");
      if (!panel) {
        return;
      }
      panel.classList.toggle("hidden");
      if (!panel.classList.contains("hidden")) {
        const input = panel.querySelector("textarea[name='instructions']");
        if (input) {
          input.focus();
        }
      }
    });
  });

  document.querySelectorAll("[data-favorite-form]").forEach((form) => {
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      const favoriteStore = form.querySelector("input[name='favorite_store']")?.value;
      const button = form.querySelector("[data-favorite-button]");
      if (button) {
        button.disabled = true;
        button.setAttribute("aria-busy", "true");
      }
      try {
        const body = new URLSearchParams(new FormData(form));
        const response = await fetch(form.action, {
          method: "POST",
          headers: {
            "Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
          },
          body,
          credentials: "same-origin",
        });
        if (response.status === 401) {
          window.location.assign("/");
          return;
        }
        if (!response.ok) {
          throw new Error("favorite update failed");
        }
        let payload = null;
        try {
          payload = await response.json();
        } catch (error) {
          payload = null;
        }
        const updatedFavorite = payload?.favorite_store ?? favoriteStore ?? "";
        updateFavoriteUI(updatedFavorite);
      } catch (error) {
        console.error(error);
      } finally {
        if (button) {
          button.disabled = false;
          button.removeAttribute("aria-busy");
        }
      }
    });
  });

  document.querySelectorAll("[data-instructions-panel]").forEach((panel) => {
    const cancelButton = panel.querySelector("[data-instructions-cancel]");
    if (!cancelButton) {
      return;
    }
    cancelButton.addEventListener("click", () => {
      panel.classList.add("hidden");
    });
  });
})();
