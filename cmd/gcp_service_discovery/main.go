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
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/m-lab/go/rtx"

	flag "github.com/spf13/pflag"

	"github.com/m-lab/gcp-service-discovery/aeflex"
	"github.com/m-lab/gcp-service-discovery/discovery"
	"github.com/m-lab/gcp-service-discovery/gke"
	"github.com/m-lab/gcp-service-discovery/web"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpSources = []string{}
	httpTargets = []string{}
	project     = flag.String("project", "", "GCP project name.")
	aefTarget   = flag.String("aef-target", "", "Write targets configuration to given filename.")
	gkeTarget   = flag.String("gke-target", "", "Write targets configuration to given filename.")
	refresh     = flag.Duration("refresh", time.Minute, "Number of seconds between refreshing.")
)

func init() {
	flag.StringArrayVar(&httpSources, "http-source", nil, "Read configuration from HTTP(S) source.")
	flag.StringArrayVar(&httpTargets, "http-target", nil, "Write HTTP(S) source to the given filename.")
}

func main() {
	flag.Parse()
	var start time.Time
	manager := discovery.NewManager(*refresh)
	factories := []discovery.Factory{}

	if len(httpSources) != len(httpTargets) {
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Error: http sources and targets must match.\n")
		os.Exit(1)
	}
	if (*aefTarget != "" && *project == "") || (*gkeTarget != "" && *project == "") {
		flag.Usage()
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Error: Specify a GCP project.\n")
		os.Exit(1)
	}

	// TODO(p2, soltesz): add timeout parameter to aeflex and gke NewSourceFactory.

	// Allocate every relevant source factories.
	if *aefTarget != "" {
		// Allocate a new authenticated client for App Engine API.
		s, err := aeflex.NewService(*project)
		rtx.Must(err, "Failed to create an aeflex.Service for project: %q", *project)
		manager.Register(s, *aefTarget)
	}
	if *gkeTarget != "" {
		// Allocate a new authenticated client for GCE & GKE API.
		factories = append(factories, gke.NewSourceFactory(*project, *gkeTarget))
	}
	for i := range httpSources {
		// Allocate a new client for downloading an HTTP(S) source.
		manager.Register(web.NewService(httpSources[i]), httpTargets[i])
	}

	// Verify that there is at least one source factory allocated before continuing.
	if len(factories) == 0 && manager.Count() == 0 {
		flag.Usage()
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Error: Specify at least one output target file.\n")
		os.Exit(1)
	}

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		// Port allocated from:
		// https://github.com/prometheus/prometheus/wiki/Default-port-allocations
		log.Fatal(http.ListenAndServe(":9373", nil))
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		// Run discovery forever.
		manager.Run(ctx, *refresh)
	}()

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
}
