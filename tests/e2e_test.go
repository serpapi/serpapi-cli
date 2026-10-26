package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binaryPath string

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

// run builds the binary into a temp dir and runs the tests. It is separate
// from TestMain because os.Exit skips deferred calls, which leaked the dir.
func run(m *testing.M) int {
	tmpDir, err := os.MkdirTemp("", "serpapi-e2e-*")
	if err != nil {
		panic("failed to create temp dir: " + err.Error())
	}
	defer os.RemoveAll(tmpDir)

	bin := filepath.Join(tmpDir, "serpapi")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/serpapi")
	cmd.Dir = findRepoRoot()
	if out, err := cmd.CombinedOutput(); err != nil {
		panic("failed to build binary: " + err.Error() + "\n" + string(out))
	}
	binaryPath = bin
	return m.Run()
}

func findRepoRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("could not find repo root")
		}
		dir = parent
	}
}

// requireLive returns SERPAPI_KEY and skips the test without it. Live tests
// reach the real API and can use searches.
func requireLive(t *testing.T) string {
	t.Helper()
	key := os.Getenv("SERPAPI_KEY")
	if key == "" {
		t.Skip("requires SERPAPI_KEY")
	}
	return key
}

type result struct {
	code           int
	stdout, stderr string
}

// serpapi runs the binary with a fresh config directory and no key in the environment.
func serpapi(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	return serpapiIn(t, t.TempDir(), stdin, args...)
}

func serpapiIn(t *testing.T, home, stdin string, args ...string) result {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(),
		"HOME="+home, "XDG_CONFIG_HOME="+home, "AppData="+home,
		"SERPAPI_KEY=", "SERPAPI_API_KEY=", "NO_COLOR=1")
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	var res result
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running serpapi: %v", err)
		}
		res.code = exitErr.ExitCode()
	}
	res.stdout, res.stderr = stdout.String(), stderr.String()
	return res
}

func wantSuccess(t *testing.T, res result) {
	t.Helper()
	if res.code != 0 {
		t.Fatalf("exit code %d, stderr: %s", res.code, res.stderr)
	}
}

// errorJSON parses the error that failed commands print as the last stderr line.
func errorJSON(t *testing.T, res result) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(res.stderr), "\n")
	var v struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &v); err != nil || v.Error == nil {
		t.Fatalf("last stderr line is not a JSON error: %q", res.stderr)
	}
	return v.Error
}

func TestUsageErrorsWithoutNetwork(t *testing.T) {
	tests := []struct {
		args []string
		msg  string
	}{
		{nil, "No command given"},
		{[]string{"account"}, "No API key found"},
		{[]string{"archive", "abc123"}, "No API key found"},
		{[]string{"--api-key", "x", "archive", "../bad"}, "Invalid archive ID"},
		{[]string{"--api-key", "x", "search", "engine=google", "q=coffee", "output=xml"}, "Unknown output=xml"},
		{[]string{"locations"}, "Pass a location query"},
		{[]string{"extract", "https://example.com"}, "No API key found"},
		{[]string{"--api-key", "x", "extract", "example.com"}, "e.g. https://example.com"},
		{[]string{"crawl", "https://example.com", "mode=map"}, "crawl always uses mode=crawl"},
	}
	for _, tt := range tests {
		res := serpapi(t, "", tt.args...)
		if res.code != 2 {
			t.Errorf("%v: exit code %d, want 2; stderr: %s", tt.args, res.code, res.stderr)
			continue
		}
		e := errorJSON(t, res)
		if msg, _ := e["message"].(string); e["code"] != "usage_error" || !strings.Contains(msg, tt.msg) {
			t.Errorf("%v: error %v, want usage_error containing %q", tt.args, e, tt.msg)
		}
		if res.stdout != "" {
			t.Errorf("%v: stdout %q, want empty", tt.args, res.stdout)
		}
	}
}

