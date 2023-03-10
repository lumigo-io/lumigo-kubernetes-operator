#!/bin/bash

set -eo pipefail

readonly OTELCOL_CONFIG_FILE_PATH="/lumigo/etc/otelcol/config.yaml"
readonly OTELCOL_CONFIG_TEMPLATE_FILE_PATH="/lumigo/etc/otelcol-config.yaml.tpl"
readonly GENERATION_CONFIG_FILE_PATH="/lumigo/etc/otelcol/generation-config.json"
readonly NAMESPACES_FILE_PATH="/lumigo/etc/namespaces/namespaces_to_monitor.json"
readonly NAMESPACES_FILE_SHA_PATH="${NAMESPACES_FILE_PATH}.sha1"

function generate_configs() {
    # Update config
    mkdir -p $(dirname "${OTELCOL_CONFIG_FILE_PATH}")

    local debug=
    if [ "${LUMIGO_DEBUG,,}" = 'true' ]; then
        debug='true'
    fi


    # Build configs as minified JSON
    echo -n "\"${debug}\"" | jq -r '{debug:((. | type) == "string" and (. | ascii_downcase) == "true")}' > "${GENERATION_CONFIG_FILE_PATH}"

    gomplate -f "${OTELCOL_CONFIG_TEMPLATE_FILE_PATH}" -d "config=${GENERATION_CONFIG_FILE_PATH}" -d "namespaces=${NAMESPACES_FILE_PATH}" --in "${config}" > "${OTELCOL_CONFIG_FILE_PATH}"

    if [ -n "${debug}" ]; then
        cat "${OTELCOL_CONFIG_FILE_PATH}"
    fi

    sha1sum "${NAMESPACES_FILE_PATH}" > "${NAMESPACES_FILE_SHA_PATH}"
}

function trigger_config_reload() {
    local OTELCOL_PID="$(pgrep otelcol)"

    if [ -z "${OTELCOL_PID}" ]; then
        echo "Cannot find PID of the OpenTelemetry Collector:\n$(ps aux)" > /dev/stderr
        exit 1
    fi

    echo "Reloading configurations"
    kill -SIGHUP "${OTELCOL_PID}"
}

function watch_namespaces_file() {
    while true; do
        sleep 1s

        if ! sha1sum -c "${NAMESPACES_FILE_SHA_PATH}" > /dev/null 2>&1; then
            # Config changed
            generate_configs
            trigger_config_reload
        fi
    done
}

mkdir -p "$(dirname "${NAMESPACES_FILE_PATH}")"

if [ ! -s "${NAMESPACES_FILE_PATH}" ]; then
    # `NAMESPACES_FILE_PATH` file not existing or empty. Init the `NAMESPACES_FILE_PATH` with an empty JSON object
    # so that the configs can be generated correctly
    echo "Initializing '${NAMESPACES_FILE_PATH}' namespaces file"
    echo -n '{}' > "${NAMESPACES_FILE_PATH}"
fi

generate_configs

echo "Starting watch for config updates on file ${NAMESPACES_FILE_PATH}"

watch_namespaces_file &

exec /lumigo/bin/otelcol "--config=${OTELCOL_CONFIG_FILE_PATH}"