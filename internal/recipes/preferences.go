package recipes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/routing"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
)

const preferencePrefix = "preference-suggestions/"

type preferenceProposal struct {
	ID      string
	Hash    string
	Notes   string
	Before  string
	After   string
	Changes []ai.PreferenceChange
	State   string
	Expires time.Time
	Message string `json:"-"`
}

func preferenceKey(userID, hash string) string {
	sum := sha256.Sum256([]byte(userID + "\x00" + hash))
	return preferencePrefix + hex.EncodeToString(sum[:])
}

func (s *server) loadPreferenceProposal(ctx context.Context, userID, hash string) (*preferenceProposal, error) {
	reader, err := s.Cache.Get(ctx, preferenceKey(userID, hash))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := reader.Close(); err != nil {
			slog.ErrorContext(ctx, "close preference proposal", "error", err)
		}
	}()
	var proposal preferenceProposal
	if err := json.NewDecoder(reader).Decode(&proposal); err != nil {
		return nil, fmt.Errorf("decode preference proposal: %w", err)
	}
	if !time.Now().Before(proposal.Expires) {
		return nil, cache.ErrNotFound
	}
	if proposal.State == "pending" && time.Now().After(proposal.Expires.Add(-24*time.Hour).Add(3*time.Minute)) {
		proposal.State = "failed"
	}
	return &proposal, nil
}

func (s *server) savePreferenceProposal(ctx context.Context, userID string, p *preferenceProposal) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return s.Cache.Put(ctx, preferenceKey(userID, p.Hash), string(data), cache.Unconditional())
}

// bestEffortSuggestPreferences is optional and never delays recipe generation for AI work.
func (s *server) bestEffortSuggestPreferences(ctx context.Context, user *utypes.User, p *generatorParams) {
	if user.ID == guestUser.ID || strings.TrimSpace(p.Instructions) == "" {
		return
	}
	if _, err := s.loadPreferenceProposal(ctx, user.ID, p.Hash()); err == nil {
		return
	} else if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "load preference suggestion", "error", err)
		return
	}
	proposal := &preferenceProposal{ID: rand.Text(), Hash: p.Hash(), Notes: p.Instructions, Before: user.Directive, State: "pending", Expires: time.Now().Add(24 * time.Hour)}
	if err := s.savePreferenceProposal(ctx, user.ID, proposal); err != nil {
		slog.ErrorContext(ctx, "start preference suggestion", "error", err)
		return
	}
	s.extractPreferences(ctx, user.ID, proposal)
}

func (s *server) extractPreferences(ctx context.Context, userID string, p *preferenceProposal) {
	ctx = context.WithoutCancel(ctx)
	owned := *p
	p = &owned
	s.wg.Go(func() {
		jobCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		changes, err := s.preferenceSuggester.SuggestPreferences(jobCtx, p.Notes, p.Before)
		if err == nil {
			p.After, err = ai.ApplyPreferenceChanges(p.Before, changes)
		}
		p.Changes = changes
		switch {
		case err != nil:
			p.State = "failed"
			slog.ErrorContext(ctx, "suggest cooking preferences", "error", err)
		case len(changes) == 0:
			p.State = "none"
		default:
			p.State = "ready"
		}
		// A dismissed or superseded proposal must not reappear after a slow extraction.
		current, loadErr := s.loadPreferenceProposal(ctx, userID, p.Hash)
		if loadErr != nil || current.ID != p.ID || current.State != "pending" {
			return
		}
		if err := s.savePreferenceProposal(ctx, userID, p); err != nil {
			slog.ErrorContext(ctx, "save preference suggestion", "error", err)
		}
	})
}

func (s *server) registerPreferenceRoutes(mux routing.Registrar) {
	mux.HandleFunc("GET /recipes/{hash}/preferences", s.handlePreferences)
	mux.HandleFunc("POST /recipes/{hash}/preferences", s.handlePreferences)
}

func (s *server) handlePreferences(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx := r.Context()
	user, err := s.storage.FromRequest(ctx, r, s.clerk)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			http.Error(w, "Sign in to save cooking preferences.", http.StatusUnauthorized)
		} else {
			http.Error(w, "Could not load your account.", http.StatusInternalServerError)
		}
		return
	}
	p, err := s.loadPreferenceProposal(ctx, user.ID, r.PathValue("hash"))
	if errors.Is(err, cache.ErrNotFound) {
		w.WriteHeader(http.StatusOK)
		return
	}
	if err != nil {
		http.Error(w, "Could not load cooking preferences.", http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid preference request.", http.StatusBadRequest)
			return
		}
		if r.FormValue("proposal_id") != p.ID {
			p.Message = "Preferences changed. Review the updated suggestion."
			s.renderPreferenceProposal(w, p)
			return
		}
		switch r.FormValue("action") {
		case "dismiss":
			if p.State != "ready" {
				s.renderPreferenceProposal(w, p)
				return
			}
			p.State = "dismissed"
			if err := s.savePreferenceProposal(ctx, user.ID, p); err != nil {
				p.State = "ready"
				p.Message = "Could not dismiss. Try again, chef."
			}
		case "save":
			if p.State != "ready" {
				s.renderPreferenceProposal(w, p)
				return
			}
			if user.Directive != p.Before {
				p.Before = user.Directive
				p.After = ""
				p.Changes = nil
				p.ID = rand.Text()
				p.State = "pending"
				p.Expires = time.Now().Add(24 * time.Hour)
				if err := s.savePreferenceProposal(ctx, user.ID, p); err != nil {
					http.Error(w, "Could not refresh preferences. Try again, chef.", http.StatusInternalServerError)
					return
				}
				s.extractPreferences(ctx, user.ID, p)
				// The worker owns p; render a separate value to avoid races.
				view := *p
				view.Message = "Your cooking preferences changed. Review a fresh suggestion before saving."
				s.renderPreferenceProposal(w, &view)
				return
			}
			updated, err := ai.ApplyPreferenceChanges(p.Before, p.Changes)
			if err != nil {
				http.Error(w, "Invalid preference suggestion.", http.StatusInternalServerError)
				return
			}
			user.Directive = updated
			if err := s.storage.Update(user); err != nil {
				p.Message = "Could not save. Try again, chef."
				s.renderPreferenceProposal(w, p)
				return
			}
			p.State = "saved"
			if err := s.savePreferenceProposal(ctx, user.ID, p); err != nil {
				slog.ErrorContext(ctx, "record saved preference suggestion", "error", err)
			}
		default:
			http.Error(w, "Invalid preference action.", http.StatusBadRequest)
			return
		}
	}
	s.renderPreferenceProposal(w, p)
}

func (s *server) renderPreferenceProposal(w http.ResponseWriter, p *preferenceProposal) {
	renderHTML(w, templates.ShoppingList, "preference_suggestion", p)
}
