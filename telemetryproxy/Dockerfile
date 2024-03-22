# Byuild the telemetry-proxy from sources
FROM golang:1.20-alpine AS telemetry-proxy-builder

ARG version='dev'
ENV VERSION=${version}

RUN apk add --update make git

ADD ./src /src
WORKDIR /src

RUN make VERSION=${VERSION} OTELCONTRIBCOL_FILENAME=otelcontribcol install-tools otelcontribcolbuilder

# We need to chmod files to fit the expected uid and gid
# at runtime. Since we do not want to depend on buildkit,
# which has the handy `ADD --chown=<uid>:<gid>` facility,
# we rely on a helper image to avoid ugliness in unnecessary
# layers in the final image
FROM alpine:latest AS chmod-helper

ADD ./docker/etc/config.yaml.tpl /lumigo/etc/otelcol-config.yaml.tpl
RUN chown -R 1234:1234 /lumigo/etc/otelcol-config.yaml.tpl

COPY --from=telemetry-proxy-builder /src/dist/otelcontribcol /lumigo/bin/otelcol
ADD ./docker/*.sh /lumigo/bin/

RUN chmod -R u+x /lumigo/bin/*
RUN chown -R 1234:1234 /lumigo/bin/*

FROM alpine:latest

# Temporary fix to bring in 3.11.6 version of gomplate from Alpine Edge
RUN apk add gomplate --repository=https://dl-cdn.alpinelinux.org/alpine/edge/community

# Be able to validate the TLS cert of the Lumigo SaaS endpoint
RUN apk add ca-certificates
# Update OtelCollector config on namespace file change
RUN apk add bash gomplate procps 

COPY --from=chmod-helper /lumigo /lumigo

ENTRYPOINT ["/bin/bash"]
CMD ["/lumigo/bin/entrypoint.sh"]