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

Global flags must appear before the command name:

```bash
foxhound -v run --config config.yaml
foxhound -vv check --browser firefox
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

**Example:**

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

**Examples:**

```bash
# Basic run
foxhound run --config config.yaml

# Override workers
foxhound run --config config.yaml --workers 8

# Override target domain
foxhound run --config config.yaml --hunt books.toscrape.com

# Validate config without running
foxhound run --config config.yaml --dry-run

# Verbose logging
foxhound -v run --config config.yaml
```

The default processor (used when running from the CLI) extracts page titles and all same-domain links from each page. To use custom extraction logic, use the Go API — see [Go API](api.md).

**Signals:** SIGINT and SIGTERM trigger a graceful shutdown. Walkers finish their current job before exiting.

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

**Examples:**

```bash
foxhound check
foxhound check --browser chrome --os macos
foxhound -v check --browser firefox --os windows
```

**Sample output:**

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

**Example:**

```bash
foxhound proxy-test --config config.yaml
```

### shell

Start an interactive scraping shell (REPL).

```
foxhound shell
```

The shell provides an interactive environment for testing URLs, inspecting responses, and experimenting with selectors. Useful for debugging before writing a full processor.

### resume

Resume an interrupted hunt from a persistent queue.

```
foxhound resume [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--hunt-id` | *(required)* | ID of the hunt to resume |
| `--queue` | *(config value)* | Queue URL or file path (e.g. `redis://localhost:6379/0` or `/data/hunt.db`) |
| `--config` | `config.yaml` | Path to configuration file |

The `--queue` flag supports:
- `redis://host:port/db` — Redis queue
- `sqlite://path/to/file.db` — SQLite queue
- `/path/to/file.db` — SQLite file path (no scheme prefix)

**Examples:**

```bash
# Resume from a Redis queue
foxhound resume --hunt-id my-hunt --queue redis://localhost:6379/0 --config config.yaml

# Resume from a SQLite file
foxhound resume --hunt-id my-hunt --queue /data/foxhound.db --config config.yaml
```

If the queue has zero pending jobs, the command prints a message and exits cleanly.

### version

Print the Foxhound version string.

```bash
foxhound version
# foxhound v0.0.1
```

### help

Print the usage summary.

```bash
foxhound help
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Error (bad config, hunt failure, check failure) |
