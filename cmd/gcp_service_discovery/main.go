// gcp_service_discovery uses various GCP APIs to discover prometheus targets.
// Using metadata collected during discovery, gcp_service_discovery generates a
// JSON prometheus service discovery targets file, suitable for prometheus.
//
// gcp_service_discovery supports the following sources:
//  * App Engine Admin API - find AE Flex instances.
//  * Container Engine API - find clusters annotated for federation scraping.
//
// TODO:
//  * Generic HTTP(s) sources - download a pre-generated service discovery file.

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/m-lab/gcp-service-discovery/aeflex"
	"github.com/m-lab/gcp-service-discovery/discovery"
	"github.com/m-lab/gcp-service-discovery/gke"
)

var (
	project    = flag.String("project", "", "GCP project name.")
	aefTarget  = flag.String("aef-target", "", "Write targets configuration to given filename.")
	gkeTarget  = flag.String("gke-target", "", "Write targets configuration to given filename.")
	httpTarget = flag.String("http-target", "", "Write targets configuration to given filename.")
	refresh    = flag.Duration("refresh", time.Minute, "Number of seconds between refreshing.")
)

func main() {
	flag.Parse()
	var start time.Time
	factories := []discovery.Factory{}

	if *aefTarget != "" {
		// Allocate a new authenticated client for App Engine API.
		factories = append(factories, aeflex.NewSourceFactory(*project, *aefTarget))
	}
	if *gkeTarget != "" {
		// Allocate a new authenticated client for GCE & GKE API.
		factories = append(factories, gke.NewSourceFactory(*project, *gkeTarget))
	}
	if *httpTarget != "" {
		fmt.Fprintf(os.Stderr, "Error: http targets are not yet supported.\n")
		os.Exit(1)
	}

	if *project == "" {
		flag.Usage()
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Error: Specify a GCP project.\n")
		os.Exit(1)
	}

	if len(factories) == 0 {
		flag.Usage()
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Error: Specify at least one output target file.\n")
		os.Exit(1)
	}

	// Only sleep as long as we need to, before starting a new iteration.
	for ; ; time.Sleep(*refresh - time.Since(start)) {
		start = time.Now()
		log.Printf("Starting a new round at: %s", start)

		for i := range factories {
			// Allocate a new authenticated client.
			target, err := factories[i].Create()
			if err != nil {
				log.Printf("Failed to get client from factory: %s", err)
				continue
			}

			// Collect targets and labels.
			err = target.Collect()
			if err != nil {
				log.Printf("Failed to Collect targets: %s", err)
				continue
			}

			// Write the targets to a file.
			err = target.Save()
			if err != nil {
				log.Printf("Failed to save: %s", err)
				continue
			}
		}

		log.Printf("Finished round after: %s", time.Since(start))
	}
	return
}
