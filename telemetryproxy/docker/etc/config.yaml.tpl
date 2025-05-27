{{- $namespaces := (datasource "namespaces") -}}
{{- $config := (datasource "config") -}}
{{- $debug := $config.debug | conv.ToBool -}}
{{- $clusterName := getenv "KUBERNETES_CLUSTER_NAME" "" }}
{{- $infraMetricsToken := getenv "LUMIGO_INFRA_METRICS_TOKEN" "" }}
{{- $infraMetricsFrequency := getenv "LUMIGO_INFRA_METRICS_SCRAPING_FREQUENCY" "15s" }}
{{- $otelcolInternalMetricsFrequency := getenv "LUMIGO_OTELCOL_METRICS_SCRAPING_FREQUENCY" "15s" }}
{{- $watchdogEnabled := getenv "LUMIGO_WATCHDOG_ENABLED" "" | conv.ToBool }}
{{- $infraMetricsEnabled := getenv "LUMIGO_INFRA_METRICS_ENABLED" "" | conv.ToBool }}
{{- $metricsScrapingEnabled := or $watchdogEnabled $infraMetricsEnabled}}

receivers:

  otlp:
    protocols:
      http:
        auth:
          authenticator: lumigoauth/server
        include_metadata: true # Needed by `headers_setter/lumigo`

  prometheus/collector-self-metrics:
    config:
      scrape_configs:
        - job_name: 'lumigo-operator-self-metrics'
          scrape_interval: {{ $otelcolInternalMetricsFrequency }}
          static_configs:
            - targets: ['0.0.0.0:8888']

{{- if $infraMetricsEnabled }}
  prometheus/cluster-infra-metrics:
    config:
      scrape_configs: []
    target_allocator:
      endpoint: {{ getenv "LUMIGO_TARGET_ALLOCATOR_ENDPOINT" }}
      interval: 30s
      collector_id: {{ getenv "HOSTNAME" }}
{{- end }}

extensions:

  health_check:

  headers_setter/lumigo:
    headers:
    # Use the same authorization header as the one accompanying
    # the request received in the receiver. It needs the
    # `include_metadata: true` parameter in the `otlp` exporter
    - key: authorization
      from_context: Authorization
      action: upsert
  lumigoauth/server:
    type: server

{{- range $i, $namespace := $namespaces }}
  lumigoauth/ns_{{ $namespace.name }}:
    type: client
    token: {{ $namespace.token }}
{{- end }}


exporters:

  otlphttp/lumigo:
    endpoint: {{ getenv "LUMIGO_ENDPOINT" "https://ga-otlp.lumigo-tracer-edge.golumigo.com" }}
    auth:
      authenticator: headers_setter/lumigo

  otlphttp/lumigo_logs:
    endpoint: {{ getenv "LUMIGO_LOGS_ENDPOINT" "https://ga-otlp.lumigo-tracer-edge.golumigo.com" }}
    auth:
      authenticator: headers_setter/lumigo

{{- if $metricsScrapingEnabled }}
  otlphttp/lumigo_metrics:
    endpoint: {{ getenv "LUMIGO_METRICS_ENDPOINT" "https://ga-otlp.lumigo-tracer-edge.golumigo.com" }}
    headers:
      # We cannot use headers_setter/lumigo since it assumes the headers are already set by the sender, and in this case -
      # since we're scraping Prometheus metrics and not receiving any metrics from customer code - we don't have any incoming headers.
      Authorization: "LumigoToken {{ $infraMetricsToken }}"
{{- end }}

{{- if $debug }}
  logging:
    verbosity: detailed
    sampling_initial: 1
    sampling_thereafter: 1
{{- end }}

{{- range $i, $namespace := $namespaces }}
  otlphttp/lumigo_ns_{{ $namespace.name }}:
    endpoint: $LUMIGO_ENDPOINT
    auth:
      authenticator: lumigoauth/ns_{{ $namespace.name }}
{{- end }}


processors:

  batch:

  k8sdataenricherprocessor:
    auth_type: serviceAccount

