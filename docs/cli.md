# CLI Reference

## Installation

```bash
git clone https://github.com/sadewadee/foxhound.git
cd foxhound
go build -o foxhound ./cmd/foxhound/
```

## Usage

```
foxhound [global flags] <command> [command flags]
```

## Global Flags

| Flag | Description |
|------|-------------|
| `-v`, `--verbose` | Verbose output (debug level logging) |
| `-vv` | Very verbose output (debug + source location in log records) |
| `--headless MODE` | Browser display mode: `"true"` (headless), `"false"` (visible window), `"virtual"` (Xvfb). Overrides config value. |

Global flags must appear before the command name:

```bash
foxhound -v run --config config.yaml
foxhound -vv check --browser firefox
foxhound --headless false run --config config.yaml   # force visible browser
foxhound --headless=true browser-shell                # headless browser REPL
```

## Commands

### init

Scaffold a new Foxhound project directory.

```
foxhound init <project-name>
```

Creates the following files in a new directory named `<project-name>`:

| File | Purpose |
|------|---------|
| `go.mod` | Go module file, requires `github.com/sadewadee/foxhound` |
| `main.go` | Skeleton scraper with a `Processor` type and `main` function |
| `config.yaml` | Full configuration template with all sections |
| `.env.example` | Environment variable reference (copy to `.env`, never commit) |

```bash
foxhound init ecommerce-scraper
cd ecommerce-scraper
go mod tidy
foxhound run --config config.yaml
```

### run

Load a config file and run the hunt.

```
foxhound run [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `config.yaml` | Path to the configuration file |
| `--hunt` | *(config value)* | Override `hunt.domain` from the config |
| `--workers` | *(config value)* | Override `hunt.walkers` from the config |
| `--dry-run` | false | Validate the config and print the run summary without actually running |

```bash
foxhound run --config config.yaml
foxhound run --config config.yaml --workers 8
foxhound run --config config.yaml --hunt books.toscrape.com
foxhound run --config config.yaml --dry-run
foxhound -v run --config config.yaml
```

SIGINT and SIGTERM trigger a graceful shutdown. Walkers finish their current job before exiting.

### check

Generate an identity profile and verify its internal consistency.

```
foxhound check [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--browser` | `firefox` | Browser to test: `firefox` or `chrome` |
| `--os` | *(random)* | OS to test: `windows`, `macos`, or `linux` |

Prints a report with PASS/FAIL indicators for UA-browser consistency, TLS profile, header order, screen dimensions, locale, and timezone. Exits with code 1 if any check fails.

```bash
foxhound check
foxhound check --browser chrome --os macos
foxhound -v check --browser firefox --os windows
```

Sample output:

```
Foxhound Identity Check
--------------------------------------------------
  Browser:             firefox
  OS:                  windows
  User-Agent:          Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:134.0) ...
  TLS Profile:         firefox_134.0
  Platform:            Win32
  ...
--------------------------------------------------

Consistency Checks:
  [PASS] UA contains browser name
  [PASS] UA contains OS hint
  [PASS] TLS profile set
  [PASS] Header order non-empty
  [PASS] Screen dimensions set
  [PASS] Locale set
  [PASS] Timezone set

