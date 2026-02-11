(() => {
  const script = document.currentScript;
  const serverSignedIn = script?.dataset.serverSignedIn === "true";

  const maybeReloadForSSRSessionSync = async () => {
    const key = "clerk-ssr-sync-reloaded:" + window.location.pathname + window.location.search;

    while (!window.Clerk?.load) {
      await new Promise((resolve) => window.setTimeout(resolve, 10));
    }
    await window.Clerk.load();

    const clerkSignedIn = !!window.Clerk.isSignedIn;
    if (!serverSignedIn && clerkSignedIn && !window.sessionStorage.getItem(key)) {
      window.sessionStorage.setItem(key, "1");
      window.location.reload();
    }
  };

  void maybeReloadForSSRSessionSync();
})();
