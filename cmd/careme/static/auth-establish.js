(() => {
  const waitForClerk = async () => {
    while (!window.Clerk?.load) {
      await new Promise((resolve) => window.setTimeout(resolve, 10));
    }
  };

  const establishSession = async () => {
    await waitForClerk();
    await window.Clerk.load();

    // Loading Clerk on this origin is often enough to finalize cookies in dev.
    const url = new URL(window.location.href);
    url.searchParams.delete("__clerk_db_jwt");
    window.history.replaceState({}, "", url.toString());
    window.location.replace("/");
  };

  void establishSession();
})();
