package config

import (
	"os"
	"strconv"
)

type Config struct {
	MAX_BATCH_SIZE            int
	KUBE_WATCHER_INTERVAL     int
	LUMIGO_OPERATOR_NAMESPACE string
	LUMIGO_METRICS_ENDPOINT   string
	LUMIGO_TOKEN              string
	TELEMETRY_PROXY_ENDPOINT  string
	TELEMETRY_INTERVAL        int
	TOP_WATCHER_INTERVAL      int
}

func LoadConfig() *Config {
	return &Config{
		MAX_BATCH_SIZE:            getEnvInt("LUMIGO_WATCHDOG_MAX_BATCH_SIZE", 5),
		KUBE_WATCHER_INTERVAL:     getEnvInt("LUMIGO_WATCHDOG_MAX_INTERVAL", 10),
		LUMIGO_OPERATOR_NAMESPACE: getEnvString("LUMIGO_OPERATOR_NAMESPACE", "lumigo-system"),
		LUMIGO_METRICS_ENDPOINT:   getEnvString("LUMIGO_METRICS_ENDPOINT", "http://localhost:8000"),
		TELEMETRY_PROXY_ENDPOINT:  getEnvString("TELEMETRY_PROXY_METRICS_ENDPOINT", "http://localhost:8888/metrics"),
		TELEMETRY_INTERVAL:        getEnvInt("LUMIGO_WATCHDOG_TELEMETRY_INTERVAL", 10),
		TOP_WATCHER_INTERVAL:      getEnvInt("LUMIGO_WATCHDOG_TOP_INTERVAL", 10),
		LUMIGO_TOKEN:              getEnvString("LUMIGO_INFRA_METRICS_TOKEN", ""),
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
