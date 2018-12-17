package gke

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/m-lab/gcp-service-discovery/discovery"
	compute "google.golang.org/api/compute/v1"
	container "google.golang.org/api/container/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	apiv1 "k8s.io/client-go/pkg/api/v1"
	k8stesting "k8s.io/client-go/testing"
)

// fakeGKEImpl implements the gke/iface.GKE interface.
type fakeGKEImpl struct {
	zones            *compute.ZoneList
	clusters         *container.ListClustersResponse
	Interface        kubernetes.Interface
	zonePagesError   error
	clusterListError error
	kubeClientError  error
}

func (f *fakeGKEImpl) ZonePages(ctx context.Context, pageFunc func(zones *compute.ZoneList) error) error {
	if f.zonePagesError != nil {
		return f.zonePagesError
	}
	return pageFunc(f.zones)
}

func (f *fakeGKEImpl) ClusterList(ctx context.Context, zone string) (*container.ListClustersResponse, error) {
	if f.clusterListError != nil {
		return nil, f.clusterListError
	}
	return f.clusters, nil
}

func (f *fakeGKEImpl) GetKubeClient(c *container.Cluster) (kubernetes.Interface, error) {
	if f.kubeClientError != nil {
		return nil, f.kubeClientError
	}
	return f.Interface, nil
}

func TestMustNewService(t *testing.T) {
	_ = MustNewService("fake-project")
}

func TestService_Discover(t *testing.T) {
	zoneList := &compute.ZoneList{
		Items: []*compute.Zone{
			{Name: "us-central1-z"},
		},
	}
	clustersResponse := &container.ListClustersResponse{
		Clusters: []*container.Cluster{
			{
				Name: "fake-cluster",
				MasterAuth: &container.MasterAuth{
					ClusterCaCertificate: "",
				},
				Endpoint: "https://localhost:6443",
			},
		},
	}
	gkeSuccess := &fakeGKEImpl{
		zones:    zoneList,
		clusters: clustersResponse,
	}
	gkeWithZoneError := &fakeGKEImpl{
		zonePagesError: fmt.Errorf("Failed to list zones"),
		clusters:       clustersResponse,
	}
	gkeWithClusterError := &fakeGKEImpl{
		zones:            zoneList,
		clusterListError: fmt.Errorf("Failed to list clusters"),
	}
	gkeWithKubeError := &fakeGKEImpl{
		zones:           zoneList,
		clusters:        clustersResponse,
		kubeClientError: fmt.Errorf("Failed to get kube client"),
	}

	tests := []struct {
		name        string
		project     string
		gke         *fakeGKEImpl
		service     apiv1.Service
		ctx         context.Context
		want        []discovery.StaticConfig
		wantErr     bool
		wantKubeErr bool
	}{
		{
			name:    "success-target-with-external-ip",
			project: "fake-project",
			gke:     gkeSuccess,
			service: apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"gke-prometheus-federation/scrape": "true"},
				},
				Spec: apiv1.ServiceSpec{
					Ports:       []apiv1.ServicePort{{Port: 1122}},
					ExternalIPs: []string{"192.168.1.1"},
				},
			},
			want: []discovery.StaticConfig{
				{
					Targets: []string{"192.168.1.1:1122"},
					Labels:  map[string]string{"zone": "us-central1-z", "service": "", "cluster": "fake-cluster"},
				},
			},
		},
		{
			name:    "success-target-with-loadbalancer-ingress",
			project: "fake-project",
			gke:     gkeSuccess,
			service: apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"gke-prometheus-federation/scrape": "true"},
				},
				Spec: apiv1.ServiceSpec{
					Ports: []apiv1.ServicePort{{Port: 1122}},
				},
				Status: apiv1.ServiceStatus{
					LoadBalancer: apiv1.LoadBalancerStatus{
						Ingress: []apiv1.LoadBalancerIngress{{IP: "192.168.1.1"}},
					},
				},
			},
			want: []discovery.StaticConfig{
				{
					Targets: []string{"192.168.1.1:1122"},
					Labels:  map[string]string{"zone": "us-central1-z", "service": "", "cluster": "fake-cluster"},
				},
			},
		},
		{
			name:    "success-target-empty",
			project: "fake-project",
			gke:     gkeSuccess,
			service: apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"gke-prometheus-federation/scrape": "true"},
				},
			},
			want: []discovery.StaticConfig{},
		},
		{
			name:    "success-skip-false-label",
			project: "fake-project",
			gke:     gkeSuccess,
			service: apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"gke-prometheus-federation/scrape": "false"},
				},
			},
			want: []discovery.StaticConfig{},
		},
		{
			name:    "failure-using-kube-client",
			project: "fake-project",
			gke:     gkeSuccess,
			service: apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"gke-prometheus-federation/scrape": "true"},
				},
				Spec: apiv1.ServiceSpec{
					Ports:       []apiv1.ServicePort{{Port: 1122}},
					ExternalIPs: []string{"192.168.1.1"},
				},
			},
			wantKubeErr: true,
			wantErr:     true,
		},
		{
			name:    "failure-zone-list",
			project: "fake-project",
			gke:     gkeWithZoneError,
			wantErr: true,
		},
		{
			name:    "failure-cluster-list",
			project: "fake-project",
			gke:     gkeWithClusterError,
			wantErr: true,
		},
		{
			name:    "failure-get-kube-client",
			project: "fake-project",
			gke:     gkeWithKubeError,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := fake.NewSimpleClientset()
			i.Fake.PrependReactor("list", "services", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
				if tt.wantKubeErr {
					return true, nil, fmt.Errorf("Fake error")
				}
				return true, &apiv1.ServiceList{Items: []apiv1.Service{tt.service}}, nil
			})
			tt.gke.Interface = i
			s := &Service{
				project: tt.project,
				gke:     tt.gke,
			}
			got, err := s.Discover(tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("Service.Discover() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Service.Discover() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getKubeClient(t *testing.T) {
	tests := []struct {
		name    string
		c       *container.Cluster
		want    *kubernetes.Clientset
		wantErr bool
	}{
		{
			name: "success",
			c: &container.Cluster{
				MasterAuth: &container.MasterAuth{
					ClusterCaCertificate: "",
				},
				Endpoint: "https://localhost:6443",
			},
		},
		{
			name: "failure-parsing-certificate",
			c: &container.Cluster{
				MasterAuth: &container.MasterAuth{
					ClusterCaCertificate: ":::::invalid:::::",
				},
				Endpoint: "https://localhost:6443",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := getKubeClient(tt.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("gkeClusterToKubeClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}
