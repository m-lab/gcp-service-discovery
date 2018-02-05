FROM golang:1.8
COPY . /go/src/github.com/m-lab/gcp-service-discovery
RUN go get -v github.com/m-lab/gcp-service-discovery/cmd/gcp_service_discovery
ENTRYPOINT ["/go/bin/gcp_service_discovery"]
