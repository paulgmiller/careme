# Careme TODO List

## High Priority
- [x] Create todo.md file with project tasks
- [ ] Initialize Go module and create basic project structure
- [ ] Create main.go with basic CLI input handling for location
- [ ] Set up configuration structure for AI models (ChatGPT/Anthropic)

## Medium Priority
- [ ] Create package structure for MCP server integration (Kroger API)
- [ ] Create package for Epicurious Seasonal Ingredient Map integration
- [ ] Create recipe history storage and retrieval system
- [ ] Implement recipe generation logic using AI models

## Low Priority
- [ ] Create recipe output formatting and display
- [ ] Add error handling and logging throughout the application

## Project Structure Overview
```
careme/
├── cmd/
│   └── careme/
│       └── main.go
├── internal/
│   ├── config/
│   ├── kroger/
│   ├── ingredients/
│   ├── history/
│   ├── ai/
│   └── recipes/
├── pkg/
├── go.mod
├── go.sum
└── README.md
```

## Notes
- Application takes location as input
- Generates 4 recipes per week
- Uses ChatGPT or Anthropic models
- Integrates with Kroger API via MCP server
- Uses Epicurious Seasonal Ingredient Map
- Maintains 2-week recipe history to avoid repetition