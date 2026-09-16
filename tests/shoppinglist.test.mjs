import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { runInNewContext } from "node:vm";

const script = readFileSync(new URL("../internal/static/shoppinglist.js", import.meta.url), "utf8");

function setup() {
  const listeners = new Map();
  let cards = [];
  const document = {
    addEventListener: (name, callback) => listeners.set(name, callback),
    querySelectorAll: () => cards,
  };
  runInNewContext(script, { document });
  return {
    before(oldCards, xhr, id = "shopping-content", shouldSwap = true) {
      listeners.get("htmx:beforeSwap")({
        detail: { xhr, shouldSwap, target: { id, querySelectorAll: () => oldCards } },
      });
    },
    after(newCards, xhr) {
      cards = newCards;
      listeners.get("htmx:afterSwap")({ detail: { xhr } });
    },
  };
}

function card(id, open) {
  const details = open === undefined ? null : { open };
  return { id, details, querySelector: () => details };
}

test("generation keeps open and closed details through reordering and completion", () => {
  const page = setup();
  const request = {};
  page.before([card("beans", true), card("tofu", false), card("pending")], request);
  const updated = [card("tofu", true), card("new", false), card("beans", false)];
  page.after(updated, request);
  assert.deepEqual(updated.map((item) => item.details.open), [false, false, true]);
  // A later poll/final response preserves the latest user choice, not a stale snapshot.
  updated[2].details.open = false;
  const completion = {};
  page.before(updated, completion);
  const finished = [card("beans", true), card("tofu", false)];
  page.after(finished, completion);
  assert.equal(finished[0].details.open, false);
});

test("removed recipes, image swaps, and failed polls do not overwrite details", () => {
  const page = setup();
  const request = {};
  page.before([card("removed", true)], request);
  const added = card("new", false);
  page.after([added], request);
  assert.equal(added.details.open, false);
  for (const [id, shouldSwap] of [["image", true], ["shopping-content", false]]) {
    const ignored = {};
    page.before([card("new", true)], ignored, id, shouldSwap);
    page.after([added], ignored);
    assert.equal(added.details.open, false);
  }
});
