#!/bin/bash

set -eo pipefail

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
    echo "Setting memory limits on the controller to ${memory_limit_mib} MiB"
else
    echo "No memory limits found on the container; using the ${DEFAULT_MEMORY_LIMIT_MIB} MiB default"
    memory_limit_mib="${DEFAULT_MEMORY_LIMIT_MIB}"
fi

export GOMEMLIMIT="${memory_limit_mib}MiB"

exec /watchdog "--config=${OTELCOL_CONFIG_FILE_PATH}"
