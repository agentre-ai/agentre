// Package llmurl builds upstream LLM API endpoint URLs from user-configured
// provider base URLs.
package llmurl

import (
	"errors"
	"net/url"
	"strings"
)

// Build joins baseURL and path while accepting a versioned base URL such as
// https://example.com/v1 together with a versioned request path such as
// /v1/messages. Exactly one shared /v1 segment is retained.
func Build(baseURL, path string) (*url.URL, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil, errors.New("provider base url is empty")
	}
	requestPath := "/" + strings.TrimLeft(strings.TrimSpace(path), "/")
	if strings.HasSuffix(base, "/v1") && (requestPath == "/v1" || strings.HasPrefix(requestPath, "/v1/")) {
		base = strings.TrimSuffix(base, "/v1")
	}
	full := base + requestPath
	u, err := url.Parse(full)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, errors.New("invalid upstream URL: " + full)
	}
	return u, nil
}