func TestHelpAndVersion(t *testing.T) {
	res := serpapi(t, "", "--help")
	wantSuccess(t, res)
	if !strings.Contains(res.stdout, "search") {
		t.Errorf("help does not mention search: %s", res.stdout)
	}

	res = serpapi(t, "", "--version")
	wantSuccess(t, res)
	if !strings.HasPrefix(res.stdout, "serpapi version ") {
		t.Errorf("version output %q", res.stdout)
	}
}

func TestSearchBasic(t *testing.T) {
	key := requireLive(t)
	res := serpapi(t, "", "--api-key", key, "search", "engine=google", "q=coffee")
	wantSuccess(t, res)
	if !strings.Contains(res.stdout, "search_metadata") {
		t.Error("expected output to contain search_metadata")
	}
}

func TestSearchWithFields(t *testing.T) {
	key := requireLive(t)
	res := serpapi(t, "", "--api-key", key, "--fields", "organic_results[0:2]", "search", "engine=google", "q=coffee")
	wantSuccess(t, res)
	var v struct {
		OrganicResults []any           `json:"organic_results"`
		Metadata       json.RawMessage `json:"search_metadata"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &v); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if n := len(v.OrganicResults); n == 0 || n > 2 {
		t.Errorf("got %d organic results, want 1-2", n)
	}
	if v.Metadata != nil {
		t.Error("--fields kept search_metadata")
	}
}

func TestSearchWithJQ(t *testing.T) {
	key := requireLive(t)
	res := serpapi(t, "", "--api-key", key, "--jq", ".search_metadata.id", "search", "engine=google", "q=coffee")
	wantSuccess(t, res)
	if id := strings.TrimSpace(res.stdout); id == "" || strings.Contains(id, `"`) {
		t.Errorf("want one unquoted search ID, got %q", res.stdout)
	}
}

