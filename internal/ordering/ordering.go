// Package ordering coordinates provider handoffs without prescribing how users
// authenticate or how a provider matches products.
package ordering

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"careme/internal/cache"
)

type Workflow string

const (
	CartTransfer     Workflow = "cart_transfer"
	ShoppingListLink Workflow = "shopping_list_link"
)

type ResultKind string

const (
	CartUpdated         ResultKind = "cart_updated"
	ShoppingListCreated ResultKind = "shopping_list_created"
)

type Store struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Chain    string `json:"chain"`
}

type Measurement struct {
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
}

// Item keeps textual recipe amounts separate from optional structured provider
// measurements and package counts. Product references are provider-qualified.
type Item struct {
	ProductID       string        `json:"id"`
	Provider        string        `json:"provider,omitempty"`
	Name            string        `json:"name"`
	Needed          string        `json:"quantity"`
	Size            string        `json:"size,omitempty"`
	Aisle           string        `json:"aisle_number,omitempty"`
	Buy             *float64      `json:"buy,omitempty"`
	UPCs            []string      `json:"upcs,omitempty"`
	Measurements    []Measurement `json:"measurements,omitempty"`
	PackageQuantity float64       `json:"package_quantity,omitempty"`
}

type Request struct {
	UserID      string
	Hash        string
	Fingerprint string
	Title       string
	LinkBackURL string
	Source      Store
	Items       []Item
}

type Prepared struct {
	Sent []Item
	Gaps []Item
	Data any
}

type Result struct {
	Kind        ResultKind `json:"kind,omitempty"`
	URL         string     `json:"url,omitempty"`
	ExpiresAt   time.Time  `json:"expires_at,omitempty"`
	Added       int        `json:"added"`
	Sent        []Item     `json:"sent,omitempty"`
	Gaps        []Item     `json:"gaps,omitempty"`
	Status      string     `json:"status"`
	Fingerprint string     `json:"fingerprint,omitempty"`
}

// Provider owns authentication and product resolution. Prepare must not change
// external carts or create shopping links. Its Sent items identify the products
// to add or the unresolved items to include in a shopping link, respectively.
// Submit must return the workflow's result kind and an absolute HTTP(S) review
// URL on success. Only cart results may claim confirmed additions or gaps.
// The service owns Status and Fingerprint; adapters supply the destination,
// expiration, and confirmed cart addition count.
type Provider interface {
	ID() string
	Workflow() Workflow
	Supports(Request) bool
	Presentation(Request) Presentation
	Prepare(context.Context, Request) (Prepared, error)
	Submit(context.Context, Request, Prepared) (Result, error)
}

type Presentation struct {
	Brand       string
	URL         string
	ActionLabel string
	Description string
}

// ConnectionRequired lets the caller initiate the adapter's account connection.
type ConnectionRequired struct {
	Provider string
	Err      error
}

func (e *ConnectionRequired) Error() string {
	return fmt.Sprintf("connect ordering provider %s", e.Provider)
}
func (e *ConnectionRequired) Unwrap() error { return e.Err }

// StepError distinguishes safe preparation failures from failures after a
// potentially irreversible cart operation.
type StepError struct {
	Code string
	Err  error
}

func (e *StepError) Error() string { return fmt.Sprintf("ordering %s: %v", e.Code, e.Err) }
func (e *StepError) Unwrap() error { return e.Err }

type Service struct {
	cache     cache.Cache
	providers []Provider
	byID      map[string]Provider
}

func New(c cache.Cache, providers []Provider) *Service {
	s := &Service{cache: c, byID: make(map[string]Provider)}
	for _, p := range providers {
		if p.ID() == "" || s.byID[p.ID()] != nil || (p.Workflow() != CartTransfer && p.Workflow() != ShoppingListLink) {
			panic("invalid or duplicate ordering provider")
		}
		s.byID[p.ID()] = p
		s.providers = append(s.providers, p)
	}
	return s
}

func (s *Service) Provider(id string) (Provider, bool) { p, ok := s.byID[id]; return p, ok }
func (s *Service) Providers() []Provider               { return append([]Provider(nil), s.providers...) }

