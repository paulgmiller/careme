const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { test } = require("node:test");
const vm = require("node:vm");

const script = readFileSync(`${__dirname}/recipe.js`, "utf8");

function openRecipe(storage, action = "/recipe/hash/question") {
  const listeners = {};
  const form = {
    getAttribute: () => action,
    addEventListener: (name, fn) => { listeners[name] = fn; },
  };
  const question = {
    value: "",
    form,
    addEventListener: (name, fn) => { listeners[name] = fn; },
  };
  vm.runInNewContext(`${script}\ninitializeQuestionDraft();`, {
    document: { getElementById: () => question, addEventListener() {} },
    window: { sessionStorage: storage },
  });
  return { question, listeners };
}

function response(successful, redirect = null) {
  return { detail: { successful, xhr: { getResponseHeader: () => redirect } } };
}

test("question survives expired session and login, then clears after an answer", () => {
  const storage = new Map();
  storage.setItem = storage.set.bind(storage);
  storage.getItem = storage.get.bind(storage);
  storage.removeItem = storage.delete.bind(storage);
  const first = openRecipe(storage);
  first.question.value = "Can I use tofu?\nEven with this sauce?";
  first.listeners["input"]();
  first.listeners["htmx:beforeRequest"]();
  first.listeners["htmx:afterRequest"](response(false, "/sign-in"));
  assert.equal(openRecipe(storage, "/recipe/other/question").question.value, "");
  const returned = openRecipe(storage);
  assert.equal(returned.question.value, first.question.value);
  returned.listeners["htmx:afterRequest"](response(false));
  assert.equal(openRecipe(storage).question.value, first.question.value);
  returned.listeners["htmx:afterRequest"](response(true, "/sign-in"));
  assert.equal(openRecipe(storage).question.value, first.question.value);
  returned.listeners["htmx:afterRequest"](response(true));
  assert.equal(openRecipe(storage).question.value, "");
});

test("unavailable storage does not break question submission", () => {
  const unavailable = () => { throw new Error("Storage disabled"); };
  const page = openRecipe({ getItem: unavailable, setItem: unavailable, removeItem: unavailable });
  page.question.value = "Can I use tofu?";
  page.listeners["input"]();
  page.listeners["htmx:beforeRequest"]();
  page.listeners["htmx:afterRequest"](response(true));
  assert.equal(page.question.value, "Can I use tofu?");
});
