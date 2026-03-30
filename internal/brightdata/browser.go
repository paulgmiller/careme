package brightdata

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const (
	DefaultBrowserWSEndpoint = "wss://brd.superproxy.io:9222"
	defaultBrowserWait       = 5 * time.Second
)

type BrowserClient struct {
	wsEndpoint string
}

type BrowserClientConfig struct {
	Auth       string
	WSEndpoint string
}

type BrowserOptions struct {
	WaitAfterNavigation time.Duration
}

type BrowserCookie struct {
	Name     string
	Value    string
	Domain   string
	Path     string
	Expires  *time.Time
	HTTPOnly bool
	Secure   bool
	Session  bool
}

func NewBrowserClient(cfg BrowserClientConfig) (*BrowserClient, error) {
	wsEndpoint, err := browserWSEndpoint(cfg.WSEndpoint, cfg.Auth)
	if err != nil {
		return nil, err
	}
	return &BrowserClient{wsEndpoint: wsEndpoint}, nil
}

func (c *BrowserClient) Cookies(ctx context.Context, targetURL string, opts BrowserOptions) ([]BrowserCookie, error) {
	targetURL = strings.TrimSpace(targetURL)
	if targetURL == "" {
		return nil, errors.New("target URL is required")
	}
	wait := opts.WaitAfterNavigation
	if wait <= 0 {
		wait = defaultBrowserWait
	}

	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(ctx, c.wsEndpoint, chromedp.NoModifyURL)
	defer cancelAlloc()

	taskCtx, cancelTask := chromedp.NewContext(allocCtx)
	defer cancelTask()

	var rawCookies []*network.Cookie
	if err := chromedp.Run(taskCtx,
		network.Enable(),
		chromedp.Navigate(targetURL),
		chromedp.Sleep(wait),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			rawCookies, err = network.GetCookies().WithURLs([]string{targetURL}).Do(ctx)
			if err != nil {
				return fmt.Errorf("get browser cookies: %w", err)
			}
			return nil
		}),
	); err != nil {
		return nil, fmt.Errorf("navigate browser session: %w", err)
	}

	cookies := make([]BrowserCookie, 0, len(rawCookies))
	for _, cookie := range rawCookies {
		if cookie == nil {
			continue
		}
		cookies = append(cookies, BrowserCookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   cookie.Domain,
			Path:     cookie.Path,
			Expires:  expiresAt(cookie.Expires),
			HTTPOnly: cookie.HTTPOnly,
			Secure:   cookie.Secure,
			Session:  cookie.Session,
		})
	}
	return cookies, nil
}

func CookieNamed(cookies []BrowserCookie, name string) (BrowserCookie, bool) {
	name = strings.TrimSpace(name)
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie, true
		}
	}
	return BrowserCookie{}, false
}

func browserWSEndpoint(rawEndpoint, auth string) (string, error) {
	endpoint := strings.TrimSpace(rawEndpoint)
	if endpoint == "" {
		endpoint = DefaultBrowserWSEndpoint
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse Bright Data browser endpoint: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("bright data browser endpoint must be an absolute websocket URL")
	}
	if parsed.User == nil {
		user, pass, ok := strings.Cut(strings.TrimSpace(auth), ":")
		if !ok || user == "" || pass == "" {
			return "", errors.New("bright data browser auth must be in USER:PASS format")
		}
		parsed.User = url.UserPassword(user, pass)
	}

	return parsed.String(), nil
}

func expiresAt(unixSeconds float64) *time.Time {
	if unixSeconds <= 0 || math.IsNaN(unixSeconds) || math.IsInf(unixSeconds, 0) {
		return nil
	}
	expires := time.Unix(0, int64(unixSeconds*float64(time.Second))).UTC()
	return &expires
}
