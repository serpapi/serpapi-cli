# serpapi-cli

> HTTP client for structured web search data via SerpApi

## Installation

### Homebrew (macOS/Linux)

```bash
brew tap serpapi/homebrew-tap
brew install serpapi-cli
```

### Pre-built Binaries

Download directly from [GitHub Releases](https://github.com/serpapi/serpapi-cli/releases)

### Go (compile from source)

```bash
go install github.com/serpapi/serpapi-cli/cmd/serpapi@latest
```

## Quick Start

```bash
# Authenticate
serpapi login

# Perform a search
serpapi search engine=google q=coffee
```

## Commands

### search

Perform a search with any supported SerpApi engine. Parameters are passed as bare `key=value` pairs.

```bash
# Basic search
serpapi search engine=google q=coffee

# Multiple parameters
serpapi search engine=google q="coffee shops" location="Austin,TX"

# Use a different engine
serpapi search engine=google_maps q="pizza" ll="@40.7455096,-74.0083012,14z"

# With server-side field filtering (reduces response size at API level)
serpapi search --fields "organic_results[].{title,link}" engine=google q=coffee

# With client-side jq filtering (like gh --jq)
serpapi search --jq ".organic_results[0:3] | [.[] | {title, link}]" engine=google q=coffee

# Both: server-side reduces payload, then client-side refines
serpapi search --fields "organic_results" --jq ".organic_results[0:3] | [.[] | {title, link}]" engine=google q=coffee
```

#### Pagination Flags

- `--all-pages` — Fetch all result pages and merge array fields across pages
- `--max-pages <n>` — Maximum number of pages to fetch when using `--all-pages`

```bash
# Fetch all pages and merge array results
serpapi search engine=google q=coffee --all-pages

# Limit to first 3 pages
serpapi search engine=google q=coffee --all-pages --max-pages 3
```

### account

Retrieve account information and usage statistics.

```bash
serpapi account
```

### locations

Lookup available locations for search queries (no API key required).

```bash
# Find locations matching "austin"
serpapi locations q=austin num=5
```

### archive

Retrieve a previously cached search by ID.

```bash
serpapi archive <search-id>
```

### login

Interactive authentication flow to save API key to config file.

```bash
serpapi login
```

## Global Flags

- `--fields <expr>` — Server-side field filtering (maps to SerpApi's `json_restrictor` parameter). Note: The `--fields` filter uses SerpApi's server-side field restrictor syntax—see [SerpApi docs](https://serpapi.com) for supported expressions.
- `--jq <expr>` — Client-side [jq](https://jqlang.github.io/jq/) filter applied to the JSON response, same as `gh --jq` (see the [jq tutorial](https://jqlang.github.io/jq/tutorial/) for syntax). Runs locally after the API call; the jq engine is embedded so no external `jq` install is required. Use `--fields` to shrink the payload server-side, then `--jq` to reshape it client-side. Examples:
  ```bash
  # Pick the first 3 organic results, keeping only title and link
  serpapi search --jq '.organic_results[0:3] | [.[] | {title, link}]' engine=google q=coffee

  # Print just the link of each organic result
  serpapi search --jq '.organic_results[].link' engine=google q=coffee

  # Count organic results
  serpapi search --jq '.organic_results | length' engine=google q=coffee
  ```
  Quote the expression with single quotes in bash/zsh to avoid shell interpretation of `$`, `|`, and `"`.
- `--api-key <key>` — Override API key (takes priority over environment and config file)
- `--timeout <seconds>` — HTTP request timeout (default `60`, env: `SERPAPI_TIMEOUT`). Use `0` to wait indefinitely. Slow engines such as `google_ai_mode`, or searches with `no_cache=true`, can take longer than the default; raise this if you see a `network_error` mentioning `Client.Timeout exceeded`. Note that a timed-out search may still complete on SerpApi's side and be billed and retrievable with `serpapi archive <search-id>`.
  ```bash
  # Give an AI Mode query up to two minutes
  serpapi search --timeout 120 engine=google_ai_mode q="..."

  # Or set it once for the shell session
  export SERPAPI_TIMEOUT=120
  ```
- `--debug` — Print request tracing to stderr (env: `SERPAPI_DEBUG=1`): DNS, TCP connect (local and remote address), TLS handshake, time to first byte, response status and headers (including `Serpapi-Search-Id` and `X-Request-Id`), and body size. Each line carries a UTC wall-clock timestamp and the elapsed time since the request started. For searches, the server's `search_metadata` timestamps are correlated against the client timeline so you can see whether a long wait happened before SerpApi created the search (network path, proxy, queueing), during processing (slow engine), or after processing finished. The API key is redacted. Stdout is unaffected, so `--jq` and piping still work.
  ```bash
  serpapi search --debug engine=google_ai_mode q="..." > result.json
  ```
  ```
  [debug] 2026-10-09T11:50:54.272Z + 0.000s GET https://serpapi.com/search.json?api_key=[REDACTED]&engine=google&q=coffee
  [debug] 2026-10-09T11:50:54.272Z + 0.000s Timeout: 60s
  [debug] 2026-10-09T11:50:54.300Z + 0.028s DNS resolved: 162.159.142.21, 172.66.2.17
  [debug] 2026-10-09T11:50:54.314Z + 0.042s Connected: tcp 162.159.142.21:443
  [debug] 2026-10-09T11:50:54.346Z + 0.074s TLS handshake done: TLS 1.3, ALPN="h2"
  [debug] 2026-10-09T11:50:54.413Z + 0.141s Using connection: 192.168.1.20:62494 -> 162.159.142.21:443 (new)
  [debug] 2026-10-09T11:50:54.414Z + 0.142s Request sent, waiting for response headers
  [debug] 2026-10-09T11:50:55.227Z + 0.955s First response byte received
  [debug] 2026-10-09T11:50:55.227Z + 0.955s Response: HTTP/2.0 200 OK
  [debug] 2026-10-09T11:50:55.227Z + 0.955s   Serpapi-Search-Id: 6ac8d51eaf5fdcac5434ae91
  [debug] 2026-10-09T11:50:55.234Z + 0.962s Body received: 57483 bytes
  [debug] 2026-10-09T11:50:55.234Z + 0.962s Server search: id=6ac8d51eaf5fdcac5434ae91 status=Success total_time_taken=0.5s
  [debug] 2026-10-09T11:50:55.234Z + 0.962s Server timeline: created_at=11:50:54Z (+0.0s after request sent), processed_at=11:50:54Z (+0.0s processing), headers received +1.2s after processed_at
  ```
  Server timestamps have one-second resolution, so sub-second deltas in the `Server timeline` line are noise. When the client waited noticeably longer than `total_time_taken`, a `Note:` line points at the phase that dominated.

## Configuration

### Authentication Priority Chain

The CLI checks for API keys in this order:

1. `--api-key` flag
2. `SERPAPI_KEY` environment variable
3. Config file: `~/.config/serpapi/config.toml`

If no API key is found, run `serpapi login` to authenticate interactively.

> **Security note:** For security, prefer setting `SERPAPI_KEY` as an environment variable over
> passing `--api-key` on the command line (command-line arguments are visible in process listings).

### Config File Format

```toml
api_key = "your_serpapi_key_here"
```

## For AI Agents

This CLI is optimized for consumption by AI agents (Claude, Codex, etc.):

- **Use `--fields` for server-side filtering** to reduce token usage:
  - Example: `--fields "organic_results[0:3]"` returns only first 3 results
  - Filtering happens at the API level, saving bandwidth and context window tokens
  - Syntax follows SerpApi's `json_restrictor` parameter
- **Use `--jq` for client-side filtering** (same as `gh --jq`):
  - Example: `--jq ".organic_results | length"` counts results locally
  - Full jq expression support: pipes, array slicing, object construction, `select`, `map`, etc.
  - Runs after API response is received
- **Combine both** for maximum efficiency:
  - `--fields` reduces the API response size (less bandwidth)
  - `--jq` refines the result further (less context window tokens)
- **Exit codes**:
  - `0` = success
  - `1` = API error (invalid key, rate limit, etc.)
  - `2` = usage error (missing arguments, invalid flags)
- **Errors are always JSON** on stderr for structured parsing

## Links

- [SerpApi Website](https://serpapi.com/)
- [API Documentation](https://serpapi.com/search-api)
- [MCP Server](https://github.com/serpapi/serpapi-mcp)
- [serpapi-go Library](https://github.com/serpapi/serpapi-golang)
