package requests

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetch(t *testing.T) {
	// starting http test server
	server := httptest.NewServer(http.HandlerFunc(testHTTPFunc))
	defer server.Close()

	// creating HTTPRequestHandler for tests
	handler := NewHTTPRequestHandler(100 * time.Millisecond)

	var tests = []struct {
		name               string
		givenURL           string
		expectedBody       []byte
		expectedStatusCode int
		expectedErr        error
	}{
		{
			"Fetch/GET request with success",
			fmt.Sprintf("%s/test", server.URL),
			[]byte("OK"),
			http.StatusOK,
			nil,
		},
		{
			"Fetch/GET non-existent endpoint",
			server.URL,
			nil,
			http.StatusInternalServerError,
			nil,
		},
		{
			"Fetch/GET request timeout",
			fmt.Sprintf("%s/timeout", server.URL),
			nil,
			0,
			context.DeadlineExceeded,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			body, statusCode, err := handler.Fetch(tt.givenURL)

			// assert expected error
			if err != nil && errors.Is(err, tt.expectedErr) {
				t.Errorf("(%s): expected error %s, actual %s", tt.givenURL, tt.expectedErr, err)
			}

			// assert expected status code
			if statusCode != tt.expectedStatusCode {
				t.Errorf("(%s): expected status code %d, actual %d", tt.givenURL, tt.expectedStatusCode, statusCode)
			}

			// assert expected body
			if !bytes.Equal(body, tt.expectedBody) {
				t.Errorf("(%s): expected body %s, actual %s", tt.givenURL, tt.expectedBody, body)
			}

		})
	}
}

func TestHead(t *testing.T) {
	// starting http test server
	server := httptest.NewServer(http.HandlerFunc(testHTTPFunc))
	defer server.Close()

	// creating HTTPRequestHandler for tests
	handler := NewHTTPRequestHandler(100 * time.Millisecond)

	var tests = []struct {
		name               string
		givenURL           string
		expectedHeaders    []string
		expectedStatusCode int
		expectedErr        error
	}{
		{
			"HEAD request with success",
			fmt.Sprintf("%s/test", server.URL),
			[]string{
				"Content-Length",
				"Content-Type",
			},
			http.StatusOK,
			nil,
		},
		{
			"HEAD non-existent endpoint",
			server.URL,
			nil,
			http.StatusInternalServerError,
			nil,
		},
		{
			"HEAD request timeout",
			fmt.Sprintf("%s/timeout", server.URL),
			nil,
			0,
			context.DeadlineExceeded,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			headers, statusCode, err := handler.Head(tt.givenURL)

			// assert expected error
			if err != nil && errors.Is(err, tt.expectedErr) {
				t.Errorf("(%s): expected error %s, actual %s", tt.givenURL, tt.expectedErr, err)
			}

			// assert expected status code
			if statusCode != tt.expectedStatusCode {
				t.Errorf("(%s): expected status code %d, actual %d", tt.givenURL, tt.expectedStatusCode, statusCode)
			}

			// assert expected body
			for _, key := range tt.expectedHeaders {
				if headers[key][0] == "" {
					t.Errorf("(%s): expected header %s", tt.givenURL, key)
				}
			}

		})
	}
}

func testHTTPFunc(w http.ResponseWriter, r *http.Request) {
	// check if we're receiving the expected route for success
	if r.URL.String() == "/test" {
		r.Header.Add("Content-Type", "text/html")
		r.Header.Add("Content-Length", "2")
		w.Write([]byte("OK"))
		return
	}

	// check if we're receiving the expected route for timeout
	if r.URL.String() == "/timeout" {
		time.Sleep(120 * time.Millisecond)
		w.Write([]byte("OK"))
		return
	}

	w.WriteHeader(http.StatusInternalServerError)
}
