// gcp_service_discovery contacts the AppEngine Admin API to finds all
// AppEngine Flexible Environments VMs in a RUNNING and SERVING state.
// gcp_service_discovery generates a JSON targets file based on the VM
// metadata suitable for input to prometheus.
//
// TODO:
//   * run continuously as a daemon.
//   * bundled process in a docker container and deploy with the prometheus pod.
//   * generalize to read from generic web sources.

package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"time"

	"github.com/kr/pretty"
	"github.com/m-lab/gcp-service-discovery/aeflex"
)

var (
	project  = flag.String("project", "", "GCP project name.")
	filename = flag.String("output", "targets.json", "Write targets configuration to given filename.")
	refresh  = flag.Duration("refresh", time.Minute, "Number of seconds between refreshing.")
)

// TargetSource defines the interface for collecting targets from various
// services. New services should implement this interface.
type TargetSource interface {
	Collect() error
	Targets() []interface{}
}

func main() {
	flag.Parse()
	var start time.Time

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

		// Convert to JSON.
		data, err := json.MarshalIndent(client.Targets(), "", "    ")
		if err != nil {
			log.Printf("Failed to Marshal JSON: %s", err)
			log.Printf("Pretty data: %s", pretty.Sprint(client.Targets))
			continue
		}

		// Save targets to output file.
		err = ioutil.WriteFile(*filename, data, 0644)
		if err != nil {
			log.Printf("Failed to write %s: %s", *filename, err)
			continue
		}
		log.Printf("Finished round after: %s", time.Since(start))
	}
	return
}
