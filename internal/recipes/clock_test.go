package recipes

import (
	"testing"
	"time"
)

func withNow(t *testing.T, now time.Time) {
	t.Helper()
	oldNowFn := nowFn
	nowFn = func() time.Time {
		return now
	}
	t.Cleanup(func() {
		nowFn = oldNowFn
	})
}
