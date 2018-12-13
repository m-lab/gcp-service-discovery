[![Go Report Card](https://goreportcard.com/badge/github.com/m-lab/gcp-service-discovery)](https://goreportcard.com/report/github.com/m-lab/gcp-service-discovery) [![Build Status](https://travis-ci.org/m-lab/gcp-service-discovery.svg?branch=master)](https://travis-ci.org/m-lab/gcp-service-discovery) [![Coverage Status](https://coveralls.io/repos/github/m-lab/gcp-service-discovery/badge.svg?branch=master)](https://coveralls.io/github/m-lab/gcp-service-discovery?branch=master)

# gcp-service-discovery

gcp-service-discovery extends Prometheus service discovery by using Google
Cloud Platform (GCP) APIs to discover targets to scrape.

Using metadata collected during discovery, gcp-service-discovery generates
and writes [file-based service discovery][filesd] target configuration, which
Prometheus can read directly.

Supported discovery sources:

* Scraping individual AppEngine Flex Instances - using AppEngine Admin API
* Scraping Prometheus federation in GKE Clusters - using Kubernetes Engine API
* Download generic, pre-generated HTTP(s) targets - using Go http.Client

Additional configuration is necessary for AppEngine Flex Instances and GKE
Services.

[filesd]: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#%3Cfile_sd_config%3E

## AppEngine Flex Instances

AppEngine Flex runs GCE VMs (instances) behind a public-facing load balancer.
So by default, these GCE VMs are not individually addressable, which makes
standard Prometheus scraping impossible.

gcp-service-discovery uses the [GCP AppEngine Admin API][aeflexapi] to query
all known services, versions, and instances to discovery instances with
forwarded ports that *can* be scraped directly by Prometheus.

For this discovery to work, the AppEngine Flex service configuration must
include a network `forwarded_ports` section like the one below. The config
forwards a container port to a port on the GCE VM's public IP. The service you
deploy to AppEngine should export metrics on the same port.

NOTE: metrics could be publicly visible and will be unencrypted by default.

Example AppEngine Flex network configuration:
```
network:
  instance_tag: <tag name>
  name: default
  # Note: default AppEngine container port 8080 cannot be forwarded.
  forwarded_ports:
    - 9990/tcp
```

[aeflexapi]: https://cloud.google.com/appengine/docs/admin-api/reference/rest/

## GKE Services

Prometheus service discovery works automatically within a single Kubernetes
cluster. So, for some it is common practice to run one Prometheus server per
cluster. In order to take advantage of metric aggregation across clusters
using [Prometheus federation][federation], we want to automatically discover
these other Prometheus servers.

gcp-service-discovery use the [GCP Kubernetes Engine API][gkeapi] to discover
credentials for all known clusters and then directly queries the master API
to find services with a specific annotation.

For this discovery to work, the service must include the annotation
`gke-prometheus-federation/scrape=true`.

NOTE: depending on the service type and VPC network configuration, the
Prometheus instance may be publicly accessible.

```
apiVersion: v1
kind: Service
metadata:
  ... [snip]
  annotations:
    gke-prometheus-federation/scrape: 'true'
spec:
  ports:
  - port: 9090
    protocol: TCP
    targetPort: 9090
  ... [snip]
```

[federation]: https://prometheus.io/docs/prometheus/latest/federation/
[gkeapi]: https://cloud.google.com/kubernetes-engine/docs/reference/rest/

# Running gcp-service-discovery

To run this locally using docker, try:
```
docker run -d measurementlab/gcp-service-discovery:latest \
    --aef-target=/targets/aeflex.json \
    --gke-target=/targets/gke.json \
    --http-target=/targets/http.json \
    --http-source=https://some-random-url-or-service.com/targets.json
```

In kubernetes, the gcp-service-discovery container should be deployed as a
sidecar service with Prometheus.
