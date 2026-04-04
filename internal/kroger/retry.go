package kroger

import (
	"context"
	"errors"
	"net/http"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
)

func krogerRetriable(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err != nil {
		return !errors.Is(err, context.Canceled), err
	}
	if resp == nil || resp.Request == nil {
		return false, nil
	}
	switch resp.Request.Method {
	case http.MethodGet, http.MethodHead:
	default:
		return false, nil
	}
	return resp.StatusCode >= http.StatusInternalServerError && resp.StatusCode <= 599, nil
}

func newRetryingHTTPClient(baseClient *http.Client) *http.Client {
	retryClient := retryablehttp.NewClient()
	retryClient.HTTPClient = baseClient
	retryClient.Logger = nil
	retryClient.CheckRetry = krogerRetriable
	retryClient.ErrorHandler = retryablehttp.PassthroughErrorHandler
	return retryClient.StandardClient()
}
