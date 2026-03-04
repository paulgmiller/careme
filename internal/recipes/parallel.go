package recipes

import (
	"errors"

	lop "github.com/samber/lo/parallel"
)

// we need to make a bunch of calls and merge results but not lose track of errors.
func asParallel[T any, T2 any](items []T, fn func(T) ([]T2, error)) ([]T2, error) {
	if len(items) == 0 {
		return []T2{}, nil
	}

	type result struct {
		values []T2
		err    error
	}

	mapped := lop.Map(items, func(item T, _ int) result {
		values, err := fn(item)
		return result{values: values, err: err}
	})

	merged := make([]T2, 0)
	errs := make([]error, 0)
	for _, r := range mapped {
		if r.err != nil {
			errs = append(errs, r.err)
			continue
		}
		merged = append(merged, r.values...)
	}

	return merged, errors.Join(errs...)
}
