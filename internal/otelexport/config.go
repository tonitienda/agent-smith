package otelexport

import (
	"fmt"
	"strings"
	"time"
)

// configReader is the slice of the layered config this package reads; *config.Config
// satisfies it via Decode. Kept as a tiny consumer-side interface so otelexport owns
// the `telemetry` paths and their parsing without importing internal/config
// (AS-093: typed config views over the layered substrate).
type configReader interface {
	Decode(path string, v any) (bool, error)
}

// defaultTimeout bounds an export POST so a slow or absent collector never stalls
// a run on its best-effort telemetry side channel.
const defaultTimeout = 5 * time.Second

// Config is the validated OpenTelemetry export settings read from the `telemetry`
// section. Export is off by default (PRD §7.23): an empty Endpoint leaves the zero
// Config, which Enabled reports as disabled, so a run with no telemetry config does
// no network I/O.
type Config struct {
	// Endpoint is the OTLP/HTTP base URL of the collector (e.g.
	// "http://localhost:4318"). Empty disables export. The /v1/traces path is
	// appended by TracesURL unless Endpoint already ends in it.
	Endpoint string
	// TimeoutSeconds bounds an export POST. Zero defers to defaultTimeout.
	TimeoutSeconds int
}

// ConfigFrom reads the `telemetry` section out of the layered config into a
// validated Config. A missing section yields the zero (disabled) Config. A
// malformed section degrades to disabled with a warning rather than failing the
// session — the tolerate-but-warn rule (PRD D2): a telemetry typo must not block a
// run. The dotted path lives here, not in the composition root.
func ConfigFrom(c configReader) (Config, []string) {
	var raw struct {
		OTelEndpoint string `json:"otel_endpoint"`
		OTelTimeout  int    `json:"otel_timeout_seconds"`
	}
	if _, err := c.Decode("telemetry", &raw); err != nil {
		return Config{}, []string{fmt.Sprintf("ignoring telemetry config: %v", err)}
	}
	var warns []string
	endpoint := strings.TrimSpace(raw.OTelEndpoint)
	if raw.OTelTimeout < 0 {
		warns = append(warns, fmt.Sprintf("telemetry.otel_timeout_seconds is negative (%d); using the default", raw.OTelTimeout))
		raw.OTelTimeout = 0
	}
	return Config{Endpoint: endpoint, TimeoutSeconds: raw.OTelTimeout}, warns
}

// Enabled reports whether export is configured. A run skips BuildTrace/Export
// entirely when this is false.
func Enabled(c Config) bool { return c.Endpoint != "" }

// TracesURL returns the OTLP/HTTP traces endpoint, appending the conventional
// /v1/traces path unless the configured endpoint already targets it.
func (c Config) TracesURL() string {
	base := strings.TrimRight(c.Endpoint, "/")
	if strings.HasSuffix(base, "/v1/traces") {
		return base
	}
	return base + "/v1/traces"
}

func (c Config) timeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return defaultTimeout
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}
