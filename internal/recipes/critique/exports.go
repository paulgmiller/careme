package critique

import (
	"careme/internal/cache"
	"careme/internal/config"
)

type RecipeCritiquer = recipeCritiquer
type CachingCritiquer = cachingCritiquer
type MultiCritiquer = multiCritiquer
type Rubberstamp = rubberstamp

func NewCachingCritiquer(critiquer RecipeCritiquer, c cache.Cache) *CachingCritiquer {
	if c == nil {
		panic("cache must not be nil")
	}
	return newCachingCritiquer(critiquer, NewStore(c))
}

func NewMultiCritiquer(critiquer RecipeCritiquer) *MultiCritiquer {
	if critiquer == nil {
		panic("critiquer must not be nil")
	}
	return &multiCritiquer{critiquer: critiquer}
}

func NewService(cfg *config.Config, c cache.Cache) Manager {
	return NewManager(cfg, c)
}
