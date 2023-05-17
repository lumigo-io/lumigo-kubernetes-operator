kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    extraMounts:
      - hostPath: '{{OTLP_SINK_CONFIG_VOLUME_PATH}}'
        containerPath: /lumigo/otlp-sink/config
      - hostPath: '{{OTLP_SINK_DATA_VOLUME_PATH}}'
        containerPath: /lumigo/otlp-sink/data