package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
	"github.com/serpapi/serpapi-cli/pkg/version"
)

const (
	// DefaultBaseURL is the SerpApi production endpoint.
	DefaultBaseURL = "https://serpapi.com"
	// DefaultTimeout bounds one HTTP request, including reading the response
	// body. It matches the 60s default of the official serpapi-golang library.
	// Slow engines such as google_ai_mode can take longer with no_cache=true,
	// and SerpApi still finishes and counts a search the client gave up on.
	DefaultTimeout = 60 * time.Second
)

// maxResponseBytes caps the response body; a variable so tests can lower it.
var maxResponseBytes = 100 << 20

// quoteEscaper mirrors the escaping mime/multipart applies to form-data
// header values.
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

// Client is an HTTP client for the SerpApi service.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client

	// debug, if set, receives a trace of each request; see WithDebug.
	debug     io.Writer
	debugJSON bool
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at a different SerpApi-compatible host.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(baseURL, "/") }
}

// WithTimeout sets the per-request timeout. Zero or negative disables it.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.http.Timeout = max(d, 0) }
}

// NewWithTimeout creates a client whose requests are bounded by timeout; zero
// or negative disables the limit.
func NewWithTimeout(apiKey string, timeout time.Duration) *Client {
	return New(apiKey, WithTimeout(timeout))
}

// New creates a new SerpApi client. apiKey may be empty for unauthenticated requests.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:  apiKey,
		baseURL: DefaultBaseURL,
		http: &http.Client{
			Timeout: DefaultTimeout,
			// Never follow redirects: the request URL carries the API key.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func userAgent() string {
	return "serpapi-go-cli/" + version.Version
}

// get performs a GET request and returns the body of a 200 response verbatim.
func (c *Client) get(ctx context.Context, endpoint string, params map[string]string) ([]byte, error) {
	return c.getTraced(ctx, endpoint, params, c.newTracer())
}

// getTraced is get with a caller-supplied tracer, so Search can keep logging
// against the same timeline after the body is read.
func (c *Client) getTraced(ctx context.Context, endpoint string, params map[string]string, tr *tracer) ([]byte, error) {
	u, err := url.Parse(c.baseURL + endpoint)
	if err != nil {
		return nil, &clierrors.NetworkError{Message: "Invalid URL: " + err.Error(), Cause: err}
	}

	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	if tr != nil {
		ctx = httptrace.WithClientTrace(ctx, tr.clientTrace())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, &clierrors.NetworkError{Message: "Invalid request: " + endpoint, Cause: err}
	}
	return c.do(req, endpoint, tr)
}

// doPostMultipart sends a multipart/form-data POST request. params are sent as
// form fields alongside a single file part named fieldName.
func (c *Client) doPostMultipart(ctx context.Context, endpoint string, params map[string]string, fieldName, fileName string, content []byte, tr *tracer) ([]byte, error) {
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

	if tr != nil {
		ctx = httptrace.WithClientTrace(ctx, tr.clientTrace())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpoint, &body)
	if err != nil {
		return nil, &clierrors.NetworkError{Message: "Invalid request: " + endpoint, Cause: err}
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	return c.do(req, endpoint, tr)
}

// do sends req and returns the body of a 200 response verbatim, logging each
// step to tr, which may be nil.
func (c *Client) do(req *http.Request, endpoint string, tr *tracer) ([]byte, error) {
	req.Header.Set("User-Agent", userAgent())
	tr.logRequest(req, c.http.Timeout)

	resp, err := c.http.Do(req)
	if err != nil {
		tr.logf("Request failed: %v", err)
		return nil, c.networkError(endpoint, err)
	}
	defer resp.Body.Close()
	tr.logResponse(resp)

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxResponseBytes)+1))
	if err != nil {
		tr.logf("Read body failed: %v", err)
		return nil, c.networkError(endpoint, err)
	}
	tr.logf("Body received: %d bytes", len(body))

	if resp.StatusCode != http.StatusOK {
		message := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		if msg := errorMessage(body); msg != "" {
			message = msg
		}
		return nil, &clierrors.APIError{Message: message, Status: resp.StatusCode}
	}
	if len(body) > maxResponseBytes {
		return nil, &clierrors.APIError{Message: "Response exceeded 100 MB limit"}
	}
	return body, nil
}

