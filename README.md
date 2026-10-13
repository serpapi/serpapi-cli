# serpapi-cli

Search engine results as JSON from [SerpApi](https://serpapi.com), for people, scripts and AI agents

## Installation

### Homebrew (macOS/Linux)

```bash
brew tap serpapi/homebrew-tap
brew install serpapi-cli
```

### Pre-built binaries

Download directly from [GitHub Releases](https://github.com/serpapi/serpapi-cli/releases)

### Go (compile from source)

```bash
go install github.com/serpapi/serpapi-cli/cmd/serpapi@latest
```

This needs Go 1.25 or newer; with the default `GOTOOLCHAIN=auto`, Go 1.21 and later download it on their own.

## Quick start

Your API key is at <https://serpapi.com/manage-api-key>. Without one, `search` still answers a few demo queries such as `q=coffee`.

```bash
serpapi login                                    # check and save your API key
serpapi search engine=google q=coffee            # JSON results
serpapi search engine=google coffee --fields "organic_results[].{title, link}"
serpapi search engine=google q=coffee output=md  # Markdown
```

`serpapi --help` and each command's `--help` document parameters, errors and costs.

## Commands

### search

Parameters are `key=value` pairs sent to the API as-is; words without `=` are joined into `q`, and a parameter given twice keeps its last value, with a warning. Each engine's parameters are listed at <https://serpapi.com/search-engine-apis>. Some engines name the query differently: `search_query` (YouTube), `k` (Amazon), `_nkw` (eBay), `query` (Walmart, Naver), `p` (Yahoo), `text` (Yandex).

```bash
serpapi search engine=google q="coffee shops" location="Austin, Texas, United States"
serpapi search engine=youtube search_query="lofi beats" --jq '.video_results[:3][].link'
serpapi search engine=google_maps q=pizza ll="@40.7455096,-74.0083012,14z" --fields "local_results[0:5]"
```

`--fields` sets SerpApi's [`json_restrictor`](https://serpapi.com/json-restrictor), so the server returns only the listed paths:

| `--fields` | Returns |
|---|---|
| `organic_results[0:3]` | the first three results |
| `organic_results[].{title, link}` | two fields of every result |
| `search_metadata.id,local_results[0]` | several paths, comma-separated |

Unknown paths are dropped silently; if nothing matches, you get `{}` and a warning on stderr. The server also drops `null`, `false` and empty values. `--fields` is part of the cache key, so a different `--fields` can miss the cache and count as a new search; `--jq` filters the response locally without changing the request.

`output=md` returns Markdown; `html`, `plain` and `csv` are also available. They are not JSON, so `--jq` and `--all-pages` don't apply.

`--all-pages` follows `serpapi_pagination.next` and merges the arrays of every page into one object, up to `--max-pages` pages (default 100). Each page is one search. It stops at a page that `--fields` matches nothing on, so a typo costs one search; to page past such pages, filter with `--jq` instead. If a later page fails, or Ctrl-C stops the run, the pages fetched so far are printed and the error says which page it stopped at.

```bash
serpapi search engine=google q=coffee --all-pages --max-pages 3 --jq '.organic_results | length'
```

`async=true` returns at once with `search_metadata.id`; collect the results with `serpapi archive`.

### archive

Fetches an earlier search by its `search_metadata.id`, for free. A search made with `async=true` has `search_metadata.status` `Queued` or `Processing` until it is `Success`, so poll. `output=` takes the same values as in search.

```bash
serpapi archive 6a2b3c4d5e6f7a8b9c0d1e2f
serpapi archive 6a2b3c4d5e6f7a8b9c0d1e2f output=md
```

### locations

Finds values for the `location` search parameter: pass a result's `canonical_name`. Free, and needs no API key. `limit` is at most 10. A query is required, because without one the API returns every location (66 MB).

```bash
serpapi locations q=Austin limit=3 --jq '.[0].canonical_name'
```

### account

Shows the plan, usage and searches left, for free. The API key in the output is masked.

```bash
serpapi account --jq .total_searches_left
```

### login

Checks an API key with a free account request, then saves it. The key comes from `--api-key`, a hidden prompt on a terminal, or else the first line of stdin, which `login` waits for.

```bash
serpapi login
printf %s "$KEY" | serpapi login
```

### version

Prints the CLI version, as `--version` does.

## Global flags

- `--jq <expr>`: [jq](https://jqlang.github.io/jq/manual/) filter applied to the JSON result, like `gh --jq`; strings print unquoted and `null` as an empty line, and results stream as they are produced. A filter that prints only `null` gets a warning listing the response's top-level keys. jq is built in, so nothing else needs installing. In bash and zsh, quote the expression with single quotes.
- `--fields <expr>`: search only; see [search](#search).
- `--api-key <key>`: API key. Prefer `SERPAPI_KEY`, since flags show up in `ps` and shell history.
- `--timeout <seconds>`: HTTP request timeout in seconds, such as `120`, or a duration, such as `2m` (default `60`; env `SERPAPI_TIMEOUT`). `0` waits indefinitely. Slow engines such as `google_ai_mode`, and searches with `no_cache=true`, can take longer than the default; raise it when a `network_error` says the request timed out. A timed-out search may still finish on SerpApi's side, count against your searches and be available in your search archive.
  ```bash
  # Give an AI Mode query up to two minutes
  serpapi search --timeout 120 engine=google_ai_mode q="..."

  # Or set it once for the shell session
  export SERPAPI_TIMEOUT=120
  ```

## Configuration

The API key comes from the first of:

1. `--api-key`
2. `SERPAPI_KEY`
3. `SERPAPI_API_KEY`
4. the key saved by `serpapi login`

Without a key, `search` still runs, but the API answers only a few demo queries such as `q=coffee` and returns 401 for the rest. `account` and `archive` need a key.

`serpapi login` saves the key, readable only by you, to `serpapi/config.toml` in the OS config directory: `$XDG_CONFIG_HOME` or `~/.config` on Linux, `~/Library/Application Support` on macOS, `%AppData%` on Windows. Without an absolute `HOME` (`%AppData%` on Windows) there is no config file: `login` exits 2, and other commands need `--api-key` or `SERPAPI_KEY`.

```toml
api_key = "your_serpapi_key_here"
```

A config file that can't be read or isn't valid TOML is reported by path and line, never by content: `search` warns and runs without a key, while `account` and `archive` stop. `serpapi login` rewrites it.

## For AI agents

`serpapi --help` is the full contract. In short:

- **stdout carries only results**: JSON indented by two spaces, colored on a terminal. Piped, it is indented too, so `head`, `grep` and file viewers show whole lines; `head` cuts the JSON short, so take parts with `--jq`. A terminal gets control characters from results escaped, so a page can't drive it; piped output keeps them. Shrink them on the server with `--fields`, then locally with `--jq`, or ask for `output=md`.
- **Unfamiliar engine?** Responses are often 20–300 KB, and one key can be large too (Maps `place_results` holds reviews and photos). Piped without `--jq`, a result over 16 KB comes with a warning on stderr that lists its top-level keys by size, unless stderr goes into the same pipe (`2>&1`), where it would break a JSON parser. Print a sample with one item per array, `--jq 'walk(if type == "array" then .[:1] else . end)'`; nested arrays are cut too, so it shows the shape, not the answer. Then take what you need from the stored search for free: `serpapi archive SEARCH_METADATA_ID --jq '...'`.
- **Errors and warnings go to stderr**, one JSON object per line:
  ```
  {"error":{"code":"api_error","message":"Invalid API key. ...","status":401}}
  {"warning":{"message":"..."}}
  ```
- **The exit code says what to do next**:

  | Exit | `code` | Meaning |
  |---|---|---|
  | 0 | | Success; warnings may still be on stderr. |
  | 2 | `usage_error` | The command is wrong; fix it before retrying. |
  | 1 | `api_error` | `status` is the HTTP status: 200 the search ran but reported a problem, such as no results; 400 invalid parameters; 401 invalid API key; 403 account not allowed; 429 rate limited or out of searches; 5xx SerpApi failed. No `status`: the response was unreadable. |
  | 1 | `network_error` | No answer, such as a timeout. |
  | 130 | `interrupted` | Ctrl-C or SIGTERM stopped it before it finished, after printing what had arrived, such as the `--all-pages` pages fetched so far. |

- **Cost**: each successful search counts against your monthly searches, including each `--all-pages` page; cached, errored and failed ones don't, though errored ones count toward the hourly limit ([pricing FAQ](https://serpapi.com/pricing)). An identical search, `--fields` included, repeated within an hour comes from the cache. `no_cache=true` forces a new, counted search; cached results are at most an hour old, so add it only when that is too old. `zero_trace=true` (Enterprise) stores nothing, so it skips the cache too and leaves no search for `archive` to read. `account`, `locations` and `archive` are free.
- **No prompts without a terminal.** Only `serpapi login` reads stdin, and it waits for the first line, so pipe the key in (`printf %s "$KEY" | serpapi login`) or, simpler, set `SERPAPI_KEY`.

## Links

- [SerpApi Website](https://serpapi.com/)
- [API Documentation](https://serpapi.com/search-api)
- [MCP Server](https://github.com/serpapi/serpapi-mcp)
- [serpapi-go Library](https://github.com/serpapi/serpapi-golang)
