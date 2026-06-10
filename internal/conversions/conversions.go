package conversions

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"

	"careme/internal/cache"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Event string

const (
	EventSignIn             Event = "sign_in"
	EventRecipeGeneration   Event = "recipe_generation"
	EventRecipeRegeneration Event = "recipe_regeneration"
	EventLocationLookup     Event = "location_lookup"
	EventRecipeSave         Event = "recipe_save"
	EventRecipeQuestion     Event = "recipe_question"
	EventRecipeCooked       Event = "recipe_cooked"
)

const (
	metricName             = "careme.conversions"
	oncePrefix             = "conversions/once/"
	browserPendingPrefix   = "conversions/browser-pending/"
	browserConsumedPrefix  = "conversions/browser-consumed/"
	htmxTriggerHeader      = "HX-Trigger"
	htmxConversionEvent    = "careme:conversion"
	envGoogleLabelsJSON    = "GOOGLE_CONVERSION_LABELS_JSON"
	envConversionHead      = "CONVERSION_HEAD_SNIPPET"
	envConversionEventBody = "CONVERSION_EVENT_SCRIPT"
)

var cleanKeyPart = regexp.MustCompile(`[^A-Za-z0-9._=-]+`)

type Recorder struct {
	cache   cache.Cache
	counter metric.Int64Counter
}

func NewRecorder(c cache.Cache) *Recorder {
	counter, err := otel.Meter("careme/conversions").Int64Counter(metricName)
	if err != nil {
		slog.Warn("failed to create conversion counter", "error", err)
	}
	return &Recorder{cache: c, counter: counter}
}

func (r *Recorder) Record(ctx context.Context, event Event) {
	if r == nil || event == "" || r.counter == nil {
		return
	}
	r.counter.Add(ctx, 1, metric.WithAttributes(attribute.String("event", string(event))))
}

func (r *Recorder) RecordOnce(ctx context.Context, event Event, key string) bool {
	if r == nil || r.cache == nil || event == "" || strings.TrimSpace(key) == "" {
		r.Record(ctx, event)
		return true
	}
	cacheKey := oncePrefix + safeKeyPart(string(event)) + "/" + safeKeyPart(key)
	if err := r.cache.Put(ctx, cacheKey, "1", cache.IfNoneMatch()); err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			return false
		}
		slog.WarnContext(ctx, "failed to store conversion once key", "event", event, "error", err)
	}
	r.Record(ctx, event)
	return true
}

func (r *Recorder) MarkBrowserPending(ctx context.Context, event Event, key string) {
	if r == nil || r.cache == nil || event == "" || strings.TrimSpace(key) == "" {
		return
	}
	if err := r.cache.Put(ctx, browserPendingKey(event, key), "1", cache.IfNoneMatch()); err != nil && !errors.Is(err, cache.ErrAlreadyExists) {
		slog.WarnContext(ctx, "failed to mark pending browser conversion", "event", event, "error", err)
	}
}

func (r *Recorder) ConsumeBrowserPending(ctx context.Context, event Event, key string) bool {
	if r == nil || r.cache == nil || event == "" || strings.TrimSpace(key) == "" {
		return false
	}
	ok, err := r.cache.Exists(ctx, browserPendingKey(event, key))
	if err != nil {
		slog.WarnContext(ctx, "failed to check pending browser conversion", "event", event, "error", err)
		return false
	}
	if !ok {
		return false
	}
	if err := r.cache.Put(ctx, browserConsumedKey(event, key), "1", cache.IfNoneMatch()); err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			return false
		}
		slog.WarnContext(ctx, "failed to store consumed browser conversion", "event", event, "error", err)
	}
	return true
}

func TriggerHTMX(w http.ResponseWriter, event Event) {
	if w == nil || event == "" {
		return
	}
	payload, err := json.Marshal(map[string]map[string]string{
		htmxConversionEvent: {"event": string(event)},
	})
	if err != nil {
		return
	}
	w.Header().Set(htmxTriggerHeader, string(payload))
}

