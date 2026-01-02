Careme is your personal chef and sommilier. It will

1. Take yoru favorite grocery store based on location
2. Check the stores inventory for fresh meat and seasonal produce
3. Generate a weekly meal plan from a variety of cuisines and cooking styles.


## Development

### Building CSS

The application uses Tailwind CSS for styling. The built CSS file (`static/output.css`) is committed to the repository, so **you don't need to build it** to run the application with `go run ./cmd/careme -serve`.

To regenerate the CSS file after making changes to templates or Tailwind configuration:

```bash
# Quick rebuild (recommended)
./build-css.sh

# Or manually from the tailwind directory:
cd tailwind
npm install  # First time only
npm run build:css
cd ..

# Watch for changes during development
cd tailwind && npm run watch:css
```

The color scheme is defined in `internal/seasons/seasons.go` and changes dynamically based on the current season (Winter: blue, Spring: green, Summer: yellow, Fall: orange). The Tailwind configuration in `tailwind/` uses CSS variables that are set at runtime by the Go templates.

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

