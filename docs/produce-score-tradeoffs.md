# Produce Score Tradeoffs

Date: 2026-02-22  
Location: `70500874` (default from `cmd/producecheck`)  
Command: `go run ./cmd/producecheck` with `.envtest` credentials

## Result Summary

- Baseline before changes: `62/107` (`0.579439`) with `332` ingredients.
- Best observed after iteration: `93/107` (`0.869159`) with `672` ingredients.
- Observed range on repeated runs with the final config: `89-93` / `107` (Kroger fuzzy ordering varies).

## What Changed

### 1) Produce filter strategy (`internal/recipes/params.go`)

I expanded `Produce()` from a small set of broad terms to a mixed set:

- Broad recall terms: `produce`, `fresh produce`
- Targeted recovery terms for persistent misses:
  - peppers/chiles: `bell peppers`, `red chili peppers`, `green chili peppers`, `mini sweet peppers`
  - mushrooms: `mushrooms produce`, `king trumpet mushrooms`
  - sprouts: `alfalfa sprouts`, `broccoli sprouts`, `bean sprouts`
  - cucumbers: `cucumber produce`, `seedless cucumbers`
  - other gaps: `little gem produce`, `chives`, `napa cabbage`, `eggplant`, `parsnip produce`

All produce filters now consistently use `Brands: []string{"*"}` to avoid accidental exclusion of branded produce.

### 2) Produce matching robustness (`cmd/producecheck/main.go`)

I improved term normalization and matching to reduce obvious false negatives:

- strips diacritics (`jalapeño` -> `jalapeno`)
- removes parenthetical aliases (`green onions (scallions)` -> `green onions`)
- normalizes token morphology (basic singularization/plural handling)
- maps known token variants (`portobello` -> `portabella`, `kiwifruit` -> `kiwi`, `chile` -> `chili`)
- uses token-set matching instead of raw substring matching
- allows optional trailing `lettuce` token to match `little gem` descriptions

### 3) "Fresh" Seems to get more than what produce gets with out losing much. 

too many false positives?

