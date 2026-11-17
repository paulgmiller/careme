import { readFile } from "node:fs/promises";
import assert from "node:assert/strict";
import test from "node:test";

const source = await readFile(new URL("./about-feed.js", import.meta.url), "utf8");
const { isCaremePost, loadCaremePosts, textParts, mountFeed, attachVideo } = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);
const actor = "did:plc:qv4hafijvatyb773lcm6c6ru";
const linkType = "app.bsky.richtext.facet#link";

function item(uri = "recipe", link = "https://careme.cooking/recipe/example") {
  return { post: {
    uri, author: { did: actor },
    record: { text: "Dinner", createdAt: "2026-11-01T12:00:00Z",
      facets: [{ index: { byteStart: 0, byteEnd: 6 }, features: [{ $type: linkType, uri: link }] }] },
  } };
}

test("includes original Careme links and excludes personal posts, replies, reposts, and lookalike domains", () => {
  assert.equal(isCaremePost(item()), true);
  assert.equal(isCaremePost(item("home", "https://Careme.cooking")), true);
  for (const url of ["https://example.com/careme.cooking", "https://careme.cooking.example.com", "javascript:alert(1)"]) {
    assert.equal(isCaremePost(item("personal", url)), false);
  }
  const reply = item(); reply.post.record.reply = {};
  assert.equal(isCaremePost(reply), false);
  const repost = item(); repost.reason = {};
  assert.equal(isCaremePost(repost), false);
  const other = item(); other.post.author.did = "someone-else";
  assert.equal(isCaremePost(other), false);
  const personal = item(); personal.post.record = { text: "Just thinking about Careme" };
  assert.equal(isCaremePost(personal), false);
  personal.post.record.embed = { external: { uri: "https://careme.cooking/recipe/example" } };
  assert.equal(isCaremePost(personal), true);
});

test("paginates past personal posts, deduplicates, and returns newest posts first", async () => {
  const urls = [];
  const newer = item("newer"); newer.post.record.createdAt = "2026-11-02T12:00:00Z";
  const pages = [
    { feed: [item("personal", "https://example.com")], cursor: "page2" },
    { feed: [item("older"), item("older"), newer] },
  ];
  const posts = await loadCaremePosts(async url => {
    urls.push(url);
    return { ok: true, json: async () => pages.shift() };
  }, 2);
  assert.deepEqual(posts.map(post => post.uri), ["newer", "older"]);
  assert.equal(urls[1].searchParams.get("cursor"), "page2");
  assert.equal(urls[0].searchParams.get("actor"), actor);
});

test("reports failures without returning an incomplete feed", async () => {
  await assert.rejects(loadCaremePosts(async () => ({ ok: false, status: 503 })), /503/);
  await assert.rejects(loadCaremePosts(async () => { throw new Error("offline"); }), /offline/);
  assert.deepEqual(await loadCaremePosts(async () => ({ ok: true, json: async () => ({ feed: [] }) })), []);
  let requests = 0;
  await loadCaremePosts(async () => ({ ok: true, json: async () => ({ feed: [], cursor: String(++requests) }) }));
  assert.equal(requests, 5);
});

test("shows only the four newest matching posts by default", async () => {
  const feed = Array.from({ length: 6 }, (_, index) => {
    const entry = item(`post-${index}`);
    entry.post.record.createdAt = `2026-11-0${index + 1}T12:00:00Z`;
    return entry;
  });
  const posts = await loadCaremePosts(async () => ({ ok: true, json: async () => ({ feed }) }));
  assert.deepEqual(posts.map(post => post.uri), ["post-5", "post-4", "post-3", "post-2"]);
});

