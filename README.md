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

#### Image Search (Google Lens)

- `--image <file>` — Upload an image with the [Image API](https://serpapi.com/image-api) and search with its `image_id` in one step. Use `-` to read the image from stdin. Only `engine=google_lens` takes an `image_id`: other engines drop it and would run a normal search without the image, so they are refused before the upload, and so is `url=`.

```bash
# Reverse image search with Google Lens
serpapi search engine=google_lens --image ./photo.jpg

# Pipe an image in from another command
curl -s https://example.com/photo.jpg | serpapi search engine=google_lens --image -

# Keep only the visual matches
serpapi search engine=google_lens --image ./photo.jpg --jq ".visual_matches[:5]"
```

Supported formats are JPG/JPEG, PNG and WebP up to 500 KB. See the [Google Lens image upload docs](https://serpapi.com/google-lens-upload-an-image).

### extract

Fetches a public web page with the [Extract API](https://serpapi.com/extract-api) and returns its content as structured JSON, or as Markdown with `output=md`. It is a search with `engine=extract`, so `--fields`, `--jq`, `output=`, `async=true` and `archive` work as they do for `search`. The URL must be absolute `http` or `https`; quote it when it has `?` or `&`. It can also be given as `url=URL`. Other parameters are `key=value` pairs sent as-is:

| Parameter | Does |
|---|---|
| `mode=scrape` | extracts the page itself (default) |
| `mode=map` | lists the website's URLs; shortcut: [`serpapi map`](#map) |
| `mode=crawl` | extracts every page of the website it visits; shortcut: [`serpapi crawl`](#crawl) |
| `include_tags=`, `exclude_tags=` | CSS selectors to keep or drop before parsing |
| `include_markdown=true` | adds the main content as Markdown in `text_content.markdown` |

```bash
serpapi extract https://example.com
serpapi extract https://example.com output=md
serpapi extract https://example.com --jq '.inline_links[].link'
```

It counts one search. The Extract API runs no JavaScript and follows each site's `robots.txt`. Search pages that have their own engine, such as Google or Bing, are redirected to it and return that engine's JSON. Unlike `search`, `extract`, `map` and `crawl` need an API key.

### map

`serpapi map URL` is a shortcut for `serpapi extract URL mode=map`: it sends the same request and takes the same parameters and flags.

Lists a website's URLs, from the links on the page and the website's sitemaps. `include_sitemap=false` keeps to the page's links; `include_subdomains=true` adds other subdomains, such as `blog.example.com` from `www.example.com`. It counts one search.

```bash
serpapi map https://books.toscrape.com --jq '.urls[].url'
```

### crawl

`serpapi crawl URL` is a shortcut for `serpapi extract URL mode=crawl`: it sends the same request and takes the same parameters and flags.

Starts at the URL, follows links on the same website and extracts every page it visits, one entry each in `pages`. `max_pages` caps the pages, the first included (default 10, at most 100), and `max_depth` how many links deep it goes (default 2, at most 3; `0` is only the first page).

```bash
serpapi crawl https://books.toscrape.com max_pages=5 --jq '.pages[].url'
serpapi crawl https://books.toscrape.com max_pages=20 max_depth=1 --timeout 300
```

It counts one search per page it extracts, so the default `max_pages=10` can cost 10; cached and failed pages are free, and `search_metadata.credits_consumed` says what it cost. A crawl can outlast the default 60s `--timeout`, so raise it; `timeout=` is the API's own per-page fetch limit in milliseconds.

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

### image

Upload an image with the [Image API](https://serpapi.com/image-api) and print its `image_id` without searching. Useful when you want to reuse the same upload across several `engine=google_lens` searches, the only engine that takes an `image_id`; image IDs expire after 10 minutes. For a one-off search, prefer `serpapi search engine=google_lens --image <file>`.

```bash
# Upload an image file
serpapi image ./photo.jpg

# Read the image from stdin
curl -s https://example.com/photo.jpg | serpapi image -

# Reuse the image_id across searches
IMAGE_ID=$(serpapi image ./photo.jpg --jq .image_id)
serpapi search engine=google_lens image_id=$IMAGE_ID
serpapi search engine=google_lens image_id=$IMAGE_ID type=exact_matches
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
- `--fields <expr>`: search, extract, map and crawl only; see [search](#search).
- `--api-key <key>`: API key. Prefer `SERPAPI_KEY`, since flags show up in `ps` and shell history.
- `--timeout <seconds>`: HTTP request timeout in seconds, such as `120`, or a duration, such as `2m` (default `60`; env `SERPAPI_TIMEOUT`). `0` waits indefinitely. Slow engines such as `google_ai_mode`, and searches with `no_cache=true`, can take longer than the default, as can `serpapi crawl`, which extracts every page it visits; raise it when a `network_error` says the request timed out, and add `--debug` to see which step stalled. A timed-out search may still finish on SerpApi's side, count against your searches and be available in your search archive.
  ```bash
  # Give an AI Mode query up to two minutes
  serpapi search --timeout 120 engine=google_ai_mode q="..."

  # Or set it once for the shell session
  export SERPAPI_TIMEOUT=120
  ```
- `--debug`: trace each request on stderr (env `SERPAPI_DEBUG=1`): DNS, TCP connect (local and remote address), TLS handshake, time to first byte, response status and headers (including `Serpapi-Search-Id` and `X-Request-Id`), body size, and, for searches, the server's `search_metadata` timeline correlated with the client's, so you can see whether a long wait happened on the network path before SerpApi received the request, during processing, or while the response travelled back. Each line carries a UTC timestamp and the seconds elapsed since the request started; the API key is redacted; stdout is untouched, so `--jq` and piping still work. On a terminal the trace is plain text:
  ```
  [debug] 2026-10-09T11:50:54.272Z + 0.000s GET https://serpapi.com/search.json?api_key=[REDACTED]&engine=google&q=coffee
  [debug] 2026-10-09T11:50:54.272Z + 0.000s Timeout: 60s
  [debug] 2026-10-09T11:50:54.300Z + 0.028s DNS resolved: 162.159.142.21, 172.66.2.17
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
  Piped or redirected, stderr stays one JSON object per line, so scripts that parse it keep working; headers and the timeline travel as structured `data`:
  ```
  {"debug":{"at":"2026-10-09T11:50:54.414Z","elapsed":0.142,"event":"Request sent, waiting for response headers"}}
  {"debug":{"at":"2026-10-09T11:50:55.227Z","elapsed":0.955,"event":"Response: HTTP/2.0 200 OK","data":{"status":200,"proto":"HTTP/2.0","headers":{"Serpapi-Search-Id":"6ac8d51e..."}}}}
  {"debug":{"at":"2026-10-09T11:50:55.234Z","elapsed":0.962,"event":"Server timeline: ...","data":{"sent_to_created_s":0.0,"processing_s":0.0,"processed_to_headers_s":1.2}}}
  ```
  Server timestamps have one-second resolution, so sub-second deltas in the timeline are noise. When the client waited noticeably longer than `total_time_taken`, a `Note:` event points at the phase that dominated, if this machine's clock agrees with SerpApi's: it says so, and names no phase when the timestamps contradict each other. To send a trace along with a support request: `SERPAPI_DEBUG=1 serpapi search ... > result.json 2> trace.jsonl`.

## Configuration

The API key comes from the first of:

1. `--api-key`
2. `SERPAPI_KEY`
3. `SERPAPI_API_KEY`
4. the key saved by `serpapi login`

Without a key, `search` still runs, but the API answers only a few demo queries such as `q=coffee` and returns 401 for the rest. `account`, `archive`, `extract`, `map` and `crawl` need a key.

`serpapi login` saves the key, readable only by you, to `serpapi/config.toml` in the OS config directory: `$XDG_CONFIG_HOME` or `~/.config` on Linux, `~/Library/Application Support` on macOS, `%AppData%` on Windows. Without an absolute `HOME` (`%AppData%` on Windows) there is no config file: `login` exits 2, and other commands need `--api-key` or `SERPAPI_KEY`.

```toml
api_key = "your_serpapi_key_here"
```

A config file that can't be read or isn't valid TOML is reported by path and line, never by content: `search` warns and runs without a key, while `account`, `archive`, `extract`, `map` and `crawl` stop. `serpapi login` rewrites it.

## For AI agents

`serpapi --help` is the full contract. In short:

- **stdout carries only results**: JSON indented by two spaces, colored on a terminal. Piped, it is indented too, so `head`, `grep` and file viewers show whole lines; `head` cuts the JSON short, so take parts with `--jq`. A terminal gets control characters from results escaped, so a page can't drive it; piped output keeps them. Shrink them on the server with `--fields`, then locally with `--jq`, or ask for `output=md`.
- **Unfamiliar engine?** Responses are often 20–300 KB, and one key can be large too (Maps `place_results` holds reviews and photos). Piped without `--jq`, a result over 16 KB comes with a warning on stderr that lists its top-level keys by size, unless stderr goes into the same pipe (`2>&1`), where it would break a JSON parser. Print a sample with one item per array, `--jq 'walk(if type == "array" then .[:1] else . end)'`; nested arrays are cut too, so it shows the shape, not the answer. Then take what you need from the stored search for free: `serpapi archive SEARCH_METADATA_ID --jq '...'`.
- **Errors and warnings go to stderr**, one JSON object per line; so does the `--debug` trace, as `{"debug":{...}}` lines, when stderr is not a terminal:
  ```
  {"error":{"code":"api_error","message":"Invalid API key. ...","status":401}}
  {"warning":{"message":"..."}}
  {"debug":{"at":"2026-10-09T11:50:54.414Z","elapsed":0.142,"event":"Request sent, waiting for response headers"}}
  ```
- **The exit code says what to do next**:

  | Exit | `code` | Meaning |
  |---|---|---|
  | 0 | | Success; warnings may still be on stderr. |
  | 2 | `usage_error` | The command is wrong; fix it before retrying. |
  | 1 | `api_error` | `status` is the HTTP status: 200 the search ran but reported a problem, such as no results; 400 invalid parameters; 401 invalid API key; 403 account not allowed; 429 rate limited or out of searches; 5xx SerpApi failed. No `status`: the response was unreadable. |
  | 1 | `network_error` | No answer, such as a timeout. |
  | 130 | `interrupted` | Ctrl-C or SIGTERM stopped it before it finished, after printing what had arrived, such as the `--all-pages` pages fetched so far. |

- **Cost**: each successful search counts against your monthly searches, including each `--all-pages` page; cached, errored and failed ones don't, though errored ones count toward the hourly limit ([pricing FAQ](https://serpapi.com/pricing)). An identical search, `--fields` included, repeated within an hour comes from the cache. `no_cache=true` forces a new, counted search; cached results are at most an hour old, so add it only when that is too old. `zero_trace=true` (Enterprise) stores nothing, so it skips the cache too and leaves no search for `archive` to read. `account`, `locations` and `archive` are free, and so is an `image` upload, though it needs searches left this month and this hour, and has its own hourly limit, `account_rate_limit_per_hour` in `serpapi account`. `crawl` counts one search per page it extracts.
- **No prompts without a terminal.** Only `serpapi login` and an image given as `-` (`serpapi image -`, `search --image -`) read stdin. `login` waits for the first line, so pipe the key in (`printf %s "$KEY" | serpapi login`) or, simpler, set `SERPAPI_KEY`; an image is read until stdin closes.

## Links

- [SerpApi Website](https://serpapi.com/)
- [API Documentation](https://serpapi.com/search-api)
- [MCP Server](https://github.com/serpapi/serpapi-mcp)
- [serpapi-go Library](https://github.com/serpapi/serpapi-golang)
