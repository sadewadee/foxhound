package parse

// AdaptiveOption configures an AdaptiveExtractor created via
// NewAdaptiveExtractorWithOptions. Options are applied in order; later
// options override earlier ones when they touch the same field.
type AdaptiveOption func(*adaptiveConfig)

// adaptiveConfig is the internal configuration assembled from
// AdaptiveOption functions before constructing an AdaptiveExtractor.
type adaptiveConfig struct {
	jsonPath    string
	sqliteStore *SQLiteAdaptiveStore
}

// WithJSONStorage configures the extractor to persist learned signatures as
// a JSON file at the given path. The file is read on construction (if it
// exists) and written on Save. An empty path disables persistence.
func WithJSONStorage(path string) AdaptiveOption {
	return func(c *adaptiveConfig) { c.jsonPath = path }
}

// WithSQLiteStorage configures the extractor to persist learned signatures
// in a SQLite database at the given path. The database and schema are
// created on first use.
//
// Returns an option that opens the SQLite store lazily; any error from
// opening the database is reported when NewAdaptiveExtractorWithOptions
// returns. When the store cannot be opened, the option is silently dropped
// and the extractor falls back to in-memory only.
func WithSQLiteStorage(path string) AdaptiveOption {
	return func(c *adaptiveConfig) {
		store, err := NewSQLiteAdaptiveStore(path)
		if err != nil {
			// Best-effort: leave SQLite unset so the extractor still works
			// in-memory. Callers wanting strict error handling should call
			// NewSQLiteAdaptiveStore directly.
			return
		}
		c.sqliteStore = store
	}
}

// NewAdaptiveExtractorWithOptions constructs an AdaptiveExtractor with the
// supplied options. With no options, the returned extractor is in-memory
// only and Save is a no-op.
//
// This is the preferred constructor for new code. The legacy
// NewAdaptiveExtractor(savePath string) entry point is retained for
// backward compatibility and is equivalent to calling this function with
// WithJSONStorage(savePath).
func NewAdaptiveExtractorWithOptions(opts ...AdaptiveOption) *AdaptiveExtractor {
	cfg := &adaptiveConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	ae := NewAdaptiveExtractor(cfg.jsonPath)
	ae.sqliteStore = cfg.sqliteStore
	return ae
}
