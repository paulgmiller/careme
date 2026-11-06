package recipes

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes/status"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type steeringAIClient struct {
	*captureGenerateAIClient
	generate func(context.Context, []string, ai.ResponseRef) (*ai.Recipe, error)
	replace  func(context.Context, []string, ai.ResponseRef, int) (*ai.MenuPlan, error)
}

func (c steeringAIClient) GenerateRecipe(ctx context.Context, instructions []string, response ai.ResponseRef) (*ai.Recipe, error) {
	return c.generate(ctx, instructions, response)
}

func (c steeringAIClient) RegenerateMenuPlan(ctx context.Context, instructions []string, previous ai.ResponseRef, count int) (*ai.MenuPlan, error) {
	return c.replace(ctx, instructions, previous, count)
}

type steeringCritiquer struct {
	critique func(context.Context, ai.Recipe) (*ai.RecipeCritique, error)
}

func (c steeringCritiquer) CritiqueRecipe(ctx context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error) {
	return c.critique(ctx, recipe)
}

func (steeringCritiquer) CritiqueRecipeInBackground(context.Context, ai.Recipe) {}

func TestMealReplacementCancelsOnlyCurrentSlot(t *testing.T) {
	t.Parallel()
	for _, regeneration := range []bool{false, true} {
		for _, phase := range []string{"generation", "critique"} {
			name := phase
			if regeneration {
				name += "_regeneration"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				c := cache.NewInMemoryCache()
				owner, otherReplica := status.NewStore(c), status.NewStore(c)
				rio := IO(c)
				p := DefaultParams(&locations.Location{ID: "70000123", Name: "Store"}, time.Now())
				p.Directive, p.Instructions = "vegetarian", "use my broccoli"
				initial := &ai.MenuPlan{ResponseID: "initial", PromptCacheKey: "store-day", Plans: []ai.RecipePlan{{Cuisine: "Rejected"}, {Cuisine: "Steady"}}}
				if regeneration {
					p.PreviousMenuPlanResponseID = "prior"
					p.Saved = []ai.Recipe{{Title: "Saved dinner"}}
				}
				require.NoError(t, owner.Start(t.Context(), p.Hash(), ""))
				started, canceled := make(chan string, 4), make(chan string, 4)
				var mu sync.Mutex
				var instructions [][]string
				var parents []ai.ResponseRef
				counts := make(map[string]int)
				block := func(ctx context.Context, title string) {
					started <- title
					<-ctx.Done()
					canceled <- title
				}
				client := steeringAIClient{
					captureGenerateAIClient: &captureGenerateAIClient{menuPlan: initial},
					generate: func(ctx context.Context, in []string, _ ai.ResponseRef) (*ai.Recipe, error) {
						title := "Steady"
						for _, candidate := range []string{"Rejected", "Second", "Final"} {
							if strings.Contains(strings.Join(in, " "), candidate) {
								title = candidate
							}
						}
						mu.Lock()
						counts[title]++
						mu.Unlock()
						if phase == "generation" && (title == "Rejected" || title == "Second") {
							block(ctx, title)
						}
						// Deliberately return a late success after cancellation.
						return &ai.Recipe{Title: title, ResponseID: "recipe-" + title}, nil
					},
					replace: func(_ context.Context, in []string, previous ai.ResponseRef, count int) (*ai.MenuPlan, error) {
						if previous.ID == "prior" {
							return initial, nil
						}
						assert.Equal(t, 1, count)
						mu.Lock()
						defer mu.Unlock()
						instructions = append(instructions, in)
						parents = append(parents, previous)
						title := "Second"
						if len(parents) > 1 {
							title = "Final"
						}
						return &ai.MenuPlan{ResponseID: title, PromptCacheKey: previous.PromptCacheKey, Plans: []ai.RecipePlan{{Cuisine: title}}}, nil
					},
				}
				critiquer := steeringCritiquer{critique: func(ctx context.Context, r ai.Recipe) (*ai.RecipeCritique, error) {
					if phase == "critique" && (r.Title == "Rejected" || r.Title == "Second") {
						block(ctx, r.Title)
					}
					return &ai.RecipeCritique{OverallScore: 10}, nil
				}}
				g := newTestGenerator(t, client, critiquer, fixedStaplesService{}, owner, rio)
				ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
				defer cancel()
				type outcome struct {
					list *ai.ShoppingList
					err  error
				}
				done := make(chan outcome, 1)
				go func() { list, err := g.GenerateRecipes(ctx, p); done <- outcome{list, err} }()
				for revision, title := range []string{"Rejected", "Second"} {
					select {
					case got := <-started:
						require.Equal(t, title, got)
					case <-ctx.Done():
						t.Fatal("meal worker did not start")
					}
					require.NoError(t, otherReplica.RequestReplacement(ctx, p.Hash(), 0, revision))
					require.NoError(t, otherReplica.RequestReplacement(ctx, p.Hash(), 0, revision), "duplicate is idempotent")
					progress, err := otherReplica.Load(ctx, p.Hash())
					require.NoError(t, err)
					assert.True(t, progress.Slots[0].Replacing)
					assert.Empty(t, progress.Slots[0].RecipeHash, "accepted dismissal hides the old draft immediately")
					select {
					case got := <-canceled:
						require.Equal(t, title, got)
					case <-ctx.Done():
						t.Fatal("meal worker was not canceled")
					}
				}
				var result outcome
				select {
				case result = <-done:
				case <-ctx.Done():
					t.Fatal("generation did not finish")
				}
				require.NoError(t, result.err)
				assert.Equal(t, "Final", result.list.Recipes[0].Title)
				assert.Equal(t, "Steady", result.list.Recipes[1].Title)
				if regeneration {
					assert.Equal(t, "Saved dinner", result.list.Recipes[2].Title)
				}
				assert.Equal(t, "Final", result.list.Plan.Plans[0].Cuisine)
				assert.Equal(t, "Steady", result.list.Plan.Plans[1].Cuisine)
				assert.Equal(t, "Final", result.list.Plan.ResponseID)
				assert.Equal(t, "store-day", result.list.Plan.PromptCacheKey)
				mu.Lock()
				defer mu.Unlock()
				assert.Equal(t, map[string]int{"Rejected": 1, "Steady": 1, "Second": 1, "Final": 1}, counts)
				require.Len(t, parents, 2)
				assert.Equal(t, "initial", parents[0].ID)
				assert.Equal(t, "Second", parents[1].ID)
				assert.Contains(t, instructions[1], "vegetarian")
				assert.Contains(t, instructions[1], "use my broccoli")
				assert.Contains(t, strings.Join(instructions[1], " "), `"cuisine":"Rejected"`)
				assert.Contains(t, strings.Join(instructions[1], " "), `"cuisine":"Second"`)
				progress, err := otherReplica.Load(ctx, p.Hash())
				require.NoError(t, err)
				assert.True(t, progress.Slots[0].Reviewed)
				assert.Equal(t, 2, progress.Slots[0].Revision)
			})
		}
	}
}

