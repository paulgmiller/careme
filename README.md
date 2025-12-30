Careme is your personal chef and sommilier. It will

1. Take yoru favorite grocery store based on location
2. Check the stores inventory for fresh meat and seasonal produce
3. Generate a weekly meal plan from a variety of cuisines and cooking styles.


## Development

### Building CSS

The application uses Tailwind CSS for styling with a local build process. The CSS needs to be built before running the application:

```bash
# Install dependencies (first time only)
npm install

# Build CSS for production
npm run build:css

# Watch for changes during development
npm run watch:css
```

The Tailwind configuration supports dynamic seasonal colors that change throughout the year (Winter: blue, Spring: green, Summer: yellow, Fall: orange).

## Configuration

The application is configured via environment variables:

- `KROGER_CLIENT_ID` - Kroger API client ID (required)
- `KROGER_CLIENT_SECRET` - Kroger API client secret (required)
- `AI_API_KEY` - OpenAI or Anthropic API key (required)
- `CLARITY_PROJECT_ID` - Microsoft Clarity project ID for web analytics (optional)
- `HISTORY_PATH` - Path to store recipe history (default: "./data/history.json")

## Live site

* Uptime https://stats.uptimerobot.com/ehEFlvlNM9
* Cloudflare for dns and https proxying

