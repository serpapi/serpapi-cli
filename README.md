# serpapi-cli

> HTTP client for structured web search data via SerpApi

## Installation

### Homebrew (macOS/Linux)

```bash
brew tap serpapi/homebrew-tap
brew install serpapi
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
- `--jq <expr>` — Client-side jq filter applied to JSON output (same as `gh --jq`)
- `--api-key <key>` — Override API key (takes priority over environment and config file)

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
