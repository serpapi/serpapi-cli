package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
	"github.com/serpapi/serpapi-cli/pkg/version"
)

const (
	defaultTimeout     = 30 * time.Second
	maxResponseBytes   = 100 << 20 // 100 MB
)

// quoteEscaper mirrors the escaping mime/multipart applies to form-data
// header values.
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

// Client is an HTTP client for the SerpApi service.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// New creates a new SerpApi client. apiKey may be empty for unauthenticated requests.
func New(apiKey string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: "https://serpapi.com",
		http: &http.Client{
			Timeout: defaultTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *Client) userAgent() string {
	return "serpapi-go-cli/" + version.Version
}

func (c *Client) doGet(ctx context.Context, endpoint string, params map[string]string) ([]byte, error) {
	u, err := url.Parse(c.baseURL + endpoint)
	if err != nil {
		return nil, &clierrors.NetworkError{Message: "Invalid URL: " + err.Error(), Cause: err}
	}

	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, &clierrors.NetworkError{Message: err.Error(), Cause: err}
	}
	return c.do(req)
}

// doPostMultipart sends a multipart/form-data POST request. params are sent as
// form fields alongside a single file part named fieldName.
func (c *Client) doPostMultipart(ctx context.Context, endpoint string, params map[string]string, fieldName, fileName string, content []byte) ([]byte, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range params {
		if err := w.WriteField(k, v); err != nil {
			return nil, &clierrors.NetworkError{Message: "Failed to encode request: " + err.Error(), Cause: err}
		}
	}

	// CreateFormFile hardcodes application/octet-stream, so build the part
	// header by hand to send the sniffed image content type.
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
		quoteEscaper.Replace(fieldName), quoteEscaper.Replace(fileName)))
	h.Set("Content-Type", http.DetectContentType(content))
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, &clierrors.NetworkError{Message: "Failed to encode request: " + err.Error(), Cause: err}
	}
	if _, err := part.Write(content); err != nil {
		return nil, &clierrors.NetworkError{Message: "Failed to encode request: " + err.Error(), Cause: err}
	}
	if err := w.Close(); err != nil {
		return nil, &clierrors.NetworkError{Message: "Failed to encode request: " + err.Error(), Cause: err}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+endpoint, &body)
	if err != nil {
		return nil, &clierrors.NetworkError{Message: err.Error(), Cause: err}
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	return c.do(req)
}

// do executes the request and returns the body after validating that the
// response is a successful JSON payload.
func (c *Client) do(req *http.Request) ([]byte, error) {
	req.Header.Set("User-Agent", c.userAgent())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &clierrors.NetworkError{Message: err.Error(), Cause: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, &clierrors.NetworkError{Message: "Failed to read response: " + err.Error(), Cause: err}
	}
	// Detect truncation: try reading one more byte; if it succeeds the body exceeded the limit.
	if len(body) == maxResponseBytes {
		var extra [1]byte
		if n, _ := resp.Body.Read(extra[:]); n > 0 {
			return nil, &clierrors.APIError{Message: "Response exceeded 100 MB limit"}
		}
	}

	if resp.StatusCode != http.StatusOK {
		// Try to extract error message from JSON body
		var parsed map[string]any
		if json.Unmarshal(body, &parsed) == nil {
			if errVal, ok := parsed["error"]; ok {
				if msg, ok := errVal.(string); ok {
					return nil, &clierrors.APIError{Message: msg}
				}
			}
		}
		return nil, &clierrors.APIError{
			Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
		}
	}

	// Validate response is JSON (guard against HTML error pages with 200 status).
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return nil, &clierrors.APIError{Message: "Unexpected non-JSON response from server"}
	}

	return body, nil
}

// checkAPIError returns an APIError if the JSON body contains a top-level "error" key.
func checkAPIError(body []byte) error {
	var envelope struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != "" {
		return &clierrors.APIError{Message: envelope.Error}
	}
	return nil
}

// Search performs a search request. If the client has an API key it is added to params.
func (c *Client) Search(ctx context.Context, params map[string]string) (json.RawMessage, error) {
	p := make(map[string]string, len(params)+1)
	for k, v := range params {
		p[k] = v
	}
	if c.apiKey != "" {
		p["api_key"] = c.apiKey
	}

	body, err := c.doGet(ctx, "/search.json", p)
	if err != nil {
		return nil, err
	}
	if err := checkAPIError(body); err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

// Account retrieves account information.
func (c *Client) Account(ctx context.Context) (json.RawMessage, error) {
	params := map[string]string{"api_key": c.apiKey}

	body, err := c.doGet(ctx, "/account.json", params)
	if err != nil {
		return nil, err
	}
	if err := checkAPIError(body); err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

// Locations queries the locations endpoint. No API key is added.
func (c *Client) Locations(ctx context.Context, params map[string]string) (json.RawMessage, error) {
	body, err := c.doGet(ctx, "/locations.json", params)
	if err != nil {
		return nil, err
	}
	// Locations returns a JSON array; check for an error object.
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '{' {
		if err := checkAPIError(body); err != nil {
			return nil, err
		}
		return nil, &clierrors.APIError{Message: "Unexpected response from locations endpoint"}
	}
	return json.RawMessage(body), nil
}

// UploadImage uploads an image file using the Image API. The returned payload
// contains an image_id usable with engines that accept uploaded images, such
// as google_lens. Image IDs expire after 10 minutes.
func (c *Client) UploadImage(ctx context.Context, path string) (json.RawMessage, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, &clierrors.UsageError{Message: "Cannot read image file: " + err.Error()}
	}
	return c.UploadImageBytes(ctx, content, filepath.Base(path))
}

// UploadImageBytes uploads an in-memory image using the Image API. fileName
// is sent as the multipart filename; when empty, one is derived from the
// detected content type.
func (c *Client) UploadImageBytes(ctx context.Context, content []byte, fileName string) (json.RawMessage, error) {
	if fileName == "" {
		fileName = defaultImageFileName(content)
	}

	params := map[string]string{}
	if c.apiKey != "" {
		params["api_key"] = c.apiKey
	}

	body, err := c.doPostMultipart(ctx, "/image", params, "image", fileName, content)
	if err != nil {
		return nil, err
	}
	if err := checkAPIError(body); err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

// defaultImageFileName picks a filename with an extension matching the
// sniffed image type, for uploads that have no natural filename (e.g. stdin).
func defaultImageFileName(content []byte) string {
	switch http.DetectContentType(content) {
	case "image/jpeg":
		return "image.jpg"
	case "image/png":
		return "image.png"
	case "image/webp":
		return "image.webp"
	}
	return "image"
}

// Archive retrieves a previously cached search result by ID.
func (c *Client) Archive(ctx context.Context, id string) (json.RawMessage, error) {
	params := map[string]string{"api_key": c.apiKey}

	body, err := c.doGet(ctx, "/searches/"+id+".json", params)
	if err != nil {
		return nil, err
	}
	if err := checkAPIError(body); err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}