// TestSearchDebugKeepsStderrJSON pipes stderr, as scripts and agents do, so
// the --debug trace must arrive as {"debug":{...}} lines and leave stdout
// untouched.
func TestSearchDebugKeepsStderrJSON(t *testing.T) {
	key := requireLive(t)
	res := serpapi(t, "", "--api-key", key, "--debug", "--jq", ".search_metadata.id", "search", "engine=google", "q=coffee")
	wantSuccess(t, res)
	if id := strings.TrimSpace(res.stdout); id == "" || strings.Contains(id, "debug") {
		t.Errorf("stdout should carry only the search ID, got %q", res.stdout)
	}

	var events []string
	for _, line := range strings.Split(strings.TrimSpace(res.stderr), "\n") {
		var v struct {
			Debug *struct {
				Event string `json:"event"`
			} `json:"debug"`
			Warning map[string]any `json:"warning"`
		}
		if err := json.Unmarshal([]byte(line), &v); err != nil || (v.Debug == nil && v.Warning == nil) {
			t.Fatalf("stderr line is not a JSON debug or warning object: %q", line)
		}
		if v.Debug != nil {
			events = append(events, v.Debug.Event)
		}
	}
	joined := strings.Join(events, "\n")
	for _, want := range []string{"GET https://serpapi.com/search.json?api_key=[REDACTED]", "TLS handshake done", "Request sent", "Response: HTTP/", "Server search: id="} {
		if !strings.Contains(joined, want) {
			t.Errorf("trace missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(res.stderr, key) {
		t.Error("trace leaked the API key")
	}
}

func TestAccount(t *testing.T) {
	key := requireLive(t)
	res := serpapi(t, "", "--api-key", key, "account")
	wantSuccess(t, res)
	if !strings.Contains(res.stdout, "account_email") {
		t.Error("expected account_email in output")
	}
	if strings.Contains(res.stdout, key) {
		t.Error("account output contains the full API key")
	}
}

func TestLocations(t *testing.T) {
	requireLive(t)
	res := serpapi(t, "", "locations", "q=austin", "limit=3")
	wantSuccess(t, res)
	var locations []map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &locations); err != nil {
		t.Fatalf("output is not a JSON array: %v", err)
	}
	if n := len(locations); n == 0 || n > 3 {
		t.Fatalf("got %d locations, want 1-3", n)
	}
	if locations[0]["canonical_name"] == nil {
		t.Errorf("location has no canonical_name: %v", locations[0])
	}
}

func TestArchive(t *testing.T) {
	key := requireLive(t)

	// Search without no_cache so the result is immediately archived.
	res := serpapi(t, "", "--api-key", key, "--jq", ".search_metadata.id", "search", "engine=google", "q=coffee")
	wantSuccess(t, res)
	searchID := strings.TrimSpace(res.stdout)

	res = serpapi(t, "", "--api-key", key, "archive", searchID)
	wantSuccess(t, res)
	if !strings.Contains(res.stdout, searchID) {
		t.Error("expected the search ID in the archived result")
	}

	res = serpapi(t, "", "--api-key", key, "archive", searchID, "output=md")
	wantSuccess(t, res)
	if strings.TrimSpace(res.stdout) == "" {
		t.Error("expected Markdown output for the archived search")
	}
}

func TestExtract(t *testing.T) {
	key := requireLive(t)
	res := serpapi(t, "", "--api-key", key, "--jq", ".page_information.title", "extract", "https://example.com/")
	wantSuccess(t, res)
	if got := strings.TrimSpace(res.stdout); got != "Example Domain" {
		t.Errorf("page title = %q, want Example Domain", got)
	}

	res = serpapi(t, "", "--api-key", key, "extract", "https://example.com/", "output=md")
	wantSuccess(t, res)
	// Markdown opens with the search's metadata as YAML front matter.
	if !strings.HasPrefix(res.stdout, "---\n") || !strings.Contains(res.stdout, "engine: extract") || !strings.Contains(res.stdout, "documentation examples") {
		t.Errorf("want the page as Markdown, got %q", res.stdout)
	}
}

// TestMapAndCrawl costs two searches: a map, and a crawl of one page.
func TestMapAndCrawl(t *testing.T) {
	key := requireLive(t)
	res := serpapi(t, "", "--api-key", key, "--jq", ".search_parameters.mode", "map", "https://example.com/")
	wantSuccess(t, res)
	if got := strings.TrimSpace(res.stdout); got != "map" {
		t.Errorf("map ran mode %q, want map", got)
	}

	res = serpapi(t, "", "--api-key", key, "--jq", ".pages | map(.url) | join(\" \")", "crawl", "https://example.com/", "max_pages=1")
	wantSuccess(t, res)
	if got := strings.TrimSpace(res.stdout); got != "https://example.com/" {
		t.Errorf("crawl pages = %q, want only https://example.com/", got)
	}
}

// smallest valid 1x1 transparent PNG
var png1x1 = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

func writeTestPNG(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, png1x1, 0o600); err != nil {
		t.Fatalf("failed to write test image: %v", err)
	}
	return path
}

func TestImageUpload(t *testing.T) {
	key := requireLive(t)
	cmd := exec.Command(binaryPath, "--api-key", key, "image", writeTestPNG(t))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("image upload failed: %v\n%s", err, out)
	}

	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("failed to parse upload output: %v", err)
	}
	if result["message"] != "Image uploaded successfully." {
		t.Errorf("unexpected message: %v", result["message"])
	}
	if id, _ := result["image_id"].(string); id == "" {
		t.Error("expected non-empty image_id")
	}
}

func TestImageUploadFromStdin(t *testing.T) {
	key := requireLive(t)
	cmd := exec.Command(binaryPath, "--api-key", key, "--jq", ".image_id", "image", "-")
	cmd.Stdin = bytes.NewReader(png1x1)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("image upload from stdin failed: %v\n%s", err, out)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		t.Error("expected non-empty image_id")
	}
}

