# Careme UI Review (Screenshot Tour)

Date: March 3, 2026  
Reviewed artifact: `tour.md` and referenced screenshots in `screenshots/`  
Note: `doc.md` was not present in this folder, so this review is based on `tour.md`.

## Executive Summary

Careme has a clear visual identity and a calm, friendly tone that matches the cooking use case. The core flow is understandable: find a store, generate recipes, save what you like, and assemble a shopping list. The UI feels cohesive, but it currently asks users to process too much content at once in critical decision screens.

Top priorities:
1. Reduce information overload on recipe list/detail screens.
2. Clarify primary actions and standardize button semantics.
3. Improve accessibility (contrast, color dependence, dense text blocks).
4. Improve list management for repeat use (search/filter/history controls).

## What Is Working Well

1. Strong thematic fit: green palette, culinary tone, and “chef” language feel consistent.
2. Clear card-based layout: sections are visually separated and generally easy to parse.
3. Useful personalization hooks: chef notes, saved recipes, preferences, and feedback loop.
4. Good practical detail: ingredients, cook times, health notes, and pairing tips add value.
5. Progressive user journey exists: landing -> location -> recipe generation -> saved list -> cooking feedback.

## High-Priority Issues (Fix First)

| ID | Severity | Where Seen | Problem | Why It Matters | Recommended Fix |
|---|---|---|---|---|---|
| H1 | High | `shoppinglistv2.jpeg`, `assembled.jpg`, `recipefeedback.png` | Excessive vertical density and long cards with full ingredients + full instructions repeated per recipe. | Users must scroll heavily and lose context; comparing options becomes slow. | Default recipe cards to collapsed summary. Show only title, short blurb, key metadata, and actions. Expand details on demand. |
| H2 | High | `locations.png`, `shoppinglist.jpeg`, `savedimsiss.jpeg` | Action hierarchy is inconsistent (`Recipes!`, `Build Recipes`, `Try again, chef`, `Assemble Shopping List`) and sometimes ambiguous. | Users cannot quickly identify “what to do next” on each screen. | Define one primary action per page, make naming consistent, and keep secondary actions visually subordinate. |
| H3 | High | `shoppinglist.jpeg`, `savedimsiss.jpeg`, `shoppinglistv2.jpeg` | `Save` and `Dismiss` are adjacent on every card; dismissal appears destructive and easy to tap by mistake. | High risk of accidental loss/confusion, especially on mobile. | Move `Dismiss` to overflow/menu or require confirm/undo toast. Keep `Save` as the only prominent inline action. |
| H4 | High | Across nearly all screens | Contrast is often low (light green borders, gray text on pale backgrounds, placeholder-heavy inputs). | Readability and accessibility suffer; likely WCAG failures in several text/border states. | Increase contrast tokens for body text, placeholders, borders, and muted labels. Add non-color cues for status. |
| H5 | High | `recipefeedback.png` | Q&A history can become very long and dominates page length. | Feedback input and recipe info are pushed far down; users lose task focus. | Collapse previous Q&A by default (“Show previous answers”), pin Ask/Feedback panel near top, and add jump links. |

## Medium-Priority Issues

| ID | Severity | Where Seen | Problem | Why It Matters | Recommended Fix |
|---|---|---|---|---|---|
| M1 | Medium | `assembled.jpg`, `shoppinglistv2.jpeg`, `recipefeedback.png` | Ingredient rows are hard to scan (mixed alignment, variable row heights, repeated pantry items, duplicated quantities in some rows). | Shopping tasks require quick scanning and confidence; current layout increases cognitive load. | Use fixed columns on desktop and stacked key/value on mobile; group duplicate pantry items and deduplicate quantities. |
| M2 | Medium | `pastrecipes.png` | Long “Recent history” list lacks search/filter/sort controls. | Repeat users will struggle to find older recipes quickly. | Add search by recipe name, filters (date/protein/style), and sort options. |
| M3 | Medium | `about.png` | About page combines marketing, privacy summary, repo links, and FAQ in one long stream. | Important trust and support content is discoverable but not prioritized. | Add sticky in-page section nav and tighten top hero height to surface key links earlier. |
| M4 | Medium | `locations.png` | Store cards present star, `Recipes!`, and `Chef notes` actions with little explanatory context. | First-time users may not know the expected sequence. | Add microcopy under each store row: “1) Add notes (optional) 2) Build recipes”. |
| M5 | Medium | `customize.png` | Preference form is long, with a large free-text field and no helper structure beyond examples. | Users may provide inconsistent prompts, reducing generation quality. | Offer structured chips/toggles for common constraints (diet, prep time, servings), then optional free text. |
| M6 | Medium | `spinner.png` | Loading state asks users to manually refresh every 10 seconds. | Feels brittle; can break trust during generation delays. | Auto-refresh/poll with visible progress states; keep manual refresh as fallback only. |

