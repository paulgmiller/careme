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

### Query Parameters

The `/recipes` endpoint accepts the following query parameters:

- `location` - Kroger location ID (required)
- `date` - Date for meal planning in YYYY-MM-DD format (default: today)
- `instructions` - Additional instructions for recipe generation (optional)
- `model` - AI model to use for this request (optional, overrides `AI_MODEL` env var)
  - Examples: `gpt-4o-mini`, `gpt-5-mini`, `gpt-5-nano`, `gpt-4o`, `o1-mini`
  - See [OpenAI's model documentation](https://platform.openai.com/docs/models) for available models
  - Cheaper/faster models like `gpt-4o-mini` or `gpt-5-nano` can reduce costs
- `conversation_id` - Continue a previous conversation (optional)
- `saved` - Array of recipe hashes to include (optional)
- `dismissed` - Array of recipe hashes to exclude (optional)

## Live site

* Uptime https://stats.uptimerobot.com/ehEFlvlNM9
* Cloudflare for dns and https proxying

