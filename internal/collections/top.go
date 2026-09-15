package collections

type topHeap[T any] struct {
	items []T
	less  func(T, T) bool
	limit int
}

func (h *topHeap[T]) push(item T) {
	h.items = append(h.items, item)
	for i := len(h.items) - 1; i > 0; {
		parent := (i - 1) / 2
		if !h.less(h.items[i], h.items[parent]) {
			break
		}
		h.items[i], h.items[parent] = h.items[parent], h.items[i]
		i = parent
	}
	if len(h.items) > h.limit {
		h.pop()
	}
}

func (h *topHeap[T]) pop() T {
	item := h.items[0]
	last := len(h.items) - 1
	h.items[0] = h.items[last]
	h.items = h.items[:last]
	for i := 0; ; {
		left := i*2 + 1
		if left >= len(h.items) {
			break
		}
		smallest := left
		right := left + 1
		if right < len(h.items) && h.less(h.items[right], h.items[left]) {
			smallest = right
		}
		if !h.less(h.items[smallest], h.items[i]) {
			break
		}
		h.items[i], h.items[smallest] = h.items[smallest], h.items[i]
		i = smallest
	}
	return item
}

// Top returns up to limit greatest items according to less, greatest first.
// It leaves items unchanged and uses a bounded heap: O(n log limit) time and
// O(limit) additional space. A nonpositive limit returns an empty slice.
func Top[T any](items []T, limit int, less func(T, T) bool) []T {
	if limit <= 0 {
		return []T{}
	}
	candidates := topHeap[T]{
		less:  less,
		limit: limit,
	}
	for _, item := range items {
		candidates.push(item)
	}
	result := make([]T, len(candidates.items))
	for i := len(result) - 1; i >= 0; i-- {
		result[i] = candidates.pop()
	}
	return result
}