• Compared as unique non-empty lines (normalized line endings), here’s the difference:

  freshproduce.txt but not produce.txt (49 lines):

   - Butternut Squash:(Produce,Produce))
   - Chayote Squash:(International,Produce,Produce))
   - Fresh Kroger® D'Anjou Pears - 2 Pound Bag:(Produce,Produce))
   - Fresh Tomatillo:(Produce,International,Produce))
   - Golden Acorn Squash:(Produce,Produce))
   - Kroger® Fresh French Green Beans Bag:(Produce,Produce,Produce))
   - Mini Seedless Whole Watermelon:(Produce,Produce))
   - Organic Parsley:(Produce,Produce))
   - Spaghetti Squash:(Produce,Produce))
  Driscoll's - Driscollâ€™sÂ® Limited Edition Sweetest Batchâ„¢ Fresh Blueberries:(Produce,Produce))
  Fresh Apples - Organic Granny Smith Apple – Each:(Produce,Produce))
  Fresh Apples - Small Gala Apple â€“ Each:(Produce,Produce))
  Fresh Berries - Driscoll's® Only the Finest Berries™ Raspberries:(Produce,Produce))
  Fresh Berries - Driscoll’s Rainbow Pack Fresh Blackberries Blueberries & Raspberries:(Produce,Produce))
  Fresh Berries - Driscoll’s Sweetest Batch Fresh Blackberries:(Produce,Produce))
  Fresh Berries - Fresh Red Raspberries - 12 OZ Clamshell:(Produce,Produce))
  Fresh Corn - Fresh Sweet Corn on the Cob-Each:(Produce,Produce))
  Fresh Melons - Honeydew Melon:(Produce,Produce))
  Fresh Melons - Seeded Whole Watermelon:(Produce,Produce))
  Fresh Melons - Seedless Whole Watermelon:(Produce,Produce))
  Fresh Stone fruit - Fresh California Yellow Peach – Each:(Produce,Produce))
  Fresh Stone fruit - Fresh White Nectarine:(Produce,Produce,International))
  Fresh Stone fruit - Fresh White Peach - Each:(Produce,Produce))
  Fresh Stone fruit - Fresh Yellow Nectarine:(Produce,Produce,International))
  Fresh Stone fruit - Kroger® Fresh Peaches in 2 LB Bag:(Produce,Produce))
  Fresh Stone fruit - Kroger® Fresh Plums in 2 LB Bag:(Produce,Produce))
  Fresh Tomatoes - Fresh Heirloom Tomato:(Produce,Produce))
  Fresh Tomatoes - Fresh Large Hothouse Grown Premium Red Tomato:(Produce,Produce))
  Fresh Tomatoes - Fresh Large Red Slicing Tomato:(Produce,Produce))
  Fresh Tomatoes - Fresh Red Tomatoes:(Produce,Produce))
  Frieda's - Frieda's Opo Squash:(Produce,Produce))
  Kroger - Fresh Kroger® Bosc Pears - 2 Pound Bag:(Produce,Produce))
  Kroger - Kroger® Brussels Sprouts BIG DEAL!:(Produce,Produce))
  Kroger - Kroger® Brussels Sprouts Halves:(Produce,Produce))
  Kroger - Kroger® Fresh Bartlett Pears – 2 Pound Bag:(Produce,Produce))
  Kroger - Kroger® Fresh Navel Oranges Bag:(Produce,Produce))
  Nature Sweet - NatureSweet Cherubs Fresh Heavenly Salad Grape Tomatoes, 10 oz:(Produce,Produce))
  Nature Sweet - NatureSweet Cherubs® Fresh Grape Tomatoes:(Produce,Produce))
  Nature Sweet - NatureSweet Constellation® Medley Fresh Snacking Tomatoes:(Produce,Produce))
  Nature Sweet - NatureSweet Glorys Cherry Tomatoes, 10 oz:(Produce,Produce))
  Private Selection - Private Selection® Fresh Colossal Blueberries:(Produce,Produce))
  Private Selection - Private Selection® Fresh Petite Cherry Snacking Tomatoes:(Produce,Produce))
  Private Selection - Private Selection® Petite Grape Snacking Tomatoes:(Produce,Produce))
  Private Selection - Private Selection® Sweet Karoline Fresh Blackberries:(Produce,Produce))
  Private Selection - Private SelectionÂ® Campari Tomatoes:(Produce,Produce))
  Private Selection - Private SelectionÂ® Ruby Rowsâ„¢ Fresh Cherry Tomatoes on the Vine:(Produce,Produce))
  Private Selection - Private Selection™ Fresh Petite Cherry Snacking Tomatoes:(Produce,Produce))
  Simple Truth Organic - Simple Truth Organic® Fresh Grapefruit Bag:(Produce,Produce))
  Simple Truth Organic - Simple Truth OrganicÂ® Fresh Cranberries:(Produce,Produce))

  produce.txt but not freshproduce.txt (48 lines):

   - Spinach:(Produce,Produce))
  Adwood Manufacturing - Adwood Manufacturing Wooden Produce Crate:(Cleaning Products))
  Bluey - Bluey Medley with Peeled Sweet Apple Slices Cheese and Pretzels:(Produce))
  Bob's Red Mill - Bob's Red Mill® Organic Whole Grain Tri-Color Quinoa:(Pasta, Sauces, Grain,Natural & Organic,Natural & Organic))
  Core Kitchen - Core™ Kitchen Produce Crisper Bin with Draining Board:(Cleaning Products))
  Crunch Pak - Crunch Pak® Grab 'N Go Organic Sweet Apple Slices:(Natural & Organic,Produce,Produce))
  Crunch Pak - Crunch Pak® Grab'N Go Sweet Apple Slice Snack Pack:(Produce))
  Crunch Pak - Crunch Pak® Medley With Reese's Minis and Apples Snack Tray:(Produce))
  Crunch Pak - Crunch Pak® Minions Medley With Apple Slices Turkey Sausage Bites & Pretzels:(Produce,Meat & Seafood))
  Crunch Pak - Crunch Pak® Nickelodeon Paw Patrol™ Sliced Sweet Apples Cheese Grapes and Cookies Snack Tray:(Produce,Meat & Seafood))
  Crunch Pak - Crunch Pak® Nickelodeon™ Paw Patrol™ Peeled Sweet Apples Fruit Snacks Cheese and Cookies Snack Tray:(Produce))
  Crunch Pak - Crunch Pak® Peeled Apple Slices:(Produce))
  Crunch Pak - Crunch PakÂ® Disney FoodleÂ® Sweet Apples Cheese & Crackers Tray:(Produce,Meat & Seafood))
  Del Monte - Del Monte No Sugar Added Citrus Salad in Sweetened Water:(International,Produce))
  Del Monte - Del Monte® No Sugar Added Red Grapefruit Jar:(International,Produce))
  Del Monte - Del Monte® Red Grapefruit in Extra Light Syrup:(International,Produce))
  GoodCook® - GoodCook® Touch® Produce Chopper:(Kitchen))
  Gotham Greens - Gotham Greens® Gourmet Spring Mix Lettuce:(Produce))
  Gourmet Garden - Gourmet Garden Lightly Dried Chopped Parsley:(Natural & Organic,Baking Goods))
  Kroger - Kroger® Carrots Celery And Broccoli Snack Tray With Ranch Dip:(Produce,Snacks))
  Kroger - Kroger® No Sugar Added Red Grapefruit Cup:(Snacks,Produce))
  Kroger - Kroger® Sliced Apples Chocolatey Caramels and Pretzels Snack Tray:(Meat & Seafood,Produce))
  Kroger - Kroger® Sweet Apples Cheddar Cheese and Pretzels Snack Tray:(Produce))
  Kroger - Kroger® Sweet Apples and Caramel Snack Tray:(Produce,Snacks))
  Kroger - Kroger® Tart Apples and Caramel Snack Tray BIG DEAL!:(Produce))
  Kroger - Kroger® Veggie Chips & Buffalo Dip:(Produce,Produce))
  Naturipe - Naturipe Snacks® Berry Buddies™ Berries & Pancakes:(Produce))
  Naturipe - Naturipe Snacks® Bliss Bento® Berry Lemony Snack Pack:(Produce))
  Oh Snap! Pickling Co. - OH SNAP!Â® Dilly BitesÂ® Pickle Pouches:(Meat & Seafood,Snacks,Canned & Packaged,Produce))
  Oh Snap! Pickling Co. - Oh Snap!® Pickling Co. Hottie Bites® Pickle Cuts Snack Pack:(Canned & Packaged,Produce,Meat & Seafood,Snacks))
  Oh Snap! Pickling Co. - Oh Snap!® Sassy Bites™ Sweet N' Spicy Pickle Snack Pack:(Canned & Packaged,Produce,Meat & Seafood,Snacks))
  Seoul - Seoul Original Kimchi:(Produce,International))
  Simple Truth - Simple Truth OrganicÂ® Broccoli Florets:(Natural & Organic))
  Simple Truth Organic - Simple Truth Organic® Avocado Ranch Chopped Salad Kit:(Produce,Produce,Natural & Organic,International))
  Simple Truth Organic - Simple Truth Organic® Baby Red Butter & Arugula Salad Mix:(Natural & Organic))
  Simple Truth Organic - Simple Truth Organic® Classic Caesar Salad Kit:(Natural & Organic,Natural & Organic))
  Simple Truth Organic - Simple Truth Organic® Ginger Container:(Natural & Organic,Baking Goods))
  Simple Truth Organic - Simple Truth Organic® Gourmet Fingerling Potatoes:(Natural & Organic))
  Simple Truth Organic - Simple Truth Organic® Little Gem Butter Crunch Salad Mix:(Natural & Organic,Produce,Produce))
  Simple Truth Organic - Simple Truth Organic® Little Gem Romaine Butter Crunch Salad Mix:(Natural & Organic))
  Simple Truth Organic - Simple Truth Organic® Mediterranean Style Medley Salad Starter:(Natural & Organic))
  Simple Truth Organic - Simple Truth Organic® Tomato Medley Snacking Tomatoes:(Natural & Organic))
  Simple Truth Organic - Simple Truth Organic® Very Veggie Medley Salad Starter:(Natural & Organic))
  Simple Truth Organic - Simple Truth Organic® Wild Red Arugula:(Produce,Natural & Organic))
  Simple Truth Organic - Simple Truth OrganicÂ® Trimmed Green Beans:(Natural & Organic))
  Simple Truth Organic - Simple Truth Organic™ Minced Garlic:(Natural & Organic))
  Sunset - Organic Tomatoes on the Vine in 1lb Bag:(Natural & Organic))
  Ziploc - Ziploc® Brand Gallon Produce Storage Bags, Breathable Bag, Moisture Control, 15 count:(Cleaning Products))

