# Kubernetes Data Enhancer Processor

| Status                   |              |
|--------------------------|--------------|
| Stability                | [alpha]      |
| Supported pipeline types | logs, traces |
| Distributions            | [lumigo-k8s] |

This processor ensures that we send a variety of necessary data to the downstream OpenTelemetry endpoint.

## Enhancements

### Trace data

For trace data, it ensures that the following resource attributes are set:

* `k8s.cluster.uid` with the UID of the `kube-system` namespace

* `k8s.pod.name`
* `k8s.pod.uid` (this is should be provided by the Lumigo OpenTelemetry distros through resource detectors, but this processor has a fallback implementation of pod identification based on the ip address of the connection to the collector, borrowed from [`k8sattributesprocessor`](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/k8sattributesprocessor))
* `k8s.namespace.name`
* `k8s.namespace.uid`

Other attributes are set depending on the owner-reference chain originating in the pod.

If the the pod belongs to a daemon set, the following attributes are set:

* `k8s.daemonset.name`
* `k8s.daemonset.uid`

If the the pod belongs to a replica set, the following attributes are set:

* `k8s.replicaset.name`
* `k8s.replicaset.uid`

If the pod's replica set is owned by a deployment, the following attributes are set:

* `k8s.deployment.name`
* `k8s.deployment.uid`

If the the pod belongs to a stateful set, the following attributes are set:

* `k8s.stateful.name`
* `k8s.stateful.uid`

If the the pod belongs to a job, the following attributes are set:

* `k8s.job.name`
* `k8s.job.uid`

If the pod's job is owned by a cronjob, the following attributes are set:

* `k8s.cronjob.name`
* `k8s.cronjob.uid`

These capabilities (and more) are also nominally present in the [`k8sattributesprocessor`](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/k8sattributesprocessor), but our tests hsowed that to be entirely unreliable in the face of the Kube API's eventual consistency and the configuration reload of the telemetry-proxy.

### Log data

The goal is to ensure we can have smooth and easy correlation of issues (e.g., `v1.Event` objects with `WARNING` type) with the history of the objects they affect (which, in `v1.Event` terms, it's the object referenced to by the `involvedObject` field).
Here we do two things:

1. Add the `k8s.cluster.uid` resource attribute with the UID of the `kube-system` namespace

1. Add the `rootOwnerReference` field of type `v1.ObjectReference` to `v1.Event` that stream through `k8sdataenricherprocessor` in the format generated by [`k8sobjectsreceiver`](https://github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sobjectsreceiver).
  The `rootOwnerReference` is the [workload object](https://kubernetes.io/docs/concepts/workloads/) at the top of the hierarchy of the event's `involvedObject`.
  For example, if the `involvedObject` points to a `v1.Pod`, and the pod is scheduled by a `v1.ReplicaSet` owned by a `v1.Deployment`, the `rootOwnerReference` is the deployment.
  In cases in which the hierarchy has only one level (e.g., an event affecting a deployment or a standalone pod), the `rootOwnerReference` will be equivalent to the `involvedObject`.
  In Lumigo, the `rootOwnerReference` allows us to group events into issues that make immediate sense for the user: for example, if multiple pods in the same deployment crash, the issue is almost always to be found in its [`v1.PodTemplateSpec`](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-template-v1/#PodTemplateSpec) rather than the single pods.

1. If a specific [`resourceVersion`](https://kubernetes.io/docs/reference/using-api/api-concepts/#efficient-detection-of-changes) of an object is sent downstream through as an `involvedObject` of an `v1.Event`, this processor will try _really hard_ to ensure that that specific `resourceVersion` is sent downstream in its entirety in the same format as used by [`k8sobjectsreceiver`](https://github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sobjectsreceiver).
  In the Lumigo backend, this allows us to bridge the gap between `resourceVersions` and [`generations`](https://stackoverflow.com/a/66092577/6188451) (that is, the "versions" of the workload resources with changes that actually affect which pods are scheduled), and know what was the actual structure of an object affected by an issue.

## Implementation

This processor was hard to come up with for a number of reasons:
1. The Kubernetes API is eventually consistent, yet the Lumigo backend wants strong consistency of data (e.g., all the needed `resourceVersion`s)
2. The `resourceVersion`s of an object are around only for a very limited amount of time (it seems on most Kubernetes clusters around 5 minutes), after which they can get garbage-collected; so, collecting the ones we are not sure we sent downstream is a time-sensitive issue.
3. When the Lumigo operator sees a new namespace being monitored, or the stop of monitoring a namespace, the configuration of the entire `telemetry-proxy` is reloaded; for "normal" processors, that means they get re-instantiated, and whatever caches we would be normally lost.

The solution is to keep a (1) single [Kubernetes watch](https://kubernetes.io/docs/reference/using-api/api-concepts/) stream per type of object we care about, i.e.:
* [`v1/Pod`](https://kubernetes.io/docs/concepts/workloads/pods/)
* [`apps/v1.Deployment`](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/)
* [`apps/v1.DaemonSet`](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/)
* [`apps/v1.ReplicaSet`](https://kubernetes.io/docs/concepts/workloads/controllers/replicaset/)
* [`apps/v1.StatefulSet`](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
* [`batch/v1.CronJob`](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/)
* [`batch/v1.Job`](https://kubernetes.io/docs/concepts/workloads/controllers/job/)

The watch is not dependent on the lifecycle of the instances of `k8sdataenricherprocessor` that use it.
It effectively stops only when the `telemetry-proxy` process is shutdown.
This way, the caches are not lost on configuration reloads.

The data we get is stored in LRU caches that use a combination of the object UID and `resourceVersion` as cache keys.
As logs issued by the [`k8sobjectsreceiver`](https://github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sobjectsreceiver), we also add those objects and `resouerceVersion`s into the cache, so that we have fewer misses.
When we need a `resourceVersion` we do not have and that we need to stream to the backend, we go and immediately get it from the Kube API, using retries with exponential backoffs to avoid failing when the `resourceVersion` still exists in the system, but is now yet available via the API (which is surprisingly common for events about workload objects being created or modified).

[alpha]: https://github.com/open-telemetry/opentelemetry-collector#alpha
[lumigo-k8s]: https://github.com/lumigo-io/lumigo-kubernetes-operator/telemetryproxy
