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
Usage: serpapi-cli [OPTIONS] --api-key <API_KEY> <COMMAND>

Commands:
  search    Perform a search
  archive   Retrieve a previously made search
  location  Perform a SerpApi location API lookup
  account   Retrieve SerpApi account data
  docs      Search SerpApi documentation
  help      Print this message or the help of the given subcommand(s)

Options:
      --api-key <API_KEY>  Your private SerpApi API key [env: SERPAPI_KEY]
      --json               JSON output (default)
      --html               HTML output if available
  -v, --verbose...         Verbose output, specify more than once for more
  -h, --help               Print help
  -V, --version            Print version

$ serpapi-cli search --help
Perform a search

Usage: serpapi-cli --api-key <API_KEY> search [PARAMS]...

Arguments:
  [PARAMS]...  A key=value parameter pair, or a value for the q parameter

Options:
  -h, --help  Print help
$ export SERPAPI_KEY="..."

$ serpapi-cli account | jq '.account_email, .plan_searches_left'
"thomas@serpapi.com"
30

$ serpapi-cli location "Austin" | jq '.[0].canonical_name'
"Austin,TX,Texas,United States"

$ serpapi-cli search "serpapi" "location=Austin,TX,Texas,United States" | jq '.organic_results[0] | .link, .snippet'
"https://serpapi.com/"
"SerpApi is a real-time API to access Google search results. We handle proxies, solve captchas, and parse all rich structured data for you."
```

[SerpApi]: https://serpapi.com/
[crates.io]: https://crates.io/