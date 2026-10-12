package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/serpapi/serpapi-cli/pkg/output"
)

func TestLargeResultHintListsTwelveKeysLargestFirst(t *testing.T) {
	members := make([]string, 0, 14)
	for i := 1; i <= 14; i++ {
		members = append(members, fmt.Sprintf(`"k%02d":"%s"`, i, strings.Repeat("x", i*300)))
	}
	raw := []byte(`{"search_metadata":{"id":"abc123"},` + strings.Join(members, ",") + "}")

	resetFlags(rootCmd)
	var stdout, stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	hintLargeJSON(cmd, raw, searchHint)

	var got struct {
		Warning struct {
			Message string `json:"message"`
		} `json:"warning"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &got); err != nil {
		t.Fatalf("stderr is not one JSON warning: %v: %q", err, stderr.String())
	}
	want := "Large result: 31 KB; top-level keys by size: k14 4 KB, k13 4 KB, k12 4 KB, k11 3 KB, k10 3 KB, " +
		"k09 3 KB, k08 2 KB, k07 2 KB, k06 2 KB, k05 1 KB, k04 1 KB, k03 902 B and 3 more. " +
		"To print only what you need, read the stored search for free: serpapi archive abc123 --jq '.KEY'"
	if got.Warning.Message != want {
		t.Errorf("warning =\n%s\nwant\n%s", got.Warning.Message, want)
	}
}

func TestLargeResultHintSkipsATerminal(t *testing.T) {
	isTerminal := output.IsTerminal
	output.IsTerminal = func(io.Writer) bool { return true }
	t.Cleanup(func() { output.IsTerminal = isTerminal })
	fakeAPI(t, respond(bigResult(`"search_metadata":{"id":"abc123"},`, 1500)))
	t.Setenv("SERPAPI_KEY", "envkey")

	res := execute(t, "", search...)

	if warnings := wantOK(t, res); len(warnings) > 0 {
		t.Errorf("warnings = %q, want none on a terminal", warnings)
	}
}

// Agents often run `serpapi search ... 2>&1 | python3 -c 'json.load(...)'`.
// In an agent study, the warning in that merged stream broke 5 of 6 such
// parses, so a merged stream must stay one JSON document.
func TestLargeResultHintSkipsAMergedStream(t *testing.T) {
	fakeAPI(t, respond(bigResult(`"search_metadata":{"id":"abc123"},`, 1500)))
	t.Setenv("SERPAPI_KEY", "envkey")
	runTo := func(stdout, stderr *os.File) {
		t.Helper()
		resetFlags(rootCmd)
		rootCmd.SetOut(stdout)
		rootCmd.SetErr(stderr)
		rootCmd.SetArgs(append([]string{}, search...))
		if code := run(context.Background()); code != 0 {
			t.Fatalf("exit code %d", code)
		}
	}
	create := func(name string) *os.File {
		f, err := os.Create(filepath.Join(t.TempDir(), name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return f
	}

	merged := create("merged")
	runTo(merged, merged)
	out, err := os.ReadFile(merged.Name())
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		t.Errorf("the merged stream is not one JSON document: %v: %.200s", err, out)
	}

	stdout, stderr := create("stdout"), create("stderr")
	runTo(stdout, stderr)
	if warning, _ := os.ReadFile(stderr.Name()); !strings.Contains(string(warning), "Large result: 52 KB") {
		t.Errorf("separate stderr = %q, want the large-result warning", warning)
	}
}
