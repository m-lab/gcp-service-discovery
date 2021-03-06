package aeflex

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"testing"

	"github.com/m-lab/gcp-service-discovery/aeflex/iface"
	"github.com/m-lab/gcp-service-discovery/discovery"
	"github.com/m-lab/go/prometheusx/promtest"
	appengine "google.golang.org/api/appengine/v1"
)

type fakeAppAPIImpl struct {
	services       []*appengine.Service
	versions       []*appengine.Version
	instances      []*appengine.Instance
	servicesError  error
	versionsError  error
	instancesError error
}

func (api *fakeAppAPIImpl) ServicesPages(
	ctx context.Context, f func(listVer *appengine.ListServicesResponse) error) error {
	if api.servicesError != nil {
		return api.servicesError
	}
	return f(&appengine.ListServicesResponse{Services: api.services})
}

func (api *fakeAppAPIImpl) VersionsPages(
	ctx context.Context, serviceID string,
	f func(listVer *appengine.ListVersionsResponse) error) error {
	if api.versionsError != nil {
		return api.versionsError
	}
	return f(&appengine.ListVersionsResponse{Versions: api.versions})
}

func (api *fakeAppAPIImpl) InstancesPages(
	ctx context.Context, serviceID, versionID string,
	f func(listInst *appengine.ListInstancesResponse) error) error {
	if api.instancesError != nil {
		return api.instancesError
	}
	return f(&appengine.ListInstancesResponse{Instances: api.instances})
}

