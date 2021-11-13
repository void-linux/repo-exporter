package main

import "context"

// HTTPRequester set default operations for clients
type HTTPRequester interface {
	Fetch(url string) ([]byte, int, error)
	Head(url string) (map[string][]string, int, error)
}

// Cacher set default operations for cache services
type Cacher interface {
	Get(ctx context.Context, key string, dst *[]byte) error
}

type handler struct {
	client  HTTPRequester
	storage Cacher
}
