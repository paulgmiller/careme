package recipes

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"careme/internal/ai"
)

const (
	DefaultCasesPath = "evals/recipes/regression_cases.jsonl"
	evalName         = "careme-recipe-regression"
	pythonImageTag   = "2025-05-08"
)

type CaseFileRow struct {
	CaseID              string         `json:"case_id"`
	Date                string         `json:"date"`
	Directive           string         `json:"directive,omitempty"`
	ExpectedRecipeCount int            `json:"expected_recipe_count"`
	ForbiddenTerms      []string       `json:"forbidden_terms,omitempty"`
	FutureLabels        map[string]any `json:"future_labels,omitempty"`
	IngredientsPath     string         `json:"ingredients_path"`
	Instructions        string         `json:"instructions,omitempty"`
	LastRecipes         []string       `json:"last_recipes,omitempty"`
	LocationState       string         `json:"location_state"`
	Notes               string         `json:"notes,omitempty"`
	RequiredTerms       []string       `json:"required_terms,omitempty"`
}

type Case struct {
	CaseFileRow
	DateValue      time.Time
	IngredientsTSV string
	InputMessages  []ai.EvalMessage
}

type CreateEvalRequest struct {
	DataSourceConfig map[string]any `json:"data_source_config"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	Name             string         `json:"name,omitempty"`
	TestingCriteria  []any          `json:"testing_criteria"`
}

type CreateRunRequest struct {
	DataSource map[string]any `json:"data_source"`
	Name       string         `json:"name,omitempty"`
}

type EvalResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type RunResponse struct {
	EvalID    string `json:"eval_id"`
	ID        string `json:"id"`
	Model     string `json:"model"`
	Name      string `json:"name"`
	ReportURL string `json:"report_url"`
	Status    string `json:"status"`
}

func LoadCases(path string) ([]Case, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open cases: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close cases file: %w", closeErr))
		}
	}()

	var (
		cases   []Case
		lineNum int
		seenIDs = map[string]struct{}{}
		baseDir = filepath.Dir(path)
	)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var row CaseFileRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("decode case line %d: %w", lineNum, err)
		}
		if err := validateCaseRow(&row, seenIDs); err != nil {
			return nil, fmt.Errorf("case line %d: %w", lineNum, err)
		}

		dateValue, err := time.Parse("2006-01-02", row.Date)
		if err != nil {
			return nil, fmt.Errorf("parse date for case %q: %w", row.CaseID, err)
		}

		ingredientsPath := row.IngredientsPath
		if !filepath.IsAbs(ingredientsPath) {
			ingredientsPath = filepath.Join(baseDir, ingredientsPath)
		}
		ingredientsTSVBytes, err := os.ReadFile(ingredientsPath)
		if err != nil {
			return nil, fmt.Errorf("read ingredients fixture for case %q: %w", row.CaseID, err)
		}
		ingredientsTSV := strings.TrimSpace(string(ingredientsTSVBytes))
		ingredientCount, err := countTSVRows(ingredientsTSV)
		if err != nil {
			return nil, fmt.Errorf("count ingredients for case %q: %w", row.CaseID, err)
		}

		inputMessages := ai.RecipeEvalMessages(
			row.LocationState,
			ingredientsTSV,
			ingredientCount,
			[]string{row.Directive, row.Instructions},
			dateValue,
			row.LastRecipes,
		)

		cases = append(cases, Case{
			CaseFileRow:    row,
			DateValue:      dateValue,
			IngredientsTSV: ingredientsTSV,
			InputMessages:  inputMessages,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan cases: %w", err)
	}
	if len(cases) == 0 {
		return nil, errors.New("no eval cases found")
	}
	return cases, nil
}

func BuildCreateEvalRequest() CreateEvalRequest {
	return CreateEvalRequest{
		Name: evalName,
		DataSourceConfig: map[string]any{
			"type":                  "custom",
			"include_sample_schema": true,
			"item_schema":           itemSchema(),
		},
		Metadata: map[string]any{
			"suite": "recipe_regression",
		},
		TestingCriteria: testingCriteria(),
	}
}

func BuildCreateRunRequest(model string, cases []Case) CreateRunRequest {
	content := make([]map[string]any, 0, len(cases))
	for _, c := range cases {
		content = append(content, map[string]any{
			"item": c.item(),
		})
	}

	return CreateRunRequest{
		Name: fmt.Sprintf("recipe-regression-%s", model),
		DataSource: map[string]any{
			"type": "responses",
			"source": map[string]any{
				"type":    "file_content",
				"content": content,
			},
			"input_messages": map[string]any{
				"type":           "item_reference",
				"item_reference": "item.input_messages",
			},
			"model": model,
			"sampling_params": map[string]any{
				"response_format": map[string]any{
					"type": "json_schema",
					"json_schema": map[string]any{
						"name":   "recipes",
						"schema": ai.RecipeEvalSchema(),
					},
				},
			},
		},
	}
}

func (c Case) item() map[string]any {
	item := map[string]any{
		"case_id":               c.CaseID,
		"date":                  c.Date,
		"directive":             c.Directive,
		"expected_recipe_count": c.ExpectedRecipeCount,
		"forbidden_terms":       c.ForbiddenTerms,
		"future_labels":         c.FutureLabels,
		"ingredients_tsv":       c.IngredientsTSV,
		"input_messages":        c.InputMessages,
		"instructions":          c.Instructions,
		"last_recipes":          c.LastRecipes,
		"location_state":        c.LocationState,
		"notes":                 c.Notes,
		"required_terms":        c.RequiredTerms,
	}
	if item["future_labels"] == nil {
		item["future_labels"] = map[string]any{}
	}
	return item
}

func itemSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"case_id":               map[string]any{"type": "string"},
			"date":                  map[string]any{"type": "string"},
			"directive":             map[string]any{"type": "string"},
			"expected_recipe_count": map[string]any{"type": "integer"},
			"forbidden_terms": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"future_labels": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
			"ingredients_tsv": map[string]any{"type": "string"},
			"instructions":    map[string]any{"type": "string"},
			"last_recipes": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"location_state": map[string]any{"type": "string"},
			"notes":          map[string]any{"type": "string"},
			"required_terms": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"input_messages": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"content": map[string]any{"type": "string"},
						"role":    map[string]any{"type": "string"},
						"type":    map[string]any{"type": "string"},
					},
					"required": []string{"content", "role"},
				},
			},
		},
		"required": []string{
			"case_id",
			"date",
			"expected_recipe_count",
			"forbidden_terms",
			"ingredients_tsv",
			"input_messages",
			"last_recipes",
			"location_state",
			"required_terms",
		},
	}
}

func testingCriteria() []any {
	return []any{
		pythonCriterion("required_fields", requiredFieldsPython(), 1.0),
		pythonCriterion("recipe_count", recipeCountPython(), 1.0),
		pythonCriterion("forbidden_terms", forbiddenTermsPython(), 1.0),
		pythonCriterion("required_terms", requiredTermsPython(), 1.0),
		pythonCriterion("unique_titles", uniqueTitlesPython(), 1.0),
		pythonCriterion("wine_styles_format", wineStylesPython(), 1.0),
		pythonCriterion("priced_ingredients_present", pricedIngredientsPresentPython(), 1.0),
		pythonCriterion("price_fidelity", priceFidelityPython(), 1.0),
		pythonCriterion("prior_recipe_avoidance", priorRecipeAvoidancePython(), 1.0),
	}
}

func pythonCriterion(name, source string, passThreshold float64) map[string]any {
	return map[string]any{
		"type":           "python",
		"name":           name,
		"source":         source,
		"image_tag":      pythonImageTag,
		"pass_threshold": passThreshold,
	}
}

func validateCaseRow(row *CaseFileRow, seenIDs map[string]struct{}) error {
	if row == nil {
		return errors.New("nil case row")
	}
	row.CaseID = strings.TrimSpace(row.CaseID)
	if row.CaseID == "" {
		return errors.New("case_id is required")
	}
	if _, ok := seenIDs[row.CaseID]; ok {
		return fmt.Errorf("duplicate case_id %q", row.CaseID)
	}
	seenIDs[row.CaseID] = struct{}{}
	if row.ExpectedRecipeCount <= 0 {
		return fmt.Errorf("case %q must set expected_recipe_count", row.CaseID)
	}
	if strings.TrimSpace(row.Date) == "" {
		return fmt.Errorf("case %q must set date", row.CaseID)
	}
	if strings.TrimSpace(row.IngredientsPath) == "" {
		return fmt.Errorf("case %q must set ingredients_path", row.CaseID)
	}
	if strings.TrimSpace(row.LocationState) == "" {
		return fmt.Errorf("case %q must set location_state", row.CaseID)
	}
	row.ForbiddenTerms = compactStrings(row.ForbiddenTerms)
	row.LastRecipes = compactStrings(row.LastRecipes)
	row.RequiredTerms = compactStrings(row.RequiredTerms)
	return nil
}

func compactStrings(values []string) []string {
	values = slices.Clone(values)
	out := values[:0]
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func countTSVRows(tsv string) (int, error) {
	lines := strings.Split(strings.TrimSpace(tsv), "\n")
	if len(lines) < 2 {
		return 0, errors.New("tsv fixture must include a header and at least one row")
	}
	if !strings.Contains(lines[0], "\t") {
		return 0, errors.New("tsv fixture header must be tab separated")
	}
	count := 0
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		count++
	}
	if count == 0 {
		return 0, errors.New("tsv fixture must include at least one data row")
	}
	return count, nil
}

func requiredFieldsPython() string {
	return `
from typing import Any

def grade(sample: dict[str, Any], item: dict[str, Any]) -> float:
    output = sample.get("output_json") or {}
    recipes = output.get("recipes")
    if not isinstance(recipes, list) or not recipes:
        return 0.0
    required_strings = ["title", "description", "cook_time", "cost_estimate", "health"]
    for recipe in recipes:
        if not isinstance(recipe, dict):
            return 0.0
        for field in required_strings:
            value = recipe.get(field)
            if not isinstance(value, str) or not value.strip():
                return 0.0
        ingredients = recipe.get("ingredients")
        instructions = recipe.get("instructions")
        if not isinstance(ingredients, list) or not ingredients:
            return 0.0
        if not isinstance(instructions, list) or not instructions:
            return 0.0
        for ingredient in ingredients:
            if not isinstance(ingredient, dict):
                return 0.0
            if not str(ingredient.get("name", "")).strip():
                return 0.0
            if not str(ingredient.get("quantity", "")).strip():
                return 0.0
        for instruction in instructions:
            if not isinstance(instruction, str) or not instruction.strip():
                return 0.0
    return 1.0
`
}

func recipeCountPython() string {
	return `
from typing import Any

def grade(sample: dict[str, Any], item: dict[str, Any]) -> float:
    output = sample.get("output_json") or {}
    recipes = output.get("recipes")
    if not isinstance(recipes, list):
        return 0.0
    expected = int(item.get("expected_recipe_count", 0))
    return 1.0 if len(recipes) == expected else 0.0
`
}

func forbiddenTermsPython() string {
	return `
from typing import Any
import json

def grade(sample: dict[str, Any], item: dict[str, Any]) -> float:
    forbidden = [str(term).strip().lower() for term in item.get("forbidden_terms", []) if str(term).strip()]
    if not forbidden:
        return 1.0
    haystack = json.dumps(sample.get("output_json") or sample.get("output_text") or "", ensure_ascii=False).lower()
    for term in forbidden:
        if term and term in haystack:
            return 0.0
    return 1.0
`
}

func requiredTermsPython() string {
	return `
from typing import Any
import json

def grade(sample: dict[str, Any], item: dict[str, Any]) -> float:
    required = [str(term).strip().lower() for term in item.get("required_terms", []) if str(term).strip()]
    if not required:
        return 1.0
    haystack = json.dumps(sample.get("output_json") or sample.get("output_text") or "", ensure_ascii=False).lower()
    for term in required:
        if term not in haystack:
            return 0.0
    return 1.0
`
}

func uniqueTitlesPython() string {
	return `
from typing import Any

def normalize(value: str) -> str:
    return " ".join(value.lower().split())

def grade(sample: dict[str, Any], item: dict[str, Any]) -> float:
    output = sample.get("output_json") or {}
    recipes = output.get("recipes")
    if not isinstance(recipes, list):
        return 0.0
    titles = [normalize(str(recipe.get("title", ""))) for recipe in recipes]
    if any(not title for title in titles):
        return 0.0
    return 1.0 if len(titles) == len(set(titles)) else 0.0
`
}

func wineStylesPython() string {
	return `
from typing import Any

def grade(sample: dict[str, Any], item: dict[str, Any]) -> float:
    output = sample.get("output_json") or {}
    recipes = output.get("recipes")
    if not isinstance(recipes, list):
        return 0.0
    for recipe in recipes:
        styles = recipe.get("wine_styles") or []
        if not isinstance(styles, list):
            return 0.0
        if len(styles) > 2:
            return 0.0
        for style in styles:
            value = str(style).strip()
            lower = value.lower()
            if not value:
                return 0.0
            if "," in value or "(" in value or ")" in value:
                return 0.0
            if " or " in lower or "blend" in lower:
                return 0.0
    return 1.0
`
}

func pricedIngredientsPresentPython() string {
	return `
from typing import Any

def grade(sample: dict[str, Any], item: dict[str, Any]) -> float:
    output = sample.get("output_json") or {}
    recipes = output.get("recipes")
    if not isinstance(recipes, list):
        return 0.0
    for recipe in recipes:
        ingredients = recipe.get("ingredients") or []
        if not isinstance(ingredients, list):
            return 0.0
        for ingredient in ingredients:
            if isinstance(ingredient, dict) and str(ingredient.get("price", "")).strip():
                return 1.0
    return 0.0
`
}

func priceFidelityPython() string {
	return `
from typing import Any
import csv
import io
import re

def normalize_text(value: str) -> str:
    lowered = value.lower()
    lowered = re.sub(r"[^a-z0-9]+", " ", lowered)
    return " ".join(lowered.split())

def normalize_price(value: str) -> str:
    text = str(value).strip()
    if not text:
        return ""
    text = text.replace("$", "")
    match = re.search(r"([0-9]+(?:\.[0-9]{1,2})?)", text)
    if not match:
        return ""
    return f"{float(match.group(1)):.2f}"

def parse_rows(tsv: str) -> list[dict[str, Any]]:
    rows = []
    reader = csv.DictReader(io.StringIO(tsv), delimiter="\t")
    for row in reader:
        description = normalize_text(row.get("Description", ""))
        prices = {normalize_price(row.get("PriceRegular", "")), normalize_price(row.get("PriceSale", ""))}
        prices.discard("")
        rows.append({"description": description, "prices": prices})
    return rows

def has_matching_row(name: str, price: str, rows: list[dict[str, Any]]) -> bool:
    normalized_name = normalize_text(name)
    name_tokens = set(normalized_name.split())
    if not normalized_name or not price:
        return False
    for row in rows:
        if price not in row["prices"]:
            continue
        description = row["description"]
        desc_tokens = set(description.split())
        if normalized_name in description or description in normalized_name:
            return True
        if name_tokens and desc_tokens and len(name_tokens & desc_tokens) >= 1:
            return True
    return False

def grade(sample: dict[str, Any], item: dict[str, Any]) -> float:
    rows = parse_rows(str(item.get("ingredients_tsv", "")))
    if not rows:
        return 0.0
    output = sample.get("output_json") or {}
    recipes = output.get("recipes")
    if not isinstance(recipes, list):
        return 0.0
    for recipe in recipes:
        for ingredient in recipe.get("ingredients", []) or []:
            if not isinstance(ingredient, dict):
                return 0.0
            price = normalize_price(ingredient.get("price", ""))
            if not price:
                continue
            if not has_matching_row(str(ingredient.get("name", "")), price, rows):
                return 0.0
    return 1.0
`
}

func priorRecipeAvoidancePython() string {
	return `
from typing import Any
from rapidfuzz import fuzz

def normalize(value: str) -> str:
    return " ".join(str(value).lower().split())

def grade(sample: dict[str, Any], item: dict[str, Any]) -> float:
    prior = [normalize(v) for v in item.get("last_recipes", []) if normalize(v)]
    if not prior:
        return 1.0
    output = sample.get("output_json") or {}
    recipes = output.get("recipes")
    if not isinstance(recipes, list):
        return 0.0
    for recipe in recipes:
        title = normalize(recipe.get("title", ""))
        if not title:
            return 0.0
        for previous in prior:
            if fuzz.WRatio(title, previous) >= 90:
                return 0.0
    return 1.0
`
}