type BrowserConfig struct {
	GoogleTagID       string
	GoogleLabels      map[Event]string
	HeadSnippet       string
	EventScriptBody   string
	LegacySignInLabel string
}

func BrowserConfigFromEnv(googleTagID, legacySignInLabel string) BrowserConfig {
	labels := map[Event]string{}
	if raw := strings.TrimSpace(os.Getenv(envGoogleLabelsJSON)); raw != "" {
		var decoded map[string]string
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			slog.Warn("failed to parse GOOGLE_CONVERSION_LABELS_JSON", "error", err)
		} else {
			for key, value := range decoded {
				event := Event(strings.TrimSpace(key))
				label := strings.TrimSpace(value)
				if event != "" && label != "" {
					labels[event] = label
				}
			}
		}
	}
	if legacySignInLabel = strings.TrimSpace(legacySignInLabel); legacySignInLabel != "" {
		if _, ok := labels[EventSignIn]; !ok {
			labels[EventSignIn] = legacySignInLabel
		}
	}
	return BrowserConfig{
		GoogleTagID:       strings.TrimSpace(googleTagID),
		GoogleLabels:      labels,
		HeadSnippet:       strings.TrimSpace(os.Getenv(envConversionHead)),
		EventScriptBody:   strings.TrimSpace(os.Getenv(envConversionEventBody)),
		LegacySignInLabel: legacySignInLabel,
	}
}

func (c BrowserConfig) GoogleConversionTag(event Event) string {
	if c.GoogleTagID == "" {
		return ""
	}
	label := strings.TrimSpace(c.GoogleLabels[event])
	if label == "" {
		return ""
	}
	if strings.Contains(label, "/") {
		return label
	}
	return c.GoogleTagID + "/" + label
}

func (c BrowserConfig) Script(pending Event) template.HTML {
	googleLabels := make(map[string]string, len(c.GoogleLabels))
	for event := range c.GoogleLabels {
		if tag := c.GoogleConversionTag(event); tag != "" {
			googleLabels[string(event)] = tag
		}
	}
	labelsJSON, _ := json.Marshal(googleLabels)
	pendingJSON, _ := json.Marshal(string(pending))
	eventBody := c.EventScriptBody
	if eventBody == "" {
		eventBody = ""
	}
	var b strings.Builder
	if c.HeadSnippet != "" {
		b.WriteString(c.HeadSnippet)
		b.WriteByte('\n')
	}
	b.WriteString(`<script>
(function() {
  const googleLabels = `)
	b.Write(labelsJSON)
	b.WriteString(`;
  const pendingEvent = `)
	b.Write(pendingJSON)
	b.WriteString(`;
  const customConversion = function(eventName) {
`)
	b.WriteString(eventBody)
	b.WriteString(`
  };
  window.caremeConversion = function(eventName) {
    if (!eventName) return;
    const googleTag = googleLabels[eventName];
    if (googleTag && typeof gtag === "function") {
      gtag("event", "conversion", { send_to: googleTag });
    }
    customConversion(eventName);
  };
  document.addEventListener("careme:conversion", function(event) {
    window.caremeConversion(event.detail && event.detail.event);
  });
  if (pendingEvent) {
    window.caremeConversion(pendingEvent);
  }
})();
</script>`)
	return template.HTML(b.String())
}

func browserPendingKey(event Event, key string) string {
	return browserPendingPrefix + safeKeyPart(string(event)) + "/" + safeKeyPart(key)
}

func browserConsumedKey(event Event, key string) string {
	return browserConsumedPrefix + safeKeyPart(string(event)) + "/" + safeKeyPart(key)
}

func safeKeyPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_"
	}
	return cleanKeyPart.ReplaceAllString(value, "_")
}

type RecordingRecorder struct {
	mu     sync.Mutex
	Events []Event
}

func (r *RecordingRecorder) Record(_ context.Context, event Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = append(r.Events, event)
}
