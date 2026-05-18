package produce

import (
	"slices"
	"sort"
	"strings"

	"careme/internal/ai"
)

const (
	maxFamilyPoints      = 10.0
	duplicateGradeWeight = 0.35
)

type Score struct {
	Score              float64       `json:"score"`
	FamilyCount        int           `json:"family_count"`
	MatchedFamilies    int           `json:"matched_families"`
	IngredientCount    int           `json:"ingredient_count"`
	GradedCount        int           `json:"graded_count"`
	UngradedCount      int           `json:"ungraded_count"`
	MatchedGradeAvg    float64       `json:"matched_grade_average"`
	FamilyScores       []FamilyScore `json:"family_scores"`
	MissingFamilies    []string      `json:"missing_families"`
	TopMatchedFamilies []FamilyScore `json:"top_matched_families"`
}

type FamilyScore struct {
	Family          string  `json:"family"`
	Points          float64 `json:"points"`
	MatchCount      int     `json:"match_count"`
	GradedCount     int     `json:"graded_count"`
	BestGrade       int     `json:"best_grade"`
	BestProductID   string  `json:"best_product_id,omitempty"`
	BestBrand       string  `json:"best_brand,omitempty"`
	BestDescription string  `json:"best_description,omitempty"`
	gradeSum        int
}

type familyMatch struct {
	productID   string
	brand       string
	description string
	grade       int
	graded      bool
}

func ScoreIngredients(ingredients []ai.InputIngredient) Score {
	families := ReferenceFamilies()
	byFamily := make(map[string][]familyMatch, len(families))
	gradedCount := 0
	for _, ingredient := range ingredients {
		if ingredient.Grade != nil {
			gradedCount++
		}
		for _, family := range MatchFamilies(ingredient.Description) {
			match := familyMatch{
				productID:   ingredient.ProductID,
				brand:       ingredient.Brand,
				description: strings.TrimSpace(ingredient.Description),
			}
			if ingredient.Grade != nil {
				match.grade = ingredient.Grade.Score
				match.graded = true
			}
			byFamily[family] = appendUniqueFamilyMatch(byFamily[family], match)
		}
	}

	familyScores := make([]FamilyScore, 0, len(families))
	missing := make([]string, 0)
	totalPoints := 0.0
	gradeSum := 0
	matchedGradeCount := 0
	matchedFamilies := 0
	for _, family := range families {
		score := scoreFamily(family, byFamily[family])
		if score.MatchCount == 0 {
			missing = append(missing, family)
		} else {
			matchedFamilies++
		}
		totalPoints += score.Points
		gradeSum += score.gradeSum
		matchedGradeCount += score.GradedCount
		familyScores = append(familyScores, score)
	}

	topMatched := append([]FamilyScore(nil), familyScores...)
	topMatched = slices.DeleteFunc(topMatched, func(score FamilyScore) bool {
		return score.MatchCount == 0
	})
	sort.Slice(topMatched, func(i, j int) bool {
		if topMatched[i].Points == topMatched[j].Points {
			return topMatched[i].Family < topMatched[j].Family
		}
		return topMatched[i].Points > topMatched[j].Points
	})
	if len(topMatched) > 10 {
		topMatched = topMatched[:10]
	}

	result := Score{
		FamilyCount:        len(families),
		MatchedFamilies:    matchedFamilies,
		IngredientCount:    len(ingredients),
		GradedCount:        gradedCount,
		UngradedCount:      len(ingredients) - gradedCount,
		FamilyScores:       familyScores,
		MissingFamilies:    missing,
		TopMatchedFamilies: topMatched,
	}
	if len(families) > 0 {
		result.Score = totalPoints / (float64(len(families)) * maxFamilyPoints) * 100
	}
	if matchedGradeCount > 0 {
		result.MatchedGradeAvg = float64(gradeSum) / float64(matchedGradeCount)
	}
	return result
}

func appendUniqueFamilyMatch(matches []familyMatch, match familyMatch) []familyMatch {
	key := strings.TrimSpace(match.productID)
	if key == "" {
		key = strings.ToLower(match.description)
	}
	for _, existing := range matches {
		existingKey := strings.TrimSpace(existing.productID)
		if existingKey == "" {
			existingKey = strings.ToLower(existing.description)
		}
		if existingKey == key {
			return matches
		}
	}
	return append(matches, match)
}

func scoreFamily(family string, matches []familyMatch) FamilyScore {
	score := FamilyScore{
		Family:     family,
		MatchCount: len(matches),
	}
	graded := make([]familyMatch, 0, len(matches))
	for _, match := range matches {
		if !match.graded {
			continue
		}
		score.GradedCount++
		score.gradeSum += match.grade
		graded = append(graded, match)
	}
	sort.Slice(graded, func(i, j int) bool {
		if graded[i].grade == graded[j].grade {
			return graded[i].description < graded[j].description
		}
		return graded[i].grade > graded[j].grade
	})
	weight := 1.0
	for i, match := range graded {
		if i == 0 {
			score.BestGrade = match.grade
			score.BestProductID = match.productID
			score.BestBrand = match.brand
			score.BestDescription = match.description
		}
		score.Points += float64(match.grade) * weight
		if score.Points >= maxFamilyPoints {
			score.Points = maxFamilyPoints
			break
		}
		weight *= duplicateGradeWeight
	}
	return score
}
