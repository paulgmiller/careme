Careme is your personal chef and sommilier. It will

1. Take your favorite grocery store based on location
2. Check the stores inventory for fresh meat and seasonal produce
3. Generate a weekly meal plan from a variety of cuisines and cooking styles.


## Configuration

The application is configured via environment variables:
### Mandatory 
- `KROGER_CLIENT_ID` - Kroger API client ID (required)
- `KROGER_CLIENT_SECRET` - Kroger API client secret (required)
- `AI_API_KEY` - OpenAI or Anthropic API key (required)
### Optional 
- `CLARITY_PROJECT_ID` - Microsoft Clarity project ID for web analytics (optional)
- `GOOGLE_TAG_ID` - Google Ads/gtag ID for web analytics (optional)
- `GOOGLE_CONVERSION_LABEL` - Google Ads conversion label used on `/auth/establish?signup=true` (optional)
- `SENDGRID_API_KEY` - To allow sending weekly recipe lists via email

if you're
- `ENABLE_MOCKS` - For testing if you have none of the above

## Development
See agents.md for some more but 
go test ./... on any go change 
and 
```
bash tailwind/generate.sh
```
if you change input css or any *.html

## Safeway Weekly Ads

The repo now includes a batch scraper for full Safeway weekly-ad page images plus OpenAI ingredient extraction:

```bash
go run ./cmd/safewayads -start-store 490 -end-store 492 -limit 1
```

Useful flags:
- `-delay 5s` to slow the crawl between stores
- `-resume=true` to skip stores already marked `success`
- `-extract=false` to archive all rendered ad pages and run metadata without calling OpenAI
- `SAFEWAYADS_STORAGE_CONTAINER=safewayads` to write weekly-ad artifacts into the dedicated Azure blob container instead of `recipes`

Safeway weekly-ad artifacts are stored under the prefixes documented in [docs/cache-layout.md](docs/cache-layout.md).

For Kubernetes backfills:

```bash
docker build -f Dockerfile.safewayads -t ghcr.io/paulgmiller/careme-safewayads:${IMAGE_TAG} .
kubectl create -f deploy/safewayads-job.yaml
```

The job iterates stores `001` through `500` with `-delay=1m` and writes to the Azure blob container `safewayads`.

## Cache Key Layout
See [docs/cache-layout.md](docs/cache-layout.md) for the authoritative cache key/prefix layout and backend notes.

## Frontend Approach
- Prefer server-rendered HTML and HTMX for interactive behavior.
- Avoid SPA-style architecture for routine page interactions.
- Keep custom JavaScript minimal and focused on browser-only APIs.
- Migration plan: [docs/htmx-migration-plan.md](docs/htmx-migration-plan.md)

## Live site

* Uptime https://stats.uptimerobot.com/ehEFlvlNM9
* Cloudflare for dns and https proxying
