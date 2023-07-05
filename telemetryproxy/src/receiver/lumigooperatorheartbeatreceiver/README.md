# Lumigo Operator Heartbeat Receiver

| Status                   |            |
|--------------------------|------------|
| Stability                | [alpha]    |
| Supported pipeline types | logs |
| Distributions            | [lumigo-k8s] |

This receiver sends heartbeat to lumigo every hour


### Configuration

Example configuration:

```
    receiver:
        lumigooperatorheartbeat:
            namespace: <your-namespace>
```

This will create a pipeline to send usage heartbeat for a single namespace. If you want to send heartbeats for multiple namespaces, you need to create multiple pipelines.


### Implementation

The receiver has an internal ticket that ticks in every top of the hour, gets the data from kubernetes API, creates log records, and sends it down the log pipeline.