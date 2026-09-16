# Pantry context: implementation options

Status: options for evaluation; no production approach selected.

## Goal

Keep menu planning focused on high-quality proteins and seasonal produce while giving recipe generation enough pantry ingredients to make practical, varied dishes using actual store products. Pantry can include spices, sauces, condiments, pasta, grains, dairy, and international ingredients.

The decision is when to introduce pantry products and who selects them. All three options reuse a separately fetched store pantry catalog and its product IDs, sizes, prices, grades, and categories.

## Option 1: Full pantry in shared context

### Plan

Add the eligible pantry catalog to the ingredient context supplied to menu planning and inherited by recipe generation. Deduplicate products and maintain stable ordering. Place reusable content before changing instructions and verify actual prompt-cache reuse across both stages.

### Pros

- Simplest generation flow; no retrieval decisions or additional model round trips.
- Both planners see the available ingredients and can reason about combinations directly.
- Avoids retrieval accidentally excluding a key ingredient.
- Lets store availability influence the menu before a dish is chosen.

### Cons

- More input tokens on every request; savings depend on cache reuse.
- Similar products can crowd the context and distract from useful choices.
- Larger catalogs may increase processing or reasoning time.
- Requires attention to shared prompt structure: matching ingredient text alone does not guarantee a cache hit when preceding instructions differ.

### Best fit

A modest catalog, such as roughly 300 additional products, where simplicity and broad ingredient coverage matter most. Use this as the quality baseline.

## Option 2: Retrieve pantry after the recipe plan

### Plan

Generate a menu from the main ingredient context. For each recipe plan, retrieve supporting pantry products using cuisine, anchor, and side ingredients, then append those products before recipe generation. Retain retrieved product metadata for pricing and shopping-list enrichment.

Two retrieval variants are worth comparing:

- **Text embeddings:** use the existing brand-stripped query and store product embeddings. Filter grades below 5 and select up to five matches per originating pantry query category.
- **Culinary embeddings:** map store products and plan ingredients to Epicure's canonical vocabulary, cache those mappings, and rank mapped pantry candidates using ingredient relationships. Start with Cooc; evaluate Core and cuisine steering separately. Rank the available store candidates directly rather than retrieving a small global list and hoping the store carries it.

### Pros

- Small, bounded recipe context and predictable orchestration.
- Retrieval can run independently for each recipe plan.
- Culinary embeddings offer a signal based on recipe co-occurrence, closer to supporting-ingredient selection than product-text similarity.
- Local culinary vectors avoid a query embedding API call once canonical mappings are available.

### Cons

- Missing candidates remain invisible to recipe generation.
- Cuisine, anchor, and side do not fully specify a dish or its pantry needs.
- Text similarity can retrieve irrelevant products, as seen with cheese for Vietnamese pork.
- Fixed category quotas can include unnecessary dairy or miss additional useful spices; several slots may be occupied by variants of one ingredient.
- Epicure requires product-to-ingredient mapping, with difficult cases for blends, prepared sauces, and ingredients outside its vocabulary.
- Combining anchor, side, and cuisine signals requires evaluation; culinary embeddings do not establish recipe-level compatibility by themselves.

### Best fit

A larger catalog when retrieval demonstrably retains the ingredients needed for good recipes. Epicure makes this option worth testing, rather than assuming generic nearest-neighbor search is sufficient.

## Option 3: Recipe generation searches the pantry

### Plan

Give recipe generation a batched search tool over the pre-fetched store catalog, for example `search_pantry(["fish sauce", "rice vermicelli", "rice vinegar"])`. The model decides what it needs, receives matching product IDs and metadata, and completes the recipe. Support follow-up searches within an explicit call budget and propagate tool failures as contextual errors. Include selected products in downstream metadata enrichment.

The search backend can use text matching, text embeddings, or culinary embeddings. Tool calling and embedding choice are separate decisions.

### Pros

- Searches reflect the dish the model is actually developing.
- The model can request concrete ingredient names and adapt to results.
- Small initial context, with additional products introduced when needed.
- Can recover from an incomplete first search or explore alternatives.

### Cons

- Additional model round trips, tool output tokens, and variable latency/cost.
- More orchestration, error handling, and testing.
- The model may omit a needed search, issue broad searches, or keep searching unnecessarily.
- Menu planning still lacks detailed pantry availability and may propose a direction that later needs adaptation.

### Best fit

A large or diverse catalog where the recipe model benefits from targeted lookups and refinement. Compare its quality gains with its additional latency and complexity.

## Evaluation and decision

Use the same store catalog, instructions, and fixed recipe plans to isolate recipe-stage differences. Separately compare end-to-end menu quality, since option 1 also changes what the menu planner sees.

Measure:

- Recipe suitability, variety, and adherence to user instructions.
- Useful pantry ingredients omitted and irrelevant products supplied or selected.
- Exact store-product coverage, including canonical mapping coverage for Epicure.
- Duplicate product variants occupying retrieval slots.
- Input, output, cached-read, and cache-write tokens; actual total API cost.
- Time to first recipe and all three completed recipes; tool calls and failures.

Include Vietnamese pork and peppers, vegetarian meals, multiple cuisines, blends/sauces, and stores with sparse category coverage. Have human review assess cooking quality; embedding similarity is not a quality score.

Start with option 1 as the baseline and option 2 with Epicure-Cooc as the next experiment. Test option 3 if targeted model-directed searches address failures that fixed retrieval cannot. Keep the current text-embedding retrieval as a comparison. Do not select an approach solely on context size.

## Cost assumptions and references

The earlier estimate assumed 300 additional TSV products at 30–40 tokens each: about 9,000–12,000 tokens, not a measured catalog token count. At GPT-6 Astra standard pricing, two cache writes and two reads across one menu plus three recipes would add approximately $0.24–$0.32 in input cost. One write and three reads would add $0.14–$0.19. Output changes, critiques, images, and other existing costs are excluded. Measure cache behavior rather than assuming it; the cause of the reported first-recipe miss has not been confirmed from logs.

- [OpenAI model pricing](https://developers.openai.com/api/docs/models/gpt-6-astra) and [prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching). Rates checked September 15, 2026.
- [Latency guidance](https://developers.openai.com/api/docs/guides/latency-optimization): input length is only one contributor; benchmark the full workflow.
- [Epicure paper](https://arxiv.org/html/2605.22391v1): compares recipe-co-occurrence and chemistry-based ingredient relationships. It does not establish superiority for this recipe-generation pipeline.
- [Epicure-Cooc release](https://huggingface.co/Kaikaku/epicure-cooc): downloadable 300-dimensional vectors for 1,790 canonical ingredients. The model card documents heuristic cuisine-direction reconstruction and its limitations; it is not an arbitrary-text encoder.
- [Epicure MCP](https://github.com/KAIKAKU-AI/epicure-mcp): available tools for ingredient relationship exploration.
