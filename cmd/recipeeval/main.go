package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	evalrecipes "careme/internal/evals/recipes"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func main() {
	var (
		casesPath = flag.String("cases", evalrecipes.DefaultCasesPath, "path to the eval case jsonl file")
		create    = flag.Bool("create", false, "create the eval definition")
		dryRun    = flag.Bool("dry-run", false, "validate cases and print payloads without calling OpenAI")
		evalID    = flag.String("eval-id", "", "existing eval id to run")
		model     = flag.String("model", openai.ChatModelGPT5_4, "model to evaluate")
		run       = flag.Bool("run", false, "create an eval run")
	)
	flag.Parse()

	if !*dryRun && !*create && !*run {
		exitf("specify at least one of -dry-run, -create, or -run")
	}

	cases, err := evalrecipes.LoadCases(*casesPath)
	if err != nil {
		exitf("load cases: %v", err)
	}

	createReq := evalrecipes.BuildCreateEvalRequest()
	runReq := evalrecipes.BuildCreateRunRequest(*model, cases)

	if *dryRun {
		printJSON("create_eval_request", createReq)
		printJSON("create_run_request", runReq)
		return
	}

	apiKey := openAIKey()
	if apiKey == "" {
		exitf("AI_API_KEY or OPENAI_API_KEY must be set")
	}

	client := openai.NewClient(option.WithAPIKey(apiKey))
	ctx := context.Background()

	createdEvalID := strings.TrimSpace(*evalID)
	if *create {
		var resp evalrecipes.EvalResponse
		if err := client.Post(ctx, "/evals", createReq, &resp); err != nil {
			exitf("create eval: %v", err)
		}
		if resp.ID == "" {
			exitf("create eval: empty eval id in response")
		}
		createdEvalID = resp.ID
		fmt.Printf("created eval %s\n", resp.ID)
	}

	if *run {
		if createdEvalID == "" {
			exitf("-run requires -eval-id or -create")
		}
		var resp evalrecipes.RunResponse
		path := fmt.Sprintf("/evals/%s/runs", createdEvalID)
		if err := client.Post(ctx, path, runReq, &resp); err != nil {
			exitf("create eval run: %v", err)
		}
		if resp.ID == "" {
			exitf("create eval run: empty run id in response")
		}
		fmt.Printf("created eval run %s for %s\n", resp.ID, createdEvalID)
		if resp.ReportURL != "" {
			fmt.Printf("report url: %s\n", resp.ReportURL)
		}
	}
}

func openAIKey() string {
	if key := os.Getenv("AI_API_KEY"); key != "" {
		return key
	}
	return os.Getenv("OPENAI_API_KEY")
}

func printJSON(label string, value any) {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		exitf("marshal %s: %v", label, err)
	}
	fmt.Printf("%s\n%s\n", label, payload)
}

func exitf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