func TestMealReplacementFailureCancelsOtherMeals(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"planner", "count", "response ID", "recipe"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			c := cache.NewInMemoryCache()
			progress := status.NewStore(c)
			p := DefaultParams(&locations.Location{ID: "70000123"}, time.Now())
			require.NoError(t, progress.Start(t.Context(), p.Hash(), ""))
			otherCanceled := make(chan struct{})
			started := make(chan struct{}, 2)
			client := steeringAIClient{
				captureGenerateAIClient: &captureGenerateAIClient{menuPlan: &ai.MenuPlan{ResponseID: "initial", Plans: []ai.RecipePlan{{Cuisine: "Rejected"}, {Cuisine: "Other"}}}},
				generate: func(ctx context.Context, in []string, _ ai.ResponseRef) (*ai.Recipe, error) {
					if strings.Contains(strings.Join(in, " "), "Replacement") {
						return nil, errors.New("replacement recipe failed")
					}
					started <- struct{}{}
					<-ctx.Done()
					if strings.Contains(strings.Join(in, " "), "Other") {
						close(otherCanceled)
					}
					return nil, ctx.Err()
				},
				replace: func(context.Context, []string, ai.ResponseRef, int) (*ai.MenuPlan, error) {
					switch failure {
					case "planner":
						return nil, errors.New("replacement planner failed")
					case "count":
						return &ai.MenuPlan{ResponseID: "new", Plans: []ai.RecipePlan{{}, {}}}, nil
					case "response ID":
						return &ai.MenuPlan{Plans: []ai.RecipePlan{{}}}, nil
					default:
						return &ai.MenuPlan{ResponseID: "new", Plans: []ai.RecipePlan{{Cuisine: "Replacement"}}}, nil
					}
				},
			}
			g := newTestGenerator(t, client, &captureCritiqueService{}, fixedStaplesService{}, progress, IO(c))
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := g.GenerateRecipes(ctx, p); done <- err }()
			for range 2 {
				select {
				case <-started:
				case <-ctx.Done():
					t.Fatal("workers did not start")
				}
			}
			require.NoError(t, status.NewStore(c).RequestReplacement(ctx, p.Hash(), 0, 0))
			select {
			case err := <-done:
				require.Error(t, err)
				assert.NotErrorIs(t, err, context.DeadlineExceeded)
			case <-ctx.Done():
				t.Fatal("generation did not fail")
			}
			select {
			case <-otherCanceled:
			default:
				t.Fatal("failure did not join and cancel unaffected worker")
			}
		})
	}
}

