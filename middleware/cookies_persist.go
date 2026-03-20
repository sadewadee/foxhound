package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	foxhound "github.com/sadewadee/foxhound"
)

// PersistentCookies is a middleware that persists cookies to a JSON file.
// Cookies are loaded from disk on creation and saved after each request.
type PersistentCookies struct {
	jar      *cookiejar.Jar
	filePath string
	mu       sync.Mutex
	// tracked keeps a copy of cookies keyed by origin URL so we can
	// enumerate and persist them — http.CookieJar does not expose iteration.
	tracked map[string][]*http.Cookie
}

// storedCookie is the JSON-serializable form of a cookie.
type storedCookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Secure   bool   `json:"secure"`
	HttpOnly bool   `json:"http_only"`
}

// storedEntry groups cookies by origin URL for serialisation.
type storedEntry struct {
	URL     string         `json:"url"`
	Cookies []storedCookie `json:"cookies"`
}

// NewPersistentCookies creates a cookie middleware that loads/saves to filePath.
func NewPersistentCookies(filePath string) (*PersistentCookies, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	pc := &PersistentCookies{
		jar:      jar,
		filePath: filePath,
		tracked:  make(map[string][]*http.Cookie),
	}
	_ = pc.Load() // best-effort load from disk
	return pc, nil
}

// Wrap implements foxhound.Middleware.
func (pc *PersistentCookies) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		// Inject cookies into job headers.
		u, _ := url.Parse(job.URL)
		if u != nil {
			cookies := pc.jar.Cookies(u)
			if len(cookies) > 0 && job.Headers == nil {
				job.Headers = make(http.Header)
			}
			for _, c := range cookies {
				job.Headers.Add("Cookie", c.String())
			}
		}

		resp, err := next.Fetch(ctx, job)
		if err != nil {
			return resp, err
		}

		// Store response cookies.
		if u != nil && resp.Headers != nil {
			var responseCookies []*http.Cookie
			for _, cookieStr := range resp.Headers["Set-Cookie"] {
				header := http.Header{"Set-Cookie": {cookieStr}}
				httpResp := &http.Response{Header: header}
				responseCookies = append(responseCookies, httpResp.Cookies()...)
			}
			if len(responseCookies) > 0 {
				pc.jar.SetCookies(u, responseCookies)
				pc.mu.Lock()
				pc.tracked[u.String()] = responseCookies
				pc.mu.Unlock()
				_ = pc.Save()
			}
		}

		return resp, nil
	})
}

// Save writes the tracked cookies to disk as JSON.
func (pc *PersistentCookies) Save() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	var entries []storedEntry
	for rawURL, cookies := range pc.tracked {
		var sc []storedCookie
		for _, c := range cookies {
			sc = append(sc, storedCookie{
				Name:     c.Name,
				Value:    c.Value,
				Domain:   c.Domain,
				Path:     c.Path,
				Secure:   c.Secure,
				HttpOnly: c.HttpOnly,
			})
		}
		entries = append(entries, storedEntry{URL: rawURL, Cookies: sc})
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	// Ensure parent directory exists.
	if dir := filepath.Dir(pc.filePath); dir != "" {
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			return mkErr
		}
	}

	return os.WriteFile(pc.filePath, data, 0o644)
}

// Load reads cookies from disk into the jar.
func (pc *PersistentCookies) Load() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	data, err := os.ReadFile(pc.filePath)
	if err != nil {
		return err
	}

	var entries []storedEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}

	for _, entry := range entries {
		u, err := url.Parse(entry.URL)
		if err != nil {
			continue
		}
		var cookies []*http.Cookie
		for _, sc := range entry.Cookies {
			cookies = append(cookies, &http.Cookie{
				Name:     sc.Name,
				Value:    sc.Value,
				Domain:   sc.Domain,
				Path:     sc.Path,
				Secure:   sc.Secure,
				HttpOnly: sc.HttpOnly,
			})
		}
		pc.jar.SetCookies(u, cookies)
		pc.tracked[u.String()] = cookies
	}
	return nil
}

// FilePath returns the path where cookies are stored.
func (pc *PersistentCookies) FilePath() string {
	return pc.filePath
}
