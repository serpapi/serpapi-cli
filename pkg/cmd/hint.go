package cmd

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/serpapi/serpapi-cli/pkg/output"
	"github.com/serpapi/serpapi-cli/pkg/params"
)

// largeResult is the size above which an unfiltered result sent to a pipe
// gets a hint on stderr. Agent tools keep only about 20 KB of a command's
// output inline: Copilot CLI saves anything longer to a file to page through.
const largeResult = 16 << 10

type keySize struct {
	key  string
	size int
}

// valueSize records the encoded length of a JSON value. encoding/json hands
// UnmarshalJSON a slice of the input, so measuring copies nothing.
type valueSize int

func (s *valueSize) UnmarshalJSON(b []byte) error {
	*s = valueSize(len(b))
	return nil
}

// topLevelSizes returns the top-level keys of a JSON object with the
// encoded size of each value, largest first, and search_metadata.id.
func topLevelSizes(raw []byte) ([]keySize, string) {
	var top map[string]valueSize
	if json.Unmarshal(raw, &top) != nil {
		return nil, ""
	}
	keys := make([]keySize, 0, len(top))
	for k, v := range top {
		keys = append(keys, keySize{k, int(v)})
	}
	sortKeySizes(keys)
	var meta struct {
		SearchMetadata struct {
			ID string `json:"id"`
		} `json:"search_metadata"`
	}
	_ = json.Unmarshal(raw, &meta)
	return keys, meta.SearchMetadata.ID
}

func sortKeySizes(keys []keySize) {
	slices.SortFunc(keys, func(a, b keySize) int {
		if a.size != b.size {
			return b.size - a.size
		}
		return strings.Compare(a.key, b.key)
	})
}

// hintLargeResult warns that a result about to be printed to a pipe is
// large, listing its top-level keys by size and how to print only part of
// it. It is printed before the result because agents read stdout and stderr
// together and may see only the start. A terminal or a --jq run gets none.
func hintLargeResult(cmd *cobra.Command, size int, keys []keySize, next string) {
	if !wantsLargeHint(cmd, size) || len(keys) == 0 {
		return
	}
	const shown = 12
	parts := make([]string, 0, shown)
	for _, k := range keys[:min(len(keys), shown)] {
		parts = append(parts, k.key+" "+sizeText(k.size))
	}
	list := strings.Join(parts, ", ")
	if len(keys) > shown {
		list += fmt.Sprintf(" and %d more", len(keys)-shown)
	}
	warn(cmd, "Large result: %s; top-level keys by size: %s. To print only what you need, %s",
		sizeText(size), list, next)
}

// wantsLargeHint reports whether a result of size bytes gets the large
// result hint, before any work is spent on its keys. A stream merged with
// 2>&1 gets none: agents often pipe it into a JSON parser, which the extra
// line would break.
func wantsLargeHint(cmd *cobra.Command, size int) bool {
	w := cmd.OutOrStdout()
	return jqFlag == "" && size > largeResult && !output.IsTerminal(w) && !output.SameStream(w, cmd.ErrOrStderr())
}

// hintLargeJSON is hintLargeResult for JSON as it is about to be printed;
// next turns its search_metadata.id into the suggested next step.
func hintLargeJSON(cmd *cobra.Command, raw []byte, next func(id string) string) {
	if !wantsLargeHint(cmd, len(raw)) {
		return
	}
	keys, id := topLevelSizes(raw)
	hintLargeResult(cmd, len(raw), keys, next(id))
}

// warnRepeated names each parameter given twice with different values: the
// server, like the CLI, keeps only the last one.
func warnRepeated(cmd *cobra.Command, parsed []params.Param) {
	for _, p := range params.Repeated(parsed) {
		warn(cmd, "%s given more than once; using the last value, %q", p.Key, p.Value)
	}
}

// archiveHint is the next step for a large stored search.
func archiveHint(string) string { return "add --jq '.KEY'; reading the archive is free" }

// searchHint returns the next step for a large result of the search sent
// with p: re-reading the stored search is free, and so is the same search
// within the cache hour, unless no_cache asked for a new one. ZeroTrace
// stores nothing and skips the cache.
func searchHint(p map[string]string) func(id string) string {
	return func(id string) string {
		zeroTrace, noCache := apiBool(p, "zero_trace"), apiBool(p, "no_cache")
		if id != "" && zeroTrace == paramOff {
			return fmt.Sprintf("read the stored search for free: serpapi archive %s --jq '.KEY'", id)
		}
		hint := "repeat the command with --jq '.KEY'"
		switch {
		case zeroTrace == paramOn:
			hint += "; zero_trace=" + p["zero_trace"] + " stores nothing and makes the repeat a new, counted search"
		case noCache == paramOn:
			hint += "; no_cache=" + p["no_cache"] + " makes the repeat a new, counted search"
		case zeroTrace == paramUnsure:
			hint += "; with zero_trace=" + p["zero_trace"] + " the repeat may be a new, counted search"
		case noCache == paramUnsure:
			hint += "; with no_cache=" + p["no_cache"] + " the repeat may be a new, counted search"
		default:
			return hint + ": the same search within an hour comes from the cache"
		}
		if allPagesFlag {
			hint += " for each page"
		}
		return hint
	}
}

type paramBool int

const (
	paramOff paramBool = iota
	paramOn
	paramUnsure
)

// apiBool reads a boolean parameter as the API does, through Mongoid 7.5's
// Boolean.evolve: its true words, in any case, are on, and anything else is
// off. Mongoid 8 keeps any other word as a string, which Ruby takes as true,
// so only its false words are sure to stay off.
func apiBool(p map[string]string, key string) paramBool {
	v, ok := p[key]
	if !ok {
		return paramOff
	}
	switch strings.ToLower(v) {
	case "true", "t", "yes", "y", "on", "1", "1.0":
		return paramOn
	case "false", "f", "no", "n", "off", "0", "0.0":
		return paramOff
	}
	return paramUnsure
}

func sizeText(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%d KB", (n+512)/1024)
}

// objectKeys lists the top-level keys of v, sorted, when v is an object.
func objectKeys(v any) string {
	obj, ok := v.(map[string]any)
	if !ok || len(obj) == 0 {
		return ""
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return strings.Join(keys, ", ")
}
