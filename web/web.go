// web implements service discovery for generic HTTP or HTTPS URLs.
package web

import (
	"io/ioutil"
	"net/http"
	"time"

	"github.com/m-lab/gcp-service-discovery/discovery"
)

// Factory stores information needed to create new Source instances.
type Factory struct {
	// The configuration source, as an http or https URL.
	srcUrl string

	// The output filename.
	dstFile string

	// timeout limits the total HTTP request time.
	timeout time.Duration
}

// NewSourceFactory returns a new Factory object that can create new Web Sources.
func NewSourceFactory(source, target string, timeout time.Duration) *Factory {
	return &Factory{
		srcUrl:  source,
		dstFile: target,
		timeout: timeout,
	}
}

// Create returns a discovery.Source initialized with an http.Client ready for
// Collection.
func (f *Factory) Create() (discovery.Source, error) {
	client := http.Client{
		Timeout: f.timeout,
	}
	source := &Source{
		factory: *f,
		client:  client,
	}
	return source, nil
}

// Source caches data collected from the web.
type Source struct {
	// factory is a copy of the original instance that created this source.
	factory Factory

	// client caches an http client for a web download.
	client http.Client

	// data is the result of the web download.
	data []byte
}

// Saves collected targets to the given filename.
func (source *Source) Save() error {
	// Save targets to output file.
	err := ioutil.WriteFile(source.factory.dstFile, source.data, 0644)
	if err != nil {
		return err
	}
	return nil
}

// Collect uses http.Client library to download a file from an HTTP(S) url.
func (source *Source) Collect() error {
	// TODO(p3, soltesz): right now these files are pretty small, but it's
	// unnecessary to download them again if they have not changed. Add an HTTP
	// header check to optionally skip download.

	// Download the srcUrl.
	resp, err := source.client.Get(source.factory.srcUrl)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Read and store the contents.
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	source.data = data
	return nil
}
