# serpapi-cli

> SerpApi CLI for humans and AI agents

## Installation

### Homebrew (macOS/Linux)

```bash
brew tap serpapi/homebrew-tap
brew install serpapi
```

### cargo-binstall (pre-built binary, no compilation)

```bash
cargo binstall serpapi-cli
```

### Shell script (Linux/macOS)

```bash
curl --proto '=https' --tlsv1.2 -LsSf https://github.com/serpapi/serpapi-cli/releases/latest/download/serpapi-installer.sh | sh
```

### PowerShell (Windows)

```powershell
powershell -ExecutionPolicy ByPass -c "irm https://github.com/serpapi/serpapi-cli/releases/latest/download/serpapi-installer.ps1 | iex"
```

### Pre-built Binaries

Download directly from [GitHub Releases](https://github.com/serpapi/serpapi-cli/releases)

### Cargo (compile from source)

```bash
cargo install serpapi-cli
```

## Quick Start

```bash
# Authenticate
serpapi login

# Perform a search (note: --json comes BEFORE the command)
serpapi --json search engine=google q=coffee
```

## Commands

### search

Perform a search with any supported SerpApi engine.

```bash
# Basic search
serpapi --json search engine=google q=coffee

# Fetch all pages and merge array results
serpapi --json search engine=google q=coffee --all-pages

# Limit to first 3 pages
serpapi --json search engine=google q=coffee --all-pages --max-pages 3

# With server-side field filtering (reduces response size at API level)
serpapi --json --fields "organic_results[].{title,link}" search engine=google q=coffee

# With client-side jq filtering (like gh --jq)
serpapi --json --jq ".organic_results[0:3] | [.[] | {title, link}]" search engine=google q=coffee

# Both: server-side reduces payload, then client-side refines
serpapi --json --fields "organic_results" --jq ".organic_results[0:3] | [.[] | {title, link}]" search engine=google q=coffee

# Multiple parameters
serpapi --json search engine=google q="coffee shops" location="Austin,TX"
```

### account

Retrieve account information and usage statistics.

```bash
serpapi --json account
```

### locations

Lookup available locations for search queries (no API key required).

```bash
# Find locations matching "austin"
serpapi --json locations q=austin num=5
```

### archive

Retrieve a previously cached search by ID.

```bash
serpapi --json archive <search-id>
```

### login

Interactive authentication flow to save API key to config file.

```bash
serpapi login
```

## Global Flags

- `--json` — Clean JSON output (no ANSI colors, for AI agents and pipelines)
- `--fields <expr>` — Server-side field filtering (maps to SerpApi's `json_restrictor` parameter). Note: The `--fields` filter uses SerpApi's server-side field restrictor syntax—see [SerpApi docs](https://serpapi.com) for supported expressions.
- `--jq <expr>` — Client-side jq filter applied to JSON output (same as `gh --jq`)
- `--api-key <key>` — Override API key (takes priority over environment and config file)
- `--all-pages` — Fetch all result pages and merge array fields across pages
- `--max-pages <n>` — When used with `--all-pages`, limit fetching to the first `n` pages

**⚠️ Important: Flag Position**

Global flags must come **BEFORE** the subcommand, not after:

```bash
# ✅ Correct
serpapi --json account
serpapi --json --jq ".organic_results[0:3]" search engine=google q=coffee

# ❌ Incorrect (will fail with "unexpected argument")
serpapi account --json
serpapi search engine=google q=coffee --json
```

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

- **Use `--json` flag** for clean, parseable JSON output (no ANSI colors or formatting)
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
- [serpapi-rust Crate](https://github.com/serpapi/serpapi-rust)
