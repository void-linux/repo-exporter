// Package requests contain functions for calling HTTP/HTTPS requests
// for each HTTP method.
package requests

import (
	"io/ioutil"
	"net/http"
	"time"
)

// HTTPClient struct implements the RequestHandler
// It needs to receive a expected timeout value for his clients.
type HTTPClient struct {
	timeout time.Duration
}

// NewHTTPRequestHandler build a new request handler
func NewHTTPRequestHandler(timeout time.Duration) HTTPClient {
	return HTTPClient{timeout: timeout}
}

// Fetch create a GET HTTP request asking for content
// The request will timeout after 10s
func (h HTTPClient) Fetch(url string) ([]byte, int, error) {
	c := http.Client{Timeout: h.timeout}

	resp, err := c.Get(url)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	bytes, err := ioutil.ReadAll(resp.Body)
	return bytes, resp.StatusCode, err
}

// Head creates a HEAD HTTP request asking for headers
// The request will timeout after 10s
func (h HTTPClient) Head(url string) (map[string][]string, int, error) {
	c := http.Client{Timeout: h.timeout}

	resp, err := c.Head(url)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	return resp.Header, resp.StatusCode, err
}
