// gcp_service_discovery uses various GCP APIs to discover prometheus targets.
// Using metadata collected during discovery, gcp_service_discovery generates a
// JSON prometheus service discovery targets file, suitable for prometheus.
//
// gcp_service_discovery supports the following sources:
//  * App Engine Admin API - find AE Flex instances.
//
// TODO:
//  * Generic HTTP(s) sources - download a pre-generated service discovery file.
//  * Container Engine API - find clusters with annotated services or federation scraping.

package main

import (
	"flag"
	"log"
	"time"

	"github.com/m-lab/gcp-service-discovery/aeflex"
)

var (
	project   = flag.String("project", "", "GCP project name.")
	aefTarget = flag.String("aef-target", "aef-target.json", "Write targets configuration to given filename.")
	refresh   = flag.Duration("refresh", time.Minute, "Number of seconds between refreshing.")
)

// TargetSource defines the interface for collecting targets from various
// services. New services should implement this interface.
type TargetSource interface {
	// Collect retrieves all targets from a source.
	Collect() error

	// Save writes the targets to the named file.
	Save(name string) error
}

func main() {
	flag.Parse()
	var start time.Time

	// TODO(dev): create and loop over an array of TargetSource instances for
	// aeflex, gke, and web.

	// Only sleep as long as we need to, before starting a new iteration.
	for ; ; time.Sleep(*refresh - time.Since(start)) {
		start = time.Now()
		log.Printf("Starting a new round at: %s", start)

		// Allocate a new authenticated client for App Engine API.
		client, err := aeflex.NewAEFlexSource(*project)
		if err != nil {
			log.Printf("Failed to get authenticated client: %s", err)
			continue
		}

		// Collect AE Flex targets and labels.
		err = client.Collect()
		if err != nil {
			log.Printf("Failed to Collect targets: %s", err)
			continue
		}

		// Write the targets to a file.
		err = client.Save(*aefTarget)
		if err != nil {
			log.Printf("Failed to save to %s: %s", *aefTarget, err)
			continue
		}

		log.Printf("Finished round after: %s", time.Since(start))
	}
	return
}
