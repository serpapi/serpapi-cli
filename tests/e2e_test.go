package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binaryPath string

func TestMain(m *testing.M) {
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
	os.Exit(m.Run())
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

func requireKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("SERPAPI_KEY")
	if key == "" {
		t.Skip("requires SERPAPI_KEY")
	}
	return key
}

func TestSearchBasic(t *testing.T) {
	key := requireKey(t)
	cmd := exec.Command(binaryPath, "--api-key", key, "search", "engine=google", "q=coffee")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if !strings.Contains(string(out), "search_metadata") {
		t.Error("expected output to contain search_metadata")
	}
}

func TestSearchWithFields(t *testing.T) {
	key := requireKey(t)
	cmd := exec.Command(binaryPath, "--api-key", key, "--fields", "organic_results[0:2]", "search", "engine=google", "q=coffee")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("search with fields failed: %v", err)
	}
	output := string(out)
	if !strings.Contains(output, "[") && !strings.Contains(output, "{") {
		t.Error("expected JSON output")
	}
}

func TestSearchWithJQ(t *testing.T) {
	key := requireKey(t)
	cmd := exec.Command(binaryPath, "--api-key", key, "--jq", ".search_metadata.id", "search", "engine=google", "q=coffee")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("search with jq failed: %v", err)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		t.Error("expected non-empty jq output")
	}
}

func TestAccount(t *testing.T) {
	key := requireKey(t)
	cmd := exec.Command(binaryPath, "--api-key", key, "account")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("account failed: %v", err)
	}
	output := string(out)
	if !strings.Contains(output, "account_email") && !strings.Contains(output, "plan") {
		t.Error("expected account info in output")
	}
}

func TestLocations(t *testing.T) {
	// Locations don't require API key but need network
	cmd := exec.Command(binaryPath, "locations", "q=austin", "num=3")
	out, err := cmd.Output()
	if err != nil {
		t.Skip("locations endpoint unreachable")
	}
	output := string(out)
	if !strings.Contains(output, "[") && !strings.Contains(output, "canonical_name") {
		t.Error("expected locations JSON output")
	}
}

func TestArchive(t *testing.T) {
	key := requireKey(t)

	// Search without no_cache so the result is immediately archived.
	searchCmd := exec.Command(binaryPath, "--api-key", key, "search", "engine=google", "q=coffee")
	searchOut, err := searchCmd.Output()
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(searchOut, &result); err != nil {
		t.Fatalf("failed to parse search output: %v", err)
	}

	meta, ok := result["search_metadata"].(map[string]any)
	if !ok {
		t.Fatal("no search_metadata in result")
	}
	searchID, ok := meta["id"].(string)
	if !ok || searchID == "" {
		t.Fatal("no search ID in search_metadata")
	}

	cmd := exec.Command(binaryPath, "--api-key", key, "archive", searchID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("archive failed: %v\n%s", err, out)
	}
	output := string(out)
	if !strings.Contains(output, "search_metadata") {
		t.Error("expected search_metadata in archive result")
	}
}

func TestSearchAllPages(t *testing.T) {
	key := requireKey(t)
	cmd := exec.Command(binaryPath, "--api-key", key, "search", "engine=google", "q=coffee", "num=1", "--all-pages", "--max-pages", "2")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("search all pages failed: %v", err)
	}
	if !strings.Contains(string(out), "organic_results") {
		t.Error("expected organic_results in merged output")
	}
}

func TestInvalidAPIKey(t *testing.T) {
	cmd := exec.Command(binaryPath, "--api-key", "invalid", "search", "engine=google", "q=test")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected failure with invalid API key")
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 1 {
			t.Errorf("expected exit code 1, got %d", exitErr.ExitCode())
		}
	}
}

func TestNoArgs(t *testing.T) {
	cmd := exec.Command(binaryPath)
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected failure with no args")
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 2 {
			t.Errorf("expected exit code 2, got %d", exitErr.ExitCode())
		}
	}
}

func TestLoginFlow(t *testing.T) {
	key := requireKey(t)
	// Run login in an isolated HOME so it doesn't touch the real config.
	tmpHome := t.TempDir()
	cmd := exec.Command(binaryPath, "login")
	cmd.Stdin = strings.NewReader(key + "\n")
	cmd.Env = append(os.Environ(), "HOME="+tmpHome, "XDG_CONFIG_HOME="+tmpHome)
	if err := cmd.Run(); err != nil {
		t.Fatalf("login failed: %v", err)
	}
}
