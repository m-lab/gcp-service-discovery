FROM golang:1.11 as build
COPY . /go/src/github.com/m-lab/gcp-service-discovery
RUN CGO_ENABLED=0 go get -v github.com/m-lab/gcp-service-discovery/cmd/gcp_service_discovery

# Now copy the built image into the minimal base image
FROM alpine
COPY --from=build /go/bin/gcp_service_discovery /
# Install ca-certificates so the gcp_service_discovery process can contact TLS
# services securely.
RUN apk update && apk add ca-certificates
WORKDIR /
ENTRYPOINT ["/gcp_service_discovery"]