## Key Constraints and Tradeoffs

1. Score vs query volume
- Higher score required significantly more query terms.
- Baseline query set was much smaller; final set increases Kroger calls materially.
- Approximate API call growth: from ~10 calls/run to ~25-30 calls/run (depends on pagination and Kroger fuzzy ordering).

2. Kroger API limit
- `filter.limit` is hard-capped at `50` (attempting `200` returns `PRODUCT-2013`), so call reduction by page-size increase is not available.

3. Stability vs strictness
- Kroger product search ordering is fuzzy and can vary between runs.
- Some targeted terms intermittently return fewer useful rows; simpler direct terms (`bean sprouts`, `napa cabbage`, `king trumpet mushrooms`, etc.) were more stable.

4. Precision vs recall
- Broader/tokenized matching improves recall (score) but can include non-produce or adjacent packaged items in some categories.
- This is acceptable for `cmd/producecheck` benchmarking, but should be considered if the same logic is reused elsewhere.

## Remaining Misses at Best Run

At `93/107`, the remaining unmatched terms were:

- `aloe vera leaf`
- `broccolini`
- `celery root`
- `curly kale`
- `daikon radish`
- `horseradish root`
- `lemongrass`
- `mixed sprout`
- `oyster mushroom`
- `radicchio`
- `red chili pepper`
- `rutabaga`
- `sliced mushroom blend`
- `yuca`

These appear to be a mix of true availability gaps and query/indexing gaps at this location.
