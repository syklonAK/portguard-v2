// Package nodeclient talks to remote PortGuard nodes over their node API:
	// GET/POST /api/node/* endpoints authenticated with the shared node token.
	package nodeclient

	import (
		"bytes"
		"encoding/json"
		"fmt"
		"io"
		"net/http"
		"strconv"
		"strings"
		"time"
	)

	// Client is an authenticated client for one remote node.
	type Client struct {
		BaseURL string
		Token   string
		HTTP    *http.Client
	}

	// RoundTripperFunc allows custom transport implementations.
	type RoundTripperFunc func(*http.Request) (*http.Response, error)

	func (f RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
		return f(req)
	}

	// sharedTransport keeps idle keep-alive connections to nodes open so the
	// frequent polling loops (metrics, alerter, bandwidth sync) reuse TCP
	// connections instead of re-handshaking on every request.
	var sharedTransport = &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
	}

	func New(host string, port int, token string) *Client {
		scheme := "http"
		base := strings.TrimSuffix(host, "/")
		if strings.HasPrefix(host, "https://") {
			scheme = "https"
			base = strings.TrimPrefix(host, "https://")
		} else if strings.HasPrefix(host, "http://") {
			base = strings.TrimPrefix(host, "http://")
		}
		return &Client{
			BaseURL: fmt.Sprintf("%s://%s:%d", scheme, base, port),
			Token:   token,
			HTTP:    &http.Client{Timeout: 15 * time.Second, Transport: sharedTransport},
		}
	}

	// NewWithTransport creates a client with a custom RoundTripper (e.g., hub-backed).
	func NewWithTransport(transport http.RoundTripper, token string) *Client {
		return &Client{
			BaseURL: "hub://",
			Token:   token,
			HTTP:    &http.Client{Timeout: 15 * time.Second, Transport: transport},
		}
	}

func (c *Client) do(method, path string, body any, out any) error {
	// generic helpers (Get/Post/Delete) receive bare agent-router paths
	// ("/mappings", "/certs", "/ratelimit/apply", ...) while the typed
	// methods pass fully-qualified "/api/node/..." paths — normalize bare
	// ones so both transports hit the same node routes
	if !strings.HasPrefix(path, "/api/node/") {
		p := "/" + strings.TrimPrefix(path, "/")
		if !strings.HasPrefix(p, "/api/node/") {
			p = "/api/node" + p
		}
		path = p
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return fmt.Errorf("node returned %d: %s", resp.StatusCode, msg)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("node response parse error: %v", err)
		}
	}
	return nil
}

// Ping checks reachability + token validity.
func (c *Client) Ping() error {
	return c.do(http.MethodGet, "/api/node/ping", nil, nil)
}

// Summary is the aggregated node overview.
type Summary struct {
	Version  string         `json:"version"`
	Role     string         `json:"role"`
	System   map[string]any `json:"system"`
	Mappings map[string]any `json:"mappings"`
	Ports    map[string]any `json:"ports"`
	Health   map[string]any `json:"health"`
	Tunnel   map[string]any `json:"tunnel"`
}

func (c *Client) GetSummary() (*Summary, error) {
	var s Summary
	if err := c.do(http.MethodGet, "/api/node/summary", nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ListMappings fetches the node's mappings.
func (c *Client) ListMappings() (json.RawMessage, error) {
	var out struct {
		Mappings json.RawMessage `json:"mappings"`
	}
	if err := c.do(http.MethodGet, "/api/node/mappings", nil, &out); err != nil {
		return nil, err
	}
	return out.Mappings, nil
}

// Apply triggers config apply on the node.
func (c *Client) Apply() error {
	return c.do(http.MethodPost, "/api/node/apply", nil, nil)
}

// Connections fetches the node's live connection log.
func (c *Client) Connections() (json.RawMessage, error) {
	var out struct {
		Data json.RawMessage `json:"data"`
	}
	if err := c.do(http.MethodGet, "/api/node/connections", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// ToolState mirrors internal/tools.State over the wire.
type ToolState struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Binary    string `json:"binary,omitempty"`
	Category  string `json:"category"`
}

// Tools lists the managed-tool state on the node.
func (c *Client) Tools() ([]ToolState, error) {
	var out struct {
		Tools []ToolState `json:"tools"`
	}
	if err := c.do(http.MethodGet, "/api/node/tools", nil, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

// Get performs a token-authenticated GET returning the raw JSON.
func (c *Client) Get(path string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Post sends a JSON body and returns the raw JSON response.
func (c *Client) Post(path string, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Put sends a JSON body and returns the raw JSON response.
func (c *Client) Put(path string, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(http.MethodPut, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Delete performs a token-authenticated DELETE returning the raw JSON.
func (c *Client) Delete(path string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(http.MethodDelete, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Logs fetches a bounded tail of one allowlisted log source from the node.
func (c *Client) Logs(source string, lines int) (string, error) {
	var out struct {
		Lines string `json:"lines"`
	}
	path := "/logs/" + source
	if lines > 0 {
		path += "?lines=" + strconv.Itoa(lines)
	}
	if err := c.do(http.MethodGet, path, nil, &out); err != nil {
		return "", err
	}
	return out.Lines, nil
}

// InstallTool runs the official installer for one tool on the node.
func (c *Client) InstallTool(id string) (*InstallResult, error) {
	var out InstallResult
	if err := c.do(http.MethodPost, "/api/node/tools/"+id+"/install", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// InstallResult mirrors internal/tools.InstallResult over the wire.
type InstallResult struct {
	ToolID  string `json:"tool_id"`
	OK      bool   `json:"ok"`
	Output  string `json:"output"`
	Elapsed string `json:"elapsed"`
}
