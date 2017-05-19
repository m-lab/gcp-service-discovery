// gke implements service discovery for GKE clusters with k8s services annotated
// for federation scraping.
package gke

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/dchest/safefile"
	"github.com/kr/pretty"

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

// Factory stores information needed to create new Source instances.
type Factory struct {
	// The GCP project id.
	project string

	// The output filename.
	filename string
}

// NewSourceFactory returns a new Factory object that can create new GKE Sources.
func NewSourceFactory(project, filename string) *Factory {
	return &Factory{
		project:  project,
		filename: filename,
	}
}

// Create returns a discovery.Source initialized with authenticated clients for
// Compute & Container APIs, ready for Collection.
func (f *Factory) Create() (discovery.Source, error) {
	source := &Source{
		factory: *f,
	}
	var err error

	// Create a new authenticated HTTP client.
	source.client, err = google.DefaultClient(oauth2.NoContext, gkeScopes...)
	if err != nil {
		return nil, fmt.Errorf("Error setting up Compute client: %s", err)
	}

	// Create a new Compute service instance.
	source.computeService, err = compute.New(source.client)
	if err != nil {
		return nil, fmt.Errorf("Error setting up Compute client: %s", err)
	}

	// Create a new Container Engine service object.
	source.containerService, err = container.New(source.client)
	if err != nil {
		return nil, fmt.Errorf("Error setting up Container Engine client: %s", err)
	}

	return source, nil
}

// Source caches information collected from the GCE, GKE, and K8S APIs during target discovery.
type Source struct {
	// factory is a copy of the original instance that created this source.
	factory Factory

	// client caches an http client authenticated for access to GCP APIs.
	client *http.Client

	// computeService is the entry point to all GCE services.
	computeService *compute.Service

	// containerService is the entry point to all GKE services.
	containerService *container.Service

	// targets collects found targets.
	targets []interface{}
}

// Saves collected targets to the given filename.
func (source *Source) Save() error {
	// Convert the targets to JSON.
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

// Collect uses the Compute Engine, Container Engine, and Kubernetes APIs to
// check every GCE zone for Container Engine (gke) clusters, and checks each
// cluster for services annotated for federated scraping.
//
// Collect returns every gke cluster with a k8s service annotation that equals:
//    gke-prometheus-federation/scrape: true
func (source *Source) Collect() error {
	// Allocate space for the list of targets.
	source.targets = make([]interface{}, 0)

	// Get all zones in a project.
	zoneListCall := source.computeService.Zones.List(source.factory.project)
	err := zoneListCall.Pages(nil, func(zones *compute.ZoneList) error {
		for _, zone := range zones.Items {

			// Get all clusters in a zone.
			clusterList, err := source.containerService.Projects.Zones.Clusters.List(
				source.factory.project, zone.Name).Do()
			if err != nil {
				return err
			}

			// Look for targets from every cluster.
			for _, cluster := range clusterList.Clusters {
				targets, err := checkCluster(zone, cluster)
				if err != nil {
					return err
				}
				source.targets = append(source.targets, targets...)
			}
		}
		return nil
	})
	// TODO(p2, soltesz): consider using goroutines to speed up collection.
	return err
}

// checkCluster uses the kubernetes API to search for GKE targets.
func checkCluster(zone *compute.Zone, cluster *container.Cluster) ([]interface{}, error) {
	targets := []interface{}{}
	// Use information from the GKE cluster to create a k8s API client.
	kubeClient, err := gkeClusterToKubeClient(cluster)
	if err != nil {
		return nil, err
	}

	// List all services in the k8s cluster.
	services, err := kubeClient.CoreV1().Services("").List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	log.Printf("%s - %s - There are %d services in the cluster\n",
		zone.Name, cluster.Name, len(services.Items))

	// Check each service, and collect targets that have matching annotations.
	for _, service := range services.Items {
		// Federation scraping is opt-in only.
		if service.ObjectMeta.Annotations["gke-prometheus-federation/scrape"] != "true" {
			continue
		}
		values := findTargetAndLables(zone, cluster, service)
		if values != nil {
			targets = append(targets, values)
		}
	}
	return targets, nil
}

// findTargetAndLables identifies the first target (first port) per service and
// returns a target configuration for use with Prometheus file service discovery.
func findTargetAndLables(zone *compute.Zone, cluster *container.Cluster, service typesv1.Service) interface{} {
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
	values := map[string]interface{}{
		"labels": map[string]string{
			"service": service.ObjectMeta.Name,
			"cluster": cluster.Name,
			"zone":    zone.Name,
		},
		"targets": []string{target},
	}
	return values
}

// gkeClusterToKubeClient converts a container engine API Cluster object into
// a kubernetes API client instance.
func gkeClusterToKubeClient(c *container.Cluster) (*kubernetes.Clientset, error) {
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
			"cluster": &api.Cluster{
				Server:                   fmt.Sprintf("https://%s", c.Endpoint),
				InsecureSkipTLSVerify:    false, // Require a valid CA Certificate.
				CertificateAuthorityData: rawCaCert,
			},
		},
		AuthInfos: map[string]*api.AuthInfo{
			// Define the user credentials for access to the API.
			"user": &api.AuthInfo{
				Username: c.MasterAuth.Username,
				Password: c.MasterAuth.Password,
			},
		},
		Contexts: map[string]*api.Context{
			// Define a context that refers to the above cluster and user.
			"cluster-user": &api.Context{
				Cluster:  "cluster",
				AuthInfo: "user",
			},
		},
		// Use the above context.
		CurrentContext: "cluster-user",
	}

	// TODO: what is this?
	restConfig, err := clientcmd.NewDefaultClientConfig(
		clusterClient, &clientcmd.ConfigOverrides{
			ClusterInfo: api.Cluster{Server: ""}}).ClientConfig()
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