func TestMealReplacementCancelsWhileAnotherReplacementIsPlanning(t *testing.T) {
	t.Parallel()
	c := cache.NewInMemoryCache()
	progress := status.NewStore(c)
	p := DefaultParams(&locations.Location{ID: "70000123"}, time.Now())
	require.NoError(t, progress.Start(t.Context(), p.Hash(), ""))
	started, canceled := make(chan string, 2), make(chan string, 2)
	planning, releasePlanner := make(chan struct{}), make(chan struct{})
	client := steeringAIClient{
		captureGenerateAIClient: &captureGenerateAIClient{menuPlan: &ai.MenuPlan{ResponseID: "initial", Plans: []ai.RecipePlan{{Cuisine: "First"}, {Cuisine: "Other"}}}},
		generate: func(ctx context.Context, in []string, _ ai.ResponseRef) (*ai.Recipe, error) {
			text := strings.Join(in, " ")
			if strings.Contains(text, "Replacement") {
				return &ai.Recipe{Title: text, ResponseID: "new-recipe"}, nil
			}
			started <- text
			<-ctx.Done()
			canceled <- text
			return nil, ctx.Err()
		},
		replace: func(ctx context.Context, in []string, previous ai.ResponseRef, count int) (*ai.MenuPlan, error) {
			assert.Equal(t, 1, count)
			if previous.ID == "initial" {
				close(planning)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-releasePlanner:
				}
				return &ai.MenuPlan{ResponseID: "first-replacement", Plans: []ai.RecipePlan{{Cuisine: "Replacement A"}}}, nil
			}
			assert.Equal(t, "first-replacement", previous.ID)
			assert.Contains(t, strings.Join(in, " "), `"cuisine":"Replacement A"`)
			assert.Contains(t, strings.Join(in, " "), `Replace this meal: {"cuisine":"Other"`)
			return &ai.MenuPlan{ResponseID: "second-replacement", Plans: []ai.RecipePlan{{Cuisine: "Replacement B"}}}, nil
		},
	}
	g := newTestGenerator(t, client, &captureCritiqueService{}, fixedStaplesService{}, progress, IO(c))
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := g.GenerateRecipes(ctx, p); done <- err }()
	wait := func(ch <-chan string) string {
		t.Helper()
		select {
		case got := <-ch:
			return got
		case <-ctx.Done():
			t.Fatal("meal worker did not respond")
			return ""
		}
	}
	for range 2 {
		wait(started)
	}
	require.NoError(t, status.NewStore(c).RequestReplacement(ctx, p.Hash(), 0, 0))
	select {
	case <-planning:
	case <-ctx.Done():
		t.Fatal("replacement planner did not start")
	}
	assert.Contains(t, wait(canceled), "First")
	require.NoError(t, status.NewStore(c).RequestReplacement(ctx, p.Hash(), 1, 0))
	assert.Contains(t, wait(canceled), "Other", "a busy planner must not delay cancellation of another slot")
	close(releasePlanner)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("replacement generation did not finish")
	}
}
