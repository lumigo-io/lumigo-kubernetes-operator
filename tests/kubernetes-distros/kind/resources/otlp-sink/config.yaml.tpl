{{- $config := (datasource "config") -}}
receivers:
  otlp:
    protocols:
      http:
        endpoint: "0.0.0.0:${OTLP_PORT}"

exporters:
  logging:
    loglevel: debug
{{- if $config.lumigo_token }}
  otlphttp/lumigo:
    endpoint: {{ $config.lumigo_endpoint }}
    headers:
      Authorization: "LumigoToken {{ $config.lumigo_token }}"
{{ end }}
  file/logs:
    path: ${LOGS_PATH}
  file/traces:
    path: ${TRACES_PATH}
  file/metrics:
    path: ${METRICS_PATH}
service:
  pipelines:
    logs:
      receivers:
      - otlp
      exporters:
      - file/logs
{{- if $config.lumigo_token }}
      - otlphttp/lumigo
{{ end }}
    traces:
      receivers:
      - otlp
      exporters:
      - file/traces
{{- if $config.lumigo_token }}
      - otlphttp/lumigo
{{ end }}
    metrics:
      receivers:
      - otlp
      exporters:
      - file/metrics
      - logging