// Package mnfoodclubstub implements the persistent partner test API for caremetest.
package mnfoodclubstub

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	ordertypes "careme/internal/orders/types"
)

const testToken = "caremetest-mnfoodclub-stub"

type item struct {
	ProductID string `json:"product_id"`
	VariantID string `json:"variant_id,omitempty"`
	Quantity  *int   `json:"quantity,omitempty"`
}
type hints struct {
	Emails          []string `json:"emails"`
	UserID          string   `json:"user_id,omitempty"`
	StorefrontBrand string   `json:"storefront_brand,omitempty"`
	ZipCode         string   `json:"zip_code,omitempty"`
}
type request struct {
	Items         []item `json:"items"`
	IdentityHints hints  `json:"identity_hints"`
}
type response struct {
	ordertypes.Transfer
	UserID string `json:"user_id"`
}
type record struct {
	Created  time.Time
	Key      string
	Request  request
	Response response
}
type server struct {
	store     transferStore
	publicURL string
}

// New mounts the API and its single public HTML page under publicURL's path.
func New(publicURL string, store transferStore) http.Handler {
	s := &server{publicURL: strings.TrimRight(publicURL, "/"), store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.page)
	mux.HandleFunc("GET /ready", s.ready)
	mux.HandleFunc("GET /live", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /v1/cart-transfers", s.create)
	mux.HandleFunc("GET /v1/cart-transfers/{id}", s.get)
	base, _ := url.Parse(s.publicURL)
	handler := http.StripPrefix(base.Path, mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if base.Path != "" && r.URL.Path == base.Path {
			http.Redirect(w, r, base.Path+"/", http.StatusMovedPermanently)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

func failure(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": code})
}

func authorized(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		failure(w, 401, "unauthorized")
		return false
	}
	return true
}

func (s *server) create(w http.ResponseWriter, r *http.Request) {
	if !authorized(w, r) {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if strings.TrimSpace(key) == "" || len(key) > 255 {
		failure(w, 400, "invalid_idempotency_key")
		return
	}
	var input request
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil || len(input.Items) == 0 {
		failure(w, 400, "invalid_request")
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		failure(w, 400, "invalid_request")
		return
	}
	// Normalize omitted quantities before storing or comparing idempotent requests.
	seen := make(map[string]bool)
	for i := range input.Items {
		item := &input.Items[i]
		if strings.TrimSpace(item.ProductID) == "" || (item.Quantity != nil && *item.Quantity <= 0) {
			failure(w, 400, "invalid_item")
			return
		}
		if seen[item.ProductID] {
			failure(w, 400, "duplicate_product_id")
			return
		}
		seen[item.ProductID] = true
		if item.Quantity == nil {
			item.Quantity = new(1)
		}
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		failure(w, 500, "internal_error")
		return
	}
	id := hex.EncodeToString(idBytes)
	response := response{Transfer: ordertypes.Transfer{TransferID: id, Status: ordertypes.TransferApplied, CartURL: s.publicURL + "/#" + id}, UserID: "stub-customer"}
	for _, item := range input.Items {
		response.Items = append(response.Items, ordertypes.TransferItem{ProductID: item.ProductID, AcceptedQuantity: *item.Quantity})
	}
	rec, err := s.store.create(r.Context(), record{Created: time.Now().UTC(), Key: key, Request: input, Response: response})
	if err != nil {
		slog.ErrorContext(r.Context(), "save stub transfer", "error", err)
		failure(w, 500, "storage_error")
		return
	}
	if !reflect.DeepEqual(rec.Request, input) {
		failure(w, 409, "idempotency_conflict")
		return
	}
	writeResponse(w, rec.Response)
}

func writeResponse(w http.ResponseWriter, result response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *server) get(w http.ResponseWriter, r *http.Request) {
	if !authorized(w, r) {
		return
	}
	rec, err := s.store.get(r.Context(), r.PathValue("id"))
	if errors.Is(err, errNotFound) {
		failure(w, 404, "transfer_not_found")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "read stub transfer", "error", err)
		failure(w, 500, "storage_error")
		return
	}
	writeResponse(w, rec.Response)
}

var page = template.Must(template.New("orders").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Food Club test orders</title></head><body><h1>Food Club test orders</h1><p>Test stub. All quantities are accepted. Orders are saved in CockroachDB. Reload to see new orders.</p>{{range .}}<section id="{{.Response.TransferID}}"><h2>{{.Response.TransferID}}</h2><p>{{.Created}} — {{.Response.Status}}</p><p>Key: {{.Key}}</p><p>Identity hints: {{.Request.IdentityHints}}</p><table><thead><tr><th>Product</th><th>Variant</th><th>Accepted packages</th></tr></thead><tbody>{{range .Request.Items}}<tr><td>{{.ProductID}}</td><td>{{.VariantID}}</td><td>{{.Quantity}}</td></tr>{{end}}</tbody></table></section>{{else}}<p>No orders yet.</p>{{end}}</body></html>`))

func (s *server) page(w http.ResponseWriter, r *http.Request) {
	records, err := s.store.list(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "list stub transfers", "error", err)
		http.Error(w, "Unable to load test orders.", 500)
		return
	}
	var body bytes.Buffer
	if err := page.Execute(&body, records); err != nil {
		slog.ErrorContext(r.Context(), "render stub transfers", "error", err)
		http.Error(w, "Unable to render test orders.", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = body.WriteTo(w)
}

func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ping(r.Context()); err != nil {
		http.Error(w, "Database unavailable.", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
