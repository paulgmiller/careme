package brightdata

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

const (
	DefaultBrowserWSEndpoint = "wss://brd.superproxy.io:9222"
	defaultBrowserWait       = 5 * time.Second
)

type BrowserClient struct {
	wsEndpoint string
	authHeader string
}

type BrowserClientConfig struct {
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

type cdpMessage struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Error     *cdpError       `json:"error,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cdpClient struct {
	conn net.Conn

	writeMu sync.Mutex
	nextID  atomic.Int64

	pendingMu sync.Mutex
	pending   map[int64]chan cdpMessage
	events    chan cdpMessage
	readErr   chan error
}

func NewBrowserClient(cfg BrowserClientConfig) (*BrowserClient, error) {
	wsEndpoint, authHeader, err := browserWSEndpoint(cfg.WSEndpoint)
	if err != nil {
		return nil, err
	}
	return &BrowserClient{
		wsEndpoint: wsEndpoint,
		authHeader: authHeader,
	}, nil
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

	client, err := newCDPClient(ctx, c.wsEndpoint, c.authHeader)
	if err != nil {
		return nil, fmt.Errorf("dial browser websocket: %w", err)
	}
	defer func() {
		_ = client.Close()
	}()

	targetID, err := client.createTarget(ctx)
	if err != nil {
		return nil, fmt.Errorf("create browser target: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.closeTarget(closeCtx, targetID)
	}()

	sessionID, err := client.attachToTarget(ctx, targetID)
	if err != nil {
		return nil, fmt.Errorf("attach to browser target: %w", err)
	}

	if err := client.call(ctx, sessionID, "Page.enable", nil, nil); err != nil {
		return nil, fmt.Errorf("enable page domain: %w", err)
	}
	if err := client.call(ctx, sessionID, "Network.enable", nil, nil); err != nil {
		return nil, fmt.Errorf("enable network domain: %w", err)
	}
	if err := client.navigate(ctx, sessionID, targetURL); err != nil {
		return nil, fmt.Errorf("navigate browser target: %w", err)
	}

	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	return client.getCookies(ctx, sessionID, targetURL)
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

func browserWSEndpoint(rawEndpoint string) (string, string, error) {
	endpoint := strings.TrimSpace(rawEndpoint)
	if endpoint == "" {
		endpoint = DefaultBrowserWSEndpoint
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", "", fmt.Errorf("parse Bright Data browser endpoint: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", "", errors.New("bright data browser endpoint must be an absolute websocket URL")
	}
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return "", "", errors.New("bright data browser endpoint must use ws or wss")
	}

	headerValue, err := browserAuthHeader(parsed)
	if err != nil {
		return "", "", err
	}

	parsed.User = nil
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String(), headerValue, nil
}

func browserAuthHeader(parsed *url.URL) (string, error) {
	if parsed == nil {
		return "", errors.New("browser endpoint is required")
	}

	user := ""
	pass := ""
	if parsed.User != nil {
		user = parsed.User.Username()
		pass, _ = parsed.User.Password()
	}
	if user == "" || pass == "" {
		return "", errors.New("bright data browser endpoint must include USER:PASS credentials")
	}

	token := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
	return "Basic " + token, nil
}

func expiresAt(unixSeconds float64) *time.Time {
	if unixSeconds <= 0 || math.IsNaN(unixSeconds) || math.IsInf(unixSeconds, 0) {
		return nil
	}
	expires := time.Unix(0, int64(unixSeconds*float64(time.Second))).UTC()
	return &expires
}

func newCDPClient(ctx context.Context, wsEndpoint, authHeader string) (*cdpClient, error) {
	dialer := ws.Dialer{
		Header: ws.HandshakeHeaderHTTP(http.Header{
			"Authorization": []string{authHeader},
		}),
	}
	conn, br, _, err := dialer.Dial(ctx, wsEndpoint)
	if err != nil {
		return nil, err
	}
	if br != nil {
		_ = conn.Close()
		return nil, errors.New("unexpected buffered websocket reader")
	}

	client := &cdpClient{
		conn:    conn,
		pending: make(map[int64]chan cdpMessage),
		events:  make(chan cdpMessage, 64),
		readErr: make(chan error, 1),
	}
	go client.readLoop()
	return client, nil
}

func (c *cdpClient) Close() error {
	return c.conn.Close()
}

func (c *cdpClient) createTarget(ctx context.Context) (string, error) {
	var result struct {
		TargetID string `json:"targetId"`
	}
	if err := c.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &result); err != nil {
		return "", err
	}
	if result.TargetID == "" {
		return "", errors.New("browser target ID missing from response")
	}
	return result.TargetID, nil
}

func (c *cdpClient) attachToTarget(ctx context.Context, targetID string) (string, error) {
	var result struct {
		SessionID string `json:"sessionId"`
	}
	if err := c.call(ctx, "", "Target.attachToTarget", map[string]any{
		"targetId": targetID,
		"flatten":  true,
	}, &result); err != nil {
		return "", err
	}
	if result.SessionID == "" {
		return "", errors.New("browser session ID missing from response")
	}
	return result.SessionID, nil
}

func (c *cdpClient) navigate(ctx context.Context, sessionID, targetURL string) error {
	var result struct {
		FrameID string `json:"frameId"`
	}
	if err := c.call(ctx, sessionID, "Page.navigate", map[string]any{
		"url": targetURL,
	}, &result); err != nil {
		return err
	}
	if result.FrameID == "" {
		return errors.New("navigation frame ID missing from response")
	}
	return nil
}

func (c *cdpClient) closeTarget(ctx context.Context, targetID string) error {
	var result struct {
		Success bool `json:"success"`
	}
	if err := c.call(ctx, "", "Target.closeTarget", map[string]any{"targetId": targetID}, &result); err != nil {
		return err
	}
	if !result.Success {
		return errors.New("browser target close was not acknowledged")
	}
	return nil
}

func (c *cdpClient) getCookies(ctx context.Context, sessionID, targetURL string) ([]BrowserCookie, error) {
	var result struct {
		Cookies []struct {
			Name     string  `json:"name"`
			Value    string  `json:"value"`
			Domain   string  `json:"domain"`
			Path     string  `json:"path"`
			Expires  float64 `json:"expires"`
			HTTPOnly bool    `json:"httpOnly"`
			Secure   bool    `json:"secure"`
			Session  bool    `json:"session"`
		} `json:"cookies"`
	}
	if err := c.call(ctx, sessionID, "Network.getCookies", map[string]any{
		"urls": []string{targetURL},
	}, &result); err != nil {
		return nil, err
	}

	cookies := make([]BrowserCookie, 0, len(result.Cookies))
	for _, cookie := range result.Cookies {
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

func (c *cdpClient) call(ctx context.Context, sessionID, method string, params any, out any) error {
	id := c.nextID.Add(1)
	respCh := make(chan cdpMessage, 1)

	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	msg := map[string]any{
		"id":     id,
		"method": method,
	}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	if params != nil {
		msg["params"] = params
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal %s request: %w", method, err)
	}

	c.writeMu.Lock()
	err = wsutil.WriteClientText(c.conn, payload)
	c.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("send %s request: %w", method, err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-c.readErr:
		return err
	case resp := <-respCh:
		if resp.Error != nil {
			return fmt.Errorf("%s: %s (%d)", method, resp.Error.Message, resp.Error.Code)
		}
		if out == nil || len(resp.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("decode %s response: %w", method, err)
		}
		return nil
	}
}

func (c *cdpClient) readLoop() {
	for {
		payload, op, err := wsutil.ReadServerData(c.conn)
		if err != nil {
			c.failPending(err)
			return
		}
		if op != ws.OpText {
			continue
		}

		var msg cdpMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			c.failPending(fmt.Errorf("decode websocket message: %w", err))
			return
		}

		if msg.ID == 0 {
			select {
			case c.events <- msg:
			default:
			}
			continue
		}

		c.pendingMu.Lock()
		respCh := c.pending[msg.ID]
		c.pendingMu.Unlock()
		if respCh == nil {
			continue
		}
		respCh <- msg
	}
}

func (c *cdpClient) failPending(err error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	select {
	case c.readErr <- err:
	default:
	}

	for id, respCh := range c.pending {
		delete(c.pending, id)
		select {
		case respCh <- cdpMessage{Error: &cdpError{Message: err.Error()}}:
		default:
		}
	}
}
