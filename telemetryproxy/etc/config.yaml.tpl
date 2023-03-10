{{- $namespaces := (datasource "namespaces") }}
{{- $config := (datasource "config") }}
receivers:
  otlp:
    protocols:
      http:
        auth:
          authenticator: lumigoauth/server
        include_metadata: true # Needed by `headers_setter/lumigo`
{{- range (keys $namespaces) }}
  k8s_events/ns_{{ . }}:
    auth_type: serviceAccount
    namespaces: [ {{ . }} ]
{{ end }}

extensions:
  health_check:
  headers_setter/lumigo:
    headers:
    # Use the same authorization header as the one accompanying
    # the request received in the receiver. It needs the
    # `include_metadata: true` parameter in the `otlp` exporter
    - key: authorization
      from_context: Authorization
  lumigoauth/server:
    type: server
{{- range $key, $value := $namespaces }}
  lumigoauth/ns_{{ $key }}:
    type: client
    token: {{ $value }}
{{ end }}

exporters:
  otlphttp/lumigo:
    endpoint: {{ env.Getenv "LUMIGO_ENDPOINT" "https://ga-otlp.lumigo-tracer-edge.golumigo.com" }}
    auth:
      authenticator: headers_setter/lumigo
{{- if $config.debug }}
  logging:
    verbosity:
    sampling_initial: 1
    sampling_thereafter: 1
{{ end }}
{{- range (keys $namespaces) }}
  otlphttp/lumigo_ns_{{ . }}:
    endpoint: $LUMIGO_ENDPOINT
    auth:
      authenticator: lumigoauth/ns_{{ . }}
{{ end }}

# We cannot add the Batch processor, as it breaks the `headers_setter/lumigo`
# see https://github.com/open-telemetry/opentelemetry-collector/issues/4544
processors:
  k8sattributes:
    auth_type: serviceAccount
    passthrough: false
    extract:
      metadata:
        # Core
        - k8s.namespace.name
        - k8s.pod.name
        - k8s.pod.start_time
        - k8s.node.name
        # Apps
        - k8s.daemonset.name
        - k8s.daemonset.uid
        - k8s.deployment.name
        - k8s.replicaset.name
        - k8s.replicaset.uid
        - k8s.statefulset.name
        - k8s.statefulset.uid
        # Batch
        - k8s.cronjob.name
        - k8s.job.name
        - k8s.job.uid
    pod_association:
    - sources:
      - from: resource_attribute
        name: k8s.pod.uid

service:
  telemetry:
    logs:
      level: {{ ternary $config.debug "debug" "info" }}
  extensions:
  - headers_setter/lumigo
  - health_check
  - lumigoauth/server
{{- range (keys $namespaces) }}
  - lumigoauth/ns_{{ . }}
{{ end }}
  pipelines:
    traces:
      receivers:
      - otlp
      processors:
      - k8sattributes
      exporters:
      - otlphttp/lumigo
{{- if $config.debug }}
      - logging
{{ end }}
{{- range (keys $namespaces) }}
    logs/k8s_events_ns_{{ . }}:
      receivers:
      - k8s_events/ns_{{ . }}
      processors: []
      exporters:
      - otlphttp/lumigo_ns_{{ . }}
{{- if $config.debug }}
      - logging
{{ end }}
{{ end }}
