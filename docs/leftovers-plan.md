# Leftovers endpoint

## Agreed behavior

`/leftovers` turns food already on hand into one recipe, without a menu-planning
step, store lookup, grocery items, or shopping list. Sign-in is required for now;
free generations may be considered later.

1. Require a description of leftovers. Guess breakfast (05:00–10:59), lunch
   (11:00–15:59), or dinner (otherwise) using browser-local time. Allow an override. Ask how many
   servings (1–12, default two) and scale the recipe to that yield; do not invent
   additional leftovers when the available quantity is insufficient.
2. Ask 3–5 individual ingredient questions with explicit Yes/No controls. Use
   dietary preferences and saved-meal history when choosing useful questions.
3. Offer **Just make something** with the first batch. Unanswered questions mean
   those ingredients are unavailable for that generation.
4. Generate one recipe directly when the available food supports a cohesive dish.
   Otherwise ask more targeted ingredient questions, up to ten individual
   questions across the session. Never repeat a question or exceed the limit.
5. Use judgment about ordinary basics such as oil, salt, and pepper. Ask when in
   doubt. An explicit No always overrides a pantry assumption.
6. Use whichever stated leftovers belong together; do not force incompatible
   foods into one dish. Be honest about uncertainty. At ten questions, or when
   asked to generate immediately, explain if a cohesive recipe is not possible
   and let the user add food or revise their leftovers.
7. Show the existing recipe page with Save, feedback, and follow-up questions.
   Omit shopping/cart controls and links. Generate its image asynchronously as
   soon as the recipe is ready; the existing image panel polls until it appears.
   Image failures do not prevent cooking or saving. Generation does not save a recipe.
8. Record a recipe in user history only after explicit Save. Avoid dishes similar
   to recently saved meals in format, preparation, and flavor, not just title.
   Pantry answers apply only to the current session.

## Implementation boundaries

Keep the `pastdecisions` branch separate. Until the branches are combined, use
this checkout's recent saved-recipe history (14 days), not unsaved shopping-list
or generation history. The later integration should use the shared decision
history and existing Save action rather than introducing a second save flow.

Persist owner-scoped, expiring sessions between question rounds. Validate answer
values and question limits on the server. Retrying a completed session should
reuse the recipe. A returned explanation is a normal culinary outcome; malformed
provider output or storage failures are errors.

## Verification

Cover required leftovers, authentication and ownership, meal override, answer
validation, progressive questions and the ten-question ceiling, immediate
generation, explicit No versus pantry assumptions, inability to make a meal,
provider failures, and history remaining unchanged until Save. Check the real
provider response shape on default and Flex tiers, run Go/UI verification, and
inspect desktop/mobile screenshots of the flow and existing recipe page.
