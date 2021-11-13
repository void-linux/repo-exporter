package requests

import (
	"net/http"
	"testing"
	"time"
)

func TestGetIntegration(t *testing.T) {
	// creating HTTPRequestHandler for tests
	handler := NewHTTPRequestHandler(10 * time.Second)

	var tests = []struct {
		name                string
		givenURL            string
		expectedMinBodySize int
		expectedStatusCode  int
		expectedErr         error
	}{
		{
			"Integration test for GET HTTP request to alpha.de.repo.voidlinux.org",
			"https://alpha.de.repo.voidlinux.org/current/x86_64-repodata",
			1000000,
			http.StatusOK,
			nil,
		},
		{
			"Integration test for GET HTTP request to alpha.de.repo.voidlinux.org",
			"https://alpha.de.repo.voidlinux.org/current/randomdir/x86_64-repodata",
			0,
			http.StatusNotFound,
			nil,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			body, statusCode, err := handler.Fetch(tt.givenURL)
			if err != tt.expectedErr {
				t.Errorf("(%s): expected err %s, actual %s", tt.givenURL, tt.expectedErr, err)
			}

			if len(body) < tt.expectedMinBodySize {
				t.Errorf("(%s): expected body greater than %d, current body len %d", tt.givenURL, tt.expectedMinBodySize, len(body))
			}

			if statusCode != tt.expectedStatusCode {
				t.Errorf("(%s): expected status code %d, actual %d", tt.givenURL, tt.expectedStatusCode, statusCode)
			}

		})
	}
}

func TestHeadIntegration(t *testing.T) {
	// creating HTTPRequestHandler for tests
	handler := NewHTTPRequestHandler(10 * time.Second)

	var tests = []struct {
		name               string
		givenURL           string
		expectedHeaders    []string
		expectedStatusCode int
		expectedErr        error
	}{
		{
			name:     "Integration test for HEAD HTTP request to alpha.de.repo.voidlinux.org",
			givenURL: "https://alpha.de.repo.voidlinux.org/current/x86_64-repodata",
			expectedHeaders: []string{
				"Content-Type",
				"Content-Length",
				"Etag",
				"Date",
				"Server",
				"Accept-Ranges",
				"Last-Modified",
			},
			expectedStatusCode: http.StatusOK,
			expectedErr:        nil,
		},
		{
			name:     "Integration test for HEAD HTTP request to repo-us.voidlinux.org",
			givenURL: "https://repo-us.voidlinux.org/current/x86_64-repodata",
			expectedHeaders: []string{
				"Content-Type",
				"Content-Length",
				"Etag",
				"Date",
				"Server",
				"Accept-Ranges",
				"Last-Modified",
			},
			expectedStatusCode: http.StatusOK,
			expectedErr:        nil,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			headers, statusCode, err := handler.Head(tt.givenURL)
			if err != tt.expectedErr {
				t.Errorf("(%s): expected err %s, actual %s", tt.givenURL, tt.expectedErr, err)
			}

			for _, key := range tt.expectedHeaders {
				if headers[key][0] == "" {
					t.Errorf("(%s): expected header %s", tt.givenURL, key)
				}
			}

			if statusCode != tt.expectedStatusCode {
				t.Errorf("(%s): expected status code %d, actual %d", tt.givenURL, tt.expectedStatusCode, statusCode)
			}

		})
	}
}
