const { test, expect } = require("@playwright/test");

test("home has no JS errors and theme CSS resolves", async ({ page }) => {
  const pageErrors = [];
  const consoleErrors = [];
  const requestFailures = [];

  page.on("pageerror", (err) => pageErrors.push(err.message));
  page.on("console", (msg) => {
    if (msg.type() === "error") {
      consoleErrors.push(msg.text());
    }
  });
  page.on("requestfailed", (req) => {
    const appURL = process.env.APP_URL || "";
    if (!appURL || req.url().startsWith(appURL)) {
      const failure = req.failure();
      requestFailures.push(`${req.method()} ${req.url()} :: ${failure ? failure.errorText : "unknown failure"}`);
    }
  });

  await page.goto("/", { waitUntil: "domcontentloaded" });

  const css = await page.evaluate(() => {
    const root = getComputedStyle(document.documentElement);
    const brand500 = root.getPropertyValue("--brand-500").trim();
    const cta =
      document.querySelector('form[action="/locations"] button[type="submit"]') ||
      document.querySelector('a[href="/sign-in"]');
    const ctaBg = cta ? getComputedStyle(cta).backgroundColor : "";
    return { brand500, ctaBg };
  });

  expect(css.brand500).not.toBe("");
  expect(css.ctaBg).not.toBe("rgba(0, 0, 0, 0)");
  expect(pageErrors).toEqual([]);
  expect(consoleErrors).toEqual([]);
  expect(requestFailures).toEqual([]);
});