func TestService_Discover(t *testing.T) {
	failureToListInstances := &fakeAppAPIImpl{
		services: []*appengine.Service{
			{
				Id: "fake-service-name",
				Split: &appengine.TrafficSplit{
					Allocations: map[string]float64{
						"20181027t210126-active": 1.0,
					},
				},
			},
		},
		versions: []*appengine.Version{
			// Regular version.
			{
				Id:            "20181027t210126-active",
				ServingStatus: "SERVING",
				CreateTime:    "2018-10-27T21:01:26Z",
			},
		},
		instancesError: fmt.Errorf("failing to list instances"),
	}
	successManualScalingUDPPort := &fakeAppAPIImpl{
		services: []*appengine.Service{
			{
				Id: "fake-service-name",
				Split: &appengine.TrafficSplit{
					Allocations: map[string]float64{
						"20181027t210126-active": 1.0,
					},
				},
			},
		},
		versions: []*appengine.Version{
			// Regular version.
			{
				Id:            "20181027t210126-active",
				ServingStatus: "SERVING",
				CreateTime:    "2018-10-27T21:01:26Z",
				Network: &appengine.Network{
					ForwardedPorts: []string{"9090/udp"},
				},
				ManualScaling: &appengine.ManualScaling{
					Instances: 1,
				},
			},
			// Serving without network.
			{
				Id:            "20181027t210126-inactive",
				ServingStatus: "SERVING",
				CreateTime:    "2018-10-27T21:01:26Z",
				Network: &appengine.Network{
					ForwardedPorts: []string{},
				},
				ManualScaling: &appengine.ManualScaling{
					Instances: 1,
				},
			},
			// Not serving.
			{
				Id:            "20181027t210126-inactive",
				ServingStatus: "STOPPED",
				CreateTime:    "2018-10-27T21:01:26Z",
			},
		},
		instances: []*appengine.Instance{
			// A regular instance.
			{
				Id:       "aef-etl--sidestream--parser-20181027t210126-x2qh",
				VmIp:     "192.168.0.2",
				VmStatus: "RUNNING",
			},
			// Missing VmIp.
			{
				Id:       "aef-etl--sidestream--parser-20181027t210126-x2qi",
				VmIp:     "",
				VmStatus: "RUNNING",
			},
			// VM is stopped.
			{
				Id:       "aef-etl--sidestream--parser-20181027t210126-x2qj",
				VmIp:     "192.168.0.2",
				VmStatus: "STOPPED",
			},
		},
	}
	successAutomaticScalingTCPAndUDP := &fakeAppAPIImpl{
		services: []*appengine.Service{
			{
				Id: "fake-service-name",
				Split: &appengine.TrafficSplit{
					Allocations: map[string]float64{
						"20181027t210126-active": 1.0,
					},
				},
			},
		},
		versions: []*appengine.Version{
			{
				Id:            "20181027t210126-active",
				ServingStatus: "SERVING",
				CreateTime:    "2018-10-27T21:01:26Z",
				// When not specifying the protocol, "both" is expected.
				Network: &appengine.Network{
					ForwardedPorts: []string{"9090"},
				},
				AutomaticScaling: &appengine.AutomaticScaling{
					MaxTotalInstances: 1,
				},
			},
		},
		instances: []*appengine.Instance{
			{
				Id:       "aef-etl--sidestream--parser-20181027t210126-x2qh",
				VmIp:     "192.168.0.2",
				VmStatus: "RUNNING",
			},
		},
	}
	successAutomaticScalingTCPPort := &fakeAppAPIImpl{
		services: []*appengine.Service{
			{
				Id: "fake-service-name",
				Split: &appengine.TrafficSplit{
					Allocations: map[string]float64{
						"20181027t210126-active": 1.0,
					},
				},
			},
		},
		versions: []*appengine.Version{
			{
				Id:            "20181027t210126-active",
				ServingStatus: "SERVING",
				CreateTime:    "2018-10-27T21:01:26Z",
				Network: &appengine.Network{
					ForwardedPorts: []string{"9090/tcp"},
				},
				AutomaticScaling: &appengine.AutomaticScaling{
					MaxTotalInstances: 1,
				},
			},
			// Missing network.
			{
				Id:            "20160101t210126-inactive",
				ServingStatus: "SERVING",
				CreateTime:    "2016-01-01T21:01:26Z",
			},
			// Includes bad format for CreateTime.
			{
				Id:            "20160101t210126-inactive",
				ServingStatus: "SERVING",
				CreateTime:    "2016-00-01T21:01:26Z", // Invalid month.
			},
		},
		instances: []*appengine.Instance{
			{
				Id:       "aef-etl--sidestream--parser-20181027t210126-x2qh",
				VmIp:     "192.168.0.2",
				VmStatus: "RUNNING",
			},
		},
	}

	tests := []struct {
		name    string
		project string
		targets []discovery.StaticConfig
		api     iface.AppAPI
		ctx     context.Context
		want    []discovery.StaticConfig
		wantErr bool
	}{
		{
			name:    "failure-to-list-instances",
			project: "fake-project",
			api:     failureToListInstances,
			wantErr: true,
		},
		{
			name:    "success-manual-scaling-udp-port",
			project: "fake-project",
			api:     successManualScalingUDPPort,
			want: []discovery.StaticConfig{
				{
					Targets: []string{"192.168.0.2:9090"},
					Labels: map[string]string{
						"__aef_public_protocol":     "udp",
						"__aef_project":             "fake-project",
						"__aef_service":             "fake-service-name",
						"__aef_version":             "20181027t210126-active",
						"__aef_instance":            "aef-etl--sidestream--parser-20181027t210126-x2qh",
						"__aef_max_total_instances": "1",
						"__aef_vm_debug_enabled":    "false",
					},
				},
			},
		},
		{
			name:    "success-automatic-scaling-tcp-and-udp",
			project: "fake-project",
			api:     successAutomaticScalingTCPAndUDP,
			want: []discovery.StaticConfig{
				{
					Targets: []string{"192.168.0.2:9090"},
					Labels: map[string]string{
						"__aef_public_protocol":     "both",
						"__aef_project":             "fake-project",
						"__aef_service":             "fake-service-name",
						"__aef_version":             "20181027t210126-active",
						"__aef_instance":            "aef-etl--sidestream--parser-20181027t210126-x2qh",
						"__aef_max_total_instances": "1",
						"__aef_vm_debug_enabled":    "false",
					},
				},
			},
		},
		{
			name:    "success-automatic-scaling-tcp-port",
			project: "fake-project",
			api:     successAutomaticScalingTCPPort,
			want: []discovery.StaticConfig{
				{
					Targets: []string{"192.168.0.2:9090"},
					Labels: map[string]string{
						"__aef_public_protocol":     "tcp",
						"__aef_project":             "fake-project",
						"__aef_service":             "fake-service-name",
						"__aef_version":             "20181027t210126-active",
						"__aef_instance":            "aef-etl--sidestream--parser-20181027t210126-x2qh",
						"__aef_max_total_instances": "1",
						"__aef_vm_debug_enabled":    "false",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &Service{
				project: tt.project,
				api:     tt.api,
				targets: tt.targets,
			}
			got, err := source.Discover(tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("Service.Discover() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Service.Discover() = %v, want %v", got, tt.want)
			}
			// Call Discover again, to verify it returns the same set of targets.
			got2, err := source.Discover(tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("Service.Discover() error = %v", err)
				return
			}
			if !reflect.DeepEqual(got, got2) {
				t.Errorf("Service.Discover() = %v, want %v", got, got2)
			}
		})
	}
}

func TestNewService(t *testing.T) {
	tests := []struct {
		name       string
		project    string
		fakeCreds  bool
		forceError bool
		wantErr    bool
	}{
		{
			name:    "success",
			project: "fake-prject",
		},
		{
			name:      "failure-auth",
			project:   "fake-prject",
			fakeCreds: true,
			wantErr:   true,
		},
		{
			name:       "failure-client",
			project:    "fake-prject",
			forceError: true,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fakeCreds {
				os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/tmp/not-a-real-file")
				defer func() {
					os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
				}()
			}
			if tt.forceError {
				origFunc := newAppengineClient
				newAppengineClient = func(client *http.Client) (*appengine.APIService, error) {
					return nil, fmt.Errorf("Failing to create client")
				}
				defer func() {
					newAppengineClient = origFunc
				}()
			}
			_, err := NewService(tt.project)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewService() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestMetrics(t *testing.T) {
	InstanceCount.WithLabelValues("x", "x")
	VersionCount.WithLabelValues("x")
	promtest.LintMetrics(t)
}
