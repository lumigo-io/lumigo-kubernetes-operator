package watchers

import (
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
)

func normalizeEndpoint(originalEndpoint string, expectedSuffix string) (string, bool) {
	endpoint := originalEndpoint
	isInsecure := strings.HasPrefix(endpoint, "http://")
	// Remove http:// or https:// prefix if it exists, as the exporter will add it
	if len(endpoint) >= 7 && endpoint[:7] == "http://" {
		endpoint = endpoint[7:]
	} else if len(endpoint) >= 8 && endpoint[:8] == "https://" {
		endpoint = endpoint[8:]
	}

	// Remove trailing slash if present
	if len(endpoint) > 0 && endpoint[len(endpoint)-1] == '/' {
		endpoint = endpoint[:len(endpoint)-1]
	}

	// To remove /v1/metrics or /v1/logs if provided, as it's already added by the exporter
	if len(endpoint) > len(expectedSuffix) && endpoint[len(endpoint)-len(expectedSuffix):] == expectedSuffix {
		endpoint = endpoint[:len(endpoint)-len(expectedSuffix)]
	}

	return endpoint, isInsecure
}

func LogsExporterConfigOptions(originalEndpoint string, lumigoToken string) *[]otlploghttp.Option {
	endpoint, isInsecure := normalizeEndpoint(originalEndpoint, "/v1/logs")

	opts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(endpoint),
		otlploghttp.WithHeaders(map[string]string{
			"Authorization": fmt.Sprintf("LumigoToken %s", lumigoToken),
		}),
	}

	if isInsecure {
		opts = append(opts, otlploghttp.WithInsecure())
	}

	return &opts
}

func MetricsExporterConfigOptions(originalEndpoint string, lumigoToken string) *[]otlpmetrichttp.Option {
	endpoint, isInsecure := normalizeEndpoint(originalEndpoint, "/v1/metrics")

	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(endpoint),
		otlpmetrichttp.WithHeaders(map[string]string{
			"Authorization": fmt.Sprintf("LumigoToken %s", lumigoToken),
		}),
	}

	if isInsecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}

	return &opts
}
