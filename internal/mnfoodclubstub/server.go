// Package mnfoodclubstub implements a disposable partner API for caremetest.
package mnfoodclubstub

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	ordertypes "careme/internal/orders/types"
)

const TestToken = "caremetest-mnfoodclub-stub"

type Item struct {
	ProductID string `json:"product_id"`
	VariantID string `json:"variant_id,omitempty"`
	Quantity  *int   `json:"quantity,omitempty"`
}
type Hints struct {
	Emails          []string `json:"emails,omitempty"`
	UserID          string   `json:"user_id,omitempty"`
	StorefrontBrand string   `json:"storefront_brand,omitempty"`
	ZipCode         string   `json:"zip_code,omitempty"`
}
type Request struct {
	Items         []Item `json:"items"`
	IdentityHints Hints  `json:"identity_hints"`
}
type Response struct {
	ordertypes.Transfer
	UserID string `json:"user_id"`
}
type record struct {
	Created  time.Time
	Key      string
	Request  Request
	Response Response
}
type Server struct {
	mu        sync.Mutex
	records   []*record
	byID      map[string]*record
	byKey     map[string]*record
	publicURL string
}

// New mounts the API and its single public HTML page under publicURL's path.
func New(publicURL string) http.Handler {
	s := &Server{publicURL: strings.TrimRight(publicURL, "/"), byID: make(map[string]*record), byKey: make(map[string]*record)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.page)
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
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
	if r.Header.Get("Authorization") != "Bearer "+TestToken {
		failure(w, 401, "unauthorized")
		return false
	}
	return true
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	if !authorized(w, r) {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if strings.TrimSpace(key) == "" || len(key) > 255 {
		failure(w, 400, "invalid_idempotency_key")
		return
	}
	var input Request
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := s.byKey[key]; old != nil {
		if !reflect.DeepEqual(old.Request, input) {
			failure(w, 409, "idempotency_conflict")
			return
		}
		writeResponse(w, old.Response)
		return
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		failure(w, 500, "internal_error")
		return
	}
	id := hex.EncodeToString(idBytes)
	response := Response{Transfer: ordertypes.Transfer{TransferID: id, Status: ordertypes.TransferApplied, CartURL: s.publicURL + "/#" + id}, UserID: "stub-customer"}
	for _, item := range input.Items {
		response.Items = append(response.Items, ordertypes.TransferItem{ProductID: item.ProductID, AcceptedQuantity: *item.Quantity})
	}
	rec := &record{Created: time.Now().UTC(), Key: key, Request: input, Response: response}
	s.records = append(s.records, rec)
	s.byID[id] = rec
	s.byKey[key] = rec
	writeResponse(w, response)
}

func writeResponse(w http.ResponseWriter, result Response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	if !authorized(w, r) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.byID[r.PathValue("id")]
	if rec == nil {
		failure(w, 404, "transfer_not_found")
		return
	}
	writeResponse(w, rec.Response)
}

var page = template.Must(template.New("orders").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Food Club test orders</title></head><body><h1>Food Club test orders</h1><p>Test stub. All quantities are accepted. State is lost on restart. Reload to see new orders.</p>{{range .}}<section id="{{.Response.TransferID}}"><h2>{{.Response.TransferID}}</h2><p>{{.Created}} — {{.Response.Status}}</p><p>Key: {{.Key}}</p><p>Identity hints: {{.Request.IdentityHints}}</p><table><thead><tr><th>Product</th><th>Variant</th><th>Accepted packages</th></tr></thead><tbody>{{range .Request.Items}}<tr><td>{{.ProductID}}</td><td>{{.VariantID}}</td><td>{{.Quantity}}</td></tr>{{end}}</tbody></table></section>{{else}}<p>No orders yet.</p>{{end}}</body></html>`))

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	records := make([]*record, len(s.records))
	for i, rec := range s.records {
		records[len(records)-1-i] = rec
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = page.Execute(w, records)
}
