package watchdog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"careme/internal/routing"
)

type watchdog interface {
	Watchdog(ctx context.Context) error
}

type watcher struct {
	name   string
	period time.Duration
	dog    watchdog
}

type Server struct {
	watchers []watcher
}

func (s *Server) Add(name string, dog watchdog, period time.Duration) {
	guard := NewOncePer(period, dog)
	s.watchers = append(s.watchers, watcher{
		name:   name,
		period: period,
		dog:    &guard,
	})
}

func (s *Server) Register(mux routing.Registrar) {
	for _, watcher := range s.watchers {
		mux.HandleFunc("GET /watchdogs/"+watcher.name, func(w http.ResponseWriter, r *http.Request) {
			err := watcher.dog.Watchdog(r.Context())
			if errors.Is(err, errTooSoon) {
				http.Error(w, fmt.Sprintf("can only call watchdog every %v", watcher.period), http.StatusServiceUnavailable)
				return
			}
			if err != nil {
				http.Error(w, fmt.Sprintf("%s not ready: %v", watcher.name, err), http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		})
	}
}
