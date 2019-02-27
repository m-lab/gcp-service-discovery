// Package aeflex implements service discovery for GCE VMs running in App Engine Flex.
package aeflex

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/m-lab/gcp-service-discovery/aeflex/iface"
	"github.com/m-lab/gcp-service-discovery/discovery"
	appengine "google.golang.org/api/appengine/v1"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	aefLabel             = "__aef_"
	aefLabelProject      = aefLabel + "project"
	aefLabelService      = aefLabel + "service"
	aefLabelVersion      = aefLabel + "version"
	aefLabelInstance     = aefLabel + "instance"
	aefLabelPublicProto  = aefLabel + "public_protocol"
	aefMaxTotalInstances = aefLabel + "max_total_instances"
	aefVMDebugEnabled    = aefLabel + "vm_debug_enabled"
)

var (
	defaultScopes = []string{appengine.CloudPlatformScope, appengine.AppengineAdminScope}

	// newAppengineClient allocates a new AppEngine client. The indirection facilitates testing.
	newAppengineClient = appengine.New
)

var (
	// ServiceCount is the current number of AEFlex services.
	//
	// Provides metrics:
	//   gcp_aeflex_services
	// Example usage:
	//   ServiceCount.Set(count)
	ServiceCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "gcp_aeflex_services",
			Help: "Number of active AEFlex services.",
		},
	)

	// VersionCount is the current number of available versions.
	//
	// Provides metrics:
	//   gcp_aeflex_versions{service="etl-batch-parser"}
	// Example usage:
	//   VersionCount.WithLabelValues("etl-batch-parser").Set(count)
	VersionCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gcp_aeflex_versions",
			Help: "Total number of versions.",
		},
		[]string{"service"},
	)

	// InstanceCount is the current number of serving instances.
	//
	// Provides metrics:
	//   gcp_aeflex_instances{service="etl-batch-parser", serving="true"}
	// Example usage:
	//   InstanceCount.WithLabelValues("etl-batch-parser", "true").Set(count)
	InstanceCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gcp_aeflex_instances",
			Help: "Total number of running serving instances.",
		},
		[]string{"service", "active"},
	)
)

// Service caches information collected from the App Engine Admin API during target discovery.
type Service struct {
	project string

	// targets collects found targets.
	targets []discovery.StaticConfig

	api iface.AppAPI
}

// NewService returns a Service initialized with authenticated clients for
// App Engine Admin API. The Service implements the discovery.Service interface.
func NewService(project string) (*Service, error) {
	source := &Service{
		project: project,
	}
	// Create a new authenticated HTTP client.
	client, err := google.DefaultClient(oauth2.NoContext, defaultScopes...)
	if err != nil {
		return nil, fmt.Errorf("Error setting up AppEngine client: %s", err)
	}
	// Create a new AppEngine service instance.
	aec, err := newAppengineClient(client)
	if err != nil {
		return nil, fmt.Errorf("Error setting up AppEngine client: %s", err)
	}
	source.api = iface.NewAppAPI(source.project, aec)
	return source, nil
}

// Discover contacts the App Engine Admin API to to check every service, and
// every serving version. Collect saves every AppEngine Flexible Environments
// VMs that is in a RUNNING and SERVING state.
func (source *Service) Discover(ctx context.Context) ([]discovery.StaticConfig, error) {
	// List all services.
	services := 0
	err := source.api.ServicesPages(
		ctx, func(listSvc *appengine.ListServicesResponse) error {
			services += len(listSvc.Services)
			for _, service := range listSvc.Services {
				err := source.discoverVersions(ctx, service)
				if err != nil {
					return err
				}
			}
			return nil
		})
	ServiceCount.Set(float64(services))
	if err != nil {
		return nil, err
	}
	// TODO(p2, soltesz): collect and report metrics about number of API calls.
	// TODO(p2, soltesz): consider using goroutines to speed up collection.
	return source.targets, nil
}

func (source *Service) discoverVersions(ctx context.Context, service *appengine.Service) error {
	// List all versions of each service.
	versions := 0
	active := 0
	inactive := 0
	err := source.api.VersionsPages(
		ctx, service.Id, func(listVer *appengine.ListVersionsResponse) error {
			versions += len(listVer.Versions)
			return source.handleVersions(ctx, listVer, service, &active, &inactive)
		})
	log.Println(service.Name, "versions:", versions, "active:", active, "inactive:", inactive)
	VersionCount.WithLabelValues(service.Id).Set(float64(versions))
	InstanceCount.WithLabelValues(service.Id, "true").Set(float64(active))
	InstanceCount.WithLabelValues(service.Id, "false").Set(float64(inactive))
	return err
}

