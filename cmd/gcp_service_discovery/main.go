// gcp_service_discovery uses various GCP APIs to discover prometheus targets.
// Using metadata collected during discovery, gcp_service_discovery generates a
// JSON prometheus service discovery targets file, suitable for prometheus.
//
// gcp_service_discovery supports the following sources:
//  * App Engine Admin API - find AE Flex instances.
//  * Container Engine API - find clusters annotated for federation scraping.
//  * Generic HTTP(s) sources - download a pre-generated service discovery file.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/m-lab/go/flagx"
	"github.com/m-lab/go/prometheusx"
	"github.com/m-lab/go/rtx"

	"github.com/m-lab/gcp-service-discovery/aeflex"
	"github.com/m-lab/gcp-service-discovery/discovery"
	"github.com/m-lab/gcp-service-discovery/gke"
	"github.com/m-lab/gcp-service-discovery/web"
)

var (
	httpSources  = flagx.StringArray{}
	httpTargets  = flagx.StringArray{}
	project      = flag.String("project", "", "GCP project name.")
	aefTarget    = flag.String("aef-target", "", "Write targets configuration to given filename.")
	gkeTarget    = flag.String("gke-target", "", "Write targets configuration to given filename.")
	refresh      = flag.Duration("refresh", time.Minute, "Number of seconds between refreshing.")
	maxDiscovery = flag.Duration("max-discovery", 10*time.Minute, "Maximum time allowed for service discovery.")
)

func init() {
	flag.Var(&httpSources, "http-source", "Read configuration from HTTP(S) source.")
	flag.Var(&httpTargets, "http-target", "Write HTTP(S) source to the given filename.")

	// Override default because port is allocated from:
	// https://github.com/prometheus/prometheus/wiki/Default-port-allocations
	// --prometheusx.listen-address still works as intended.
	*prometheusx.ListenAddress = ":9373"
}

func main() {
	flag.Parse()
	manager := discovery.NewManager(*maxDiscovery)

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
		s := gke.MustNewService(*project)
		manager.Register(s, *gkeTarget)
	}
	for i := range httpSources {
		// Allocate a new client for downloading an HTTP(S) source.
		manager.Register(web.NewService(httpSources[i]), httpTargets[i])
	}

	// Verify that there is at least one source factory allocated before continuing.
	if manager.Count() == 0 {
		flag.Usage()
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Error: Specify at least one output target file.\n")
		os.Exit(1)
	}

	srv := prometheusx.MustServeMetrics()
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Run discovery forever.
	manager.Run(ctx, *refresh)
}
