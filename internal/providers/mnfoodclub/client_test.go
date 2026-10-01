package mnfoodclub

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
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

func TestFetch(t *testing.T) {
	for _, test := range []struct {
		name     string
		url      string
		pages    int
		wantURLs []string
	}{
		{"produce", "https://mnfood.club/shop-all/produce/?sort=bestselling&page=9", 3, []string{
			"https://mnfood.club/shop-all/produce/?page=1&sort=bestselling",
			"https://mnfood.club/shop-all/produce/?page=2&sort=bestselling",
			"https://mnfood.club/shop-all/produce/?page=3&sort=bestselling",
		}},
		{"pasta", "https://mnfood.club/shop/pantry/pasta/", 1, []string{"https://mnfood.club/shop/pantry/pasta/?page=1"}},
		{"wines", "https://mnfood.club/shop/beverage/n-a-tasty-drinks/wine-wine-alternatives/", 1, []string{"https://mnfood.club/shop/beverage/n-a-tasty-drinks/wine-wine-alternatives/?page=1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var urls []string
			var bodies []*trackedBody
			ctx := t.Context()
			client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, http.MethodGet, req.Method)
				assert.Equal(t, ctx, req.Context())
				urls = append(urls, req.URL.String())
				page := req.URL.Query().Get("page")
				body := &trackedBody{Reader: strings.NewReader(`<h1 class="page-heading">` + test.name + `</h1><ul class="productGrid"><li><article class="card"><a data-product-id="` + page + `"></a><h4 class="card-title">Product ` + page + `</h4><span data-product-price-without-tax>$2.50</span></article></li></ul>`)}
				bodies = append(bodies, body)
				return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
			})})
			got, err := client.Fetch(ctx, test.url, test.pages)
			require.NoError(t, err)
			assert.Equal(t, test.wantURLs, urls)
			require.Len(t, got, test.pages)
			for i, item := range got {
				assert.Equal(t, strconv.Itoa(i+1), item.ProductID)
				assert.Equal(t, pricePointer(2.50), item.PriceRegular)
				assert.True(t, bodies[i].closed)
			}
		})
	}
}

func TestFetchInvalidURL(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatal("invalid URL should not make a request")
		return nil, nil
	})})
	got, err := client.Fetch(t.Context(), "https://mnfood.club/%zz", 1)
	require.ErrorContains(t, err, "parse category URL")
	assert.Nil(t, got)
}

func TestFetchFailures(t *testing.T) {
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
			got, err := client.Fetch(t.Context(), "https://mnfood.club/shop-all/produce/?sort=bestselling", 3)
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

func TestFetchCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})})
	got, err := client.Fetch(ctx, "https://mnfood.club/shop-all/produce/?sort=bestselling", 3)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
}