func contentFingerprint(req Request) (string, error) {
	body, err := json.Marshal(struct {
		Title       string
		LinkBackURL string
		Source      Store
		Items       []Item
	}{req.Title, req.LinkBackURL, req.Source, req.Items})
	if err != nil {
		return "", fmt.Errorf("encode shopping list content: %w", err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func transferKey(p Provider, req Request) (string, error) {
	if p.ID() == "kroger" && p.Workflow() == CartTransfer {
		return "kroger_cart_transfers/" + req.UserID + "/" + req.Hash, nil
	}
	prefix := "ordering_transfers/" + url.PathEscape(p.ID()) + "/" + url.PathEscape(req.UserID) + "/"
	if p.Workflow() == ShoppingListLink {
		fingerprint, err := contentFingerprint(req)
		return prefix + fingerprint, err
	}
	return prefix + url.PathEscape(req.Hash), nil
}

func (s *Service) load(ctx context.Context, p Provider, req Request) (Result, error) {
	key, err := transferKey(p, req)
	if err != nil {
		return Result{}, err
	}
	reader, err := s.cache.Get(ctx, key)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = reader.Close() }()
	var result Result
	if err := json.NewDecoder(reader).Decode(&result); err != nil {
		return Result{}, fmt.Errorf("decode ordering result: %w", err)
	}
	if p.Workflow() == ShoppingListLink {
		if !result.ExpiresAt.IsZero() && !time.Now().Before(result.ExpiresAt) {
			return Result{}, cache.ErrNotFound
		}
	} else {
		if result.Kind == "" {
			result.Kind = CartUpdated
		}
		if p.ID() == "kroger" {
			for i := range result.Sent {
				if result.Sent[i].PackageQuantity == 0 {
					result.Sent[i].PackageQuantity = 1
				}
			}
		}
		if result.Status == "complete" && result.Fingerprint != req.Fingerprint {
			result.Status = "changed"
		}
	}
	return result, nil
}

func (s *Service) Load(ctx context.Context, id string, req Request) (Result, error) {
	if err := validateIdentity(req); err != nil {
		return Result{}, err
	}
	p, ok := s.Provider(id)
	if !ok {
		return Result{}, fmt.Errorf("unknown ordering provider %q", id)
	}
	return s.load(ctx, p, req)
}

func validateIdentity(req Request) error {
	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.Hash) == "" {
		return errors.New("ordering requires a user ID and finalized list hash")
	}
	return nil
}

func validateResult(p Provider, result Result) error {
	expected := CartUpdated
	if p.Workflow() == ShoppingListLink {
		expected = ShoppingListCreated
	}
	if result.Kind != expected {
		return fmt.Errorf("provider %s returned outcome %q, expected %q", p.ID(), result.Kind, expected)
	}
	destination, err := url.Parse(result.URL)
	if err != nil || destination.Host == "" || (destination.Scheme != "http" && destination.Scheme != "https") {
		return fmt.Errorf("provider %s did not return an absolute HTTP(S) review URL", p.ID())
	}
	if p.Workflow() == ShoppingListLink && (result.Added != 0 || len(result.Gaps) != 0) {
		return fmt.Errorf("provider %s reported cart additions or matching gaps for a shopping link", p.ID())
	}
	return nil
}

func (s *Service) save(ctx context.Context, p Provider, req Request, result Result, options cache.PutOptions) error {
	key, err := transferKey(p, req)
	if err != nil {
		return err
	}
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return s.cache.Put(ctx, key, string(body), options)
}

// Transfer prepares before reserving cart transfers, so matching failures remain
// retryable. Link creation deliberately uses no permanent reservation: it cannot
// purchase items and a failed creation can safely be retried.
func (s *Service) Transfer(ctx context.Context, id string, req Request) (Result, error) {
	if err := validateIdentity(req); err != nil {
		return Result{}, err
	}
	p, ok := s.Provider(id)
	if !ok {
		return Result{}, fmt.Errorf("unknown ordering provider %q", id)
	}
	if !p.Supports(req) {
		return Result{}, fmt.Errorf("ordering provider %q does not support this list", id)
	}
	if result, err := s.load(ctx, p, req); err == nil {
		return result, nil
	} else if !errors.Is(err, cache.ErrNotFound) {
		return Result{}, &StepError{Code: "start_failed", Err: err}
	}
	prepared, err := p.Prepare(ctx, req)
	if err != nil {
		var connection *ConnectionRequired
		var step *StepError
		if errors.As(err, &connection) || errors.As(err, &step) {
			return Result{}, err
		}
		return Result{}, &StepError{Code: "prepare_failed", Err: err}
	}
	if p.Workflow() == CartTransfer {
		if len(prepared.Sent) == 0 {
			return Result{}, &StepError{Code: "no_matches", Err: errors.New("no matching products")}
		}
		pending := Result{Kind: CartUpdated, Status: "pending", Sent: prepared.Sent, Gaps: prepared.Gaps}
		if err := s.save(ctx, p, req, pending, cache.IfNoneMatch()); err != nil {
			if errors.Is(err, cache.ErrAlreadyExists) {
				if result, err := s.load(ctx, p, req); err == nil {
					return result, nil
				}
			}
			return Result{}, &StepError{Code: "start_failed", Err: err}
		}
	}
	result, err := p.Submit(ctx, req, prepared)
	var outputErr error
	if err == nil {
		outputErr = validateResult(p, result)
		err = outputErr
	}
	if err != nil {
		if p.Workflow() == ShoppingListLink {
			return Result{}, &StepError{Code: "submit_failed", Err: err}
		}
		result = Result{Kind: CartUpdated, Status: "uncertain", URL: result.URL, Sent: prepared.Sent, Gaps: prepared.Gaps}
	} else {
		result.Status = "complete"
		result.Sent = prepared.Sent
		if p.Workflow() == CartTransfer {
			result.Gaps = prepared.Gaps
			result.Fingerprint = req.Fingerprint
		} else {
			result.Fingerprint, err = contentFingerprint(req)
			if err != nil {
				return Result{}, &StepError{Code: "confirmation_failed", Err: err}
			}
		}
	}
	if err := s.save(ctx, p, req, result, cache.Unconditional()); err != nil {
		return Result{}, &StepError{Code: "confirmation_failed", Err: err}
	}
	if outputErr != nil {
		return result, &StepError{Code: "submit_failed", Err: outputErr}
	}
	return result, nil
}
