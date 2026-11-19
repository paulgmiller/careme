# Cooking preference suggestions

Signed-in users who submit chef notes when building or regenerating recipes can
review suggested lasting preferences while recipes generate. The optional card
continues to work after generation completes. Nothing is saved until they choose
**Save preference**. **Not now** dismisses that proposal.

Suggestions exclude one-time ingredient requests, temporary restrictions, uncertain
inferences, and preferences already saved. Conflicts show the existing text and
its proposed replacement. Saving preserves unrelated cooking preferences and
account settings. Changes apply to future builds, including emailed menus; they
do not restart the current recipes. The card links directly to the existing
cooking preferences editor.

Proposals are private to the submitting user, expire after 24 hours, and are not
created by visiting shared recipe links. If preferences have changed since a
proposal was created, Careme extracts a fresh proposal and asks for approval
again. Extraction failure does not block recipe generation. Mock mode uses a
no-op suggester; deterministic tests inject explicit proposals.

## Extraction quality checks

The existing AI client uses its configured menu model, service tier, and HTTP
transport. The preference request is independent of menu and recipe requests.
Run the uncached live cases against both default and Flex tiers before rollout:

```sh
CAREME_PREFERENCE_EVAL=1 go test ./internal/ai -run '^TestPreferenceExtractionLive$' -count=1 -v
```

Supply `AI_API_KEY` through the normal environment; the test fails if it is absent.
Cases cover mixed lasting/temporary notes, temporary-only requests, semantic
duplicates, uncertainty, contradictory servings, and multiple preferences.
Ordinary Go tests use fake transports and never invoke this live suite.
