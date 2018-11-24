// Package iface defines an interface for accessing AppEngine APIs. This is
// helpful for creating testable packages.
package iface

import (
	"context"

	appengine "google.golang.org/api/appengine/v1"
)

// AppAPI defines the interface used by the aeflex logic.
type AppAPI interface {
	ServicesPages(ctx context.Context, f func(listVer *appengine.ListServicesResponse) error) error
	VersionsPages(ctx context.Context, serviceID string, f func(listVer *appengine.ListVersionsResponse) error) error
	InstancesPages(ctx context.Context, serviceID, versionID string, f func(listInst *appengine.ListInstancesResponse) error) error
}

// AppAPIImpl implements the AppAPI interface.
type AppAPIImpl struct {
	project string
	apis    *appengine.APIService
}

// NewAppAPI creates a new instance of the AppAPI for the given project.
func NewAppAPI(project string, apis *appengine.APIService) *AppAPIImpl {
	return &AppAPIImpl{project: project, apis: apis}
}

// ServicesPages lists all AppEngine services and calls the given function for
// each "page" of results.
func (a *AppAPIImpl) ServicesPages(
	ctx context.Context, f func(listVer *appengine.ListServicesResponse) error) error {
	return a.apis.Apps.Services.List(a.project).Pages(ctx, f)
}

// VersionsPages lists all AppEngine versions for the given service and calls
// the given function for each "page" of results.
func (a *AppAPIImpl) VersionsPages(
	ctx context.Context, serviceID string,
	f func(listVer *appengine.ListVersionsResponse) error) error {
	return a.apis.Apps.Services.Versions.List(a.project, serviceID).Pages(ctx, f)
}

// InstancesPages lists all AppEngine instances for the given service and
// version and calls the given function for each "page" of results.
func (a *AppAPIImpl) InstancesPages(
	ctx context.Context, serviceID, versionID string,
	f func(listInst *appengine.ListInstancesResponse) error) error {
	return a.apis.Apps.Services.Versions.Instances.List(
		a.project, serviceID, versionID).Pages(ctx, f)
}
