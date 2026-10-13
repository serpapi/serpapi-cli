package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const imageKey = "image_test_key"

// png1x1 is the smallest valid PNG, one transparent pixel.
var png1x1 = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

// imageUpload is what the fake Image API received.
type imageUpload struct {
	fileName, contentType, apiKey string
	size                          int
}

// imageAPI fakes /image, which answers with an image_id, and /search.json,
// which echoes the image_id it was sent. uploads returns what /image got.
func imageAPI(t *testing.T) (reqs *requests, uploads func() []imageUpload) {
	t.Helper()
	var mu sync.Mutex
	var got []imageUpload
	reqs = fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/image" {
			fmt.Fprintf(w, `{"search_metadata":{"id":"s1"},"search_parameters":{"image_id":%q}}`, r.URL.Query().Get("image_id"))
			return
		}
		u := imageUpload{apiKey: r.FormValue("api_key")}
		if f, h, err := r.FormFile("image"); err == nil {
			b, _ := io.ReadAll(f)
			u.fileName, u.contentType, u.size = h.Filename, h.Header.Get("Content-Type"), len(b)
		}
		mu.Lock()
		got = append(got, u)
		mu.Unlock()
		fmt.Fprint(w, `{"message":"Image uploaded successfully.","image_id":"img123"}`)
	})
	return reqs, func() []imageUpload {
		mu.Lock()
		defer mu.Unlock()
		return append([]imageUpload(nil), got...)
	}
}

func writeImage(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImageUploadsAFileAndPrintsTheResponse(t *testing.T) {
	reqs, uploads := imageAPI(t)

	res := execute(t, "", "--api-key", imageKey, "image", writeImage(t, "photo.png", png1x1))

	wantOK(t, res)
	var out map[string]string
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil || out["image_id"] != "img123" {
		t.Errorf("stdout = %q, want the upload's JSON with its image_id", res.stdout)
	}
	if u := reqs.only(t); u.Path != "/image" || u.Query().Has("api_key") {
		t.Errorf("request = %s, want /image with the key in the form, not the URL", u)
	}
	want := imageUpload{fileName: "photo.png", contentType: "image/png", apiKey: imageKey, size: len(png1x1)}
	if got := uploads(); len(got) != 1 || got[0] != want {
		t.Errorf("uploads = %+v, want %+v", got, want)
	}
}

func TestImageReadsStdin(t *testing.T) {
	_, uploads := imageAPI(t)

	res := execute(t, string(png1x1), "--api-key", imageKey, "image", "-")

	wantOK(t, res)
	want := imageUpload{fileName: "image.png", contentType: "image/png", apiKey: imageKey, size: len(png1x1)}
	if got := uploads(); len(got) != 1 || got[0] != want {
		t.Errorf("uploads = %+v, want %+v", got, want)
	}
}

func TestSearchWithImageSendsItsID(t *testing.T) {
	reqs, _ := imageAPI(t)

	res := execute(t, "", "--api-key", imageKey, "search", "engine=google_lens", "--image", writeImage(t, "photo.png", png1x1),
		"--jq", ".search_parameters.image_id")

	wantOK(t, res)
	if got := strings.TrimSpace(res.stdout); got != "img123" {
		t.Errorf("stdout = %q, want the uploaded image_id", got)
	}
	urls := reqs.list()
	if len(urls) != 2 || urls[0].Path != "/image" {
		t.Fatalf("requests = %v, want the upload, then the search", urls)
	}
	wantQuery(t, urls[1], "/search.json", map[string]string{"engine": "google_lens", "image_id": "img123", "api_key": imageKey})
}

// An upload has no engine, search or archive entry, so a timed-out upload
// gets only the retry advice, while the search after it keeps the search's.
func TestImageTimeoutAdviceFitsTheStep(t *testing.T) {
	const upload = "Request to /image timed out after 50ms: retry with a higher --timeout, such as --timeout 120, or 0 to wait indefinitely"
	const search = "Request to /search.json timed out after 50ms. Slow engines, such as google_ai_mode"
	for _, tc := range []struct {
		name, slow, msg string
		args            []string
	}{
		{"image", "/image", upload, []string{"image"}},
		{"search --image, slow upload", "/image", upload, []string{"search", "engine=google_lens", "--image"}},
		{"search --image, slow search", "/search.json", search, []string{"search", "engine=google_lens", "--image"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
				// Until it has read the body, the server can't notice the client give up.
				_, _ = io.Copy(io.Discard, r.Body)
				if r.URL.Path == tc.slow {
					hang(w, r)
					return
				}
				fmt.Fprint(w, `{"image_id":"img123"}`)
			})

			args := append([]string{"--api-key", imageKey, "--timeout", "0.05"}, tc.args...)
			res := execute(t, "", append(args, writeImage(t, "photo.png", png1x1))...)

			msg, _ := wantError(t, res, 1, "network_error", tc.msg)["message"].(string)
			wantSearchAdvice := tc.slow == "/search.json"
			for _, part := range []string{"Slow engines", "The search may still finish on SerpApi's side"} {
				if strings.Contains(msg, part) != wantSearchAdvice {
					t.Errorf("message %q: has %q = %v, want %v", msg, part, !wantSearchAdvice, wantSearchAdvice)
				}
			}
		})
	}
}

