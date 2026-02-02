Careme is your personal chef and sommilier. It will

1. Take your favorite grocery store based on location
2. Check the stores inventory for fresh meat and seasonal produce
3. Generate a weekly meal plan from a variety of cuisines and cooking styles.


## Configuration

The application is configured via environment variables:

- `KROGER_CLIENT_ID` - Kroger API client ID (required)
- `KROGER_CLIENT_SECRET` - Kroger API client secret (required)
- `AI_API_KEY` - OpenAI or Anthropic API key (required)
- `CLARITY_PROJECT_ID` - Microsoft Clarity project ID for web analytics (optional)
- `SENDGRID_API_KEY` - To allow sending weekly recipe lists via email
- `ENABLE_MOCKS` - For testing if you have none of the above

## Development

### Node/Tailwind

Tailwind output is pinned to Node `20.11.1` and npm `10.5.0` (see `.node-version`/`.nvmrc` and `tailwind/package.json` engines).

If you use nvm:

```
nvm install 20.11.1
nvm use
```

Then regenerate CSS:

```
bash tailwind/generate.sh
```

## Live site

* Uptime https://stats.uptimerobot.com/ehEFlvlNM9
* Cloudflare for dns and https proxying
