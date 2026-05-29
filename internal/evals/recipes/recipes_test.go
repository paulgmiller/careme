package recipes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"careme/internal/ai"
)

func TestLoadCasesBuildsInputMessages(t *testing.T) {
	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "ingredients.tsv")
	casePath := filepath.Join(dir, "cases.jsonl")

	ingredients := "ProductId\tAisleNumber\tBrand\tDescription\tSize\tPriceRegular\tPriceSale\n1\t7\tBrand\tChicken Thighs\t1 lb\t4.49\t3.99\n"
	if err := os.WriteFile(fixturePath, []byte(ingredients), 0o644); err != nil {
		t.Fatalf("write ingredients fixture: %v", err)
	}

	row := `{"case_id":"baseline","date":"2026-03-17","ingredients_path":"ingredients.tsv","location_state":"WA","expected_recipe_count":3,"directive":"Generate 3 recipes. No shellfish.","last_recipes":["Chicken Bowl"],"forbidden_terms":["shellfish"],"required_terms":[]}`
	if err := os.WriteFile(casePath, []byte(row+"\n"), 0o644); err != nil {
		t.Fatalf("write case file: %v", err)
	}

	cases, err := LoadCases(casePath)
	if err != nil {
		t.Fatalf("LoadCases returned error: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(cases))
	}
	if got := cases[0].IngredientsTSV; got != "ProductId\tAisleNumber\tBrand\tDescription\tSize\tPriceRegular\tPriceSale\n1\t7\tBrand\tChicken Thighs\t1 lb\t4.49\t3.99" {
		t.Fatalf("unexpected tsv content: %q", got)
	}
	if len(cases[0].InputMessages) < 6 {
		t.Fatalf("expected multiple input messages, got %d", len(cases[0].InputMessages))
	}
	if cases[0].InputMessages[0].Role != "system" {
		t.Fatalf("expected system role for first message, got %q", cases[0].InputMessages[0].Role)
	}
}

func TestBuildCreateRunRequestUsesItemReferenceMessages(t *testing.T) {
	cases := []Case{
		{
			CaseFileRow: CaseFileRow{
				CaseID:              "baseline",
				Date:                "2026-03-17",
				Directive:           "Generate 3 recipes.",
				ExpectedRecipeCount: 3,
				ForbiddenTerms:      []string{"shellfish"},
				LocationState:       "WA",
			},
			IngredientsTSV: "ProductId\tAisleNumber\tBrand\tDescription\tSize\tPriceRegular\tPriceSale\n1\t7\tBrand\tChicken Thighs\t1 lb\t4.49\t3.99",
			InputMessages: []ai.EvalMessage{
				{Role: "user", Content: "hello", Type: "message"},
			},
		},
	}

	req := BuildCreateRunRequest("gpt-5.4", cases)
	if got := req.DataSource["type"]; got != "responses" {
		t.Fatalf("unexpected data source type: %v", got)
	}
	inputMessages, ok := req.DataSource["input_messages"].(map[string]any)
	if !ok {
		t.Fatalf("input_messages has unexpected type: %T", req.DataSource["input_messages"])
	}
	if got := inputMessages["item_reference"]; got != "item.input_messages" {
		t.Fatalf("unexpected item_reference: %v", got)
	}

	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal run request: %v", err)
	}
	if !json.Valid(payload) {
		t.Fatalf("run request is not valid json: %s", payload)
	}
}
