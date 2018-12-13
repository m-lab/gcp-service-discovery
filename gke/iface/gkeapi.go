// Package iface defines an interface for accessing Google Compute & Container
// APIs, and helps mediate access to k8s clients. This is helpful for creating
// testable packages.
package iface

import (
	"context"

	compute "google.golang.org/api/compute/v1"
	container "google.golang.org/api/container/v1"
	"k8s.io/client-go/kubernetes"
)

// GKE defines the interface used by the gke logic.
type GKE interface {
	ZonePages(ctx context.Context, f func(zones *compute.ZoneList) error) error
	ClusterList(ctx context.Context, zone string) (*container.ListClustersResponse, error)
	GetKubeClient(c *container.Cluster) (kubernetes.Interface, error)
}

// GKEImpl implements the GKE interface.
type GKEImpl struct {
	project          string
	computeService   *compute.Service
	containerService *container.Service
	getKubeClient    func(c *container.Cluster) (kubernetes.Interface, error)
}

// NewGKE creates a new GKE instance.
func NewGKE(project string, compute *compute.Service, container *container.Service,
	getKubeClient func(c *container.Cluster) (kubernetes.Interface, error)) *GKEImpl {
	return &GKEImpl{project: project, computeService: compute,
		containerService: container, getKubeClient: getKubeClient}
}

// ZonePages wraps the computeService Zones.List().Pages method.
func (g *GKEImpl) ZonePages(ctx context.Context, f func(zones *compute.ZoneList) error) error {
	return g.computeService.Zones.List(g.project).Pages(ctx, f)
}

// ClusterList wraps the container service Clusters.List method for the given zone.
func (g *GKEImpl) ClusterList(ctx context.Context, zone string) (*container.ListClustersResponse, error) {
	return g.containerService.Projects.Zones.Clusters.List(g.project, zone).Context(ctx).Do()
}

// GetKubeClient returns a kubernetes interface for the given cluster.
func (g *GKEImpl) GetKubeClient(c *container.Cluster) (kubernetes.Interface, error) {
	return g.getKubeClient(c)
}
