// gke implements service discovery for GKE clusters with k8s services annotated
// for federation scraping.
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

func Test_checkCluster(t *testing.T) {
	tests := []struct {
		name        string
		Interface   kubernetes.Interface
		service     apiv1.Service
		zoneName    string
		clusterName string
		want        []discovery.StaticConfig
		wantErr     bool
	}{
		{
			name: "successful-external-ip",
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
					Labels:  map[string]string{"zone": "", "service": "", "cluster": ""},
				},
			},
		},
		{
			name: "successful-loadbalancer",
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
					Labels:  map[string]string{"zone": "", "service": "", "cluster": ""},
				},
			},
		},
		{
			name: "failure-skipping-annotation",
			service: apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"gke-prometheus-federation/scrape": "false"},
				},
			},
			want: []discovery.StaticConfig{},
		},
		{
			name: "failure-empty-target",
			service: apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"gke-prometheus-federation/scrape": "true"},
				},
			},
			want: []discovery.StaticConfig{},
		},
		{
			name:    "failure-service-list-error",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := fake.NewSimpleClientset()
			i.Fake.PrependReactor("list", "services", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
				if tt.wantErr {
					return true, nil, fmt.Errorf("Fake error")
				}
				return true, &apiv1.ServiceList{Items: []apiv1.Service{tt.service}}, nil
			})
			got, err := checkCluster(i, tt.zoneName, tt.clusterName)
			if (err != nil) != tt.wantErr {
				t.Errorf("kubeOps.checkCluster() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("kubeOps.checkCluster() = %#v, want %#v", got, tt.want)
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

func TestNewServiceMust(t *testing.T) {
	tests := []struct {
		name    string
		project string
		want    *Service
	}{
		{
			name:    "success",
			project: "fake-project",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = NewServiceMust(tt.project)
		})
	}
}

type fakeGKEImpl struct {
	project   string
	zones     *compute.ZoneList
	clusters  *container.ListClustersResponse
	Interface kubernetes.Interface
}

func (f *fakeGKEImpl) ZonePages(ctx context.Context, pageFunc func(zones *compute.ZoneList) error) error {
	return pageFunc(f.zones)
}

// ClusterList ...
func (f *fakeGKEImpl) ClusterList(ctx context.Context, zone string) (*container.ListClustersResponse, error) {
	return f.clusters, nil
}

func (f *fakeGKEImpl) GetKubeClient(c *container.Cluster) (kubernetes.Interface, error) {
	return f.Interface, nil
}

func TestService_Discover(t *testing.T) {
	fgke := &fakeGKEImpl{
		project: "fake-project",
		zones: &compute.ZoneList{
			Items: []*compute.Zone{
				{Name: "us-central1-z"},
			},
		},
		clusters: &container.ListClustersResponse{
			Clusters: []*container.Cluster{
				&container.Cluster{
					Name: "fake-cluster",
					MasterAuth: &container.MasterAuth{
						ClusterCaCertificate: "",
					},
					Endpoint: "https://localhost:6443",
				},
			},
		},
	}

	tests := []struct {
		name    string
		project string
		gke     *fakeGKEImpl
		service apiv1.Service
		ctx     context.Context
		want    []discovery.StaticConfig
		wantErr bool
	}{
		{
			name:    "success",
			project: "fake-project",
			gke:     fgke,
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := fake.NewSimpleClientset()
			i.Fake.PrependReactor("list", "services", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
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
