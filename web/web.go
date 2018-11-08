// Package web implements service discovery for generic HTTP or HTTPS URLs.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/m-lab/gcp-service-discovery/discovery"
)

// Enable unit testing of readAll.
var readAll = ioutil.ReadAll

// Service defines the data collected from the web.
type Service struct {
	// srcURL is an HTTP(S) URL of the configuration source.
	srcURL string

	// client is used for each web download.
	client http.Client

	// TODO: add cache to determine whether to update.
}

// NewService creates a new web service to download the given srcURL. The srcURL
// should be an HTTP(S) URL to a file whose contents are a JSON formatted
// Prometheus static_config.
func NewService(srcURL string) *Service {
	s := &Service{
		srcURL: srcURL,
	}
	return s
}

// Discover downloads the source URL provided at service creation time.
//  registeredthe targets configuration.
func (srv *Service) Discover(ctx context.Context) ([]discovery.StaticConfig, error) {
	// TODO: add support for srv.cache using client.Head()
	req, err := http.NewRequest(http.MethodGet, srv.srcURL, nil)
	if err != nil {
		return nil, err
	}

	req = req.WithContext(ctx)
	resp, err := http.DefaultClient.Do(req)
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