// handleVersions checks every instance for each AppEngine version.
func (source *Service) handleVersions(
	ctx context.Context, listVer *appengine.ListVersionsResponse,
	service *appengine.Service, active *int, inactive *int) error {

	for _, version := range listVer.Versions {
		// We can only monitor instances that are running.
		if version.ServingStatus != "SERVING" {
			continue
		}
		// This version has "SERVING" instances. Can it receive traffic?
		// We don't want to monitor versions that will receive no traffic.
		// This can occur during incomplete deployments.
		_, shouldMonitor := service.Split.Allocations[version.Id]

		// List instances associated with each service version.
		err := source.api.InstancesPages(
			ctx, service.Id, version.Id, func(listInst *appengine.ListInstancesResponse) error {
				found, err := source.handleInstances(listInst, service, version, shouldMonitor)
				if shouldMonitor {
					*active += found
				} else {
					*inactive += found
				}
				return err
			})
		if err != nil {
			return err
		}
	}
	return nil
}

// handleInstances checks each instance in the given instance list and
// returns the total number of VMs found that *could* be monitored. However,
// when shouldMonitor is false, the Service targets list is not updated. This
// is helpful for situations where we want to count running instances without
// monitoring them.
func (source *Service) handleInstances(
	listInst *appengine.ListInstancesResponse, service *appengine.Service,
	version *appengine.Version, shouldMonitor bool) (int, error) {
	found := 0
	for _, instance := range listInst.Instances {
		// Only flex instances have a VmIp.
		if instance.VmIp == "" {
			// Ignore standard instances.
			continue
		}
		if instance.VmStatus != "RUNNING" {
			continue
		}
		// Ignore instances without networks or forwarded ports.
		if version.Network == nil {
			continue
		}
		if len(version.Network.ForwardedPorts) == 0 {
			continue
		}
		found++
		if shouldMonitor {
			source.targets = append(
				source.targets,
				source.getLabels(service, version, instance))
		}
	}
	return found, nil
}

// getLabels creates a target configuration for a prometheus service discovery
// file. The given service version should have a "SERVING" status, the instance
// should be in a "RUNNING" state and have at least one forwarded port.
//
// In serialized form, the label set look like:
//   {
//       "labels": {
//           "__aef_instance": "aef-etl--parser-20170418t195100-abcd",
//           "__aef_max_total_instances": "20",
//           "__aef_project": "mlab-sandbox",
//           "__aef_public_protocol": "tcp",
//           "__aef_service": "etl-parser",
//           "__aef_version": "20170418t195100",
//           "__aef_vm_debug_enabled": "true"
//       },
//       "targets": [
//           "104.196.220.184:9090"
//       ]
//   }
func (source *Service) getLabels(
	service *appengine.Service, version *appengine.Version,
	instance *appengine.Instance) discovery.StaticConfig {
	var instances int64
	if version.AutomaticScaling != nil {
		instances = version.AutomaticScaling.MaxTotalInstances
	} else if version.ManualScaling != nil {
		instances = version.ManualScaling.Instances
	}
	labels := map[string]string{
		aefLabelProject:      source.project,
		aefLabelService:      service.Id,
		aefLabelVersion:      version.Id,
		aefLabelInstance:     instance.Id,
		aefMaxTotalInstances: fmt.Sprintf("%d", instances),
		aefVMDebugEnabled:    fmt.Sprintf("%t", instance.VmDebugEnabled),
	}
	if strings.HasSuffix(version.Network.ForwardedPorts[0], "/udp") {
		labels[aefLabelPublicProto] = "udp"
	} else if strings.HasSuffix(version.Network.ForwardedPorts[0], "/tcp") {
		labels[aefLabelPublicProto] = "tcp"
	} else {
		labels[aefLabelPublicProto] = "both"
	}

	// TODO(dev): collect max resource sizes: cpu, memory, disk.
	//   Resources.Cpu
	//   Resources.DiskGb
	//   Resources.MemoryGb
	//   Resources.Volumes[0].Name
	//   Resources.Volumes[0].SizeGb
	//   Resources.Volumes[0].VolumeType

	// TODO: do we need to support multiple forwarded ports? How to choose?
	// Extract target address in the form of the VM public IP and forwarded port.
	re := regexp.MustCompile("([0-9]+)(/.*)")
	port := re.ReplaceAllString(version.Network.ForwardedPorts[0], "$1")

	values := discovery.StaticConfig{
		Targets: []string{fmt.Sprintf("%s:%s", instance.VmIp, port)},
		// Construct a record for the Prometheus file service discovery format.
		// https://prometheus.io/docs/operating/configuration/#<file_sd_config>
		Labels: labels,
	}
	return values
}
