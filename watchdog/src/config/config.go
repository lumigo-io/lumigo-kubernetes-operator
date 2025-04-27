package config

import (
	"os"
	"strings"
)

type Config struct {
	LumigoOperatorNamespace string
	LumigoOperatorVersion   string
	LumigoMetricsEndpoint   string
	LumigoLogsEndpoint      string
	LumigoToken             string
	TopWatcherInterval      string
	Debug                   bool
	ClusterName             string
}

func LoadConfig() *Config {
	return &Config{
		LumigoOperatorNamespace: getEnvString("LUMIGO_OPERATOR_NAMESPACE", "lumigo-system"),
		LumigoOperatorVersion:   getEnvString("LUMIGO_OPERATOR_VERSION", "unknown"),
		LumigoMetricsEndpoint:   getEnvString("LUMIGO_METRICS_ENDPOINT", "<not set>"),
		LumigoLogsEndpoint:      getEnvString("LUMIGO_LOGS_ENDPOINT", "<not set>"),
		TopWatcherInterval:      getEnvString("LUMIGO_WATCHDOG_TOP_WATCHER_INTERVAL", "15s"),
		LumigoToken:             getEnvString("LUMIGO_INFRA_METRICS_TOKEN", ""),
		Debug:                   getEnvBool("LUMIGO_DEBUG", false),
		ClusterName:             getEnvString("KUBERNETES_CLUSTER_NAME", ""),
	}
}

func getEnvString(key string, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		return strings.ToLower(value) == "true"
	}
	return defaultValue
}
