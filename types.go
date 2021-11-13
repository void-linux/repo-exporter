package main

// HTTPRequester set default operations for clients
type HTTPRequester interface {
	Fetch(url string) ([]byte, int, error)
	Head(url string) (map[string][]string, int, error)
}

type handler struct {
	client HTTPRequester
}
