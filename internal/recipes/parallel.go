package recipes

import (
	"errors"
	"sync"

	"github.com/samber/lo"
)

// we need to make a bunch of calls and merge results but not lose track of errors.
func asParallel[T any, T2 any](items []T, fn func(T) ([]T2, error)) ([]T2, error) {
	if len(items) == 0 {
		return []T2{}, nil
	}

	resultsCh := make(chan T2)
	errCh := make(chan error, len(items)) //has to be buffered or will deadlock

	var wg sync.WaitGroup
	wg.Add(len(items))
	for _, item := range items {
		go func(item T) {
			defer wg.Done()
			values, err := fn(item)
			if err != nil {
				errCh <- err
				return
			}
			for _, v := range values {
				resultsCh <- v
			}
		}(item)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
		close(errCh)
	}()

	merged := lo.ChannelToSlice(resultsCh)
	errs := lo.ChannelToSlice(errCh)

	return merged, errors.Join(errs...)
}
