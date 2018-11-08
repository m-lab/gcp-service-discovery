// Package manager manages and runs service discovery and saves target
// configuration files.
package manager

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/dchest/safefile"
	"github.com/m-lab/gcp-service-discovery/discovery"
	"github.com/m-lab/go/rtx"
)

// Manager executes service discovery then serializes and writes targets to disk.
type Manager struct {
	services []discovery.Service
	output   []string
	Timeout  time.Duration
}

// NewManager creates a new manager instance. When calling Run, each registered
// service should take no longer than Timeout.
func NewManager(timeout time.Duration) *Manager {
	return &Manager{Timeout: timeout}
}

// Register accepts a new service. Future calls to Run will discover targets
// from this service and write them to the file named by output.
func (m *Manager) Register(s discovery.Service, output string) {
	m.services = append(m.services, s)
	m.output = append(m.output, output)
	return
}

// Count returns the number of services registered.
func (m *Manager) Count() int {
	return len(m.services)
}

// Run executes discovery for all registered services every interval period. Run
// returns once ctx is canceled.
func (m *Manager) Run(ctx context.Context, interval time.Duration) {
	tick := time.Tick(interval)
	for {
		// TODO: add waitgroup and run discovery in parallel.
		for i := range m.services {

			disCtx, cancel := context.WithTimeout(ctx, m.Timeout)
			configs, err := m.services[i].Discover(disCtx)
			cancel()
			if err != nil {
				log.Printf("Error: %T: %s", m.services[i], err)
				continue
			}
			err = writeConfigToFile(configs, m.output[i])
			if err != nil {
				log.Printf("Error: %s: %s", m.output[i], err)
			}
		}

		// Wait for ticker or exit when ctx is closed.
		select {
		case <-tick:
			continue
		case <-ctx.Done():
			return
		}
	}
}

// writeConfigToFile serializes and writes the given configs as JSON to the output filename.
func writeConfigToFile(configs []discovery.StaticConfig, filename string) error {
	// Convert to JSON.
	data, err := json.MarshalIndent(configs, "", "    ")
	rtx.Must(err, "Failed to marshal StaticConfig")

	// Write to file.
	err = safefile.WriteFile(filename, data, 0644)
	if err != nil {
		log.Printf("Failed to write %s: %s", filename, err)
		return err
	}
	return nil
}
