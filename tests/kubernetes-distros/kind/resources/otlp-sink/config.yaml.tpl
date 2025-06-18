{{- $config := (datasource "config") -}}
receivers:
  otlp:
    protocols:
      http:
        endpoint: "0.0.0.0:${env:OTLP_PORT}"

exporters:
  debug:
    verbosity: detailed
{{- if $config.lumigo_token }}
  otlphttp/lumigo:
    endpoint: {{ $config.lumigo_endpoint }}
    headers:
      Authorization: "LumigoToken {{ $config.lumigo_token }}"
{{ end }}
  file/logs:
    path: ${env:LOGS_PATH}
  file/traces:
    path: ${env:TRACES_PATH}
  file/metrics:
    path: ${env:METRICS_PATH}
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
      - debug