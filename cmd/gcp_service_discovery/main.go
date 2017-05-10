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
	"github.com/m-lab/gcp-service-discovery/gke"
	"github.com/m-lab/gcp-service-discovery/targetsource"
)

var (
	project   = flag.String("project", "", "GCP project name.")
	aefTarget = flag.String("aef-target", "", "Write targets configuration to given filename.")
	gkeTarget = flag.String("gke-target", "", "Write targets configuration to given filename.")
	refresh   = flag.Duration("refresh", time.Minute, "Number of seconds between refreshing.")
)

func main() {
	flag.Parse()
	var start time.Time
	generators := []targetsource.Generator{}

	if *aefTarget != "" {
		// Allocate a new authenticated client for App Engine API.
		generators = append(generators, aeflex.NewAEFlexSource(*project, *aefTarget))
	}
	if *gkeTarget != "" {
		// Allocate a new authenticated client for GCE & GKE API.
		generators = append(generators, gke.NewGKESource(*project, *gkeTarget))
	}

	if *project == "" {
		flag.Usage()
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Error: Specify a GCP project.\n")
		os.Exit(1)
	}

	if len(generators) == 0 {
		flag.Usage()
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Error: Specify at least one output target file.\n")
		os.Exit(1)
	}

	// TODO(dev): create and loop over an array of TargetSource instances for
	// aeflex, gke, and web.

	// Only sleep as long as we need to, before starting a new iteration.
	for ; ; time.Sleep(*refresh - time.Since(start)) {
		start = time.Now()
		log.Printf("Starting a new round at: %s", start)

		for i := range generators {
			// Allocate a new authenticated client.
			target, err := generators[i].Client()
			if err != nil {
				log.Printf("Failed to get client from generator: %s", err)
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
