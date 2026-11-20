const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { test } = require("node:test");
const vm = require("node:vm");

const script = readFileSync(`${__dirname}/session.js`, "utf8");
const tick = () => new Promise(resolve => setImmediate(resolve));
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
function page(getToken, load = async () => {}, signedIn = true, options = {}) {
  const document = new EventTarget();
  document.currentScript = { dataset: { serverSignedIn: String(options.serverSignedIn ?? true) } };
  document.visibilityState = "visible";
  const window = new EventTarget();
  let reloads = 0;
  window.location = { pathname: "/recipe/hash", search: "", reload: () => { reloads++; } };
  window.sessionStorage = options.storage;
  window.Clerk = { load, isSignedIn: signedIn, session: signedIn ? { getToken } : null };
  vm.runInNewContext(script, { document, window, setTimeout, CustomEvent });
  return {
    reloads: () => reloads,
    wake() { document.dispatchEvent(new Event("visibilitychange")); },
    restore() {
      const event = new Event("pageshow");
      event.persisted = true;
      window.dispatchEvent(event);
    },
    request(elt = { isConnected: true, dispatchEvent() {} }) {
      let issued = 0;
      const event = new CustomEvent("htmx:confirm", {
        cancelable: true,
        detail: { elt, issueRequest: () => { issued++; } },
      });
      document.dispatchEvent(event);
      assert.equal(event.defaultPrevented, true);
      return () => issued;
    },
  };
}

test("wake refresh finishes before concurrent reads and submissions are sent", async () => {
  const token = deferred();
  const options = [];
  const app = page(option => { options.push(option); return token.promise; });
  await tick();
  app.wake();
  const read = app.request();
  const submit = app.request();
  await tick();
  assert.equal(read(), 0);
  assert.equal(submit(), 0);
  assert.equal(options.length, 1);
  assert.equal(options[0].skipCache, true);
  token.resolve("fresh-token");
  await tick();
  assert.equal(read(), 1);
  assert.equal(submit(), 1);
});

test("first request waits for SDK initialization and token refresh", async () => {
  const loaded = deferred();
  const token = deferred();
  let calls = 0;
  const app = page(() => { calls++; return token.promise; }, () => loaded.promise);
  const issued = app.request();
  await tick();
  assert.equal(calls, 0);
  loaded.resolve();
  await tick();
  assert.equal(issued(), 0);
  token.resolve("fresh-token");
  await tick();
  assert.equal(issued(), 1);
});

test("failed refresh keeps the form usable and the next attempt can succeed", async () => {
  let fail = true;
  const errors = [];
  const app = page(async () => {
    if (fail) throw new Error("offline");
    return "fresh-token";
  });
  const form = { isConnected: true, value: "Can I use tofu?", dispatchEvent: e => errors.push(e.type) };
  const issued = app.request(form);
  await tick();
  assert.equal(issued(), 0);
  assert.equal(form.value, "Can I use tofu?");
  assert.deepEqual(errors, ["htmx:sendError"]);
  fail = false;
  const retry = app.request(form);
  await tick();
  assert.equal(retry(), 1);
});

test("duplicate triggers for a waiting form do not submit twice", async () => {
  const token = deferred();
  const app = page(() => token.promise);
  const form = { isConnected: true };
  const first = app.request(form);
  const duplicate = app.request(form);
  token.resolve("fresh-token");
  await tick();
  assert.equal(first(), 1);
  assert.equal(duplicate(), 0);
});

test("signed-out requests proceed to normal server authorization", async () => {
  const app = page(() => assert.fail("no token refresh without a session"), undefined, false);
  const issued = app.request();
  await tick();
  assert.equal(issued(), 1);
});

test("removed elements are not submitted after token refresh", async () => {
  const token = deferred();
  const app = page(() => token.promise);
  const form = { isConnected: true };
  const issued = app.request(form);
  form.isConnected = false;
  token.resolve("fresh-token");
  await tick();
  assert.equal(issued(), 0);
});


test("restoring from browser history refreshes the session before requests", async () => {
  const token = deferred();
  let calls = 0;
  const app = page(() => { calls++; return token.promise; });
  await tick();
  app.restore();
  const issued = app.request();
  await tick();
  assert.equal(calls, 1);
  assert.equal(issued(), 0);
  token.resolve("fresh-token");
  await tick();
  assert.equal(issued(), 1);
});


test("rehydrated signed-out pages reload once without creating a loop", async () => {
  const values = new Map();
  const storage = {getItem: key => values.get(key), setItem: (key, value) => values.set(key, value)};
  const options = {serverSignedIn: false, storage};
  const first = page(async () => "fresh-token", undefined, true, options);
  await tick();
  assert.equal(first.reloads(), 1);
  const second = page(async () => "fresh-token", undefined, true, options);
  await tick();
  assert.equal(second.reloads(), 0);
});

test("unavailable storage does not prevent authenticated requests", async () => {
  const app = page(async () => "fresh-token", undefined, true, {serverSignedIn: false});
  const issued = app.request();
  await tick();
  assert.equal(issued(), 1);
  assert.equal(app.reloads(), 0);
});

test("SDK initialization failure surfaces an error without submitting", async () => {
  const app = page(() => assert.fail("SDK initialization failed"), async () => {throw new Error("SDK unavailable");});
  const errors = [];
  const issued = app.request({isConnected: true, dispatchEvent: e => errors.push(e.type)});
  await tick();
  assert.equal(issued(), 0);
  assert.deepEqual(errors, ["htmx:sendError"]);
});
