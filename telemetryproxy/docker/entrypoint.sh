#!/bin/bash

set -eo pipefail

readonly OTELCOL_CONFIG_FILE_PATH="/lumigo/etc/otelcol/config.yaml"
readonly OTELCOL_CONFIG_TEMPLATE_FILE_PATH=${OTELCOL_CONFIG_TEMPLATE_FILE_PATH:-"/lumigo/etc/otelcol-config.yaml.tpl"}
readonly GENERATION_CONFIG_FILE_PATH="/lumigo/etc/otelcol/generation-config.json"
readonly TELEMETRY_PROXY_EXTRA_CONFIG_FILE_PATH="/lumigo/etc/telemetry/telemetry-proxy-extra-config.json"
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
    gomplate -f "${OTELCOL_CONFIG_TEMPLATE_FILE_PATH}" \
        -d "config=${GENERATION_CONFIG_FILE_PATH}" \
        -d "namespaces=${NAMESPACES_FILE_PATH}" \
        -d "telemetryProxy=${TELEMETRY_PROXY_EXTRA_CONFIG_FILE_PATH}" \
        --in "${config}" > "${OTELCOL_CONFIG_FILE_PATH}"

    if [ "${debug}" == 'true' ]; then
       cat "${OTELCOL_CONFIG_FILE_PATH}"
    fi

    sha1sum -b "${NAMESPACES_FILE_PATH}" > "${NAMESPACES_FILE_SHA_PATH}"
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
                echo 'Namespace file change detected'
                cat "${NAMESPACES_FILE_PATH}"
                echo
            fi
            generate_configs
            trigger_config_reload
        fi
    done
}

function watch_remote_namespaces_file() {
    if [ "${debug}" == 'true' ]; then
        wget_flags=""
    else
        wget_flags="-q"
    fi

    while true; do
        sleep 5s

        # override the local namespaces file with the one from the controller
        if ! wget $wget_flags "${LUMIGO_OPERATOR_NAMESPACE_LIST_URL}" -O "${NAMESPACES_FILE_PATH}" 2>&1; then
            echo "Error: Failed to download namespaces file from ${LUMIGO_OPERATOR_NAMESPACE_LIST_URL}" >&2
            continue
        fi

        # check if the fetched file has a different sha1sum than what we have
        if ! sha1sum -c "${NAMESPACES_FILE_SHA_PATH}" > /dev/null 2>&1; then
            if [ "${debug}" == 'true' ]; then
                echo 'Namespace file change detected'
                cat "${NAMESPACES_FILE_PATH}"
                echo
            fi
            generate_configs
            trigger_config_reload
            sha1sum -b "${NAMESPACES_FILE_PATH}" > "${NAMESPACES_FILE_SHA_PATH}"
        else
            if [ "${debug}" == 'true' ]; then
                echo 'No namespace file change detected'
            fi
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


if [ "${LUMIGO_OPERATOR_NAMESPACE_LIST_URL}" ]; then
    echo "Starting watch for config updates on file ${NAMESPACES_FILE_PATH} (periodically updated from ${LUMIGO_OPERATOR_NAMESPACE_LIST_URL})"
    watch_remote_namespaces_file &
else
echo "Starting watch for config updates on file ${NAMESPACES_FILE_PATH}"
    watch_namespaces_file &
fi

exec /lumigo/bin/otelcol \
    "--config=/lumigo/etc/otelcol-common-config.yaml" \
    "--config=${OTELCOL_CONFIG_FILE_PATH}"