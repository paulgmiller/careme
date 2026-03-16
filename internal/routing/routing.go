package routing

import "net/http"

type Registrar interface {
	Handle(string, http.Handler)
	HandleFunc(string, func(http.ResponseWriter, *http.Request))
}

type middlewareRegistrar struct {
	target Registrar
	wrap   func(http.Handler) http.Handler
}

func Wrap(target Registrar, wrap func(http.Handler) http.Handler) Registrar {
	if wrap == nil {
		return target
	}
	return middlewareRegistrar{
		target: target,
		wrap:   wrap,
	}
}

func (m middlewareRegistrar) Handle(pattern string, handler http.Handler) {
	m.target.Handle(pattern, m.wrap(handler))
}

func (m middlewareRegistrar) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	m.target.Handle(pattern, m.wrap(http.HandlerFunc(handler)))
}
