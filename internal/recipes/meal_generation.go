package recipes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"careme/internal/ai"
)

type mealWorker struct {
	revision  int
	cancel    context.CancelFunc
	done      chan struct{}
	replacing bool
	recipe    *ai.Recipe
}

type mealResult struct {
	index    int
	revision int
	recipe   *ai.Recipe
	err      error
	// Drafts wait for publication before starting critique.
	published chan struct{}
}

type mealPlanResult struct {
	index int
	plan  *ai.MenuPlan
	err   error
}

// generatePlannedRecipes owns slot revisions and publication. Workers never
// publish their own results, so a canceled revision cannot overwrite its successor.
func (g *generatorService) generatePlannedRecipes(ctx context.Context, p *generatorParams, menu *ai.MenuPlan, ingredients map[string]ai.InputIngredient) ([]ai.Recipe, error) {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer workers.Wait()
	defer cancel()
	hash := p.Hash()
	slots := make([]mealWorker, len(menu.Plans))
	results := make(chan mealResult, len(slots))
	plans := make(chan mealPlanResult, 1)
	start := func(index int) {
		slot := &slots[index]
		workerCtx, workerCancel := context.WithCancel(ctx)
		slot.cancel = workerCancel
		slot.done = make(chan struct{})
		done, revision := slot.done, slot.revision
		plan, response := menu.Plans[index], menu.ResponseRef()
		workers.Go(func() {
			defer close(done)
			g.generateMeal(workerCtx, p, plan, response, ingredients, index, revision, results)
		})
	}
	for index := range slots {
		start(index)
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var dismissed []ai.RecipePlan
	var queue []int
	replace := func(index int) {
		slot := &slots[index]
		slot.cancel()
		slot.replacing = true
		dismissed = append(dismissed, menu.Plans[index])
		queue = append(queue, index)
	}
	planning := false
	finished := 0
	for finished < len(slots) {
		if !planning && len(queue) > 0 {
			index := queue[0]
			queue = queue[1:]
			instructions := mealReplacementInstructions(p, menu.Plans[index], menu.Plans, dismissed)
			previous, done := menu.ResponseRef(), slots[index].done
			planning = true
			workers.Go(func() {
				// Join the canceled worker before generating its replacement.
				select {
				case <-ctx.Done():
					return
				case <-done:
				}
				plan, err := g.aiClient.RegenerateMenuPlan(ctx, instructions, previous, 1)
				select {
				case plans <- mealPlanResult{index: index, plan: plan, err: err}:
				case <-ctx.Done():
				}
			})
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			for index := range slots {
				slot := &slots[index]
				if slot.replacing || slot.recipe != nil {
					continue
				}
				requested, err := g.statusWriter.Decision(ctx, hash, index, slot.revision)
				if err != nil {
					return nil, fmt.Errorf("check meal replacement: %w", err)
				}
				if requested {
					replace(index)
				}
			}
		case result := <-plans:
			planning = false
			if result.err != nil {
				return nil, fmt.Errorf("plan meal replacement: %w", result.err)
			}
			if result.plan == nil || len(result.plan.Plans) != 1 || strings.TrimSpace(result.plan.ResponseID) == "" {
				return nil, fmt.Errorf("plan meal replacement: expected exactly one meal and a response ID")
			}
			slot := &slots[result.index]
			slot.revision++
			if err := g.statusWriter.ReplaceSlot(ctx, hash, result.index, slot.revision, result.plan.Plans[0]); err != nil {
				return nil, fmt.Errorf("publish meal replacement: %w", err)
			}
			menu.Plans[result.index] = result.plan.Plans[0]
			menu.ResponseID = result.plan.ResponseID
			menu.PromptCacheKey = result.plan.PromptCacheKey
			menu.ChefNoteSuggestion = result.plan.ChefNoteSuggestion
			slot.replacing = false
			start(result.index)
		case result := <-results:
			slot := &slots[result.index]
			if slot.replacing || result.revision != slot.revision {
				continue
			}
			// A command can arrive between polls or while a worker returns an error.
			requested, err := g.statusWriter.Decision(ctx, hash, result.index, slot.revision)
			if err != nil {
				return nil, fmt.Errorf("check meal replacement: %w", err)
			}
			if requested {
				replace(result.index)
				continue
			}
			if result.err != nil {
				return nil, result.err
			}
			if result.published != nil {
				if err := g.statusWriter.RecipeDraft(ctx, hash, result.index, result.recipe.ComputeHash()); err != nil {
					return nil, fmt.Errorf("publish draft meal: %w", err)
				}
				close(result.published)
				continue
			}
			ready, err := g.statusWriter.FinishSlot(ctx, hash, result.index, slot.revision, result.recipe.ComputeHash())
			if err != nil {
				return nil, fmt.Errorf("finish meal: %w", err)
			}
			if !ready {
				// Replacement won the atomic decision after the preceding read.
				replace(result.index)
				continue
			}
			if err := g.statusWriter.RecipeReady(ctx, hash, result.index, result.recipe.ComputeHash()); err != nil {
				return nil, fmt.Errorf("publish reviewed meal: %w", err)
			}
			slot.recipe = result.recipe
			slot.cancel()
			finished++
		}
	}
	recipes := make([]ai.Recipe, len(slots))
	for index, slot := range slots {
		recipes[index] = *slot.recipe
	}
	return recipes, nil
}

func (g *generatorService) generateMeal(ctx context.Context, p *generatorParams, plan ai.RecipePlan, response ai.ResponseRef, ingredients map[string]ai.InputIngredient, index, revision int, results chan<- mealResult) {
	ctx, span := tracer.Start(ctx, "recipes.generate.single")
	defer span.End()
	result := mealResult{index: index, revision: revision}
	send := func() bool {
		select {
		case results <- result:
			return true
		case <-ctx.Done():
			return false
		}
	}
	instructions := plan.Instructions()
	if !p.isRegeneration() {
		instructions = append([]string{p.Directive}, instructions...)
	}
	// TODO(ingredient-neighbors): Enrich this plan's instructions with the nearest
	// store-scoped neighbors of its anchor and side vegetable. Merge their TSV
	// and metadata into a per-recipe map, without mutating shared ingredients.
	recipe, err := g.aiClient.GenerateRecipe(ctx, instructions, response)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		recipe.OriginHash = p.Hash()
		enrichIngredientsMetadata(recipe.Ingredients, ingredients)
		err = g.saver.SaveRecipe(ctx, *recipe)
	}
	if err != nil {
		result.err = err
		send()
		return
	}
	result.recipe = recipe
	result.published = make(chan struct{})
	if !send() {
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-result.published:
	}
	result.published = nil
	result.recipe, result.err = g.critiqueAndMaybeRetryRecipe(ctx, p.Hash(), recipe, ingredients)
	send()
}

func mealReplacementInstructions(p *generatorParams, target ai.RecipePlan, current, dismissed []ai.RecipePlan) []string {
	// RecipePlan contains only JSON-compatible planning labels.
	currentJSON, _ := json.Marshal(current)
	dismissedJSON, _ := json.Marshal(dismissed)
	targetJSON, _ := json.Marshal(target)
	instructions := []string{
		p.Directive, p.Instructions,
		"Current menu (leave all other meals unchanged): " + string(currentJSON),
		"Passed-on meal ideas; avoid these and close variants: " + string(dismissedJSON),
		"Replace this meal: " + string(targetJSON),
		"Return exactly one replacement plan, regardless of the original menu count. Preserve the original dietary and ingredient constraints. Choose a different idea that fits the rest of the menu; its category may change.",
	}
	for _, saved := range p.Saved {
		instructions = append(instructions, "Keep this saved meal unchanged and avoid repeating it: "+saved.Title)
	}
	return instructions
}