{{- range $i, $namespace := $namespaces }}
  transform/add_ns_attributes_ns_{{ $namespace.name }}:
    log_statements:
    - context: resource
      statements:
      - set(attributes["k8s.namespace.name"], "{{ $namespace.name }}")
      - set(attributes["k8s.namespace.uid"], "{{ $namespace.uid }}")
{{- end }}

  filter/only_monitored_namespaces:
    error_mode: ignore
    logs:
      include:
        match_type: regexp
        resource_attributes:
        # We add k8s.namespace.name in the 'transform/add_ns_attributes_ns_<$namespace.name>' processor
{{- $namespaceNames := coll.Slice }}
{{- range $i, $namespace := $namespaces }}
{{- $namespaceNames = $namespaceNames | append $namespace.name }}
{{- end }}
        - key: k8s.namespace.name
          value: "{{ join $namespaceNames "|" }}"

{{- if $clusterName }}
  transform/add_cluster_name:
    trace_statements:
    - context: resource
      statements:
      - set(attributes["k8s.cluster.name"], "{{ $clusterName }}")
    metric_statements:
    - context: resource
      statements:
      - set(attributes["k8s.cluster.name"], "{{ $clusterName }}")
    log_statements:
    - context: resource
      statements:
      - set(attributes["k8s.cluster.name"], "{{ $clusterName }}")
{{- end }}

  transform/add_heartbeat_attributes:
    log_statements:
    - context: scope
      statements:
      - set(name, "lumigo-operator.namespace_heartbeat")

  transform/set_k8s_objects_scope:
    log_statements:
    - context: scope
      statements:
      - set(name, "lumigo-operator.k8s-objects")
      - set(version, "{{ $config.operator.version }}")

  transform/set_k8s_events_scope:
    log_statements:
    - context: scope
      statements:
      - set(name, "lumigo-operator.k8s-events")
      - set(version, "{{ $config.operator.version }}")

  transform/inject_operator_details_into_resource:
    trace_statements:
    - context: resource
      statements:
      - set(attributes["lumigo.k8s_operator.version"], "{{ $config.operator.version }}")
      - set(attributes["lumigo.k8s_operator.deployment_method"], "{{ $config.operator.deployment_method }}")
    metric_statements:
    - context: resource
      statements:
      - set(attributes["lumigo.k8s_operator.version"], "{{ $config.operator.version }}")
      - set(attributes["lumigo.k8s_operator.deployment_method"], "{{ $config.operator.deployment_method }}")
    log_statements:
    - context: resource
      statements:
      - set(attributes["lumigo.k8s_operator.version"], "{{ $config.operator.version }}")
      - set(attributes["lumigo.k8s_operator.deployment_method"], "{{ $config.operator.deployment_method }}")


service:

  telemetry:
    logs:
      level: {{ $debug | ternary "debug" "info" }}
    metrics:
      level: detailed
      address: ":8888"

  extensions:
  - headers_setter/lumigo
  - health_check
  - lumigoauth/server
{{- range $i, $namespace := $namespaces }}
  - lumigoauth/ns_{{ $namespace.name }}
{{- end }}

  pipelines:

{{- if $watchdogEnabled }}
    metrics/watchdog:
      receivers:
      - prometheus/collector-self-metrics
      processors:
      - filter/filter-prom-metrics
      - batch
      - k8sdataenricherprocessor
      - transform/inject_operator_details_into_resource
{{- if $clusterName }}
      - transform/add_cluster_name
{{- end }}
      exporters:
      - otlphttp/lumigo_metrics
{{- if $debug }}
      - logging
{{- end }}
{{- end }}

{{- if $infraMetricsEnabled }}
    metrics/cluster-infra-metrics:
      receivers:
      - prometheus/cluster-infra-metrics
      processors:
      - filter/filter-prom-metrics
      - k8sdataenricherprocessor
      - transform/inject_operator_details_into_resource
{{- if $clusterName }}
      - transform/add_cluster_name
{{- end }}
      exporters:
      - otlphttp/lumigo_metrics
{{- if $debug }}
      - logging
{{- end }}
{{- end }}

    traces:
      receivers:
      - otlp
      processors:
      - batch
      - k8sdataenricherprocessor
{{- if $clusterName }}
      - transform/add_cluster_name
{{- end }}
      - transform/inject_operator_details_into_resource
      exporters:
      - otlphttp/lumigo
{{- if $debug }}
      - logging
{{- end }}

    logs:
      receivers:
      - otlp
      processors:
      - batch
      - k8sdataenricherprocessor
{{- if $clusterName }}
      - transform/add_cluster_name
{{- end }}
      - transform/inject_operator_details_into_resource
      exporters:
{{- if $debug }}
      - logging
{{- end }}
      - otlphttp/lumigo_logs
