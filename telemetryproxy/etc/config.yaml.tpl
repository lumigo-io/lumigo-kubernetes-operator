{{- $namespaces := (datasource "namespaces") -}}
{{- $config := (datasource "config") -}}
receivers:
  otlp:
    protocols:
      http:
        auth:
          authenticator: lumigoauth/server
        include_metadata: true # Needed by `headers_setter/lumigo`
{{- range $i, $namespace := $namespaces }}
  k8sobjects/ns_{{ $namespace.name }}:
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
    - name: events
      mode: {{ $mode }}
      interval: 10m
      namespaces: [ {{ $namespace.name }} ]
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
{{- if $config.debug }}
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
  transform/inject_nsuid_into_trace_resources:
    trace_statements:
    - context: resource
      statements:
{{- range $i, $namespace := $namespaces }}
      - set(attributes["k8s.namespace.uid"], "{{ $namespace.uid }}") where attributes["k8s.namespace.name"] == "{{ $namespace.name }}"
{{- end }}

{{- range $i, $namespace := $namespaces }}
  transform/inject_nsuid_ns_{{ $namespace.name }}:
    log_statements:
    - context: log
      statements:
      - set(attributes["k8s.namespace.name"], "{{ $namespace.name }}")
      - set(attributes["k8s.namespace.uid"], "{{ $namespace.uid }}")
{{- end }}

service:
  telemetry:
    logs:
      level: {{ ternary $config.debug "debug" "info" }}
  extensions:
  - headers_setter/lumigo
  - health_check
  - lumigoauth/server
{{- range $i, $namespace := $namespaces }}
  - lumigoauth/ns_{{ $namespace.name }}
{{- end }}
  pipelines:
    traces:
      # We cannot add a Batch processor to this pipeline as it would break the
      # `headers_setter/lumigo` extension.
      # See https://github.com/open-telemetry/opentelemetry-collector/issues/4544
      receivers:
      - otlp
      processors:
      - k8sattributes
      - transform/inject_nsuid_into_trace_resources
      exporters:
      - otlphttp/lumigo
{{- if $config.debug }}
      - logging
{{- end }}
{{- range $i, $namespace := $namespaces }}
    logs/k8s_objects_ns_{{ $namespace.name }}:
      receivers:
      - k8sobjects/ns_{{ $namespace.name }}
      processors:
      - transform/inject_nsuid_ns_{{ $namespace.name }}
      exporters:
{{- if $config.debug }}
      - logging
{{- end }}
      - otlphttp/lumigo_ns_{{ $namespace.name }}
{{ end }}
