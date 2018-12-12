// Package gke implements service discovery for GKE clusters with k8s services annotated
// for federation collection.
package gke

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"

	"github.com/m-lab/go/rtx"

	"github.com/m-lab/gcp-service-discovery/gke/iface"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	compute "google.golang.org/api/compute/v1"
	container "google.golang.org/api/container/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	typesv1 "k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"github.com/m-lab/gcp-service-discovery/discovery"
)

var (
	// NOTE: As of 2017-05, there is no more specific scope for accessing the
	// Container Engine API. The compute-platform scope is quite permissive.
	gkeScopes = []string{compute.CloudPlatformScope}
)

// Service contains necessary data for service discovery in GKE.
type Service struct {
	// The GCP project id.
	project string

	// client caches an http client authenticated for access to GCP APIs.
	client *http.Client

	gke iface.GKE

	// cache is temporary storage to determine whether to update.
	cache string
}

// NewServiceMust creates a new GKE service discovery instance. The function
// exits if an error occurs during setup.
func NewServiceMust(project string) *Service {
	var err error

	s := &Service{
		project: project,
	}
	// Create a new authenticated HTTP client.
	s.client, err = google.DefaultClient(oauth2.NoContext, gkeScopes...)
	rtx.Must(err, "Error setting up default client")

	// Create a new Compute service instance.
	computeService, err := compute.New(s.client)
	rtx.Must(err, "Error setting up a Compute API client")

	// Create a new Container Engine service object.
	containerService, err := container.New(s.client)
	rtx.Must(err, "Error setting up a Container API client")

	s.gke = iface.NewGKE(project, computeService, containerService, getKubeClient)
	return s
}

// Discover uses the Compute Engine, Container Engine, and Kubernetes APIs to
// check every GCE zone for Container Engine (gke) clusters, and checks each
// cluster for services annotated for federated scraping.
//
// Collect returns every gke cluster with a k8s service annotation that equals:
//    gke-prometheus-federation/scrape: true
func (s *Service) Discover(ctx context.Context) ([]discovery.StaticConfig, error) {
	targets := []discovery.StaticConfig{}

	// Get all zones in a project.
	zones, err := s.getZoneList(ctx)
	if err != nil {
		return nil, err
	}
	for _, zone := range zones {
		t, err := s.findTargetsFromZone(ctx, zone)
		if err != nil {
			return nil, err
		}
		targets = append(targets, t...)
	}
	return targets, err
}

func (s *Service) getZoneList(ctx context.Context) ([]string, error) {
	zoneNames := []string{}
	err := s.gke.ZonePages(ctx, func(zones *compute.ZoneList) error {
		for _, zone := range zones.Items {
			zoneNames = append(zoneNames, zone.Name)
		}
		return nil
	})
	return zoneNames, err
}

func (s *Service) findTargetsFromZone(ctx context.Context, zoneName string) ([]discovery.StaticConfig, error) {
	targets := []discovery.StaticConfig{}

	// Get all clusters in a zone.
	clusters, err := s.gke.ClusterList(ctx, zoneName)
	if err != nil {
		return nil, err
	}

	// Look for targets from every cluster.
	for _, cluster := range clusters.Clusters {
		// Use information from the GKE cluster to create a k8s API client.

		// TODO: consider using new interface, like getKubeClient(cluster *container.Cluster)
		kubeClient, err := s.gke.GetKubeClient(cluster)
		if err != nil {
			return nil, err
		}
		t, err := checkCluster(kubeClient, zoneName, cluster.Name)
		if err != nil {
			return nil, err
		}
		targets = append(targets, t...)
	}
	return targets, nil
}

