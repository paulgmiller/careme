package httpretry

import (
	"log/slog"
	"net/http"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
)

// LogRetry returns a request hook that logs retry attempts for source.
func LogRetry(source string) retryablehttp.RequestLogHook {
	return func(_ retryablehttp.Logger, req *http.Request, attempt int) {
		if attempt == 0 || req == nil {
			return
		}

		attrs := []any{"source", source}
		if req.URL != nil {
			attrs = append(attrs, "url", req.URL.String())
		}
		attrs = append(attrs, "attempt", attempt+1)

		slog.InfoContext(req.Context(), "Retrying HTTP request", attrs...)
	}
}
