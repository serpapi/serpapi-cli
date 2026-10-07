package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

// smallest valid 1x1 transparent PNG
var png1x1 = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

type uploadRequest struct {
	method      string
	path        string
	apiKey      string
	fileName    string
	contentType string
	content     []byte
}

func newUploadServer(t *testing.T, status int, body string) (*Client, *uploadRequest) {
	t.Helper()
	got := &uploadRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.path = r.URL.Path
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart form: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		got.apiKey = r.FormValue("api_key")
		file, header, err := r.FormFile("image")
		if err != nil {
			t.Errorf("missing image part: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		got.fileName = header.Filename
		got.contentType = header.Header.Get("Content-Type")
		got.content, _ = io.ReadAll(file)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	client := New("secret_key")
	client.baseURL = srv.URL
	return client, got
}

func TestUploadImageFromFile(t *testing.T) {
	client, got := newUploadServer(t, http.StatusOK, `{"message":"Image uploaded successfully.","image_id":"test-image-id"}`)

	path := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(path, png1x1, 0o600); err != nil {
		t.Fatal(err)
	}

	raw, err := client.UploadImage(context.Background(), path)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["image_id"] != "test-image-id" {
		t.Errorf("expected image_id test-image-id, got %q", result["image_id"])
	}

	if got.method != http.MethodPost {
		t.Errorf("expected POST, got %s", got.method)
	}
	if got.path != "/image" {
		t.Errorf("expected /image path, got %s", got.path)
	}
	if got.apiKey != "secret_key" {
		t.Errorf("expected api_key form field, got %q", got.apiKey)
	}
	if got.fileName != "photo.png" {
		t.Errorf("expected filename photo.png, got %q", got.fileName)
	}
	if got.contentType != "image/png" {
		t.Errorf("expected image/png content type, got %q", got.contentType)
	}
	if string(got.content) != string(png1x1) {
		t.Error("uploaded content does not match the file")
	}
}

func TestUploadImageBytesDerivesFileName(t *testing.T) {
	client, got := newUploadServer(t, http.StatusOK, `{"message":"Image uploaded successfully.","image_id":"id"}`)

	if _, err := client.UploadImageBytes(context.Background(), png1x1, ""); err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if got.fileName != "image.png" {
		t.Errorf("expected derived filename image.png, got %q", got.fileName)
	}
}

func TestUploadImageRejectsInvalidImage(t *testing.T) {
	client, _ := newUploadServer(t, http.StatusBadRequest, `{"error":"Invalid image format. Supported format: jpg, jpeg, png, webp"}`)

	_, err := client.UploadImageBytes(context.Background(), []byte("invalid image data"), "invalid.txt")
	var apiErr *clierrors.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.Message != "Invalid image format. Supported format: jpg, jpeg, png, webp" {
		t.Errorf("unexpected message: %q", apiErr.Message)
	}
}

func TestUploadImageMissingFile(t *testing.T) {
	client := New("secret_key")
	_, err := client.UploadImage(context.Background(), filepath.Join(t.TempDir(), "does-not-exist.png"))
	var usageErr *clierrors.UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("expected UsageError, got %T: %v", err, err)
	}
}
