module github.com/lumigo-io/lumigo-kubernetes-operator/telemetryproxy

go 1.20

replace "github.com/open-telemetry/opentelemetry-collector-contrib/processor/k8sdataenricherprocessor v0.82.0" => ./processor/k8sdataenricherprocessor

replace tools => ./internal/tools
