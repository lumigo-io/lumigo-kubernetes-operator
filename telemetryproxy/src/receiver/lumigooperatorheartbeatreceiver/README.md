# Lumigo Operator Heartbeat Receiver

| Status                   |            |
|--------------------------|------------|
| Stability                | [alpha]    |
| Supported pipeline types | logs |
| Distributions            | [lumigo-k8s] |

This receiver sends downstream a heartbeat for one namespace every hour.
The heartbeat contains the spec and status of all `lumigoes.operator.lumigo.io/v1alpha1.Lumigo` resources in the given namespace, formatted as JSON inside a log record.

### Configuration

Example configuration:

```
receiver:
    lumigooperatorheartbeat:
        namespace: <your-namespace>
```

This receiver will send the heartbeat for the `<your-namespace>` namespace.
Multiple receivers are needed to send heartbeats about multiple namespaces.
The log records will be part of the `lumigo-operator.heartbeat` scope.

### Implementation

The receiver has an internal ticker that, at its start and the top of every hour, retrieves from the Kubernetes API the spec and status of all `lumigoes.operator.lumigo.io/v1alpha1.Lumigo` resources in the given namespace, creates one log record for each, and sends it down the log pipeline.

[alpha]: https://github.com/open-telemetry/opentelemetry-collector#alpha
[lumigo-k8s]: https://github.com/lumigo-io/lumigo-kubernetes-operator/telemetryproxy
