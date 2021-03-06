// Package discovery manages and runs service discovery and saves target
// configuration files.
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dchest/safefile"
	"github.com/m-lab/go/rtx"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// discoveryDurationHist provides a histogram of the time to run service discovery.
	// The metric is labeled by the output filename, which is unique for each service.
	//
	// Provides metrics:
	//   gcp_manager_discovery_seconds_bucket
	//   gcp_manager_discovery_seconds_count
	//   gcp_manager_discovery_seconds_sum
	// Usage example:
	//   discoveryDurationHist.WithLabelValues("aeflex.Service").Observe(tDiff)
	discoveryDurationHist = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "gcp_manager_discovery_seconds",
			Help: "Histogram of service discovery run times.",
			Buckets: []float64{
				10, 15, 25, 40, 60,
				100, 150, 250, 400, 600,
				1000, 1500, 2500, 4000, 6000,
			},
		},
		[]string{"service"},
	)

	// discoveryTotal counts the total number of calls to service discovery. The
	// metric is labeled by the output filename and whether the discovery succeeded
	// or failed.
	//
	// Provides metrics:
	//   gcp_manager_discovery_total
	// Usage example:
	//   discoveryTotal.WithLabelValues("aeflex.Service", "success").Inc()
	discoveryTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gcp_manager_discovery_total",
			Help: "Number of discovery runs.",
		},
		[]string{"service", "status"},
	)
)

// Manager executes service discovery then serializes and writes targets to disk.
type Manager struct {
	services []Service
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
func (m *Manager) Register(s Service, output string) {
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
			// Label the discoveryDurationHist by service name. Labeling by service
			// provides better histogram fidelity.
			service := strings.TrimPrefix(fmt.Sprintf("%T", m.services[i]), "*")
			startTime := time.Now()
			disCtx, cancel := context.WithTimeout(ctx, m.Timeout)
			configs, err := m.services[i].Discover(disCtx)
			cancel()
			if err != nil {
				log.Printf("Error: %T: %s", m.services[i], err)
				discoveryTotal.WithLabelValues(service, "error-discovery").Inc()
				continue
			}
			discoveryDurationHist.WithLabelValues(service).Observe(time.Since(startTime).Seconds())
			err = writeConfigToFile(configs, m.output[i])
			if err != nil {
				log.Printf("Error: %s: %s", m.output[i], err)
				discoveryTotal.WithLabelValues(service, "error-write").Inc()
				continue
			}
			discoveryTotal.WithLabelValues(service, "success").Inc()
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
func writeConfigToFile(configs []StaticConfig, filename string) error {
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
