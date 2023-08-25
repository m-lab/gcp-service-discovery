FROM golang:1.20 as build
COPY . /go/src/github.com/m-lab/gcp-service-discovery
RUN CGO_ENABLED=0 go install -v github.com/m-lab/gcp-service-discovery/cmd/gcp_service_discovery@latest

# Now copy the built image into the minimal base image
FROM alpine
COPY --from=build /go/bin/gcp_service_discovery /
# Install ca-certificates so the gcp_service_discovery process can contact TLS
# services securely.
RUN apk update && apk add ca-certificates
WORKDIR /
# Make sure /gcp_service_discovery can run (has no missing external dependencies).
RUN /gcp_service_discovery -h 2> /dev/null
ENTRYPOINT ["/gcp_service_discovery"]
