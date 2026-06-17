package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/recipes"
)

func main() {
	var location string
	var verbose bool
	flag.StringVar(&location, "location", "", "Location for recipe sourcing (e.g., 70100023)")
	flag.StringVar(&location, "l", "", "Location for recipe sourcing (short form)")
	flag.BoolVar(&verbose, "verbose", false, "dump all ingredients and grades")
	flag.Parse()
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %s", err)
	}

	sp, err := recipes.NewStaplesProvider(cfg)
	if err != nil {
		log.Fatalf("failed to create recipe generator: %s", err)
	}

	ings, err := sp.FetchStaples(ctx, location)
	if err != nil {
		log.Fatalf("failed to get ingredients: %s", err)
	}

	catMap := make(map[string]int)

	log.Printf("Grading %d ingredients", len(ings))
	cacheStore, err := cache.MakeCache()
	if err != nil {
		log.Fatalf("failed to create cache for ingredient grading: %s", err)
	}
	grader := ingredientgrading.NewManager(cfg, cacheStore, http.DefaultClient)
	graded, err := grader.GradeIngredients(ctx, ings)
	if err != nil {
		log.Fatalf("failed to grade ingredients: %s", err)
	}
	slices.SortFunc(graded, func(a, b ai.InputIngredient) int {
		if a.Grade.Score != b.Grade.Score {
			return b.Grade.Score - a.Grade.Score
		}
		return strings.Compare(strings.ToLower(a.Description), strings.ToLower(b.Description))
	})
	for _, result := range graded {
		for _, cat := range result.Categories {
			catMap[cat] += 1
		}
		if verbose {
			fmt.Printf(
				"%2d/10: %s - %s: regular: %s sale: %s: %s\n",
				result.Grade.Score,
				result.Brand,
				result.Description,
				priceString(result.PriceRegular),
				priceString(result.PriceSale),
				result.Grade.Reason,
			)
		}
	}
	for cat, count := range catMap {
		fmt.Printf("Category: %s, Count: %d\n", cat, count)
	}

	summary := summarizeGrades(graded)
	fmt.Println("Grade distribution:")
	printGradeDistribution(summary)
	fmt.Printf("Total count %d and score %d\n", summary.TotalCount, summary.ScoreSum)

	deduped := dedupeIngredientsBySlug(graded)
	dedupedSummary := summarizeGrades(deduped)
	fmt.Println("Deduped grade distribution:")
	printGradeDistribution(dedupedSummary)
	fmt.Printf("Deduped count %d and score %d\n", dedupedSummary.TotalCount, dedupedSummary.ScoreSum)

	printUsefulSlugGroups(groupUsefulIngredientsBySlug(graded))
}

type gradeSummary struct {
	Counts     map[int]int
	TotalCount int
	ScoreSum   int
}

func summarizeGrades(ingredients []ai.InputIngredient) gradeSummary {
	summary := gradeSummary{Counts: make(map[int]int)}
	for _, ingredient := range ingredients {
		if ingredient.Grade == nil {
			continue
		}
		summary.TotalCount++
		summary.ScoreSum += ingredient.Grade.Score
		summary.Counts[ingredient.Grade.Score]++
	}
	return summary
}

func printGradeDistribution(summary gradeSummary) {
	for score := 0; score <= 10; score++ {
		fmt.Printf("Score %2d: %d ingredients\n", score, summary.Counts[score])
	}
}

func dedupeIngredientsBySlug(ingredients []ai.InputIngredient) []ai.InputIngredient {
	byKey := make(map[string]ai.InputIngredient)
	for _, ingredient := range ingredients {
		key := ingredientDedupeKey(ingredient)
		if key == "" {
			continue
		}
		current, ok := byKey[key]
		if !ok || betterDedupeRepresentative(ingredient, current) {
			byKey[key] = ingredient
		}
	}

	deduped := make([]ai.InputIngredient, 0, len(byKey))
	for _, ingredient := range byKey {
		deduped = append(deduped, ingredient)
	}
	slices.SortFunc(deduped, func(a, b ai.InputIngredient) int {
		if gradeScore(a) != gradeScore(b) {
			return gradeScore(b) - gradeScore(a)
		}
		if cmp := strings.Compare(strings.ToLower(a.Description), strings.ToLower(b.Description)); cmp != 0 {
			return cmp
		}
		return strings.Compare(strings.ToLower(a.ProductID), strings.ToLower(b.ProductID))
	})
	return deduped
}

type slugIngredientGroup struct {
	Slug        string
	Ingredients []ai.InputIngredient
	BestGrade   int
}

func groupUsefulIngredientsBySlug(ingredients []ai.InputIngredient) []slugIngredientGroup {
	bySlug := make(map[string][]ai.InputIngredient)
	for _, ingredient := range ingredients {
		key := ingredientDedupeKey(ingredient)
		if key == "" {
			continue
		}
		bySlug[key] = append(bySlug[key], ingredient)
	}

	groups := make([]slugIngredientGroup, 0, len(bySlug))
	for slug, groupIngredients := range bySlug {
		slices.SortFunc(groupIngredients, compareIngredientsForSlugGroup)
		bestGrade := gradeScore(groupIngredients[0])
		if bestGrade <= 6 {
			continue
		}
		groups = append(groups, slugIngredientGroup{
			Slug:        slug,
			Ingredients: groupIngredients,
			BestGrade:   bestGrade,
		})
	}
	slices.SortFunc(groups, func(a, b slugIngredientGroup) int {
		if a.BestGrade != b.BestGrade {
			return b.BestGrade - a.BestGrade
		}
		return strings.Compare(a.Slug, b.Slug)
	})
	return groups
}

func printUsefulSlugGroups(groups []slugIngredientGroup) {
	fmt.Println("Useful slug groups:")
	for _, group := range groups {
		fmt.Printf("%s (%d ingredients, best %d/10)\n", group.Slug, len(group.Ingredients), group.BestGrade)
	}
}

func ingredientDedupeKey(ingredient ai.InputIngredient) string {
	if key := normalizedDedupeText(ingredient.Slug); key != "" {
		return key
	}
	if key := normalizedDedupeText(ingredient.Description); key != "" {
		return key
	}
	return normalizedDedupeText(ingredient.ProductID)
}

func normalizedDedupeText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func betterDedupeRepresentative(candidate, current ai.InputIngredient) bool {
	candidateScore := gradeScore(candidate)
	currentScore := gradeScore(current)
	if candidateScore != currentScore {
		return candidateScore > currentScore
	}
	if cmp := strings.Compare(strings.ToLower(candidate.Description), strings.ToLower(current.Description)); cmp != 0 {
		return cmp < 0
	}
	return strings.Compare(strings.ToLower(candidate.ProductID), strings.ToLower(current.ProductID)) < 0
}

func compareIngredientsForSlugGroup(a, b ai.InputIngredient) int {
	if gradeScore(a) != gradeScore(b) {
		return gradeScore(b) - gradeScore(a)
	}
	if cmp := strings.Compare(strings.ToLower(a.Description), strings.ToLower(b.Description)); cmp != 0 {
		return cmp
	}
	return strings.Compare(strings.ToLower(a.ProductID), strings.ToLower(b.ProductID))
}

func gradeScore(ingredient ai.InputIngredient) int {
	if ingredient.Grade == nil {
		return -1
	}
	return ingredient.Grade.Score
}

func gradeReason(ingredient ai.InputIngredient) string {
	if ingredient.Grade == nil {
		return ""
	}
	return ingredient.Grade.Reason
}

func priceString(price *float32) string {
	if price == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *price)
}
