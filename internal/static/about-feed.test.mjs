import { readFile } from "node:fs/promises";
import assert from "node:assert/strict";
import test from "node:test";
const source = await readFile(new URL("./about-feed.js", import.meta.url), "utf8");
const { loadCaremePosts, textParts, mountFeed } = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

const post = (id, media = []) => ({id, date: "2026-11-21T12:00:00Z", text: "Dinner https://careme.cooking/recipe/example", media});

test("loads only the local exported index and reports HTTP failures", async () => {
  const posts = [post("first")];
  assert.deepEqual(await loadCaremePosts(async url => {
    assert.equal(url, "/updates/feed.json");
    return {ok: true, json: async () => posts};
  }), posts);
  await assert.rejects(loadCaremePosts(async () => ({ok:false,status:503})), /503/);
});

test("keeps Unicode, HTML-looking text, and full recipe links as text", () => {
  const text = "🍲 Careme’s <script> https://careme.cooking/recipe/abc==";
  const parts = textParts(text);
  assert.equal(parts.map(part => part.text).join(""), text);
  assert.equal(parts[1].href, "https://careme.cooking/recipe/abc==");
  assert.deepEqual(textParts("javascript:alert(1)"), [{text:"javascript:alert(1)"}]);
});

test("shows one post, renders local photos and native video, and pauses video on navigation", () => {
  class Node {
    children = []; listeners = {}; attributes = {}; paused = false;
    constructor(tag) { this.tag = tag; }
    append(...children) { this.children.push(...children); }
    replaceChildren(...children) { this.children = children; }
    setAttribute(key,value) { this.attributes[key] = value; }
    addEventListener(name,listener) { this.listeners[name] = listener; }
    click() { this.listeners.click(); }
    pause() { this.paused = true; }
    querySelectorAll(tag) { return this.children.flatMap(child => typeof child === "object" ? [...(child.tag === tag ? [child] : []), ...child.querySelectorAll(tag)] : []); }
  }
  globalThis.document = {createElement: tag => new Node(tag), createTextNode: text => text};
  try {
    const ui = Object.fromEntries(["feed","status","controls","previous","next","position"].map(key => [key,new Node("div")]));
    mountFeed([post("one",[{src:"one/video.mp4",type:"video/mp4",alt:""}]),post("two",[{src:"two/photo.jpg",type:"image/jpeg",alt:"Dinner"}])],ui);
    assert.equal(ui.feed.children.length,1);
    assert.equal(ui.position.textContent,"Post 1 of 2");
    assert.equal(ui.previous.disabled,true);
    const video = ui.feed.querySelectorAll("video")[0];
    assert.equal(video.controls,true);
    assert.equal(video.children[0].src,"/updates/one/video.mp4");
    assert.equal(video.children[0].type,"video/mp4");
    ui.next.click();
    assert.equal(video.paused,true);
    assert.equal(ui.feed.querySelectorAll("video").length,0);
    assert.equal(ui.feed.children.length,1);
    assert.equal(ui.feed.querySelectorAll("img")[0].src,"/updates/two/photo.jpg");
    assert.equal(ui.next.disabled,true);
    ui.next.click();
    assert.equal(ui.position.textContent,"Post 2 of 2");
    ui.previous.click();
    assert.equal(ui.position.textContent,"Post 1 of 2");
    mountFeed([post("only")],ui);
    assert.equal(ui.controls.hidden,true);
    mountFeed([],ui);
    assert.equal(ui.controls.hidden,true);
    assert.equal(ui.status.textContent,"No Careme posts to show yet.");
  } finally { delete globalThis.document; }
});
