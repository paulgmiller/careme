package recipes

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"careme/internal/ai"
	"careme/internal/auth"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stepImageGenerator struct {
	calls int
	step  int
	err   error
}

func (g *stepImageGenerator) GenerateRecipeImage(context.Context, ai.Recipe) (*ai.GeneratedImage, error) {
	panic("unexpected dish image request")
}

func (g *stepImageGenerator) GenerateStepImage(_ context.Context, _ ai.Recipe, step int) (*ai.GeneratedImage, error) {
	g.calls++
	g.step = step
	if g.err != nil {
		return nil, g.err
	}
	return &ai.GeneratedImage{Body: bytes.NewReader(mockRecipeImage)}, nil
}

type noStepImageSession struct{ auth.AuthClient }

func (noStepImageSession) GetUserIDFromRequest(*http.Request) (string, error) {
	return "", auth.ErrNoSession
}

func stepImageRequest(method, hash, step string) *http.Request {
	req := httptest.NewRequest(method, "/recipe/"+hash+"/steps/"+step+"/image", nil)
	req.SetPathValue("hash", hash)
	req.SetPathValue("step", step)
	return req
}

func TestStepImageGenerationAndCacheReuse(t *testing.T) {
	g := &stepImageGenerator{}
	s := newTestServer(t, withImageGenerator(g))
	recipe := ai.Recipe{Title: "Soup", Instructions: []string{"Chop carrots.", "Simmer carrots."}}
	require.NoError(t, s.SaveRecipe(t.Context(), recipe))
	hash := recipe.ComputeHash()
	for range 2 {
		rr := httptest.NewRecorder()
		s.handleGenerateStepImage(rr, stepImageRequest(http.MethodPost, hash, "2"))
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), "Pencil sketch of step 2")
		assert.Contains(t, rr.Body.String(), `<details open>`)
	}
	assert.Equal(t, 1, g.calls)
	assert.Equal(t, 2, g.step)

	rr := httptest.NewRecorder()
	s.handleStepImage(rr, stepImageRequest(http.MethodGet, hash, "2"))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, mockRecipeImage, rr.Body.Bytes())
}

func TestStepImageRejectsInvalidStepAndAnonymousGeneration(t *testing.T) {
	g := &stepImageGenerator{}
	s := newTestServer(t, withImageGenerator(g))
	recipe := ai.Recipe{Title: "Soup", Instructions: []string{"Simmer."}}
	require.NoError(t, s.SaveRecipe(t.Context(), recipe))
	hash := recipe.ComputeHash()
	for _, step := range []string{"0", "2", "oops"} {
		rr := httptest.NewRecorder()
		s.handleGenerateStepImage(rr, stepImageRequest(http.MethodPost, hash, step))
		assert.Equal(t, http.StatusNotFound, rr.Code)
		rr = httptest.NewRecorder()
		s.handleStepImage(rr, stepImageRequest(http.MethodGet, hash, step))
		assert.Equal(t, http.StatusNotFound, rr.Code)
	}
	rr := httptest.NewRecorder()
	s.handleStepImage(rr, stepImageRequest(http.MethodGet, hash, "1"))
	assert.Equal(t, http.StatusNotFound, rr.Code)
	s.clerk = noStepImageSession{AuthClient: auth.DefaultMock()}
	rr = httptest.NewRecorder()
	s.handleGenerateStepImage(rr, stepImageRequest(http.MethodPost, hash, "1"))
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Zero(t, g.calls)
}

func TestStepImageFailureDoesNotCacheImage(t *testing.T) {
	g := &stepImageGenerator{err: errors.New("provider unavailable")}
	s := newTestServer(t, withImageGenerator(g))
	recipe := ai.Recipe{Title: "Soup", Instructions: []string{"Simmer."}}
	require.NoError(t, s.SaveRecipe(t.Context(), recipe))
	hash := recipe.ComputeHash()
	rr := httptest.NewRecorder()
	s.handleGenerateStepImage(rr, stepImageRequest(http.MethodPost, hash, "1"))
	assert.Equal(t, http.StatusBadGateway, rr.Code)
	exists, err := s.images.Exists(t.Context(), stepImageID(hash, 1))
	require.NoError(t, err)
	assert.False(t, exists)
}
