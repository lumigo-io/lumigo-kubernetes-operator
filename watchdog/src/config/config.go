package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	MAX_BATCH_SIZE            int
	KUBE_WATCHER_INTERVAL     int
	LumigoOperatorNamespace   string
	LumigoOperatorVersion     string
	LumigoMetricsEndpoint     string
	LumigoLogsEndpoint        string
	LumigoToken               string
	TELEMETRY_PROXY_ENDPOINT  string
	TELEMETRY_INTERVAL        int
	TopWatcherIntervalSeconds int
	Debug                     bool
	ClusterName               string
}

func LoadConfig() *Config {
	return &Config{
		LumigoOperatorNamespace:   getEnvString("LUMIGO_OPERATOR_NAMESPACE", "lumigo-system"),
		LumigoOperatorVersion:     getEnvString("LUMIGO_OPERATOR_VERSION", "unknown"),
		LumigoMetricsEndpoint:     getEnvString("LUMIGO_METRICS_ENDPOINT", "<not set>"),
		LumigoLogsEndpoint:        getEnvString("LUMIGO_LOGS_ENDPOINT", "<not set>"),
		TopWatcherIntervalSeconds: getEnvInt("LUMIGO_WATCHDOG_TOP_INTERVAL", 10),
		LumigoToken:               getEnvString("LUMIGO_INFRA_METRICS_TOKEN", ""),
		Debug:                     getEnvBool("LUMIGO_DEBUG", false),
		ClusterName:               getEnvString("KUBERNETES_CLUSTER_NAME", ""),
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