test("shows one post at a time and supports previous, next, and endpoint controls", () => {
  class Node {
    children = [];
    listeners = {};
    attributes = {};
    append(...children) { this.children.push(...children); }
    replaceChildren(...children) { this.children = children; }
    setAttribute(key, value) { this.attributes[key] = value; }
    addEventListener(name, listener) { this.listeners[name] = listener; }
    click() { this.listeners.click(); }
    querySelector() { return null; }
  }
  globalThis.document = { createElement: () => new Node(), createTextNode: text => text };
  try {
    const ui = Object.fromEntries(["feed", "status", "controls", "previous", "next", "position"].map(key => [key, new Node()]));
    mountFeed([item("first").post, item("second").post], ui);
    assert.equal(ui.feed.children.length, 1);
    assert.equal(ui.position.textContent, "Post 1 of 2");
    assert.equal(ui.previous.disabled, true);
    assert.equal(ui.next.disabled, false);
    const first = ui.feed.children[0];
    ui.previous.click();
    assert.equal(ui.position.textContent, "Post 1 of 2");
    ui.next.click();
    assert.equal(ui.feed.children.length, 1);
    assert.notEqual(ui.feed.children[0], first);
    assert.equal(ui.position.textContent, "Post 2 of 2");
    assert.equal(ui.next.disabled, true);
    ui.next.click();
    assert.equal(ui.position.textContent, "Post 2 of 2");
    ui.previous.click();
    assert.equal(ui.position.textContent, "Post 1 of 2");
    assert.equal(ui.controls.hidden, false);
    mountFeed([item().post], ui);
    assert.equal(ui.controls.hidden, true);
    mountFeed([], ui);
    assert.equal(ui.controls.hidden, true);
    assert.equal(ui.status.textContent, "No Careme posts to show yet.");
  } finally {
    delete globalThis.document;
  }
});

test("preserves Unicode text and full recipe links without interpreting HTML or unsafe links", () => {
  const prefix = "🍲 Careme’s dinner: ";
  const label = "careme.cooking/recipe/…";
  const suffix = " <script>alert(1)</script>";
  const byteStart = Buffer.byteLength(prefix);
  const byteEnd = byteStart + Buffer.byteLength(label);
  const record = { text: prefix + label + suffix, facets: [{
    index: { byteStart, byteEnd }, features: [{ $type: linkType, uri: "https://careme.cooking/recipe/full-hash==" }],
  }] };
  assert.deepEqual(textParts(record), [
    { text: prefix }, { text: label, href: "https://careme.cooking/recipe/full-hash==" }, { text: suffix },
  ]);
  record.facets[0].features[0].uri = "javascript:alert(1)";
  assert.deepEqual(textParts(record), [{ text: record.text }]);
});

test("plays HLS video and stops the player when the post changes", async () => {
  globalThis.window = {};
  const calls = [];
  const video = {
    canPlayType: () => "", addEventListener() {}, removeEventListener() {},
    pause() { calls.push("pause"); }, load() {}, removeAttribute() {},
  };
  let fatalError;
  class Hls {
    static Events = { ERROR: "error" };
    static isSupported() { return true; }
    on(_event, handler) { fatalError = handler; }
    loadSource(url) { calls.push(url); }
    attachMedia(target) { assert.equal(target, video); }
    destroy() { calls.push("destroy"); }
  }
  try {
    let errors = 0;
    const player = attachVideo(video, "https://video.bsky.app/playlist.m3u8", () => errors++, async () => ({ default: Hls }));
    await player.ready;
    assert.equal(calls[0], "https://video.bsky.app/playlist.m3u8");
    fatalError("error", { fatal: true });
    assert.equal(errors, 1);
    player.dispose();
    assert.deepEqual(calls.slice(-2), ["pause", "destroy"]);
    fatalError("error", { fatal: true });
    assert.equal(errors, 1);

    const failed = attachVideo(video, "playlist", () => errors++, async () => { throw new Error("CDN unavailable"); });
    await failed.ready;
    assert.equal(errors, 2);
    failed.dispose();

    let resolveImport;
    const loading = attachVideo(video, "should-not-load", () => errors++, () => new Promise(resolve => { resolveImport = resolve; }));
    loading.dispose();
    resolveImport({ default: Hls });
    await loading.ready;
    assert.equal(calls.includes("should-not-load"), false);

    window.ManagedMediaSource = class {};
    video.canPlayType = () => "probably";
    const native = attachVideo(video, "native-playlist", () => errors++, async () => { throw new Error("Should use native HLS"); });
    await native.ready;
    assert.equal(video.src, "native-playlist");
    assert.equal(errors, 2);
    native.dispose();
  } finally {
    delete globalThis.window;
  }
});
