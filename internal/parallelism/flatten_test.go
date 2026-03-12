package parallelism

import (
	"errors"
	"slices"
	"testing"
)

func TestFlatten_MergesResultsAndErrors(t *testing.T) {
	errOne := errors.New("err one")
	errTwo := errors.New("err two")

	got, err := Flatten([]int{1, 2, 3, 4}, func(i int) ([]string, error) {
		switch i {
		case 1:
			return []string{"a", "b"}, nil
		case 2:
			return []string{"c"}, errOne
		case 3:
			return nil, errTwo
		case 4:
			return []string{"d"}, nil
		default:
			return nil, nil
		}
	})

	slices.Sort(got)
	want := []string{"a", "b", "d"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected merged results: got=%v want=%v", got, want)
	}
	if !errors.Is(err, errOne) {
		t.Fatalf("expected merged error to include errOne, got: %v", err)
	}
	if !errors.Is(err, errTwo) {
		t.Fatalf("expected merged error to include errTwo, got: %v", err)
	}
}

func TestFlatten_EmptyInput(t *testing.T) {
	got, err := Flatten([]string{}, func(s string) ([]int, error) {
		return []int{1}, nil
	})
	if err != nil {
		t.Fatalf("expected nil error for empty input, got: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty result for empty input, got: %v", got)
	}
}
