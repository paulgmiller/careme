package recipes

import (
	"slices"
	"strings"
	"unicode"

	"careme/internal/ai"
	"careme/internal/recipes/critique"

	"github.com/samber/lo"
)

func linkToParents(garbage []critique.Result, newRecipes []*ai.Recipe) {
	parents := lo.Map(garbage, func(result critique.Result, _ int) *ai.Recipe {
		return result.Recipe
	})
	applyParentHashesByTitleMatch(parents, newRecipes)
}

func recipePtrs(recipes []ai.Recipe) []*ai.Recipe {
	ptrs := make([]*ai.Recipe, 0, len(recipes))
	for i := range recipes {
		ptrs = append(ptrs, &recipes[i])
	}
	return ptrs
}

func applyParentHashesByTitleMatch(parents []*ai.Recipe, newRecipes []*ai.Recipe) {
	type candidateMatch struct {
		new    *ai.Recipe
		parent *ai.Recipe
		score  int
	}

	matches := make([]candidateMatch, 0, len(newRecipes)*len(parents))
	for _, newRecipe := range newRecipes {
		for _, parent := range parents {
			score := sharedWords(newRecipe.Title, parent.Title)
			if score == 0 {
				continue
			}
			matches = append(matches, candidateMatch{
				new:    newRecipe,
				parent: parent,
				score:  score,
			})
		}
	}

	slices.SortFunc(matches, func(a, b candidateMatch) int {
		return b.score - a.score
	})

	used := make(map[*ai.Recipe]bool, len(newRecipes))
	for _, match := range matches {
		if match.new == nil || match.parent == nil {
			continue
		}
		if used[match.new] {
			continue
		}
		if used[match.parent] {
			continue
		}
		parentHash := match.parent.ComputeHash()
		childHash := match.new.ComputeHash()
		if parentHash == "" || childHash == "" || parentHash == childHash {
			continue
		}
		match.new.ParentHash = parentHash
		used[match.new] = true
		used[match.parent] = true
	}
}

func sharedWords(a, b string) int {
	wordsA := wordSet(a)
	wordsB := wordSet(b)
	return lo.CountBy(lo.Keys(wordsA), func(word string) bool {
		return wordsB[word]
	})
}

func wordSet(title string) map[string]bool {
	wordDict := make(map[string]bool)
	s := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	for _, word := range s {
		// tiny words not valuable?
		if len(word) < 2 {
			continue
		}
		wordDict[word] = true
	}
	return wordDict
}