func TestImageNeedsAKeyBeforeUploading(t *testing.T) {
	for _, args := range [][]string{{"image"}, {"search", "engine=google_lens", "--image"}} {
		t.Run(args[0], func(t *testing.T) {
			fakeAPI(t, nil)
			res := execute(t, "", append(args, writeImage(t, "photo.png", png1x1))...)
			wantError(t, res, 2, "usage_error", "No API key")
		})
	}
}

func TestImageWithImageIDIsAUsageError(t *testing.T) {
	fakeAPI(t, nil)

	res := execute(t, "", "--api-key", imageKey, "search", "engine=google_lens", "image_id=abc", "--image", writeImage(t, "photo.png", png1x1))

	wantError(t, res, 2, "usage_error", "Use either --image or image_id=<id>, not both")
}

func TestImageRefusesEmptyAndOversizedImagesUnsent(t *testing.T) {
	big := append(append([]byte{}, png1x1...), make([]byte, 600<<10)...)
	for _, tc := range []struct {
		name, stdin, msg string
		args             func(t *testing.T) []string
	}{
		{"empty file", "", "The image is empty", func(t *testing.T) []string { return []string{"image", writeImage(t, "empty.png", nil)} }},
		{"empty stdin", "", "The image is empty", func(t *testing.T) []string { return []string{"image", "-"} }},
		{"600 KB file", "", "over 500 KB", func(t *testing.T) []string { return []string{"image", writeImage(t, "big.png", big)} }},
		{"600 KB on stdin", string(big), "over 500 KB", func(t *testing.T) []string { return []string{"image", "-"} }},
		{"600 KB for a search", "", "over 500 KB", func(t *testing.T) []string {
			return []string{"search", "engine=google_lens", "--image", writeImage(t, "big.png", big)}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeAPI(t, nil)
			res := execute(t, tc.stdin, append([]string{"--api-key", imageKey}, tc.args(t)...)...)
			wantError(t, res, 2, "usage_error", tc.msg)
		})
	}
}

func TestImageStopsAtOnceWhenInterruptedOnStdin(t *testing.T) {
	fakeAPI(t, nil)
	stdin, writer := io.Pipe()
	t.Cleanup(func() { writer.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(50*time.Millisecond, cancel)

	start := time.Now()
	res := executeInput(t, ctx, stdin, "--api-key", imageKey, "image", "-")

	wantError(t, res, 130, "interrupted", "Stopped while reading the image from stdin; nothing was uploaded")
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("image took %v to stop, want well under the %v grace period", elapsed, interruptGrace)
	}
}
