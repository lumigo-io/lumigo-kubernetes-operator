#!/bin/bash

set -eo pipefail

readonly OTELCOL_CONFIG_FILE_PATH="/lumigo/etc/otelcol/config.yaml"
readonly OTELCOL_CONFIG_TEMPLATE_FILE_PATH="/lumigo/etc/otelcol-config.yaml.tpl"
readonly GENERATION_CONFIG_FILE_PATH="/lumigo/etc/otelcol/generation-config.json"
readonly NAMESPACES_FILE_PATH="/lumigo/etc/namespaces/namespaces_to_monitor.json"
readonly NAMESPACES_FILE_SHA_PATH="${NAMESPACES_FILE_PATH}.sha1"

readonly DEFAULT_MEMORY_LIMIT_MIB=4000
readonly NO_MEMORY_LIMIT=9223372036854771712

readonly CGROUPS_V1_MAX_MEMORY_PATH="/sys/fs/cgroup/memory/memory.limit_in_bytes"
readonly CGROUPS_V2_MAX_MEMORY_PATH="/sys/fs/cgroup/memory.max"

if [ -f "${CGROUPS_V1_MAX_MEMORY_PATH}" ]; then
    # cgroups v1
    memory_limit_bytes=$(<${CGROUPS_V1_MAX_MEMORY_PATH})
elif [ -f "${CGROUPS_V2_MAX_MEMORY_PATH}" ]; then
    # cgroups v2
    memory_limit_bytes=$(<${CGROUPS_V2_MAX_MEMORY_PATH})
fi

if [ -n "${memory_limit_bytes}" ] && [ "${memory_limit_bytes}" != "${NO_MEMORY_LIMIT}" ]; then
    # Memory limits are set in the container
    memory_limit_mib=$(( ${memory_limit_bytes} / 1048576 ))
fi

if [ -n "${memory_limit_mib}" ]; then
    echo "Setting memory limits on the OtelCollector to ${memory_limit_mib} MiB"
else
    echo "No memory limits found on the container; using the ${DEFAULT_MEMORY_LIMIT_MIB} MiB default"
    memory_limit_mib="${DEFAULT_MEMORY_LIMIT_MIB}"
fi

export GOMEMLIMIT="${memory_limit_mib}MiB"

debug='false'
if [ "${LUMIGO_DEBUG,,}" = 'true' ]; then
    debug='true'
fi

operator_version="${LUMIGO_OPERATOR_VERSION:-unknown}"
operator_deployment_method="${LUMIGO_OPERATOR_DEPLOYMENT_METHOD:-unknown}"

# Create generation configs
mkdir -p $(dirname "${GENERATION_CONFIG_FILE_PATH}")

echo "{
    \"operator\": {
        \"version\": \"${operator_version}\",
        \"deployment_method\": \"${operator_deployment_method}\"
    },
    \"debug\": ${debug}
}" > "${GENERATION_CONFIG_FILE_PATH}"

if [ "${debug}" == 'true' ]; then
    echo "Generation configurations: $(cat ${GENERATION_CONFIG_FILE_PATH})"
fi

function generate_configs() {
    gomplate -f "${OTELCOL_CONFIG_TEMPLATE_FILE_PATH}" -d "config=${GENERATION_CONFIG_FILE_PATH}" -d "namespaces=${NAMESPACES_FILE_PATH}" --in "${config}" > "${OTELCOL_CONFIG_FILE_PATH}"

    if [ "${debug}" == 'true' ]; then
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
            if [ "${debug}" == 'true' ]; then
                echo "Namespace file change detected: $(< ${NAMESPACES_FILE_SHA_PATH})"
                echo "$(< ${NAMESPACES_FILE_PATH})"
            fi
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
    echo -n '[]' > "${NAMESPACES_FILE_PATH}"
fi

generate_configs

echo "Starting watch for config updates on file ${NAMESPACES_FILE_PATH}"

watch_namespaces_file &

exec /lumigo/bin/otelcol "--config=${OTELCOL_CONFIG_FILE_PATH}"