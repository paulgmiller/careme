// Fixture labels classify anchors explicitly: no keyword guessing or judge call.
module.exports = (output, context) => {
  const plans = JSON.parse(output).plans;
  const normalize = (label) => label.trim().toLowerCase().replace(/\s+/g, ' ');
  const animalProteins = new Set(context.vars.protein_policy.animal_anchors.map(normalize));
  const count = plans.filter((plan) => animalProteins.has(normalize(plan.anchor_ingredient))).length;
  const expectedCount = context.vars.count;
  const meatFree = context.vars.meat_free;
  const required = meatFree ? 0 : Math.floor(expectedCount / 2) + 1;
  const pass = plans.length === expectedCount && (meatFree ? count === 0 : count >= required);
  return {
    pass,
    score: pass ? 1 : 0,
    reason: `${count}/${plans.length} meals have meat or seafood anchors; expected ${expectedCount} meals and ${meatFree ? 'no meat or seafood anchors' : `at least ${required} meat or seafood anchors`}.`,
  };
};
