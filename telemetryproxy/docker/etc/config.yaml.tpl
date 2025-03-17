{{- $namespaces := (datasource "namespaces") -}}
{{- $config := (datasource "config") -}}
{{- $debug := $config.debug | conv.ToBool -}}
{{- $clusterName := getenv "KUBERNETES_CLUSTER_NAME" "" }}
{{- $infraMetricsToken := getenv "LUMIGO_INFRA_METRICS_TOKEN" "" }}
{{- $infraMetricsFrequency := getenv "LUMIGO_INFRA_METRICS_SCRAPING_FREQUENCY" "15s" }}
{{- $essentialMetricsOnly := getenv "LUMIGO_EXPORT_ESSENTIAL_METRICS_ONLY" "" | conv.ToBool }}
receivers:
  otlp:
    protocols:
      http:
        auth:
          authenticator: lumigoauth/server
        include_metadata: true # Needed by `headers_setter/lumigo`
{{- if $infraMetricsToken }}
  prometheus:
    config:
      scrape_configs:
        - job_name: 'k8s-infra-metrics'
          metrics_path: /metrics
          scrape_interval: {{ $infraMetricsFrequency }}
          scheme: https
          tls_config:
            insecure_skip_verify: true
          kubernetes_sd_configs:
            - role: node
          authorization:
            credentials_file: "/var/run/secrets/kubernetes.io/serviceaccount/token"
        - job_name: 'k8s-infra-metrics-cadvisor'
          metrics_path: /metrics/cadvisor
          scrape_interval: {{ $infraMetricsFrequency }}
          scheme: https
          tls_config:
            insecure_skip_verify: true
          kubernetes_sd_configs:
            - role: node
          authorization:
            credentials_file: "/var/run/secrets/kubernetes.io/serviceaccount/token"
        - job_name: 'k8s-infra-metrics-resources'
          metrics_path: /metrics/resource
          scrape_interval: {{ $infraMetricsFrequency }}
          scheme: https
          tls_config:
            insecure_skip_verify: true
          kubernetes_sd_configs:
            - role: node
          authorization:
            credentials_file: "/var/run/secrets/kubernetes.io/serviceaccount/token"
        - job_name: 'prometheus-node-exporter'
          kubernetes_sd_configs:
            - role: node
          relabel_configs:
            - source_labels: [__meta_kubernetes_node_address_InternalIP]
              action: replace
              target_label: __address__
              # Scrape a custom port provided by LUMIGO_PROM_NODE_EXPORTER_PORT.
              # '$$1' escapes '$1', as Gomplate otherwise thinks it's an environment variable.
              replacement: '$$1:$LUMIGO_PROM_NODE_EXPORTER_PORT'
            - source_labels: [__meta_kubernetes_node_name]
              action: replace
              target_label: node
          metrics_path: "/metrics"
          authorization:
            credentials_file: "/var/run/secrets/kubernetes.io/serviceaccount/token"
        - job_name: 'kube-state-metrics'
          metrics_path: /metrics
          scrape_interval: {{ $infraMetricsFrequency }}
          static_configs:
            - targets: ['{{ getenv "LUMIGO_KUBE_STATE_METRICS_SERVICE" }}:{{ getenv "LUMIGO_KUBE_STATE_METRICS_PORT" }}']
{{- end }}
{{- range $i, $namespace := $namespaces }}
  lumigooperatorheartbeat/ns_{{ $namespace.name }}:
    namespace: {{ $namespace.name }}
{{- end }}
{{- range $i, $namespace := $namespaces }}
  k8sobjects/objects_ns_{{ $namespace.name }}:
    auth_type: serviceAccount
    objects:
{{- range $i, $mode := (coll.Slice "watch" "pull") }}
    - name: pods
      mode: {{ $mode }}
      interval: 10m
      namespaces: [ {{ $namespace.name }} ]
    - name: daemonsets
      group: apps
      mode: {{ $mode }}
      interval: 10m
      namespaces: [ {{ $namespace.name }} ]
    - name: deployments
      group: apps
      mode: {{ $mode }}
      interval: 10m
      namespaces: [ {{ $namespace.name }} ]
    - name: replicasets
      group: apps
      mode: {{ $mode }}
      interval: 10m
      namespaces: [ {{ $namespace.name }} ]
    - name: statefulsets
      group: apps
      mode: {{ $mode }}
      interval: 10m
      namespaces: [ {{ $namespace.name }} ]
    - name: cronjobs
      group: batch
      mode: {{ $mode }}
      interval: 10m
      namespaces: [ {{ $namespace.name }} ]
    - name: jobs
      group: batch
      mode: {{ $mode }}
      interval: 10m
      namespaces: [ {{ $namespace.name }} ]
{{- end }}
{{- end }}
{{- range $i, $namespace := $namespaces }}
  k8sobjects/events_ns_{{ $namespace.name }}:
    auth_type: serviceAccount
    objects:
{{- range $i, $mode := (coll.Slice "watch" "pull") }}
{{- range $j, $fieldSelector := (coll.Slice "involvedObject.apiVersion=v1,involvedObject.kind=Pod" "involvedObject.apiVersion=apps/v1,involvedObject.kind=DaemonSet" "involvedObject.apiVersion=apps/v1,involvedObject.kind=Deployment" "involvedObject.apiVersion=apps/v1,involvedObject.kind=ReplicaSet" "involvedObject.apiVersion=apps/v1,involvedObject.kind=StatefulSet" "involvedObject.apiVersion=batch/v1,involvedObject.kind=CronJob" "involvedObject.apiVersion=batch/v1,involvedObject.kind=Job" ) }}
    - name: events
      mode: {{ $mode }}
      interval: 10m
      namespaces: [ {{ $namespace.name }} ]
      # Filter out events without the involved object's UID, e.g., the ones generated by our webhook
      field_selector: {{ $fieldSelector }},involvedObject.uid!='',involvedObject.uid!=
{{- end }}
{{- end }}
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
    endpoint: {{ env.Getenv "LUMIGO_ENDPOINT" "https://ga-otlp.lumigo-tracer-edge.golumigo.com" }}
    auth:
      authenticator: headers_setter/lumigo
  otlphttp/lumigo_logs:
    endpoint: {{ env.Getenv "LUMIGO_LOGS_ENDPOINT" "https://ga-otlp.lumigo-tracer-edge.golumigo.com" }}
    auth:
      authenticator: headers_setter/lumigo
{{- if $infraMetricsToken }}
  otlphttp/lumigo_metrics:
    endpoint: {{ env.Getenv "LUMIGO_METRICS_ENDPOINT" "https://ga-otlp.lumigo-tracer-edge.golumigo.com" }}
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
  filter/filter-prom-metrics:
    metrics:
      exclude:
        match_type: regexp
        metric_names:
          # Exclude Prometheus scrape metrics
          - 'scrape_.+'
          - 'up'
          # Exclude Go runtime metrics
          - 'go_.+'
          # Exclude API server metrics
          - 'apiserver_.+'
          - 'authentication_token_.+'
{{ if $essentialMetricsOnly }}
  filter/essential-metrics-only:
    metrics:
      exclude:
        match_type: regexp
        - kube_lease_owner
      include:
        match_type: regexp
        metric_names:
          - container_cpu_usage_seconds_total
          - container_memory_working_set_bytes
          - kube_.+_labels
          - kube_cronjob_status_active
          - kube_daemonset_status_current_number_scheduled
          - kube_daemonset_status_desired_number_scheduled
          - kube_deployment_spec_replicas
          - kube_deployment_status_replicas_available
          - kube_job_owner
          - kube_node_status_capacity
          - kube_pod_container_resource_limits
          - kube_pod_container_status_restarts_total
          - kube_pod_container_status_terminated_reason
          - kube_pod_container_status_waiting_reason
          - kube_pod_owner
          - kube_pod_status_phase
          - kube_replicaset_owner
          - kube_statefulset_replicas
          - kube_statefulset_status_replicas_ready
          - node_cpu_seconds_total
          - node_memory_Active_bytes
{{- end }}
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
{{- range $i, $namespace := $namespaces }}
  batch/k8s_objects_ns_{{ $namespace.name }}:
    send_batch_size: 100
    timeout: 1s
  batch/k8s_events_ns_{{ $namespace.name }}:
    send_batch_size: 100
    timeout: 1s
{{- end }}
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
  extensions:
  - headers_setter/lumigo
  - health_check
  - lumigoauth/server
{{- range $i, $namespace := $namespaces }}
  - lumigoauth/ns_{{ $namespace.name }}
{{- end }}
  pipelines:
{{- if $infraMetricsToken }}
    metrics:
      receivers:
      - prometheus
      processors:
      - filter/filter-prom-metrics
{{ if $essentialMetricsOnly }}
      - filter/essential-metrics-only
{{- end }}
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
      # We cannot add a Batch processor to this pipeline as it would break the
      # `headers_setter/lumigo` extension.
      # See https://github.com/open-telemetry/opentelemetry-collector/issues/4544
      receivers:
      - otlp
      processors:
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
{{- range $i, $namespace := $namespaces }}
    logs/usage_analytics_ns_{{ $namespace.name }}:
      receivers:
      - lumigooperatorheartbeat/ns_{{ $namespace.name }}
      processors:
      - k8sdataenricherprocessor
      - transform/add_heartbeat_attributes
      - transform/add_ns_attributes_ns_{{ $namespace.name }}
{{- if $clusterName }}
      - transform/add_cluster_name
{{- end }}
      - transform/inject_operator_details_into_resource
      exporters:
{{- if $config.debug }}
      - logging
{{- end }}
      - otlphttp/lumigo_ns_{{ $namespace.name }}
    logs/application_logs_ns_{{ $namespace.name }}:
      receivers:
      - otlp
      processors:
      - k8sdataenricherprocessor
      - transform/add_ns_attributes_ns_{{ $namespace.name }}
{{- if $clusterName }}
      - transform/add_cluster_name
{{- end }}
      - transform/inject_operator_details_into_resource
      exporters:
{{- if $config.debug }}
      - logging
{{- end }}
      - otlphttp/lumigo_logs
    logs/k8s_objects_ns_{{ $namespace.name }}:
      receivers:
      - k8sobjects/objects_ns_{{ $namespace.name }}
      processors:
      - transform/set_k8s_objects_scope
      - k8sdataenricherprocessor
      - transform/add_ns_attributes_ns_{{ $namespace.name }}
      - filter/only_monitored_namespaces
{{- if $clusterName }}
      - transform/add_cluster_name
{{- end }}
      - transform/inject_operator_details_into_resource
      - batch/k8s_objects_ns_{{ $namespace.name }}
      exporters:
{{- if $debug }}
      - logging
{{- end }}
      - otlphttp/lumigo_ns_{{ $namespace.name }}
    logs/k8s_events_ns_{{ $namespace.name }}:
      receivers:
      - k8sobjects/events_ns_{{ $namespace.name }}
      processors:
      - transform/set_k8s_events_scope
      - k8sdataenricherprocessor
      - transform/add_ns_attributes_ns_{{ $namespace.name }}
      - filter/only_monitored_namespaces
      - transform/inject_operator_details_into_resource
{{- if $clusterName }}
      - transform/add_cluster_name
{{- end }}
      - batch/k8s_events_ns_{{ $namespace.name }}
      exporters:
{{- if $debug }}
      - logging
{{- end }}
      - otlphttp/lumigo_ns_{{ $namespace.name }}
{{ end }}
