{{- define "clusterAgent.otelCollectorConfig" -}}
receivers:
  filelog:
    include:
    {{ if .Values.clusterCollection.logs.include }}
    {{- range .Values.clusterCollection.logs.include }}
      - /var/log/pods/{{ .namespacePattern | default "*" }}_{{ .podPattern | default "*" }}_*/{{ .containerPattern | default "*" }}/*.log
    {{- end }}
    {{- else }}
      - /var/log/pods/*/*/*.log
    {{- end }}
    exclude:
      - /var/log/pods/kube-system_*/*/*.log
      - /var/log/pods/lumigo-system_*/*/*.log
      # Exclude logs from the otlp-sink in tests (Kind cluster)
      - /var/log/pods/local-path-storage_*/*/*.log
      - /var/log/pods/otlp-sink_*/*/*.log
      {{ if .Values.clusterCollection.logs.exclude }}
      {{- range .Values.clusterCollection.logs.exclude }}
      - /var/log/pods/{{ .namespacePattern | default "*" }}_{{ .podPattern | default "*" }}_*/{{ .containerPattern | default "*" }}/*.log
      {{- end }}
      {{- end }}
    start_at: end
    include_file_path: true
    include_file_name: false
    operators:
      # Find out which format is used by kubernetes
      - type: router
        id: get-format
        routes:
          - output: parser-docker
            expr: 'body matches "^\\{"'
          - output: parser-crio
            expr: 'body matches "^[^ Z]+ "'
          - output: parser-containerd
            expr: 'body matches "^[^ Z]+Z"'
      # Parse CRI-O format
      - type: regex_parser
        id: parser-crio
        regex:
          '^(?P<time>[^ Z]+) (?P<stream>stdout|stderr) (?P<logtag>[^ ]*) ?(?P<log>.*)$'
        output: overwrite-log-body
        timestamp:
          parse_from: attributes.time
          layout_type: gotime
          layout: '2006-01-02T15:04:05.999999999Z07:00'
      # Parse CRI-Containerd format
      - type: regex_parser
        id: parser-containerd
        regex:
          '^(?P<time>[^ ^Z]+Z) (?P<stream>stdout|stderr) (?P<logtag>[^ ]*) ?(?P<log>.*)$'
        output: overwrite-log-body
        timestamp:
          parse_from: attributes.time
          layout: '%Y-%m-%dT%H:%M:%S.%LZ'
      # Parse Docker format
      - type: json_parser
        id: parser-docker
        output: overwrite-log-body
        timestamp:
          parse_from: attributes.time
          layout: '%Y-%m-%dT%H:%M:%S.%LZ'
      - type: move
        id: overwrite-log-body
        from: attributes["log"]
        to: body
      # best effort attempt to parse the body as a stringified JSON object
      - type: json_parser
        parse_to: body
        on_error: send_quiet
      # Extract metadata from file path
      - type: regex_parser
        id: extract-metadata-from-filepath
        regex: '^.*\/(?P<namespace>[^_]+)_(?P<pod_name>[^_]+)_(?P<uid>[a-f0-9\-]{36})\/(?P<container_name>[^\._]+)\/(?P<restart_count>\d+)\.log$'
        parse_from: attributes["log.file.path"]
        cache:
          size: 128 # default maximum amount of Pods per Node is 110
      # Rename attributes
      - type: move
        from: attributes.stream
        to: attributes["log.iostream"]
      - type: move
        from: attributes.container_name
        to: resource["k8s.container.name"]
      - type: move
        from: attributes.namespace
        to: resource["k8s.namespace.name"]
      - type: move
        from: attributes.pod_name
        to: resource["k8s.pod.name"]
      - type: move
        from: attributes.restart_count
        to: resource["k8s.container.restart_count"]
      - type: move
        from: attributes.uid
        to: resource["k8s.pod.uid"]
exporters:
  otlphttp:
    endpoint: ${env:LUMIGO_LOGS_ENDPOINT}
    headers:
      Authorization: LumigoToken ${env:LUMIGO_TOKEN}
{{- if .Values.debug.enabled }}
  debug:
    verbosity: detailed
{{- end }}
processors:
  transform:
    log_statements:
      - context: log
        statements:
          - set(instrumentation_scope.name, "lumigo-operator.log_file_collector")
          - set(resource.attributes["k8s.cluster.name"], "${env:KUBERNETES_CLUSTER_NAME}")
  batch:
service:
{{- if .Values.debug.enabled }}
  telemetry:
    logs:
      level: debug
{{- end }}
  pipelines:
    logs:
      receivers:
        - filelog
      processors:
        - transform
        - batch
      exporters:
        - otlphttp
{{- if .Values.debug.enabled }}
        - debug
{{- end }}
{{- end }}