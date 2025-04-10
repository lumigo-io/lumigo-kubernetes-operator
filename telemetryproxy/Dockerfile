# Build the telemetry-proxy from sources
FROM golang:1.22-alpine AS telemetry-proxy-builder

ARG version='dev'
ENV VERSION=${version}

RUN apk add --update make git

ADD ./src /src
WORKDIR /src

RUN make VERSION=${VERSION} OTELCONTRIBCOL_FILENAME=otelcontribcol otelcontribcolbuilder

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

# Be able to validate the TLS cert of the Lumigo SaaS endpoint
RUN apk add ca-certificates

RUN apk add bash procps
# `apk add gomplate` installs a version with a vulenrability of CVE-2024-34158.
# Once the package is updated, we can install it via `apk`
COPY --from=hairyhenderson/gomplate:latest /gomplate /bin/gomplate

COPY --from=chmod-helper /lumigo /lumigo

ENTRYPOINT ["/bin/bash"]
CMD ["/lumigo/bin/entrypoint.sh"]