// networkError describes a transport failure without the request URL, which contains the API key.
func (c *Client) networkError(endpoint string, err error) error {
	cause := err
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		cause = urlErr.Err
	}
	var netErr net.Error
	if c.http.Timeout > 0 && (errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())) {
		debugHint := ""
		if c.debug == nil {
			debugHint = "; --debug shows which step stalled"
		}
		// An upload has no engine, search or archive entry to advise on.
		if endpoint == "/image" {
			return &clierrors.NetworkError{
				Message: fmt.Sprintf("Request to %s timed out after %s: retry with a higher --timeout, such as --timeout %d, or 0 to wait indefinitely%s",
					endpoint, seconds(c.http.Timeout), suggestedTimeout(c.http.Timeout), debugHint),
				Cause: cause,
			}
		}
		return &clierrors.NetworkError{
			Message: fmt.Sprintf("Request to %s timed out after %s. Slow engines, such as google_ai_mode, and no_cache=true can take longer: "+
				"retry with a higher --timeout, such as --timeout %d, or 0 to wait indefinitely%s. "+
				"The search may still finish on SerpApi's side, count against your searches and be available in your search archive",
				endpoint, seconds(c.http.Timeout), suggestedTimeout(c.http.Timeout), debugHint),
			Cause: cause,
		}
	}
	return &clierrors.NetworkError{
		Message: fmt.Sprintf("Request to %s failed: %v", endpoint, cause),
		Cause:   cause,
	}
}

// suggestedTimeout is the --timeout to suggest after d ran out: twice d in
// whole seconds, and at least 120.
func suggestedTimeout(d time.Duration) int64 {
	return max(int64((2*d+time.Second-1)/time.Second), 120)
}

// seconds formats d the way --timeout takes it: 60s rather than 1m0s.
func seconds(d time.Duration) string {
	if d%time.Second == 0 {
		return strconv.FormatInt(int64(d/time.Second), 10) + "s"
	}
	return d.String()
}

// errorMessage returns the top-level "error" string of a JSON object body, if any.
func errorMessage(body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ""
	}
	var envelope struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(trimmed, &envelope) != nil {
		return ""
	}
	msg, _ := envelope.Error.(string)
	return msg
}

func (c *Client) getJSON(ctx context.Context, endpoint string, params map[string]string) (json.RawMessage, error) {
	return c.getJSONTraced(ctx, endpoint, params, c.newTracer())
}

func (c *Client) getJSONTraced(ctx context.Context, endpoint string, params map[string]string, tr *tracer) (json.RawMessage, error) {
	body, err := c.getTraced(ctx, endpoint, params, tr)
	if err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, &clierrors.APIError{Message: "Unexpected non-JSON response from server"}
	}
	if msg := errorMessage(body); msg != "" {
		return nil, &clierrors.APIError{Message: msg, Status: http.StatusOK}
	}
	return json.RawMessage(body), nil
}

func (c *Client) searchParams(params map[string]string) map[string]string {
	p := make(map[string]string, len(params)+1)
	for k, v := range params {
		p[k] = v
	}
	if c.apiKey != "" {
		p["api_key"] = c.apiKey
	}
	return p
}

// Search performs a search request. If the client has an API key it is added to params.
func (c *Client) Search(ctx context.Context, params map[string]string) (json.RawMessage, error) {
	tr := c.newTracer()
	body, err := c.getObjectTraced(ctx, "/search.json", c.searchParams(params), tr)
	if err != nil {
		return nil, err
	}
	tr.logSearchMetadata(body)
	return body, nil
}

// getObject is getJSON for endpoints that answer with a JSON object, so a
// stray null or array is an error rather than an empty-looking success.
func (c *Client) getObject(ctx context.Context, endpoint string, params map[string]string) (json.RawMessage, error) {
	return c.getObjectTraced(ctx, endpoint, params, c.newTracer())
}

func (c *Client) getObjectTraced(ctx context.Context, endpoint string, params map[string]string, tr *tracer) (json.RawMessage, error) {
	body, err := c.getJSONTraced(ctx, endpoint, params, tr)
	if err != nil {
		return nil, err
	}
	if bytes.TrimSpace(body)[0] != '{' {
		return nil, &clierrors.APIError{Message: "Unexpected response from server: not a JSON object"}
	}
	return body, nil
}

