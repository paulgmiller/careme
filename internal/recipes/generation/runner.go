package generation

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// LastRecipe is the minimum shape needed to derive recent recipe titles.
type LastRecipe struct {
	Title     string
	CreatedAt time.Time
}

// Task describes one asynchronous generation run.
type Task struct {
	Ctx    context.Context
	Hash   string
	Params string

	LastRecipes   []LastRecipe
	AddLastRecipe func(string)

	Generate func(context.Context) (any, error)
	Save     func(context.Context, any) error
}

type Runner struct {
	wg sync.WaitGroup
}

func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) Kick(t Task) {
	for _, last := range t.LastRecipes {
		if last.CreatedAt.Before(time.Now().AddDate(0, 0, -14)) {
			break
		}
		t.AddLastRecipe(last.Title)
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ctx := context.WithoutCancel(t.Ctx)
		slog.InfoContext(ctx, "generating cached recipes", "params", t.Params, "hash", t.Hash)
		result, err := t.Generate(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "generate error", "error", err)
			return
		}
		if err := t.Save(ctx, result); err != nil {
			slog.ErrorContext(ctx, "save error", "error", err)
			return
		}
	}()
}

func (r *Runner) Wait() {
	r.wg.Wait()
}
