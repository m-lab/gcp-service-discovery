package web

import (
	"github.com/m-lab/gcp-service-discovery/discovery"
	"io/ioutil"
	"net/http"
	"time"
)

// Factory stores information needed to create new Source instances.
type Factory struct {
	// The configuration source, as an http or https URL.
	srcUrl string

	// The output filename.
	dstFile string
}

// NewSourceFactory returns a new Factory object that can create new Web Sources.
func NewSourceFactory(source, target string) *Factory {
	return &Factory{
		srcUrl:  source,
		dstFile: target,
	}
}

// Create returns a discovery.Source initialized with an http.Client ready for
// Collection.
func (f *Factory) Create() (discovery.Source, error) {
	client := http.Client{
		Timeout: time.Minute,
	}
	source := &Source{
		factory: *f,
		client:  client,
	}

	return source, nil
}

// Source caches information collected from the GCE, GKE, and K8S APIs during
// target discovery.
type Source struct {
	factory Factory
	client  http.Client
	data    []byte
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
	resp, err := source.client.Get(source.factory.srcUrl)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	source.data = data
	return nil
}