func TestImageUploadRejectsInvalidImage(t *testing.T) {
	key := requireLive(t)
	path := filepath.Join(t.TempDir(), "invalid.txt")
	if err := os.WriteFile(path, []byte("invalid image data"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(binaryPath, "--api-key", key, "image", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected failure for invalid image")
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 1 {
		t.Errorf("expected exit code 1, got %d", exitErr.ExitCode())
	}
	if !strings.Contains(stderr.String(), "Invalid image format") {
		t.Errorf("expected invalid image format error, got: %s", stderr.String())
	}
}

func TestImageUploadMissingFile(t *testing.T) {
	cmd := exec.Command(binaryPath, "--api-key", "irrelevant", "image", filepath.Join(t.TempDir(), "missing.png"))
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected failure for missing file")
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 2 {
		t.Errorf("expected exit code 2, got %d", exitErr.ExitCode())
	}
}

func TestSearchWithImage(t *testing.T) {
	key := requireLive(t)
	cmd := exec.Command(binaryPath, "--api-key", key, "search", "engine=google_lens", "--image", writeTestPNG(t),
		"--jq", ".search_parameters.image_id")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("search with --image failed: %v\n%s", err, out)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		t.Error("expected the uploaded image_id in search_parameters")
	}
}

func TestSearchWithImageConflictsWithImageID(t *testing.T) {
	cmd := exec.Command(binaryPath, "--api-key", "irrelevant", "search", "engine=google_lens", "image_id=abc", "--image", writeTestPNG(t))
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected failure when both --image and image_id are given")
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 2 {
		t.Errorf("expected exit code 2, got %d", exitErr.ExitCode())
	}
}

func TestSearchAllPages(t *testing.T) {
	key := requireLive(t)
	res := serpapi(t, "", "--api-key", key, "search", "engine=google", "q=coffee",
		"--fields", "organic_results", "--all-pages", "--max-pages", "2")
	wantSuccess(t, res)
	var v map[string]json.RawMessage
	if err := json.Unmarshal([]byte(res.stdout), &v); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if v["organic_results"] == nil {
		t.Error("expected organic_results in merged output")
	}
	if v["serpapi_pagination"] != nil {
		t.Error("merged output keeps serpapi_pagination")
	}
}

func TestSearchAllPagesWithFields(t *testing.T) {
	key := requireLive(t)
	cmd := exec.Command(
		binaryPath,
		"--api-key", key,
		"search",
		"--fields", "organic_results[].{title,link},search_parameters.start",
		"engine=google",
		"q=coffee",
		"--all-pages",
		"--max-pages", "2",
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("search all pages with fields failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("failed to parse search output: %v", err)
	}
	for field := range result {
		if field != "organic_results" && field != "search_parameters" {
			t.Errorf("unexpected top-level field %q", field)
		}
	}

	searchParameters, ok := result["search_parameters"].(map[string]any)
	if !ok {
		t.Fatal("expected search_parameters object from the final page")
	}
	start, ok := searchParameters["start"].(float64)
	if !ok || start <= 0 {
		t.Fatalf("expected a positive final-page start offset, got %v", searchParameters["start"])
	}

	organicResults, ok := result["organic_results"].([]any)
	if !ok {
		t.Fatal("expected organic_results array")
	}
	if len(organicResults) < 2 {
		t.Fatalf("expected results from multiple pages, got %d", len(organicResults))
	}
	for i, item := range organicResults {
		resultItem, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("organic result %d is not an object", i)
		}
		for field := range resultItem {
			if field != "title" && field != "link" {
				t.Errorf("organic result %d contains unexpected field %q", i, field)
			}
		}
	}
}

func TestInvalidAPIKey(t *testing.T) {
	requireLive(t)
	res := serpapi(t, "", "--api-key", "invalid", "search", "engine=google", "q=test")
	if res.code != 1 {
		t.Fatalf("exit code %d, want 1; stderr: %s", res.code, res.stderr)
	}
	if e := errorJSON(t, res); e["code"] != "api_error" || e["status"] != float64(401) {
		t.Errorf("error %v, want api_error with status 401", e)
	}
}

func TestLoginFlow(t *testing.T) {
	key := requireLive(t)
	home := t.TempDir()

	res := serpapiIn(t, home, key+"\n", "login")
	wantSuccess(t, res)
	if res.stderr != "" {
		t.Errorf("login without a terminal should succeed silently, stderr: %s", res.stderr)
	}

	res = serpapiIn(t, home, "", "account")
	wantSuccess(t, res)
	if !strings.Contains(res.stdout, "account_email") {
		t.Error("the saved key did not work")
	}
}
