Careme is a golang applicationt that takes a location as input and uses the latest chat gpt or anthropic model to 
produce 4 recipes a week that can bet resourced at a local qfc or fredmeyer. The agentic model should use 
 * an mcp server for kroger api to see whats available and fresh
 * Epicurious Seasonal Ingredient Map
 * a history of the past 2 weeks recips to not repeat itself.

## Configuration

The application is configured via environment variables:

- `KROGER_CLIENT_ID` - Kroger API client ID (required)
- `KROGER_CLIENT_SECRET` - Kroger API client secret (required)
- `AI_API_KEY` - OpenAI or Anthropic API key (required)
- `AI_PROVIDER` - AI provider ("openai" or "anthropic", default: "openai")
- `AI_MODEL` - AI model to use (default: "gpt-4")
- `CLARITY_PROJECT_ID` - Microsoft Clarity project ID for web analytics (optional)
- `HISTORY_PATH` - Path to store recipe history (default: "./data/history.json")

