package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	MAX_BATCH_SIZE            int
	KUBE_WATCHER_INTERVAL     int
	LUMIGO_OPERATOR_NAMESPACE string
	LUMIGO_OPERATOR_VERSION   string
	LUMIGO_METRICS_ENDPOINT   string
	LUMIGO_LOGS_ENDPOINT      string
	LUMIGO_TOKEN              string
	TELEMETRY_PROXY_ENDPOINT  string
	TELEMETRY_INTERVAL        int
	TOP_WATCHER_INTERVAL      int
	DEBUG                     bool
}

func LoadConfig() *Config {
	return &Config{
		MAX_BATCH_SIZE:            getEnvInt("LUMIGO_WATCHDOG_MAX_BATCH_SIZE", 5),
		KUBE_WATCHER_INTERVAL:     getEnvInt("LUMIGO_WATCHDOG_MAX_INTERVAL", 10),
		LUMIGO_OPERATOR_NAMESPACE: getEnvString("LUMIGO_OPERATOR_NAMESPACE", "lumigo-system"),
		LUMIGO_OPERATOR_VERSION:   getEnvString("LUMIGO_OPERATOR_VERSION", "latest"),
		LUMIGO_METRICS_ENDPOINT:   getEnvString("LUMIGO_METRICS_ENDPOINT", "http://localhost:8000"),
		LUMIGO_LOGS_ENDPOINT:      getEnvString("LUMIGO_LOGS_ENDPOINT", "http://localhost:8000"),
		TELEMETRY_PROXY_ENDPOINT:  getEnvString("TELEMETRY_PROXY_METRICS_ENDPOINT", "http://localhost:8888/metrics"),
		TELEMETRY_INTERVAL:        getEnvInt("LUMIGO_WATCHDOG_TELEMETRY_INTERVAL", 10),
		TOP_WATCHER_INTERVAL:      getEnvInt("LUMIGO_WATCHDOG_TOP_INTERVAL", 10),
		LUMIGO_TOKEN:              getEnvString("LUMIGO_INFRA_METRICS_TOKEN", ""),
		DEBUG:                     getEnvBool("LUMIGO_DEBUG", false),
	}
}

func getEnvString(key string, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		parsedValue, err := strconv.Atoi(value)
		if err == nil {
			return parsedValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		return strings.ToLower(value) == "true"
	}
	return defaultValue
}
