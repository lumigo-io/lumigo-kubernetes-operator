# Kubernetes Event Enhancer Processor

| Status                   |           |
|--------------------------|-----------|
| Stability                | [beta]    |
| Supported pipeline types | logs      |
| Distributions            | [contrib] |

This processor adds information about the root owner (as in: the further [owner reference](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/)) to the OTLP logs produced by the [Kubernetes Objects receiver](../../receiver/k8sobjectsreceiver/).

[beta]: https://github.com/open-telemetry/opentelemetry-collector#beta
[contrib]: https://github.com/open-telemetry/opentelemetry-collector-releases/tree/main/distributions/otelcol-contrib
