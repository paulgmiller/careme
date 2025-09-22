# Careme - Personal Chef and Sommelier

Careme is a Go-based personal chef and sommelier application that integrates with Kroger API to check store inventory and uses AI to generate weekly meal plans.

Always reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.

## Working Effectively

### Bootstrap and Build
- Install Go 1.24+ if not present
- Download dependencies: `go mod download` -- takes 5-10 seconds
- Build the application: `go build -o careme ./cmd/careme` -- takes 30-45 seconds. NEVER CANCEL. Set timeout to 60+ minutes for safety
- Alternative clean build: `rm -f careme && go build -o careme ./cmd/careme`

### Testing
- Run working tests: `go test ./internal/html ./internal/locations -v` -- takes under 5 seconds
- WARNING: `go test ./...` WILL FAIL due to broken tests in internal/recipes package. This is expected and NOT your responsibility to fix
- Run specific test modules that work: `go test ./internal/html ./internal/locations`
- Test execution time: Under 10 seconds for working modules. NEVER CANCEL. Set timeout to 30+ minutes for safety

### Code Quality
- Format code: `go fmt ./...` -- takes 1-2 seconds
- Lint code: `go vet ./...` -- takes 2-5 seconds, but WILL FAIL due to existing test issues (ignore these)
- No golangci-lint is configured

### Running the Application

#### CLI Mode (requires API credentials)
- Test zipcode lookup: `./careme -zipcode 98101`
- Generate recipes: `./careme -location 70100023` 
- List ingredients: `./careme -location 70100023 -ingredient "chicken"`
- ALL CLI commands require valid environment variables (see Configuration below)

#### Web Server Mode
- Start server: `./careme -serve` (default port 8080)
- Custom port: `./careme -serve -addr :8888`
- Health check endpoint: `curl http://localhost:8080/ready` (returns "OK")
- Home page: `curl http://localhost:8080/` (returns HTML form)
- Locations endpoint: `curl "http://localhost:8080/locations?zip=98101"` (requires API access)

## Configuration

The application requires these environment variables for full functionality:

### Required for ANY operation:
```bash
export KROGER_CLIENT_ID="your_kroger_client_id"
export KROGER_CLIENT_SECRET="your_kroger_client_secret" 
export AI_API_KEY="your_openai_or_anthropic_key"
```

### Optional:
```bash
export AI_PROVIDER="openai"  # or "anthropic", default: "openai"
export AI_MODEL="gpt-4"      # default: "gpt-4"
export CLARITY_PROJECT_ID="your_clarity_id"  # for web analytics
export HISTORY_PATH="./data/history.json"    # default path
```

### Testing Without Real Credentials
For testing server startup and basic endpoints:
```bash
KROGER_CLIENT_ID=test KROGER_CLIENT_SECRET=test AI_API_KEY=test ./careme -serve
```
This allows server to start but external API calls will fail (expected).

## Validation

### Manual Testing Scenarios
Always run these validation steps after making changes:

1. **Build and Basic Functionality**:
   ```bash
   go build -o careme ./cmd/careme
   ./careme --help  # Should show usage information
   ```

2. **Server Startup and Health Check**:
   ```bash
   KROGER_CLIENT_ID=test KROGER_CLIENT_SECRET=test AI_API_KEY=test ./careme -serve -addr :8888 &
   curl http://localhost:8888/ready  # Should return "OK"
   curl http://localhost:8888/       # Should return HTML with "Careme" title
   kill %1  # Stop the server
   ```

3. **Configuration Validation**:
   ```bash
   ./careme -zipcode 98101  # Should fail with "Kroger client ID and secret must be set"
   ```

### Expected Limitations
- Docker build may fail in sandboxed environments due to TLS certificate issues
- External API calls (Kroger, OpenAI) will fail without valid credentials or network access
- Some tests in internal/recipes package are broken (not your responsibility to fix)
- `go test ./...` will fail - use `go test ./internal/html ./internal/locations` instead

## Docker

- **Build**: `docker build -t careme .` -- may take 5-15 minutes. NEVER CANCEL. Set timeout to 60+ minutes
- **Note**: Docker build may fail in restricted environments due to TLS/certificate issues
- **Runtime**: Runs on port 8080, requires environment variables via secrets in Kubernetes

## CI/CD and Deployment

- **GitHub Actions**: `.github/workflows/go.yml` handles build and GHCR publishing
- **Kubernetes**: `deploy/deploy.yaml` contains deployment manifests
- **Image Registry**: ghcr.io/paulgmiller/careme

## Common Tasks

### Repository Structure
```
.
├── cmd/careme/          # Main application
│   ├── main.go         # CLI entry point
│   ├── web.go          # Web server handlers
│   ├── templates.go    # Template loading
│   └── html/           # HTML templates
├── internal/
│   ├── ai/             # AI integration (OpenAI/Anthropic)
│   ├── cache/          # Caching layer
│   ├── config/         # Configuration management
│   ├── history/        # Recipe history
│   ├── html/           # HTML utilities
│   ├── kroger/         # Kroger API integration
│   ├── locations/      # Store location handling
│   └── recipes/        # Recipe generation and formatting
├── deploy/             # Kubernetes manifests
├── .github/workflows/  # CI/CD pipelines
├── Dockerfile          # Container build
├── go.mod             # Go module definition
└── README.md
```

### Frequently Used Commands
```bash
# Quick development cycle
go build -o careme ./cmd/careme && ./careme --help

# Test server locally
KROGER_CLIENT_ID=test KROGER_CLIENT_SECRET=test AI_API_KEY=test ./careme -serve

# Run working tests
go test ./internal/html ./internal/locations -v

# Format and check code
go fmt ./... && go vet ./...
```

### Key Files to Check When Making Changes
- Always check `internal/config/config.go` after modifying environment variable handling
- Always check `cmd/careme/main.go` after modifying CLI flags or application entry points
- Always check `cmd/careme/web.go` after modifying HTTP endpoints
- Always check `internal/recipes/` after modifying recipe generation logic

### Build Time Expectations
- **Go Build**: 30-45 seconds for clean build, 1-5 seconds for incremental
- **Go Test** (working modules): Under 5 seconds
- **Docker Build**: 5-15 minutes (may fail in restricted environments)
- **NEVER CANCEL** any build or test command. Set timeouts to 60+ minutes to be safe.

## Troubleshooting

### Common Issues
1. **"Kroger client ID and secret must be set"**: Set environment variables as shown in Configuration
2. **Docker build fails with TLS errors**: Expected in sandboxed environments, try local build instead
3. **Tests fail in internal/recipes**: Expected, focus on other modules
4. **External API failures**: Expected without valid credentials or in restricted networks

### Debug Server Issues
1. Check server logs for specific error messages
2. Use `/ready` endpoint to verify server is responding
3. Verify environment variables are set correctly
4. Test with dummy credentials for basic server functionality