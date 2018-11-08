// Package web implements service discovery for generic HTTP or HTTPS URLs.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/m-lab/gcp-service-discovery/discovery"
)

// Service defines the data collected from the web.
type Service struct {
	// The configuration source, as an http or https URL.
	srcURL string

	// client caches an http client for a web download.
	client http.Client

	// cache is temporary storage to determine whether to update.
	cache string
}

// NewService creates a new web service that requests the given srcURL.
// Requests must always complete within the given timeout.
func NewService(srcURL string, timeout time.Duration) *Service {
	s := &Service{
		srcURL: srcURL,
		client: http.Client{Timeout: timeout},
	}
	return s
}

var readAll = ioutil.ReadAll

// Discover retrieves the targets configuration.
func (srv *Service) Discover(ctx context.Context) ([]discovery.StaticConfig, error) {
	// TODO: add support for srv.cache using client.Head()
	resp, err := srv.client.Get(srv.srcURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Error: bad HTTP status code: %d", resp.StatusCode)
	}

	// Read and store the contents.
	data, err := readAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Verify the data can be parsed.
	var configs []discovery.StaticConfig
	err = json.Unmarshal(data, &configs)
	if err != nil {
		// TODO: add metrics counting these errors.
		return nil, err
	}
	return configs, nil
}