// getRaw returns a 200 body verbatim unless it is a JSON error object, which
// SerpApi sends instead of the requested format when a search fails.
func (c *Client) getRaw(ctx context.Context, endpoint string, params map[string]string) ([]byte, error) {
	body, err := c.get(ctx, endpoint, params)
	if err != nil {
		return nil, err
	}
	if msg := errorMessage(body); msg != "" {
		return nil, &clierrors.APIError{Message: msg, Status: http.StatusOK}
	}
	return body, nil
}

// SearchRaw performs a search request and returns the body verbatim, for
// non-JSON formats selected with the output parameter (for example output=md).
func (c *Client) SearchRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.getRaw(ctx, "/search.json", c.searchParams(params))
}

// Account retrieves account information.
func (c *Client) Account(ctx context.Context) (json.RawMessage, error) {
	return c.getObject(ctx, "/account.json", map[string]string{"api_key": c.apiKey})
}

// Locations queries the locations endpoint. No API key is added.
func (c *Client) Locations(ctx context.Context, params map[string]string) (json.RawMessage, error) {
	body, err := c.getJSON(ctx, "/locations.json", params)
	if err != nil {
		return nil, err
	}
	if trimmed := bytes.TrimSpace(body); trimmed[0] != '[' {
		return nil, &clierrors.APIError{Message: "Unexpected response from locations endpoint"}
	}
	return body, nil
}

// UploadImage uploads an image file using the Image API. The returned payload
// contains an image_id, which only engine=google_lens takes. Image IDs expire
// after 10 minutes.
func (c *Client) UploadImage(ctx context.Context, path string) (json.RawMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, &clierrors.UsageError{Message: "Cannot read image file: " + err.Error()}
	}
	defer f.Close()
	content, err := ReadImage(f)
	if err != nil {
		return nil, &clierrors.UsageError{Message: "Cannot read image file: " + err.Error()}
	}
	return c.UploadImageBytes(ctx, content, filepath.Base(path))
}

// MaxImageSize is the Image API's 500 KB limit, read as 500 KiB so the CLI
// never refuses an image the API would take.
const MaxImageSize = 500 << 10

// ReadImage reads an image to upload, stopping one byte past MaxImageSize:
// UploadImageBytes refuses a larger one, which needn't be read whole.
func ReadImage(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, MaxImageSize+1))
}

// UploadImageBytes uploads an in-memory image using the Image API. fileName
// is sent as the multipart filename; when empty, one is derived from the
// detected content type.
func (c *Client) UploadImageBytes(ctx context.Context, content []byte, fileName string) (json.RawMessage, error) {
	switch {
	case len(content) == 0:
		return nil, &clierrors.UsageError{Message: "The image is empty"}
	case len(content) > MaxImageSize:
		return nil, &clierrors.UsageError{Message: "The image is over 500 KB, the Image API's limit; resize or compress it"}
	}
	if fileName == "" {
		fileName = defaultImageFileName(content)
	}

	params := map[string]string{}
	if c.apiKey != "" {
		params["api_key"] = c.apiKey
	}

	body, err := c.doPostMultipart(ctx, "/image", params, "image", fileName, content, c.newTracer())
	if err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, &clierrors.APIError{Message: "Unexpected non-JSON response from server"}
	}
	if msg := errorMessage(body); msg != "" {
		return nil, &clierrors.APIError{Message: msg, Status: http.StatusOK}
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

// Archive retrieves a previous search by ID.
func (c *Client) Archive(ctx context.Context, id string) (json.RawMessage, error) {
	return c.getObject(ctx, archivePath(id), map[string]string{"api_key": c.apiKey})
}

// ArchiveRaw retrieves a previous search in another output format (for
// example md) and returns the body verbatim.
func (c *Client) ArchiveRaw(ctx context.Context, id, output string) ([]byte, error) {
	return c.getRaw(ctx, archivePath(id), map[string]string{"api_key": c.apiKey, "output": output})
}

func archivePath(id string) string {
	return "/searches/" + url.PathEscape(id) + ".json"
}
