package main

import (
	"context"
	"fmt"
	"math/bits"
	"slices"
)

type budgetQuery struct {
	index    int
	requests int
	products []uint64
}

// optimalStudySet returns the best plan at the requested budget.
func optimalStudySet(ctx context.Context, catalogs []studyCatalog, points map[string]int, budget int) (searchSetScore, error) {
	frontier, err := optimalStudyFrontier(ctx, catalogs, points, budget)
	if err != nil {
		return searchSetScore{}, err
	}
	return frontier[len(frontier)-1], nil
}

// optimalStudyFrontier computes the best measured grade coverage at each request
// cost in one subset search. Equal scores prefer fewer requests, then searches.
// The result contains only improvements, with no artificial search-count limit.
func optimalStudyFrontier(ctx context.Context, catalogs []studyCatalog, points map[string]int, budget int) ([]searchSetScore, error) {
	if budget < 1 {
		return nil, fmt.Errorf("request budget must be positive")
	}
	positions := map[string]int{}
	for id, point := range points {
		if point > 0 {
			positions[id] = len(positions)
		}
	}
	words := (len(positions) + 63) / 64
	gradeMasks := map[int][]uint64{}
	for id, index := range positions {
		grade := points[id]
		if gradeMasks[grade] == nil {
			gradeMasks[grade] = make([]uint64, words)
		}
		gradeMasks[grade][index/64] |= uint64(1) << (index % 64)
	}
	type weightedMask struct {
		grade int
		masks []uint64
	}
	var weightedMasks []weightedMask
	for grade, masks := range gradeMasks {
		weightedMasks = append(weightedMasks, weightedMask{grade: grade, masks: masks})
	}
	var queries []budgetQuery
	for index, catalog := range catalogs {
		if catalog.Requests < 1 {
			return nil, fmt.Errorf("study query %d has invalid request count %d", index, catalog.Requests)
		}
		if catalog.Requests > budget {
			continue
		}
		query := budgetQuery{index: index, requests: catalog.Requests, products: make([]uint64, words)}
		for _, ingredient := range catalog.Ingredients {
			if pos, ok := positions[ingredient.ProductID]; ok {
				query.products[pos/64] |= uint64(1) << (pos % 64)
			}
		}
		if slices.ContainsFunc(query.products, func(word uint64) bool { return word != 0 }) {
			queries = append(queries, query)
		}
	}
	var candidates []budgetQuery
	for i, query := range queries {
		dominated := false
		for j, other := range queries {
			if i == j || other.requests > query.requests {
				continue
			}
			subset, equal := true, true
			for word, mask := range query.products {
				if mask&^other.products[word] != 0 {
					subset = false
					break
				}
				if mask != other.products[word] {
					equal = false
				}
			}
			if subset && (!equal || other.requests < query.requests || j < i) {
				dominated = true
				break
			}
		}
		if !dominated {
			candidates = append(candidates, query)
		}
	}
	// Reuse a coverage buffer at each recursion depth rather than allocating for
	// every subset. The number of products, not query count, sets bitset size.
	coverage := make([][]uint64, len(candidates)+1)
	for i := range coverage {
		coverage[i] = make([]uint64, words)
	}
	bestByCost := make([]searchSetScore, budget+1)
	var selected []int
	var visits uint64
	var search func(int, int, int, int) error
	search = func(start, depth, cost, total int) error {
		visits++
		if visits%4096 == 1 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		current := &bestByCost[cost]
		if total > current.GradePoints || (total == current.GradePoints && len(selected) < len(current.Searches)) {
			*current = searchSetScore{Searches: slices.Clone(selected), Requests: cost, GradePoints: total}
		}
		for i := start; i < len(candidates); i++ {
			query := candidates[i]
			if cost+query.requests > budget {
				continue
			}
			next := coverage[depth+1]
			gain := 0
			for word, mask := range query.products {
				added := mask &^ coverage[depth][word]
				next[word] = mask | coverage[depth][word]
				if added == 0 {
					continue
				}
				for _, weighted := range weightedMasks {
					gain += weighted.grade * bits.OnesCount64(added&weighted.masks[word])
				}
			}
			if gain == 0 {
				continue
			}
			selected = append(selected, query.index)
			if err := search(i+1, depth+1, cost+query.requests, total+gain); err != nil {
				return err
			}
			selected = selected[:len(selected)-1]
		}
		return nil
	}
	if err := search(0, 0, 0, 0); err != nil {
		return nil, err
	}
	frontier := []searchSetScore{scoreSearchSet(catalogs, points, nil)}
	for _, best := range bestByCost {
		if best.GradePoints > frontier[len(frontier)-1].GradePoints {
			frontier = append(frontier, scoreSearchSet(catalogs, points, best.Searches))
		}
	}
	return frontier, nil
}
