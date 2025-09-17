kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
#    labels:
#      node-type: really-good
    extraMounts:
      - hostPath: '{{OTLP_SINK_CONFIG_VOLUME_PATH}}'
        containerPath: /lumigo/otlp-sink/config
      - hostPath: '{{OTLP_SINK_DATA_VOLUME_PATH}}'
        containerPath: /lumigo/otlp-sink/data
    kubeadmConfigPatches:
    - |
      kind: InitConfiguration
      nodeRegistration:
        kubeletExtraArgs:
          system-reserved: memory=4Gi