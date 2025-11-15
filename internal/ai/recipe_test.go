package ai

import "testing"

func TestRecipeComputeHash(t *testing.T) {
	recipe := Recipe{
		Title:       "Test Recipe",
		Description: "A delicious test recipe",
		Ingredients: []Ingredient{
			{Name: "Ingredient 1", Quantity: "1 cup", Price: "2.99"},
			{Name: "Ingredient 2", Quantity: "2 tbsp", Price: "0.99"},
		},
		Instructions: []string{"Step 1", "Step 2"},
		Health:       "Healthy",
		DrinkPairing: "Red Wine",
	}

	hash1 := recipe.ComputeHash()
	if hash1 == "" {
		t.Fatal("hash should not be empty")
	}

	// Hash should be consistent
	hash2 := recipe.ComputeHash()
	if hash1 != hash2 {
		t.Fatalf("hash should be consistent: %s != %s", hash1, hash2)
	}

	// Different recipe should have different hash
	recipe2 := recipe
	recipe2.Title = "Different Recipe"
	hash3 := recipe2.ComputeHash()
	if hash1 == hash3 {
		t.Fatalf("different recipes should have different hashes")
	}
}

func TestRecipeHashLength(t *testing.T) {
	recipe := Recipe{
		Title: "Simple Recipe",
	}

	hash := recipe.ComputeHash()
	// SHA256 produces 64 hex characters
	if len(hash) != 64 {
		t.Fatalf("expected hash length of 64, got %d", len(hash))
	}
}

func TestEncodeIngredientsToTOON(t *testing.T) {
	tests := []struct {
		name        string
		ingredients []IngredientData
		expected    string
	}{
		{
			name:        "empty ingredients",
			ingredients: []IngredientData{},
			expected:    "ingredients[0]:",
		},
		{
			name: "single ingredient with all fields",
			ingredients: []IngredientData{
				{
					Brand:        stringPtr("Kroger"),
					Description:  stringPtr("Fresh Chicken Breast"),
					Size:         stringPtr("1 lb"),
					PriceRegular: float32Ptr(8.99),
					PriceSale:    float32Ptr(6.99),
				},
			},
			expected: `ingredients[1]{brand,description,size,regularPrice,salePrice}:
  Kroger,Fresh Chicken Breast,1 lb,8.99,6.99
`,
		},
		{
			name: "multiple ingredients",
			ingredients: []IngredientData{
				{
					Brand:        stringPtr("Simple Truth"),
					Description:  stringPtr("Organic Salmon"),
					Size:         stringPtr("8 oz"),
					PriceRegular: float32Ptr(12.99),
					PriceSale:    float32Ptr(9.99),
				},
				{
					Brand:        stringPtr("Kroger"),
					Description:  stringPtr("Fresh Broccoli"),
					Size:         stringPtr("1 bunch"),
					PriceRegular: float32Ptr(2.49),
					PriceSale:    nil,
				},
			},
			expected: `ingredients[2]{brand,description,size,regularPrice,salePrice}:
  Simple Truth,Organic Salmon,8 oz,12.99,9.99
  Kroger,Fresh Broccoli,1 bunch,2.49,
`,
		},
		{
			name: "ingredient with nil fields",
			ingredients: []IngredientData{
				{
					Brand:        nil,
					Description:  stringPtr("Generic Item"),
					Size:         nil,
					PriceRegular: nil,
					PriceSale:    float32Ptr(3.50),
				},
			},
			expected: `ingredients[1]{brand,description,size,regularPrice,salePrice}:
  "",Generic Item,"",,3.5
`,
		},
		{
			name: "ingredient with special characters requiring quotes",
			ingredients: []IngredientData{
				{
					Brand:        stringPtr("Bob's Brand"),
					Description:  stringPtr("Item, with comma"),
					Size:         stringPtr("12 oz"),
					PriceRegular: float32Ptr(5.00),
					PriceSale:    float32Ptr(4.00),
				},
			},
			expected: `ingredients[1]{brand,description,size,regularPrice,salePrice}:
  Bob's Brand,"Item, with comma",12 oz,5,4
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := encodeIngredientsToTOON(tt.ingredients)
			if result != tt.expected {
				t.Errorf("encodeIngredientsToTOON() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestQuoteIfNeeded(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple unquoted string",
			input:    "test",
			expected: "test",
		},
		{
			name:     "empty string",
			input:    "",
			expected: `""`,
		},
		{
			name:     "string with comma",
			input:    "test,value",
			expected: `"test,value"`,
		},
		{
			name:     "string with colon",
			input:    "test:value",
			expected: `"test:value"`,
		},
		{
			name:     "string with leading space",
			input:    " test",
			expected: `" test"`,
		},
		{
			name:     "string with trailing space",
			input:    "test ",
			expected: `"test "`,
		},
		{
			name:     "boolean-like string",
			input:    "true",
			expected: `"true"`,
		},
		{
			name:     "null-like string",
			input:    "null",
			expected: `"null"`,
		},
		{
			name:     "string with quotes",
			input:    `say "hi"`,
			expected: `"say \"hi\""`,
		},
		{
			name:     "string with backslash",
			input:    `C:\Users`,
			expected: `"C:\\Users"`,
		},
		{
			name:     "string with newline",
			input:    "line1\nline2",
			expected: `"line1\nline2"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := quoteIfNeeded(tt.input)
			if result != tt.expected {
				t.Errorf("quoteIfNeeded(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFloatToString(t *testing.T) {
	tests := []struct {
		name     string
		input    *float32
		expected string
	}{
		{
			name:     "nil float",
			input:    nil,
			expected: "",
		},
		{
			name:     "integer value",
			input:    float32Ptr(5.0),
			expected: "5",
		},
		{
			name:     "decimal value",
			input:    float32Ptr(5.99),
			expected: "5.99",
		},
		{
			name:     "value with trailing zeros",
			input:    float32Ptr(5.50),
			expected: "5.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := floatToString(tt.input)
			if result != tt.expected {
				t.Errorf("floatToString(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// Helper functions for tests
func stringPtr(s string) *string {
	return &s
}

func float32Ptr(f float32) *float32 {
	return &f
}
