# Kubernetes Objects Receiver

| Status                   |           |
| ------------------------ |-----------|
| Stability                | [alpha]   |
| Supported pipeline types | logs      |
| Distributions            | [contrib] |

The kubernetes Objects receiver collects(pull/watch) objects from the Kubernetes API server.

Currently this receiver supports authentication via service accounts only.
See [example](#example) for more information.

## Getting Started

The following is example configuration

```yaml
  k8sobjects:
    auth_type: serviceAccount
    objects:
      - name: pods
        mode: pull
        label_selector: environment in (production),tier in (frontend)
        field_selector: status.phase=Running
        interval: 15m
      - name: events
        mode: watch
        group: events.k8s.io
        namespaces: [default]
```

Brief description of configuration properties:
- `auth_type` (default = `serviceAccount`): Determines how to authenticate to
the K8s API server. This can be one of `none` (for no auth), `serviceAccount`
(to use the standard service account token provided to the agent pod), or
`kubeConfig` to use credentials from `~/.kube/config`.
- `name`: Name of the resource object to collect
- `mode`: define in which way it collects this type of object, either "poll" or "watch".
  - `pull` mode will read all objects of this type use the list API at an interval.
  - `watch` mode will setup a long connection using the watch API to just get updates.
- `label_selector`: select objects by label(s)
- `field_selector`: select objects by field(s)
- `interval`: the interval at which object is pulled, default 60 minutes. Only useful for `pull` mode.
- `namespaces`: An array of `namespaces` to collect events from. (default = `all`)
- `group`: API group name. It is an optional config. When given resource object is present in multiple groups,
use this config to specify the group to select. By default, it will select the first group.
For example, `events` resource is available in both `v1` and `events.k8s.io/v1` APIGroup. In 
this case, it will select `v1` by default.


The full list of settings exposed for this receiver are documented [here](./config.go)
with detailed sample configurations [here](./testdata/config.yaml).

Follow the below sections to setup various Kubernetes resources required for the deployment.

### Configuration

Create a ConfigMap with the config for `otelcontribcol`. Replace `OTLP_ENDPOINT`
with valid value.

```bash
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: otelcontribcol
  labels:
    app: otelcontribcol
data:
  config.yaml: |
    receivers:
      k8sobjects:
        objects:
          - name: pods
            mode: pull
          - name: events
            mode: watch
    exporters:
      otlp:
        endpoint: <OTLP_ENDPOINT>
        tls:
          insecure: true

    service:
      pipelines:
        logs:
          receivers: [k8sobjects]
          exporters: [otlp]
EOF
```

### Service Account

Create a service account that the collector should use.

```bash
<<EOF | kubectl apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  labels:
    app: otelcontribcol
  name: otelcontribcol
EOF
```

### RBAC

Use the below commands to create a `ClusterRole` with required permissions and a
`ClusterRoleBinding` to grant the role to the service account created above.
Following config will work for collecting pods and events only. You need to add
appropriate rule for collecting other objects.

```bash
<<EOF | kubectl apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: otelcontribcol
  labels:
    app: otelcontribcol
rules:
- apiGroups:
  - ""
  resources:
  - events
  - pods
  verbs:
  - get
  - list
  - watch
- apiGroups: 
  - "events.k8s.io"
  resources:
  - events
  verbs:
  - watch
EOF
```

```bash
<<EOF | kubectl apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: otelcontribcol
  labels:
    app: otelcontribcol
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: otelcontribcol
subjects:
- kind: ServiceAccount
  name: otelcontribcol
  namespace: default
EOF
```

### Deployment

Create a [Deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/) to deploy the collector.
Note: This receiver must be deployed as one replica, otherwise it'll be producing duplicated data.

```bash
<<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: otelcontribcol
  labels:
    app: otelcontribcol
spec:
  replicas: 1
  selector:
    matchLabels:
      app: otelcontribcol
  template:
    metadata:
      labels:
        app: otelcontribcol
    spec:
      serviceAccountName: otelcontribcol
      containers:
      - name: otelcontribcol
        image: otelcontribcol:latest # specify image
        args: ["--config", "/etc/config/config.yaml"]
        volumeMounts:
        - name: config
          mountPath: /etc/config
        imagePullPolicy: IfNotPresent
      volumes:
        - name: config
          configMap:
            name: otelcontribcol
EOF
```

## Troubleshooting

If receiver returns error similar to below, make sure that resource is added to `ClusterRole`.
```
{"kind": "receiver", "name": "k8sobjects", "pipeline": "logs", "resource": "events.k8s.io/v1, Resource=events", "error": "unknown"}
```

[alpha]: https://github.com/open-telemetry/opentelemetry-collector#alpha
[contrib]: https://github.com/open-telemetry/opentelemetry-collector-releases/tree/main/distributions/otelcol-contrib
