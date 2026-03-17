package foxhound

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for a Foxhound instance.
type Config struct {
	Hunt       HuntConfig       `yaml:"hunt"`
	Identity   IdentityConfig   `yaml:"identity"`
	Proxy      ProxyConfig      `yaml:"proxy"`
	Fetch      FetchConfig      `yaml:"fetch"`
	Middleware MiddlewareConfig `yaml:"middleware"`
	Pipeline   []PipelineEntry  `yaml:"pipeline"`
	Queue      QueueConfig      `yaml:"queue"`
	Logging    LoggingConfig    `yaml:"logging"`
}

// HuntConfig configures the scraping campaign.
type HuntConfig struct {
	Domain  string `yaml:"domain"`
	Walkers int    `yaml:"walkers"`
}

// IdentityConfig configures identity generation.
type IdentityConfig struct {
	Browser       string   `yaml:"browser"`
	OS            []string `yaml:"os"`
	FingerprintDB string   `yaml:"fingerprint_db"`
}

// ProxyConfig configures proxy management.
type ProxyConfig struct {
	Providers           []ProviderEntry `yaml:"providers"`
	Rotation            string          `yaml:"rotation"`
	Cooldown            Duration        `yaml:"cooldown"`
	MaxRequestsPerProxy int             `yaml:"max_requests_per_proxy"`
	HealthCheckInterval Duration        `yaml:"health_check_interval"`
}

// ProviderEntry defines a proxy provider in configuration.
type ProviderEntry struct {
	Type    string   `yaml:"type"`
	List    []string `yaml:"list,omitempty"`
	APIKey  string   `yaml:"api_key,omitempty"`
	Product string   `yaml:"product,omitempty"`
	Country string   `yaml:"country,omitempty"`
}

// FetchConfig configures the fetch layer.
type FetchConfig struct {
	Static  StaticFetchConfig  `yaml:"static"`
	Browser BrowserFetchConfig `yaml:"browser"`
}

// StaticFetchConfig configures the TLS-impersonating HTTP client.
type StaticFetchConfig struct {
	Timeout        Duration `yaml:"timeout"`
	MaxIdleConns   int      `yaml:"max_idle_conns"`
	TLSImpersonate bool     `yaml:"tls_impersonate"`
}

// BrowserFetchConfig configures the Camoufox browser.
type BrowserFetchConfig struct {
	Timeout     Duration `yaml:"timeout"`
	BlockImages bool     `yaml:"block_images"`
	BlockWebRTC bool     `yaml:"block_webrtc"`
	Headless    string   `yaml:"headless"`
	Instances   int      `yaml:"instances"`
}

// MiddlewareConfig configures request/response processing middleware.
type MiddlewareConfig struct {
	RateLimit   RateLimitConfig   `yaml:"ratelimit"`
	Dedup       DedupConfig       `yaml:"dedup"`
	DepthLimit  DepthLimitConfig  `yaml:"depth_limit"`
}

// RateLimitConfig configures per-domain rate limiting.
type RateLimitConfig struct {
	Enabled        bool     `yaml:"enabled"`
	RequestsPerSec float64  `yaml:"requests_per_sec"`
	BurstSize      int      `yaml:"burst_size"`
}

// DedupConfig configures URL deduplication.
type DedupConfig struct {
	Strategy string `yaml:"strategy"`
	Store    string `yaml:"store"`
}

// DepthLimitConfig configures crawl depth limiting.
type DepthLimitConfig struct {
	Max int `yaml:"max"`
}

// PipelineEntry is a polymorphic pipeline stage definition.
type PipelineEntry struct {
	Validate *ValidateConfig `yaml:"validate,omitempty"`
	Clean    *CleanConfig    `yaml:"clean,omitempty"`
	Dedup    *DedupConfig    `yaml:"dedup,omitempty"`
	Export   []ExportConfig  `yaml:"export,omitempty"`
}

// ValidateConfig configures the validation pipeline stage.
type ValidateConfig struct {
	Required []string `yaml:"required"`
}

// CleanConfig configures the cleaning pipeline stage.
type CleanConfig struct {
	TrimWhitespace bool `yaml:"trim_whitespace"`
	NormalizePrice bool `yaml:"normalize_price"`
}

// ExportConfig defines an export destination.
type ExportConfig struct {
	Type      string `yaml:"type"`
	Path      string `yaml:"path,omitempty"`
	Table     string `yaml:"table,omitempty"`
	UpsertKey string `yaml:"upsert_key,omitempty"`
	BatchSize int    `yaml:"batch_size,omitempty"`
}

// QueueConfig configures the job queue backend.
type QueueConfig struct {
	Backend string `yaml:"backend"`
}

// LoggingConfig configures structured logging.
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	Output string `yaml:"output"`
}

// Duration is a time.Duration that supports YAML marshaling.
type Duration struct {
	time.Duration
}

// UnmarshalYAML parses a duration string like "30s", "5m", "1h".
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = dur
	return nil
}

// MarshalYAML serializes the duration as a string.
func (d Duration) MarshalYAML() (any, error) {
	return d.Duration.String(), nil
}

// LoadConfig reads and parses a YAML configuration file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Expand environment variables in the config
	data = []byte(os.ExpandEnv(string(data)))

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	applyDefaults(cfg)
	return cfg, nil
}

// applyDefaults sets sensible defaults for missing configuration values.
func applyDefaults(cfg *Config) {
	if cfg.Hunt.Walkers <= 0 {
		cfg.Hunt.Walkers = 3
	}
	if cfg.Identity.Browser == "" {
		cfg.Identity.Browser = "firefox"
	}
	if len(cfg.Identity.OS) == 0 {
		cfg.Identity.OS = []string{"windows", "macos", "linux"}
	}
	if cfg.Identity.FingerprintDB == "" {
		cfg.Identity.FingerprintDB = "embedded"
	}
	if cfg.Proxy.Rotation == "" {
		cfg.Proxy.Rotation = "per_session"
	}
	if cfg.Proxy.Cooldown.Duration == 0 {
		cfg.Proxy.Cooldown.Duration = 30 * time.Minute
	}
	if cfg.Proxy.MaxRequestsPerProxy <= 0 {
		cfg.Proxy.MaxRequestsPerProxy = 100
	}
	if cfg.Proxy.HealthCheckInterval.Duration == 0 {
		cfg.Proxy.HealthCheckInterval.Duration = 60 * time.Second
	}
	if cfg.Fetch.Static.Timeout.Duration == 0 {
		cfg.Fetch.Static.Timeout.Duration = 30 * time.Second
	}
	if cfg.Fetch.Static.MaxIdleConns <= 0 {
		cfg.Fetch.Static.MaxIdleConns = 100
	}
	if cfg.Fetch.Browser.Timeout.Duration == 0 {
		cfg.Fetch.Browser.Timeout.Duration = 60 * time.Second
	}
	if cfg.Fetch.Browser.Instances <= 0 {
		cfg.Fetch.Browser.Instances = 2
	}
	if cfg.Queue.Backend == "" {
		cfg.Queue.Backend = "memory"
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "json"
	}
	if cfg.Logging.Output == "" {
		cfg.Logging.Output = "stderr"
	}
}
