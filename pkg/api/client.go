package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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

// Client is an HTTP client for the SerpApi service.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
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
	u, err := url.Parse(c.baseURL + endpoint)
	if err != nil {
		return nil, &clierrors.NetworkError{Message: "Invalid URL: " + err.Error(), Cause: err}
	}

	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, &clierrors.NetworkError{Message: "Invalid request: " + endpoint, Cause: err}
	}
	req.Header.Set("User-Agent", userAgent())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, c.networkError(endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxResponseBytes)+1))
	if err != nil {
		return nil, c.networkError(endpoint, err)
	}

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
		return &clierrors.NetworkError{
			Message: fmt.Sprintf("Request to %s timed out after %s. Slow engines, such as google_ai_mode, and no_cache=true can take longer: "+
				"retry with a higher --timeout, such as --timeout %d, or 0 to wait indefinitely. "+
				"The search may still finish on SerpApi's side, count against your searches and be available in your search archive",
				endpoint, seconds(c.http.Timeout), suggestedTimeout(c.http.Timeout)),
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
	body, err := c.get(ctx, endpoint, params)
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
	return c.getObject(ctx, "/search.json", c.searchParams(params))
}

// getObject is getJSON for endpoints that answer with a JSON object, so a
// stray null or array is an error rather than an empty-looking success.
func (c *Client) getObject(ctx context.Context, endpoint string, params map[string]string) (json.RawMessage, error) {
	body, err := c.getJSON(ctx, endpoint, params)
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
