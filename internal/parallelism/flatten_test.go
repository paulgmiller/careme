package parallelism

import (
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"
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

func TestMapWithErrors_ReturnsSuccessfulValuesInInputOrderAndJoinsErrors(t *testing.T) {
	errOne := errors.New("err one")
	errTwo := errors.New("err two")

	got, err := MapWithErrors([]int{1, 2, 3, 4}, func(i int) (string, error) {
		switch i {
		case 1:
			return "a", nil
		case 2:
			return "b", errOne
		case 3:
			return "", errTwo
		case 4:
			return "d", nil
		default:
			return "", nil
		}
	})

	want := []string{"a", "d"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected mapped results: got=%v want=%v", got, want)
	}
	if !errors.Is(err, errOne) {
		t.Fatalf("expected merged error to include errOne, got: %v", err)
	}
	if !errors.Is(err, errTwo) {
		t.Fatalf("expected merged error to include errTwo, got: %v", err)
	}
}

func TestMapWithErrors_DoesNotCancelRemainingWorkAfterError(t *testing.T) {
	errBoom := errors.New("boom")
	started := make(chan int, 3)
	release := make(chan struct{})
	done := make(chan struct{})
	var calls atomic.Int32

	var got []int
	var gotErr error
	go func() {
		defer close(done)
		got, gotErr = MapWithErrors([]int{1, 2, 3}, func(i int) (int, error) {
			calls.Add(1)
			started <- i
			if i == 1 {
				return 0, errBoom
			}
			<-release
			return i * 10, nil
		})
	}()

	for range 3 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for all workers to start")
		}
	}
	close(release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for MapWithErrors to finish")
	}

	if calls.Load() != 3 {
		t.Fatalf("expected all items to run, got %d calls", calls.Load())
	}
	if !slices.Equal(got, []int{20, 30}) {
		t.Fatalf("unexpected mapped results: got=%v want=%v", got, []int{20, 30})
	}
	if !errors.Is(gotErr, errBoom) {
		t.Fatalf("expected merged error to include errBoom, got: %v", gotErr)
	}
}

func TestMapWithErrors_EmptyInput(t *testing.T) {
	got, err := MapWithErrors([]string{}, func(s string) (int, error) {
		return 1, nil
	})
	if err != nil {
		t.Fatalf("expected nil error for empty input, got: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty result for empty input, got: %v", got)
	}
}
