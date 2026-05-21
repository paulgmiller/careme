(() => {
  const pricingTable = document.querySelector("[data-clerk-pricing-table]");
  const error = document.querySelector("[data-clerk-pricing-error]");
  if (!pricingTable) return;

  const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
  const loadClerkUI = async () => {
    if (window.__internal_ClerkUICtor) return window.__internal_ClerkUICtor;

    const src = pricingTable.dataset.clerkUiBundleUrl;
    if (!src) return null;

    await new Promise((resolve, reject) => {
      const existing = Array.from(document.scripts).find((script) => script.src === src);
      if (existing) {
        existing.addEventListener("load", resolve, { once: true });
        existing.addEventListener("error", reject, { once: true });
        return;
      }

      const script = document.createElement("script");
      script.src = src;
      script.async = true;
      script.crossOrigin = "anonymous";
      script.onload = resolve;
      script.onerror = () => reject(new Error("Failed to load Clerk UI"));
      document.head.appendChild(script);
    });
    return window.__internal_ClerkUICtor || null;
  };
  const waitForClerk = async () => {
    while (!window.Clerk?.load) await wait(10);
    const ClerkUI = await loadClerkUI();
    if (ClerkUI) {
      await window.Clerk.load({ ui: { ClerkUI } });
    } else {
      await window.Clerk.load();
    }
    return window.Clerk;
  };

  (async () => {
    if (error) error.hidden = true;
    try {
      const clerk = await waitForClerk();
      if (!clerk.isSignedIn) {
        return;
      }
      if (!clerk.mountPricingTable) {
        throw new Error("Clerk pricing table is unavailable");
      }
      clerk.mountPricingTable(pricingTable, {
        for: "user",
        newSubscriptionRedirectUrl: "/user",
      });
    } catch (pricingError) {
      console.error("Clerk pricing table failed", pricingError);
      if (error) error.hidden = false;
    }
  })();
})();
