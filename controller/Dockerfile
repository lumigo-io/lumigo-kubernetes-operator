# Build the manager binary
FROM golang:1.22 as builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the go source
COPY src/ .

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager main.go

FROM alpine:latest

# Be able to validate the TLS cert of the Lumigo SaaS endpoint
RUN apk add bash procps

WORKDIR /
COPY --from=builder --chown=65532:65532 /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/bin/bash"]
CMD ["/lumigo/bin/entrypoint.sh"]
