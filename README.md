# serpapi-cli

A prototype for a lightweight CLI client for [SerpApi].

## Description

`serpapi-cli` allows for the use of SerpApi's extensive search engine result APIs
directly from the command line - perform searches, check your account status,
and read our documentation all without leaving the comfort of your shell.

## Installation

`serpapi-cli` is written in Rust and currently requires compiling from source:

```
$ cargo install --git https://github.com/serpapi/serpapi-cli.git
```

More installation options will become available as development progresses.

## Usage

```
$ serpapi-cli --help
The official CLI client for SerpApi

Usage: serpapi-cli [OPTIONS] --api-key <API_KEY> <COMMAND>

Commands:
  search    Perform a search
  archive   Retrieve a previously made search
  location  Perform a SerpApi location API lookup
  account   Retrieve SerpApi account data
  docs      Search SerpApi documentation
  help      Print this message or the help of the given subcommand(s)

Options:
      --api-key <API_KEY>          Your private SerpApi API key [env: SERPAPI_KEY]
      --json                       JSON output (default)
  -j, --jsonpath <JSONPATH>        JSONPath expression for JSON mode
  -p, --jsonpointer <JSONPOINTER>  JSONPointer expression for JSON mode
      --html                       HTML output if available
  -v, --verbose...                 Verbose output, specify more than once for more
  -h, --help                       Print help
  -V, --version                    Print version

$ serpapi-cli search --help
Perform a search

Usage: serpapi-cli --api-key <API_KEY> search [PARAMS]...

Arguments:
  [PARAMS]...  A key=value parameter pair, or a value for the q parameter

Options:
  -h, --help  Print help

# Set your API key in the environment
$ export SERPAPI_KEY="..."

# Extract specific elements using JSONPath
$ serpapi-cli -j '$["account_email", "plan_searches_left"]' account
[
  "thomas@serpapi.com",
  0
]

# Or with multiple JSONPointer expressions
$ serpapi-cli -p '/account_email' -p '/plan_searches_left' account
[
  "thomas@serpapi.com",
  0
]

# Look up a location
$ serpapi-cli -p /0/canonical_name location Austin
"Austin,TX,Texas,United States"

# Extract the first result from a search with a given location
$ serpapi-cli -j '$.organic_results[0]["link", "snippet"]' search serpapi "location=Austin,TX,Texas,United States"
[
  "https://serpapi.com/",
  "SerpApi is a real-time API to access Google search results. We handle proxies, solve captchas, and parse all rich structured data for you."
]

# Find our social media accounts using Bing (with bonus random Wikipedia page)
$ serpapi-cli -j '$.knowledge_graph.profiles[*].link' search serpapi engine=bing
[
  "https://www.facebook.com/serpapicom/",
  "https://twitter.com/serp_api",
  "https://instagram.com/serpapicom",
  "https://youtube.com/channel/ucugihlybod3ya3ydirhg_mg",
  "https://en.wikipedia.org/wiki/iso_3166-1"
]
```

[SerpApi]: https://serpapi.com/
[crates.io]: https://crates.io/