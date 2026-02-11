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

For JavaScript moved out of templates:
```
bash scripts/lint-js.sh
```

## Live site

* Uptime https://stats.uptimerobot.com/ehEFlvlNM9
* Cloudflare for dns and https proxying
