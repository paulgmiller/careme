package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"careme/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSource(t *testing.T) {
	for _, test := range []struct {
		source, location, wantError string
	}{
		{"staples", "70100023", ""},
		{"staples", " ", "-location is required"},
		{"mnfoodclub", "", ""},
		{"mnfoodclub", "70100023", ""},
		{"unknown", "70100023", "unknown ingredient source"},
	} {
		t.Run(test.source+"/"+test.location, func(t *testing.T) {
			err := validateSource(test.source, test.location)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestFetchMNFoodClubIngredients(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "page failure"
		}
		t.Run(name, func(t *testing.T) {
			var urls []string
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				urls = append(urls, req.URL.String())
				if fail && len(urls) == 2 {
					return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<ul class="productGrid"><article class="card"><a data-product-id="1"></a><h4 class="card-title">Broccoli</h4><span data-product-price-without-tax>$2.99</span></article></ul>`))}, nil
			})}
			got, err := fetchIngredients(t.Context(), "mnfoodclub", "", &config.Config{}, client)
			if fail {
				require.ErrorContains(t, err, "HTTP 503")
				assert.Nil(t, got)
				assert.Len(t, urls, 2)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, 6)
			assert.Equal(t, "Broccoli", got[0].Description)
			assert.InDelta(t, 2.99, *got[0].PriceRegular, 0.001)
			assert.Equal(t, []string{
				"https://mnfood.club/shop-all/produce/?page=1&sort=bestselling",
				"https://mnfood.club/shop-all/produce/?page=2&sort=bestselling",
				"https://mnfood.club/shop-all/produce/?page=3&sort=bestselling",
				"https://mnfood.club/shop-all/meat/?page=1",
				"https://mnfood.club/shop-all/meat/?page=2",
				"https://mnfood.club/shop-all/meat/?page=3",
			}, urls)
		})
	}
}
