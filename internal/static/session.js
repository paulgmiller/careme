(() => {
  const serverSignedIn = document.currentScript.dataset.serverSignedIn === "true";
  const pending = new WeakSet();
  const ready = (async () => {
    await window.Clerk.load();
    // Reload once when Clerk rehydrates a session after signed-out SSR.
    const key = `clerk-ssr-sync-reloaded:${window.location.pathname}${window.location.search}`;
    try {
      if (!serverSignedIn && window.Clerk.isSignedIn && !window.sessionStorage.getItem(key)) {
        window.sessionStorage.setItem(key, "1");
        window.location.reload();
      }
    } catch { /* Storage is optional; never risk a reload loop without the guard. */ }
  })();

  ready.catch(() => {}); // A request reports initialization failures through HTMX.

  let refreshing;
  function refreshSession() {
    if (!refreshing) {
      refreshing = ready.then(() => window.Clerk.session?.getToken({ skipCache: true }))
        .finally(() => { refreshing = null; });
    }
    return refreshing;
  }

  // Android may suspend token refresh timers while the app is in the background.
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") {
      refreshSession().catch(() => {}); // The next request will surface any failure.
    }
  });
  window.addEventListener("pageshow", event => {
    if (event.persisted) refreshSession().catch(() => {});
  });

  // Delay requests before HTMX reads form values or disables inputs. getToken
  // uses Clerk's cache during normal use and refreshes an expired token on wake.
  // Concurrent requests share the refresh; submissions are never replayed.
  document.addEventListener("htmx:confirm", async event => {
    event.preventDefault();
    const { elt, issueRequest } = event.detail;
    if (pending.has(elt)) return;
    pending.add(elt);
    try {
      await ready;
      await (refreshing || window.Clerk.session?.getToken());
      if (elt.isConnected) issueRequest();
    } catch (error) {
      elt.dispatchEvent(new CustomEvent("htmx:sendError", { bubbles: true, detail: { error } }));
    } finally {
      pending.delete(elt);
    }
  });
})();
