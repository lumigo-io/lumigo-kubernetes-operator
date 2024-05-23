package config

import (
	"os"
	"strconv"
)

type Config struct {
	MAX_BATCH_SIZE           int
	KUBE_INTERVAL            int
	NAMESPACE                string
	LUMIGO_ENDPOINT          string
	LUMITO_TOKEN             string
	TELEMETRY_PROXY_ENDPOINT string
	TELEMETRY_INTERVAL       int
	TOP_INTERVAL             int
}

func LoadConfig() *Config {
	return &Config{
		MAX_BATCH_SIZE:           getEnvInt("MAX_BATCH_SIZE", 5),
		KUBE_INTERVAL:            getEnvInt("MAX_INTERVAL", 10),
		NAMESPACE:                getEnvString("NAMESPACE", "lumigo-system"),
		LUMIGO_ENDPOINT:          getEnvString("LUMIGO_ENDPOINT", "http://localhost:8000"),
		TELEMETRY_PROXY_ENDPOINT: getEnvString("TELEMETRY_PROXY_ENDPOINT", "http://localhost:8888/metrics"),
		TELEMETRY_INTERVAL:       getEnvInt("TELEMETRY_INTERVAL", 10),
		TOP_INTERVAL:             getEnvInt("TOP_INTERVAL", 10),
	}
}

func getEnvString(key string, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func (c *Config) SetToken(token string) {
	c.LUMITO_TOKEN = token
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
