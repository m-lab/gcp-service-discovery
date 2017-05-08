FROM golang:1.8
COPY . /go/src/gcp_service_discovery
RUN go get -v gcp_service_discovery
ENTRYPOINT ["/go/bin/gcp_service_discovery"]
