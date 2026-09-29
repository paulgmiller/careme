package mnfoodclub

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error {
	b.closed = true
	return nil
}

func TestFetchIngredients(t *testing.T) {
	var urls []string
	var bodies []*trackedBody
	ctx := t.Context()
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodGet, req.Method)
		assert.Equal(t, ctx, req.Context())
		urls = append(urls, req.URL.String())
		page := req.URL.Query().Get("page")
		category := "Produce"
		if strings.Contains(req.URL.Path, "meat") {
			category = "Meat"
		}
		body := &trackedBody{Reader: strings.NewReader(`<h1 class="page-heading">` + category + `</h1><ul class="productGrid"><li><article class="card"><a data-product-id="` + category + page + `"></a><h4 class="card-title">Product ` + page + `</h4><span data-product-price-without-tax>$2.50</span></article></li></ul>`)}
		bodies = append(bodies, body)
		return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
	})})
	got, err := client.FetchIngredients(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"https://mnfood.club/shop-all/produce/?page=1&sort=bestselling",
		"https://mnfood.club/shop-all/produce/?page=2&sort=bestselling",
		"https://mnfood.club/shop-all/produce/?page=3&sort=bestselling",
		"https://mnfood.club/shop-all/meat/?page=1",
		"https://mnfood.club/shop-all/meat/?page=2",
		"https://mnfood.club/shop-all/meat/?page=3",
	}, urls)
	require.Len(t, got, 6)
	for i, id := range []string{"Produce1", "Produce2", "Produce3", "Meat1", "Meat2", "Meat3"} {
		assert.Equal(t, id, got[i].ProductID)
		assert.Equal(t, pricePointer(2.50), got[i].PriceRegular)
		assert.True(t, bodies[i].closed)
	}
}

func TestFetchIngredientsFailures(t *testing.T) {
	transportErr := errors.New("connection failed")
	for _, test := range []struct {
		name   string
		status int
		body   io.Reader
		err    error
		want   string
	}{
		{"transport", 0, strings.NewReader(""), transportErr, "connection failed"},
		{"HTTP", http.StatusServiceUnavailable, strings.NewReader("unavailable"), nil, "HTTP 503"},
		{"HTML", http.StatusOK, strings.NewReader("login page"), nil, "no product grid"},
		{"body read", http.StatusOK, errorReader{transportErr}, nil, "connection failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var failedBody *trackedBody
			client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<ul class="productGrid"><article class="card"><a data-product-id="1"></a><h4 class="card-title">Apple</h4><span data-product-price-without-tax>$1</span></article></ul>`))}, nil
				}
				if test.err != nil {
					return nil, test.err
				}
				failedBody = &trackedBody{Reader: test.body}
				return &http.Response{StatusCode: test.status, Body: failedBody}, nil
			})})
			got, err := client.FetchIngredients(t.Context())
			require.ErrorContains(t, err, test.want)
			assert.Contains(t, err.Error(), "page=2&sort=bestselling")
			assert.Nil(t, got)
			assert.Equal(t, 2, calls)
			if failedBody != nil {
				assert.True(t, failedBody.closed)
			}
		})
	}
}

func TestFetchIngredientsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})})
	got, err := client.FetchIngredients(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
}