// checkCluster uses the kubernetes API to search for GKE targets.
func checkCluster(k kubernetes.Interface, zoneName, clusterName string) ([]discovery.StaticConfig, error) {
	configs := []discovery.StaticConfig{}

	// List all services in the k8s cluster.
	services, err := k.CoreV1().Services("").List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	log.Printf("%s - %s - There are %d services in the cluster\n",
		zoneName, clusterName, len(services.Items))

	// Check each service, and collect targets that have matching annotations.
	for _, service := range services.Items {
		// Federation scraping is opt-in only.
		if service.ObjectMeta.Annotations["gke-prometheus-federation/scrape"] != "true" {
			continue
		}
		target := findTargetAndLabels(zoneName, clusterName, service)
		if target != nil {
			configs = append(configs, *target)
		}
	}
	return configs, nil
}

// findTargetAndLabels identifies the first target (first port) per service and
// returns a target configuration for use with Prometheus file service discovery.
func findTargetAndLabels(zoneName, clusterName string, service typesv1.Service) *discovery.StaticConfig {
	var target string

	if len(service.Spec.ExternalIPs) > 0 && len(service.Spec.Ports) > 0 {
		// Static IP addresses appear in the Service.Spec.
		// ---
		//    Spec: v1.ServiceSpec{
		//        ExternalIPs:              {"104.196.164.214"},
		//    },
		target = fmt.Sprintf("%s:%d",
			service.Spec.ExternalIPs[0],
			service.Spec.Ports[0].Port)
	} else if len(service.Status.LoadBalancer.Ingress) > 0 {
		// Ephemeral IP addresses appear in the Service.Status field.
		// ---
		//    Status: v1.ServiceStatus{
		//        LoadBalancer: v1.LoadBalancerStatus{
		//            Ingress: {
		//                {IP:"104.197.220.28", Hostname:""},
		//            },
		//        },
		//    },
		target = fmt.Sprintf("%s:%d",
			service.Status.LoadBalancer.Ingress[0].IP,
			service.Spec.Ports[0].Port)
	}
	if target == "" {
		return nil
	}
	return &discovery.StaticConfig{
		Targets: []string{target},
		Labels: map[string]string{
			"service": service.ObjectMeta.Name,
			"cluster": clusterName,
			"zone":    zoneName,
		},
	}
}

// getKubeClient converts a container engine API Cluster object into
// a kubernetes API client instance.
func getKubeClient(c *container.Cluster) (kubernetes.Interface, error) {
	// The cluster CA certificate is base64 encoded from the GKE API.
	rawCaCert, err := base64.URLEncoding.DecodeString(c.MasterAuth.ClusterCaCertificate)
	if err != nil {
		return nil, err
	}

	// This is a low-level structure normally created from parsing a kubeconfig
	// file.  Since we know all values we can create the client object directly.
	//
	// The cluster and user names serve only to define a context that
	// associates login credentials with a specific cluster.
	clusterClient := api.Config{
		Clusters: map[string]*api.Cluster{
			// Define the cluster address and CA Certificate.
			"cluster": {
				Server:                   fmt.Sprintf("https://%s", c.Endpoint),
				InsecureSkipTLSVerify:    false, // Require a valid CA Certificate.
				CertificateAuthorityData: rawCaCert,
			},
		},
		AuthInfos: map[string]*api.AuthInfo{
			// Define the user credentials for access to the API.
			"user": {
				Username: c.MasterAuth.Username,
				Password: c.MasterAuth.Password,
			},
		},
		Contexts: map[string]*api.Context{
			// Define a context that refers to the above cluster and user.
			"cluster-user": {
				Cluster:  "cluster",
				AuthInfo: "user",
			},
		},
		// Use the above context.
		CurrentContext: "cluster-user",
	}

	// Construct a "direct client" using the auth above to contact the API server.
	defClient := clientcmd.NewDefaultClientConfig(
		clusterClient,
		&clientcmd.ConfigOverrides{
			ClusterInfo: api.Cluster{Server: ""},
		})
	restConfig, err := defClient.ClientConfig()
	if err != nil {
		return nil, err
	}

	// Creates the k8s clientset.
	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	return kubeClient, nil
}
