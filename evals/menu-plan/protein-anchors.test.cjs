const { test } = require('node:test');
const assert = require('node:assert/strict');
const grade = require('./protein-anchors.cjs');

for (const { name, anchors, count = anchors.length, meatFree = false, pass } of [
  { name: 'one dinner needs a meat anchor', anchors: ['Chicken'], pass: true },
  { name: 'one plant dinner fails the default', anchors: ['Tofu'], pass: false },
  { name: 'seafood counts toward an odd majority', anchors: ['Chicken', 'Salmon', 'Tofu'], pass: true },
  { name: 'half is not a majority', anchors: ['Chicken', 'Salmon', 'Tofu', 'Lentils'], pass: false },
  { name: 'three of four is a majority', anchors: ['Chicken', 'Salmon', 'Shrimp', 'Tofu'], pass: true },
  { name: 'cheese does not count as meat or seafood', anchors: ['Chicken', 'Halloumi', 'Tofu'], pass: false },
  { name: 'a seafood side cannot rescue plant anchors', anchors: ['Tofu', 'Lentils', 'Halloumi'], pass: false },
  { name: 'catalog capitalization and whitespace are ignored', anchors: [' CHICKEN ', 'salmon', 'Tofu'], pass: true },
  { name: 'vegetarian overrides the majority', anchors: ['Tofu', 'Lentils', 'Halloumi'], meatFree: true, pass: true },
  { name: 'vegetarian rejects a single seafood anchor', anchors: ['Tofu', 'Salmon', 'Halloumi'], meatFree: true, pass: false },
  { name: 'missing meals cannot lower the required majority', anchors: ['Chicken'], count: 4, pass: false },
  { name: 'an empty plan fails', anchors: [], count: 1, pass: false },
]) {
  test(name, () => {
    const output = JSON.stringify({ plans: anchors.map((anchor_ingredient) => ({
      anchor_ingredient, side_vegetable: 'Shrimp',
      recipe_instructions: ['Serve with chicken broth.'],
    })) });
    const result = grade(output, { vars: {
      count, meat_free: meatFree, protein_policy: { animal_anchors: ['Chicken', 'Salmon', 'Shrimp'] },
    } });
    assert.equal(result.pass, pass, result.reason);
    assert.equal(result.score, pass ? 1 : 0);
  });
}
