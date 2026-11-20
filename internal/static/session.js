(() => {
  // This is a bridge between vanilla ClerkJS and Careme's server-rendered/HTMX
  // pages. Clerk still owns session initialization, token caching, renewal, and
  // its __session cookie. We do not inspect JWTs, change token lifetimes, store
  // tokens, or implement a second authentication/refresh mechanism here.
  //
  // The extra coordination is needed because Android can suspend Clerk's
  // background timers. On return, a shopping-list focus/visibility refresh or a
  // form submission can otherwise reach Go before Clerk has renewed the cookie.
  // We use Clerk's public APIs to finish that renewal before sending the request.
  // https://clerk.com/docs/js-frontend/reference/objects/session#gettoken

  // Both scripts in clerk_refresh.html use defer, in order: the Clerk global
  // exists before this script executes, and these listeners are installed before
  // DOMContentLoaded starts HTMX's initial requests.
  const serverSignedIn = document.currentScript.dataset.serverSignedIn === "true";
  const pending = new WeakSet();
  const ready = (async () => {
    await window.Clerk.load();
    // Go rendered the page before ClerkJS loaded. An expired/missing cookie can
    // make that render signed-out even when Clerk can restore an active session.
    // Reload so Go can render with the renewed cookie. This is the existing SSR
    // synchronization behavior, separate from the resume/request coordination.
    // The per-tab, per-URL guard prevents an endless reload if Go and Clerk keep
    // disagreeing. Storage failure disables this reload, not token renewal.
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
    // skipCache asks Clerk to mint a token on resume even if its in-memory token
    // has not expired yet. Share this promise across overlapping wake events and
    // HTMX requests, then discard it on either success or failure so a later
    // attempt can recover. A genuinely ended session is not revived by this API.
    if (!refreshing) {
      refreshing = ready.then(() => window.Clerk.session?.getToken({ skipCache: true }))
        .finally(() => { refreshing = null; });
    }
    return refreshing;
  }

  // Renewal on wake is proactive. If offline, leave the displayed page usable;
  // the next HTMX request tries Clerk again and reports its own failure below.
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") {
      refreshSession().catch(() => {}); // The next request will surface any failure.
    }
  });
  window.addEventListener("pageshow", event => {
    // A page restored from the back/forward cache also retains old JS state.
    if (event.persisted) refreshSession().catch(() => {});
  });

  // htmx:confirm is HTMX's supported asynchronous request gate; it fires even
  // without hx-confirm. Unlike beforeRequest, it lets us await Clerk before
  // HTMX reads form values, disables inputs, or sends any request to Go.
  // https://htmx.org/events/#htmx:confirm
  document.addEventListener("htmx:confirm", async event => {
    event.preventDefault();
    const { elt, issueRequest } = event.detail;
    // Two clicks while waiting for Clerk must not turn into two submissions.
    if (pending.has(elt)) return;
    pending.add(elt);
    try {
      await ready;
      // Outside a wake refresh, use ordinary getToken() and let Clerk decide
      // whether its cached token is still usable. Signed-out users have no
      // session; their requests proceed to the handlers' normal authorization.
      await (refreshing || window.Clerk.session?.getToken());
      // A different response may have removed this form while we were waiting.
      // Calling without true preserves any normal hx-confirm confirmation.
      if (elt.isConnected) issueRequest();
    } catch (error) {
      // No application request was sent, so do not replay a submission. Reuse
      // HTMX's send-error UI (including the question form's error message), keep
      // the input editable, and let the user try again. If Clerk succeeds but
      // Go returns 401, the question handler also leaves the form on screen with
      // a sign-in link; we do not automatically navigate away and lose the text.
      elt.dispatchEvent(new CustomEvent("htmx:sendError", { bubbles: true, detail: { error } }));
    } finally {
      pending.delete(elt);
    }
  });
})();
