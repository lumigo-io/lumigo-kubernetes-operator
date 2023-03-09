#!/bin/bash

set -xeuo pipefail

export OTELCOL_BINARY_ASSET_NAME="otelcontribcol_${TARGETPLATFORM/\//_}"

export version="${lumigo_otel_collector_release:-$(cat /opt/VERSION.otelcontibcol)}"
echo "Downloading Downloading 'otelcontribcol_${TARGETPLATFORM//\//_}' from version ${version} from https://github.com/lumigo-io/opentelemetry-collector-contrib"

asset_download_link=$(curl -s https://api.github.com/repos/lumigo-io/opentelemetry-collector-contrib/releases/tags/${version}?draft=true | jq -r ".assets[] | select(.name|match(\"${OTELCOL_BINARY_ASSET_NAME}\")) | .browser_download_url")

mkdir /dist
curl -L "${asset_download_link}" -o /dist/otelcontribcol

chmod u+x /dist/otelcontribcol

ls -al /dist/otelcontribcol

/dist/otelcontribcol --version
/dist/otelcontribcol components
