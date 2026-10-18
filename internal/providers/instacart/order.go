package instacart

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	locationtypes "careme/internal/locations/types"
	ordertypes "careme/internal/orders/types"
	"careme/internal/routing"
)

const (
	linkLifetime  = 30 * 24 * time.Hour
	failureNotice = "Could not prepare your Instacart list. Try again, chef."
)

type OrderOptions struct {
	Cache  cache.Cache
	Client *Client
	Origin string
	Auth   auth.AuthClient
}

type OrderHandler struct {
	cache  cache.Cache
	client *Client
	origin string
	auth   auth.AuthClient
}

func NewOrderHandler(options OrderOptions) (*OrderHandler, error) {
	if options.Cache == nil || options.Client == nil || options.Auth == nil {
		return nil, fmt.Errorf("Instacart ordering requires cache, client, and authentication")
	}
	origin, err := url.Parse(options.Origin)
	if err != nil || (origin.Scheme != "https" && origin.Scheme != "http") || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") {
		return nil, fmt.Errorf("Instacart ordering requires a valid public origin")
	}
	return &OrderHandler{cache: options.Cache, client: options.Client, origin: strings.TrimRight(options.Origin, "/"), auth: options.Auth}, nil
}

func (a *OrderHandler) Register(routes routing.Registrar) {
	routes.Handle("POST /instacart/order", http.NewCrossOriginProtection().Handler(http.HandlerFunc(a.handleOrder)))
}

func (a *OrderHandler) handleOrder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	userID, err := a.auth.GetUserIDFromRequest(r)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			http.Error(w, "must be logged in", http.StatusUnauthorized)
		} else {
			http.Error(w, "Unable to load account", http.StatusInternalServerError)
		}
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid Instacart order", http.StatusBadRequest)
		return
	}
	returnURL := r.PostForm.Get("return_url")
	listKey := r.PostForm.Get("list_key")
	var items []ai.ShoppingItem
	var location locationtypes.Location
	if !localReturnURL(returnURL) || strings.TrimSpace(listKey) == "" || len(listKey) > 256 || json.Unmarshal([]byte(r.PostForm.Get("location")), &location) != nil || location.ID == "" || json.Unmarshal([]byte(r.PostForm.Get("items")), &items) != nil || len(items) == 0 {
		http.Error(w, "Invalid Instacart order", http.StatusBadRequest)
		return
	}
	payload := shoppingList{Title: "Careme shopping list", LinkType: "shopping_list", ExpiresIn: 30, LandingPage: landingPage{LinkbackURL: a.origin + returnURL}}
	for _, item := range items {
		if strings.TrimSpace(item.Name) == "" {
			http.Error(w, "Invalid Instacart item", http.StatusBadRequest)
			return
		}
		display := item.Name
		if item.Needed != "" {
			display += " — " + item.Needed
		}
		payload.Items = append(payload.Items, lineItem{Name: item.Name, DisplayText: display, Measurements: parseMeasurement(item.Needed)})
	}
	destination, err := a.shoppingLink(r, userID, payload)
	if err != nil {
		slog.ErrorContext(r.Context(), "Prepare Instacart shopping list", "error", err)
		back, _ := url.Parse(returnURL)
		query := back.Query()
		query.Set("instacart_status", "failed")
		back.RawQuery = query.Encode()
		http.Redirect(w, r, back.String(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

type linkRecord struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (a *OrderHandler) shoppingLink(r *http.Request, userID string, payload shoppingList) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode Instacart list: %w", err)
	}
	// Include the environment so development links never leak into production.
	sum := sha256.Sum256(append([]byte(a.client.baseURL+"\n"), body...))
	key := "instacart_shopping_links/" + userID + "/" + hex.EncodeToString(sum[:])
	reader, err := a.cache.Get(r.Context(), key)
	if err == nil {
		var record linkRecord
		decodeErr := json.NewDecoder(reader).Decode(&record)
		_ = reader.Close()
		if decodeErr != nil {
			return "", fmt.Errorf("decode cached Instacart link: %w", decodeErr)
		}
		if !validLink(record.URL) {
			return "", fmt.Errorf("invalid cached Instacart link")
		}
		if time.Now().Before(record.ExpiresAt) {
			return record.URL, nil
		}
	} else if !errors.Is(err, cache.ErrNotFound) {
		return "", fmt.Errorf("load Instacart link: %w", err)
	}
	started := time.Now()
	destination, err := a.client.createLink(r.Context(), body)
	if err != nil {
		return "", err
	}
	record, err := json.Marshal(linkRecord{URL: destination, ExpiresAt: started.Add(linkLifetime)})
	if err != nil {
		return "", fmt.Errorf("encode Instacart link record: %w", err)
	}
	if err := a.cache.Put(r.Context(), key, string(record), cache.Unconditional()); err != nil {
		return "", fmt.Errorf("save Instacart link: %w", err)
	}
	return destination, nil
}

func localReturnURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && !u.IsAbs() && u.Host == "" && strings.HasPrefix(raw, "/") && !strings.HasPrefix(u.Path, "//") && !strings.ContainsAny(u.Path, "\\\r\n")
}

func OrderView(input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
	items, err := json.Marshal(input.Items)
	if err != nil {
		return nil, fmt.Errorf("encode shopping items for Instacart: %w", err)
	}
	location, err := json.Marshal(input.Location)
	if err != nil {
		return nil, fmt.Errorf("encode Instacart source location: %w", err)
	}
	view := &ordertypes.OrderView{
		ActionURL: "/instacart/order", ActionLabel: "Shop with Instacart",
		Description: "Choose a store on Instacart, then review products and quantities before checkout.",
		Fields:      ordertypes.OrderFields{Items: string(items), Location: string(location), ListKey: input.ListKey, ReturnURL: input.ReturnURL},
	}
	if input.Query.Get("instacart_status") == "failed" {
		view.Notice = failureNotice
	}
	return view, nil
}
