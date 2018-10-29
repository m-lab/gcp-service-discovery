// Package aeflex implements service discovery for GCE VMs running in App Engine Flex.
package aeflex

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/dchest/safefile"
	"github.com/kr/pretty"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/m-lab/gcp-service-discovery/discovery"
	appengine "google.golang.org/api/appengine/v1"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	aefLabel             = "__aef_"
	aefLabelProject      = aefLabel + "project"
	aefLabelService      = aefLabel + "service"
	aefLabelVersion      = aefLabel + "version"
	aefLabelInstance     = aefLabel + "instance"
	aefLabelPublicProto  = aefLabel + "public_protocol"
	aefMaxTotalInstances = aefLabel + "max_total_instances"
	aefVmDebugEnabled    = aefLabel + "vm_debug_enabled"
)

var (
	defaultScopes = []string{appengine.CloudPlatformScope, appengine.AppengineAdminScope}
)

// Factory stores information needed to create new Source instances.
type Factory struct {
	// The GCP project id.
	project string

	// The output filename.
	filename string
}

var (
	// ServiceCount is the current number of AEFlex services.
	//
	// Provides metrics:
	//   gcp_aeflex_services
	// Example usage:
	//   ServiceCount.Set(count)
	ServiceCount = prometheus.NewGauge(
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
	VersionCount = prometheus.NewGaugeVec(
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
	//   VersionCount.WithLabelValues("etl-batch-parser", "true").Set(count)
	InstanceCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gcp_aeflex_instances",
			Help: "Total number of running serving instances.",
		},
		[]string{"service", "active"},
	)
)

func init() {
	prometheus.MustRegister(ServiceCount)
	prometheus.MustRegister(VersionCount)
	prometheus.MustRegister(InstanceCount)
}

// NewSourceFactory returns a new Factory object that can create new App Engine
// Flex Sources.
func NewSourceFactory(project, filename string) *Factory {
	return &Factory{
		project:  project,
		filename: filename,
	}
}

// Create returns a discovery.Source initialized with authenticated clients for
// App Engine Admin API, ready for Collection.
func (f *Factory) Create() (discovery.Source, error) {
	source := &Source{
		factory: *f,
	}
	var err error
	// Create a new authenticated HTTP client.
	source.client, err = google.DefaultClient(oauth2.NoContext, defaultScopes...)
	if err != nil {
		return nil, fmt.Errorf("Error setting up AppEngine client: %s", err)
	}

	// Create a new AppEngine service instance.
	source.apis, err = appengine.New(source.client)
	if err != nil {
		return nil, fmt.Errorf("Error setting up AppEngine client: %s", err)
	}

	return source, nil
}

// Source caches information collected from the App Engine Admin API during target discovery.
type Source struct {
	// factory is a copy of the original instance that created this source.
	factory Factory

	// client caches an http client authenticated for access to GCP APIs.
	client *http.Client

	// apis is the entry point to all AEFlex services.
	apis *appengine.APIService

	// targets collects found targets.
	targets []interface{}
}

// Save writes the content of the the collected set of targets.
func (source *Source) Save() error {
	// Convert to JSON.
	data, err := json.MarshalIndent(source.targets, "", "    ")
	if err != nil {
		log.Printf("Failed to Marshal JSON: %s", err)
		log.Printf("Pretty data: %s", pretty.Sprint(source.targets))
		return err
	}

	// Save targets to output file.
	log.Printf("Saving: %s", source.factory.filename)
	err = safefile.WriteFile(source.factory.filename, data, 0644)
	if err != nil {
		log.Printf("Failed to write %s: %s", source.factory.filename, err)
		return err
	}
	return nil
}

func jprint(v interface{}) {
	data, _ := json.MarshalIndent(v, "", "    ")
	fmt.Println(string(data))
}

// Collect contacts the App Engine Admin API to to check every service, and
// every serving version. Collect saves every AppEngine Flexible Environments
// VMs that is in a RUNNING and SERVING state.
func (source *Source) Collect() error {
	// Allocate space for the list of targets.
	source.targets = make([]interface{}, 0)

	s := source.apis.Apps.Services.List(source.factory.project)
	// List all services.
	services := 0
	err := s.Pages(nil, func(listSvc *appengine.ListServicesResponse) error {
		services += len(listSvc.Services)
		for _, service := range listSvc.Services {
			// List all versions of each service.
			v := source.apis.Apps.Services.Versions.List(source.factory.project, service.Id)
			versions := 0
			active := 0
			inactive := 0
			err := v.Pages(nil, func(listVer *appengine.ListVersionsResponse) error {
				versions += len(listVer.Versions)
				err := source.handleVersions(listVer, service, &active, &inactive)
				return err
			})
			log.Println(service.Name, "versions:", versions, "active:", active, "inactive:", inactive)
			VersionCount.WithLabelValues(service.Id).Set(float64(versions))
			InstanceCount.WithLabelValues(service.Id, "true").Set(float64(active))
			InstanceCount.WithLabelValues(service.Id, "false").Set(float64(inactive))
			if err != nil {
				return err
			}
		}
		return nil
	})
	ServiceCount.Set(float64(services))
	// TODO(p2, soltesz): collect and report metrics about number of API calls.
	// TODO(p2, soltesz): consider using goroutines to speed up collection.
	return err
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
func (source *Source) getLabels(
	service *appengine.Service, version *appengine.Version,
	instance *appengine.Instance) map[string]interface{} {
	var instances int64
	if version.AutomaticScaling != nil {
		instances = version.AutomaticScaling.MaxTotalInstances
	} else if version.ManualScaling != nil {
		instances = version.ManualScaling.Instances
	}
	labels := map[string]string{
		aefLabelProject:      source.factory.project,
		aefLabelService:      service.Id,
		aefLabelVersion:      version.Id,
		aefLabelInstance:     instance.Id,
		aefMaxTotalInstances: fmt.Sprintf("%d", instances),
		aefVmDebugEnabled:    fmt.Sprintf("%t", instance.VmDebugEnabled),
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
	targets := []string{fmt.Sprintf("%s:%s", instance.VmIp, port)}

	// Construct a record for the Prometheus file service discovery format.
	// https://prometheus.io/docs/operating/configuration/#<file_sd_config>
	values := map[string]interface{}{
		"labels":  labels,
		"targets": targets,
	}
	return values
}

// handles each version returned by an AppEngine Versions.List.
func (source *Source) handleVersions(
	listVer *appengine.ListVersionsResponse, service *appengine.Service,
	active *int, inactive *int) error {

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
		err := source.apis.Apps.Services.Versions.Instances.List(
			source.factory.project, service.Id, version.Id).Pages(
			nil, func(listInst *appengine.ListInstancesResponse) error {
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

// handles each instance returned by an AppEngine Versions.List.
func (source *Source) handleInstances(
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