All checks PASS
```

### proxy-test

Test proxy pool health and latency against a target URL.

```
foxhound proxy-test [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `config.yaml` | Path to the configuration file |

Loads proxy providers from the config, checks each proxy for reachability, and prints a latency and health table.

```bash
foxhound proxy-test --config config.yaml
```

### shell

Start an interactive scraping REPL. Useful for testing URLs and selectors before writing a full processor.

```
foxhound shell [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `config.yaml` | Config file for loading proxy settings |

The shell has 14 commands:

| Command | Usage | Description |
|---------|-------|-------------|
| `identity` | `identity` | Generate and display a new identity profile |
| `fetch` | `fetch <url>` | Fetch URL, show status, size, and duration |
| `headers` | `headers <url>` | Fetch URL, display response headers |
| `parse` | `parse <url> <selector>` | Extract elements matching CSS selector |
| `adaptive` | `adaptive <url> <selector>` | Apply selector with automatic fallback on no match |
| `extract` | `extract <url> k1=sel1 k2=sel2` | Structured multi-field extraction |
| `export` | `export <format> [path]` | Export last fetch (json, csv, markdown, text) |
| `history` | `history` | Show last 10 fetched URLs with status/duration |
| `timing` | `timing` | Show avg/min/max latency stats for this session |
| `compare` | `compare <url1> <url2>` | Diff two pages and show unique text content |
| `status` | `status` | Show session stats (totals, bytes, latency, proxies) |
| `proxy` | `proxy` | Show proxy pool status |
| `help` | `help` | Show all commands |
| `exit` / `quit` | `exit` | Exit the shell |

Shell examples:

```
foxhound> fetch https://books.toscrape.com/
Status  : 200 OK
Size    : 51274 bytes
Duration: 312ms

foxhound> parse https://books.toscrape.com/ article.product_pod h3 a
Found 20 match(es) for "article.product_pod h3 a":
  [1] A Light in the ...
  [2] Tipping the Velvet
  ...

foxhound> extract https://books.toscrape.com/ title=h1 price=p.price_color
Extraction results for https://books.toscrape.com/:
  title:               All products | Books to Scrape - Sandbox
  price:               (no match)

foxhound> history
Fetch history:
  [ 1] 200  https://books.toscrape.com/  51274 bytes  312ms

foxhound> timing
Latency stats:
  Samples : 1
  Avg     : 312ms
  Min     : 312ms
  Max     : 312ms

foxhound> export json /tmp/last-fetch.json
Exported 51274 bytes to /tmp/last-fetch.json

foxhound> compare https://books.toscrape.com/catalogue/page-1.html https://books.toscrape.com/catalogue/page-2.html
Compare: ... vs ...
  Common text nodes : 42
  Only in page 1    : 20
  Only in page 2    : 20
```

### resume

Resume an interrupted hunt from a persistent queue.

```
foxhound resume [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--hunt-id` | *(required)* | ID of the hunt to resume |
| `--queue` | *(config value)* | Queue URL or file path |
| `--config` | `config.yaml` | Path to configuration file |

Supported queue formats:
- `redis://host:port/db`
- `sqlite://path/to/file.db`
- `/path/to/file.db` (SQLite without scheme)

```bash
foxhound resume --hunt-id my-hunt --queue redis://localhost:6379/0 --config config.yaml
foxhound resume --hunt-id my-hunt --queue /data/foxhound.db --config config.yaml
```

### browser-shell

Start an interactive Camoufox browser REPL. Requires the `playwright` build tag.

```
foxhound browser-shell [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--headless` | `"false"` | Browser display mode: `"true"`, `"false"`, `"virtual"` (overridden by global `--headless`) |
| `--proxy` | `$FOXHOUND_PROXY` | Proxy URL (HTTP or SOCKS5) |
| `--extension` | `$NOPECHA_EXT` | Path to Firefox extension directory |
| `--timeout` | `120s` | Per-navigation timeout |

The browser REPL opens a live Camoufox window (or headless instance). Session cookies, localStorage, and navigation history are preserved between commands.

Commands: `goto`, `back`, `forward`, `reload`, `url`, `title`, `click`, `type`, `select`, `scroll`, `wait`, `text`, `attr`, `html`, `links`, `count`, `screenshot`, `eval`, `cookies`, `status`, `help`, `clear`, `exit`.

```bash
foxhound browser-shell
foxhound --headless false browser-shell --proxy socks5://localhost:1080
foxhound browser-shell --extension /path/to/nopecha --timeout 60s
```

Build with:

```bash
go build -tags playwright -o foxhound ./cmd/foxhound/
```

### curl2fox

Convert a curl command to foxhound Go code.

```
foxhound curl2fox <curl command...>
```

Parses the curl flags (`-H`, `-X`, `-d`, `--json`, `-A`, `-b`, `-e`, etc.) and generates a complete `main.go` with identity, fetcher, job, processor, and hunt setup.

```bash
foxhound curl2fox 'curl https://api.example.com/data -H "Accept: application/json"'
foxhound curl2fox curl -X POST https://example.com/login -d 'user=admin&pass=secret'
```

### preview

Fetch a URL using the stealth HTTP client and print the response.

```
foxhound preview <url>
```

Outputs status code, final URL, duration, fetch mode, and the full response body. Useful for quick debugging without writing a processor.

```bash
foxhound preview https://books.toscrape.com/
foxhound preview example.com              # https:// added automatically
```

### version

```bash
foxhound version
# foxhound v0.0.5
```

### help

```bash
foxhound help
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Error (bad config, hunt failure, check failure) |
