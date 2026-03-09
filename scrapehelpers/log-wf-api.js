const { chromium } = require("playwright");

(async () => {
  const browser = await chromium.launch({ headless: false });
  const context = await browser.newContext();
  const page = await context.newPage();

  page.on("request", req => {
    const url = req.url();
    const rt = req.resourceType();

    const interesting =
      url.includes("wholefoodsmarket.com") ||
      url.includes("wholefoods.com") ||
      url.includes("/_next/") ||
      rt === "xhr" ||
      rt === "fetch" ||
      req.isNavigationRequest();

    if (!interesting) return;

    console.log("\n=== REQUEST ===");
    console.log("type:", rt);
    console.log("method:", req.method());
    console.log("url:", url);

    const body = req.postData();
    if (body) {
      console.log("body:", body.slice(0, 2000));
    }
  });

  page.on("response", async res => {
    const url = res.url();
    const ct = res.headers()["content-type"] || "";

    const interesting =
      url.includes("wholefoodsmarket.com") ||
      url.includes("wholefoods.com") ||
      url.includes("/_next/") ||
      ct.includes("json") ||
      ct.includes("html");

    if (!interesting) return;

    console.log("\n=== RESPONSE ===");
    console.log("status:", res.status());
    console.log("url:", url);
    console.log("content-type:", ct);
  });

  await page.goto("https://www.wholefoodsmarket.com/grocery/search?k=syrah", {
    waitUntil: "domcontentloaded"
  });

  console.log("Interact manually now.");
})();