Careme is your personal chef and sommilier. It will

1. Take yoru favorite grocery store based on location
2. Check the stores inventory for fresh meat and seasonal produce
3. Generate a weekly meal plan from a variety of cuisines and cooking styles.


## Configuration

The application is configured via environment variables:

- `KROGER_CLIENT_ID` - Kroger API client ID (required)
- `KROGER_CLIENT_SECRET` - Kroger API client secret (required)
- `AI_API_KEY` - OpenAI or Anthropic API key (required)
- `AI_PROVIDER` - AI provider ("openai" or "anthropic", default: "openai")
- `AI_MODEL` - AI model to use (default: "gpt-4")
- `CLARITY_PROJECT_ID` - Microsoft Clarity project ID for web analytics (optional)
- `HISTORY_PATH` - Path to store recipe history (default: "./data/history.json")
- `STRIPE_PAYMENT_LINK` - Stripe payment link URL for subscriptions (optional)

## Live site

* Uptime https://stats.uptimerobot.com/ehEFlvlNM9
* Cloudflare for dns and https proxying