## Aesthetic Review

### Visual System

Strengths:
1. Consistent green-forward brand language.
2. Rounded cards and soft shadows create a welcoming “kitchen notebook” feel.

Improvements:
1. Increase typographic hierarchy contrast between headings, metadata, and helper text.
2. Reduce border-heavy styling; use spacing and subtle background layers for separation instead.
3. Introduce one accent color for non-primary actions and informational states to reduce “everything is green” fatigue.
4. Normalize corner radius and shadow depth across button/card/input components.

### Typography and Readability

Strengths:
1. Recipe titles are expressive and descriptive.
2. Body copy tone is human and approachable.

Improvements:
1. Limit long title wrapping by shortening card title display with “show full title” option.
2. Increase line-height slightly in dense instruction blocks.
3. Reduce all-caps section labels where possible for smoother reading flow.

## Usability Review by Flow

### 1) Entry and Trust (`homepage.png`, `about.png`)

What works:
1. Clear value proposition and straightforward auth CTAs.
2. About page includes trust elements (privacy and source links).

Issues:
1. Homepage secondary text is visually quiet against the background image.
2. About page is very long before critical support actions become obvious.

### 2) Signed-in Home and Store Selection (`homesignedin.png`, `locations.png`)

What works:
1. “Find a store” and “Your kitchen” are separate mental models.
2. ZIP and geolocation entry options are present.

Issues:
1. Home screen has large empty zones, making it look sparse and slightly unfinished.
2. Store row controls are compact but ambiguous for first-time users.

### 3) Recipe Generation and Triage (`shoppinglist.jpeg`, `shoppinglistv2.jpeg`, `savedimsiss.jpeg`)

What works:
1. Save/dismiss loop enables quick preference refinement.
2. Chef notes encourage iterative personalization.

Issues:
1. Too much content per recipe card when expanded.
2. Save/Dismiss button pattern invites mis-taps and repeated decision fatigue.
3. “Shopping list” area lacks clear state (empty vs populated vs hidden).

### 4) Assembled Shopping Output (`assembled.jpg`)

What works:
1. Ingredient details are rich and practical.
2. Pantry labeling is useful.

Issues:
1. No obvious grouping by recipe/aisle/category in long lists.
2. Duplicate or near-duplicate items reduce confidence and convenience.

### 5) Kitchen History and Preferences (`pastrecipes.png`, `customize.png`)

What works:
1. Good concept: one “Kitchen” hub for account + preferences + history.
2. Preferences explain how free text influences planning.

Issues:
1. History becomes unwieldy without search/filter.
2. Toggle/tab selected state is subtle in some captures.

### 6) Recipe Detail and Feedback (`recipefeedback.png`, `recipe.jpeg`)

What works:
1. End-to-end utility: ingredients, method, nutrition-ish guidance, pairing, AI Q&A, star rating.
2. Feedback capture is embedded directly into use moment.

Issues:
1. Page length is very high, especially with AI conversation history.
2. Feedback and Q&A controls are not visually separated enough from long content blocks.

## Accessibility Checklist (Observed Risk Areas)

1. Contrast likely below target in placeholder text, muted labels, and pale green borders.
2. Status appears color-driven in places (save/dismiss states) without icon/text reinforcement.
3. Very long pages need better structural navigation (sticky anchors, “back to top”, section jump menu).
4. Button semantics should include clearer destructive-state handling.

## Recommended Implementation Plan

### Quick Wins (1-2 weeks)

1. Standardize CTA naming and hierarchy by screen.
2. Improve contrast tokens and button state styles.
3. Add undo toast for dismiss/remove actions.
4. Add “Back to top” and section jump links on long recipe pages.
5. Clarify shopping list state with explicit empty-state text and count badges.

### Next Wave (2-4 weeks)

1. Collapse recipe cards by default with progressive disclosure.
2. Add search/filter/sort for Past Recipes.
3. Rework ingredients into a cleaner two-column data pattern desktop + stacked mobile pattern.
4. Introduce grouped shopping list views (by aisle/category/recipe).

### Larger Improvements (4-8 weeks)

1. Split recipe detail into tabs: `Overview`, `Cook`, `Q&A`, `Feedback`.
2. Add structured preference controls (chips/checkboxes) plus optional free text.
3. Build a mobile-first density pass to reduce cognitive load and scrolling burden.

## Suggested Success Metrics

1. Time to first successful recipe save.
2. Number of recipes reviewed before first save.
3. Dismiss undo rate (proxy for accidental actions).
4. Scroll depth before primary action on recipe and shopping pages.
5. Repeat usage of “Past recipes” with search/filter adoption.
