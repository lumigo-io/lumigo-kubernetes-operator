# Lumigo Kubernetes Operator

![The Lumigo Logo](./images/lumigo.png)

[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/lumigo-operator)](https://artifacthub.io/packages/search?repo=lumigo-operator)

The Kubernetes operator of Lumigo provides a one-click solution to monitoring Kubernetes clusters with Lumigo.

## Setup

### Installation

Install the Lumigo Kubernetes operator in your Kubernets cluster with [helm](https://helm.sh/), with one of the following methods:

#### Install & monitor namespaces

The following command installs the operator and immediately applies monitoring to the specified namespaces, with traces or logs enabled or disabled as specified (defaulting to `true` for both):

```sh
helm repo add lumigo https://lumigo-io.github.io/lumigo-kubernetes-operator && \
helm repo update && \
echo "
cluster:
  name: <cluster name>
lumigoToken:
  value: <Lumigo token>
monitoredNamespaces:
  - namespace: <namespace>
    loggingEnabled: true
    tracingEnabled: true
  - namespace: <namespace>
    loggingEnabled: false
    tracingEnabled: true
" | helm upgrade -i lumigo lumigo/lumigo-operator --namespace lumigo-system --create-namespace --values -
```

**Notes**
1. adding additional namespace(s) can be done by re-running the command above with the only additional namespace(s) added to the `monitoredNamespaces` list (no need to re-include the ones from previous runs).
2. Opting out from Lumigo monitoring a namespace specified in the `monitoredNamespaces` list is explained in the [#### Remove injection from existing resources](#remove-injection-from-existing-resources) section.
3. Using `monitoredNamespaces=all` allows the Lumigo Kubernetes operator to monitor **all** the namespaces in the cluster, with traces and logs enabled by default. It is strongly advisable to be used only in non-production environments, as it may result in many workloads being restarted over short time period causing spikes of CPU and memory consumption.

#### Install only

The following command installs the operator but requires you to create a secret and a Lumigo CRD per each monitored namespace, as described in the [Enabling automatic tracing](#enabling-automatic-tracing) section:

```sh
helm repo add lumigo https://lumigo-io.github.io/lumigo-kubernetes-operator && \
helm upgrade -i lumigo lumigo/lumigo-operator \
  --namespace lumigo-system \
  --create-namespace \
  --set cluster.name=<cluster_name> \
  --set lumigoToken.value=<token>
```

**Notes:**
1. You have the option to alter the namespace from `lumigo-system` to a name of your choosing, but its important to be aware that doing so might cause slight discrepancies throughout the steps below.
2. The `lumigoToken.value` is optional, but is highly recommended in order properly populate the cluster overview info in the Lumigo platform and have many other K8s-sourced metrics reported automatically. You can use the token from any Lumigo project, and cluster-wide metrics will be forwarded to it once the installation is complete.
3. The `cluster.name` is optional, but highly advised, see the [Naming your cluster](#naming-your-cluster) section.

You can verify that the Lumigo Kubernetes operator is up and running with:

```sh
$ kubectl get pods -n lumigo-system
NAME                                                         READY   STATUS    RESTARTS   AGE
lumigo-kubernetes-operator-7fc8f67bcc-ffh5k   2/2     Running   0          56s
```

**Note:** While installing the Lumigo Kubernetes operator via [`kustomize`](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/kustomization/) is generally expected to work (except the [uninstallation of instrumentation on removal](#remove-injection-from-existing-resources)), it is not actually supported[^1].

#### EKS on Fargate

On EKS, the pods of the Lumigo Kubernetes operator itself need to be running on nodes running on Amazon EC2 virtual machines.
Your monitored applications, however, can run on the Fargate profile without any issues.
Installing the Lumigo Kubernetes operator on an EKS cluster without EC2-backed nodegroups, results in the operator pods staying in `Pending` state:

```sh
$ kubectl describe pod -n lumigo-system lumigo-kubernetes-operator-5999997fb7-cvg5h

Namespace:    	lumigo-system
Priority:     	0
Service Account:  lumigo-kubernetes-operator
Node:         	<none>
Labels:       	app.kubernetes.io/instance=lumigo
              	app.kubernetes.io/name=lumigo-operator
              	control-plane=controller-manager
              	lumigo.auto-trace=false
              	lumigo.cert-digest=dJTiBDRVJUSUZJQ
              	pod-template-hash=5999997fb7
Annotations:  	kubectl.kubernetes.io/default-container: manager
              	kubernetes.io/psp: eks.privileged
Status:       	Pending
```

(The reason for this limitation is very long story, but it is necessary for Lumigo to figure out which EKS cluster is the operator sending data from.)
If you are installing the Lumigo Kubernetes operator on an EKS cluster with only the Fargate profile, [add a managed nodegroup](https://docs.aws.amazon.com/eks/latest/userguide/create-managed-node-group.html).

#### Naming your cluster

Kubernetes clusters does not have a built-in nothing of their identity[^1], but when running multiple Kubernetes clusters, you almost certainly have names from them.
The Lumigo Kubernetes operator will automatically add to your telemetry the `k8s.cluster.uid` OpenTelemetry resource attribute, set to the value of the UID of the `kube-system` namespace, but UIDs are not meant for humans to remember and recognize easily.
The Lumigo Kubernetes operator allows you to set a human-readable name using the `cluster.name` Helm setting, which enables you to filter all your tracing data based on the cluster in [Lumigo's Explore view](https://docs.lumigo.io/docs/explore).

[^1] Not even Amazon EKS clusters, as their ARN is not available anywhere inside the cluster itself.


### Upgrading

You can check which version of the Lumigo Kubernetes operator you have deployed in your cluster as follows:

```sh
$ helm ls -A
NAME  	NAMESPACE    	REVISION	UPDATED                              	STATUS  	CHART             	APP VERSION
lumigo	lumigo-system	2       	2023-07-10 09:20:04.233825 +0200 CEST	deployed	lumigo-operator-13	13
```

The Lumigo Kubernetes operator is reported as `APP VERSION`.

To upgrade to a newer version of the Lumigo Kubernetes operator, run:

```sh
helm repo update
helm upgrade lumigo lumigo/lumigo-operator --namespace lumigo-system
```
### Tracing, logging and metrics methods

#### Tracing

|            | Scoping                                                                          | Correlation               | Reusable for other features                                          | Mutates pods                                                         |
|------------|----------------------------------------------------------------------------------|---------------------------|----------------------------------------------------------------------|----------------------------------------------------------------------|
| [Lumigo CRD](#enabling-automatic-tracing) | Traces from different namespaces can be reported to different projects in Lumigo | Correlates traces to logs | The same CRD can be also used to apply logging for a given namespace | Pods are mutated to add auto-instrumentation for supported libraries |
| [Resource Labels](#opting-in-for-specific-resources) | More granular control - can target specific resources without affecting the entire namespace | Correlates traces to logs | Separate labels can be used for both logging and tracing | Pods from labeled resources only are mutated to add auto-instrumentation for supported libraries |

#### Logging

|                           | Scoping                                                                        | Correlation               | Reusable for other features                                          | K8s context per log line                                                                                                   | Mutates pods                                                       | Supported runtimes and loggers                                                     |
|---------------------------|--------------------------------------------------------------------------------|---------------------------|----------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------|------------------------------------------------------------------------------------|
| [Lumigo CRD](#logging-support)                | Logs from all resources in a namespace are reported to the same project in Lumigo | Correlates logs to traces | The same CRD can be also used to apply tracing for a given namespace | Each log line shows the entire resource change from container and pod to the parent resource (Deployment, Daemonset, etc). | Pods are mutated to add auto-instrumentation for supported loggers | Python, Node and Java <br>(with a selection of supported loggers for each runtime) |
| [Resource Labels](#opting-in-for-specific-resources) | Logs from different resources can be reported to different projects in Lumigo | Correlates logs to traces | Separate labels can be used for both logging and tracing | Each log line shows the entire resource change from container and pod to the parent resource (Deployment, Daemonset, etc). | Pods from labeled resources only are mutated to add auto-instrumentation for supported libraries | Python, Node and Java (with a selection of supported loggers for each runtime)
| [Container file collection](#fetching-container-logs-via-files) | All logs are reported to a single Lumigo project                                   | X                         | X                                                                    | Each log line shows only the source container, pod and namespace.                                                          | X                                                                  | Full support - all logs from all runtimes and logging libraries are collected      |

#### Metrics

Metrics are only available at the cluster level at the moment (i.e. infrastrucute metrics and not application metrics), and are enabled by default assuming you either set `lumigoToken.value` in the Helm values, or reference an existing Kubernetes secret.
By default, only metrics essential to the Lumigo K8s functionality are collected, but you can enable additional metrics by setting the `clusterCollection.metrics.essentialOnly` field to `false` in the Helm values during installation.

### Enabling automatic tracing

#### Supported resource types

The Lumigo Kubernetes operator automatically adds distributed tracing to pods created via:

* Deployments ([`apps/v1.Deployment`](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/))
* Daemonsets ([`apps/v1.DaemonSet`](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/))
* ReplicaSets ([`apps/v1.ReplicaSet`](https://kubernetes.io/docs/concepts/workloads/controllers/replicaset/))
* StatefulSets ([`apps/v1.StatefulSet`](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/))
* CronJobs ([`batch/v1.CronJob`](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/))
* Jobs ([`batch/v1.Job`](https://kubernetes.io/docs/concepts/workloads/controllers/job/))

The distributed tracing is provided by the [Lumigo OpenTelemetry distribution for JS](https://github.com/lumigo-io/opentelemetry-js-distro), the [Lumigo OpenTelemetry distribution for Java](https://github.com/lumigo-io/opentelemetry-java-distro) and the [Lumigo OpenTelemetry distribution for Python](https://github.com/lumigo-io/opentelemetry-python-distro).

The Lumigo Kubernetes operator will automatically trace all Java, Node.js and Python processes found in the containers of pods created in the namespaces that Lumigo traces.
To activate automatic tracing for resources in a namespace, create in that namespace a Kubernetes secret containing your [Lumigo token](https://docs.lumigo.io/docs/lumigo-tokens), and reference it from a `Lumigo` (`operator.lumigo.io/v1alpha1.Lumigo`) custom resource.
Save the following into the `lumigo.yml`:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: lumigo-credentials
stringData:
  # Kubectl won't allow you to deploy this dangling anchor.
  # Get the actual value from Lumigo following this documentation: https://docs.lumigo.io/docs/lumigo-tokens
  token: *lumigo-token #  <--- Change this! Example: t_123456789012345678901
---
apiVersion: operator.lumigo.io/v1alpha1
kind: Lumigo
metadata:
  labels:
    app.kubernetes.io/name: lumigo
    app.kubernetes.io/instance: lumigo
    app.kubernetes.io/part-of: lumigo-operator
  name: lumigo
spec:
  lumigoToken:
    secretRef:
      name: lumigo-credentials # This must match the name of the secret; the secret must be in the same namespace as this Lumigo custom resource
      key: token # This must match the key in the Kubernetes secret (don't touch)
```

After creating `lumigo.yml`, deploy it in the desired namespace:

```sh
kubectl apply -f lumigo.yml -n <YOUR_NAMESPACE>
```

> ℹ️ **Important note**
>
> Apply the secret and the custom resource to the namespace you wish to start tracing, not to `lumigo-system`.


Each `Lumigo` resource keeps in its state a list of resources it currently instruments:

```yaml
$ kubectl describe lumigo -n my-namespace
Name:         lumigo
Namespace:    my-namespace
API Version:  operator.lumigo.io/v1alpha1
Kind:         Lumigo
Metadata:
  ... # Data removed for readability
Spec:
  ... # Data removed for readability
Status:
  Conditions:
  ... # Data removed for readability
  Instrumented Resources:
    API Version:       apps/v1
    Kind:              StatefulSet
    Name:              my-statefulset
    Namespace:         my-namespace
    Resource Version:  320123
    UID:               93d6d809-ac2a-43a9-bc07-f0d4e314efcc
```

#### Disabling automatic tracing

The tracing feature can be entirely turned-off in case it's not desired, by setting the `spec.tracing.enabled` field to `false` in the `Lumigo` resource:

```yaml
apiVersion: operator.lumigo.io/v1alpha1
kind: Lumigo
metadata:
  labels:
    app.kubernetes.io/name: lumigo
    app.kubernetes.io/instance: lumigo
    app.kubernetes.io/part-of: lumigo-operator
  name: lumigo
spec:
  lumigoToken: ...
  tracing:
    enabled: false
  logging:
    enabled: true # usually set to when `tracing.enabled` is `false`, otherwise injecting Lumigo into the workload will not be useful
```

this is usually done when only logs from the workloads should be sent to Lumigo, regardless of the tracing content in which the logs was generated.
* Note that this does not affect the injection of the Lumigo distro into pods - only the fact that the distro will not send traces to Lumigo.
For more fine-grained control over the injection in general, see the [Opting out for specific resources](#opting-out-for-specific-resources) section.

#### Logging support

The Lumigo Kubernetes operator can automatically forward logs emitted by traced pods to [Lumigo's log-management solution](https://lumigo.io/lp/log-management/), supporting several logging providers (currently `logging` for Python apps, `Winston` and `Bunyan` for Node.js apps).
Enabling log forwarding is done by adding the `spec.logging.enabled` field to the `Lumigo` resource:

```yaml
apiVersion: operator.lumigo.io/v1alpha1
kind: Lumigo
metadata:
  labels:
    app.kubernetes.io/name: lumigo
    app.kubernetes.io/instance: lumigo
    app.kubernetes.io/part-of: lumigo-operator
  name: lumigo
spec:
  lumigoToken: ... # same token used for tracing
  logging:
    enabled: true # enables log forwarding for pods with tracing injected
```

#### Fetching container logs via files

Workloads that are using runtimes not supported by current Lumigo OTEL distro (e.g. Go, Rust) can still send logs to Lumigo, via logs files from containers that k8s manages on each node in the cluster.
The Lumigo Kubernetes operator will automatically collect logs from those files and send them to Lumigo, once the following setting is applied when installing the operator:

```sh
helm upgrade -i lumigo lumigo/lumigo-operator \
  # ...
  --set "clusterCollection.logs.enabled=true"
  --set "lumigoToken.value=<your Lumigo token>"
```

this will automatically collect logs from the file `/var/log/pods` folder in each node, and forward them to Lumigo (with the exception of the `kube-system` and `lumigo-system` namespaces).
To further customize the workloads patterns for log collection, the following settings can be provided:

```sh
echo "
lumigoToken:
  value: <your Lumigo token>
clusterCollection:
  logs:
    enabled: true
    include:
      - namespacePattern: some-ns
        podPattern: some-pod-*
        containerPattern: some-container-*
    exclude:
      - containerPattern: some-other-container-*
" | helm upgrade -i lumigo lumigo/lumigo-operator --values -
```
In the example above, logs from all containers prefixed with `some-container-` running in pods prefixed with `some-pod-` (effectively, pods from a specific deployment) under the `some-ns` namespace will be collected, with the exception of logs from containers prefixed with `some-other-container-` from the aforementioned namespace and pods.

Notes about the settings:
1. `include` and `exclude` are arrays of glob patterns to include or exclude logs, where each pattern being a combination of `namespacePattern`, `podPattern` and `containerPattern` (all are optional).
2. If a pattern is not provided for one of the components, it will be considered as a wildcard pattern - e.g. including pods while specifying `podPattern` will include all containers of those pods in all namespaces.
3. Each `exclude` value is checked against the paths matched by `include`, meaning if a path is matched by both `include` and `exclude`, it will be excluded.
4. By default, all logs from all pods in all namespaces are included, with no exclusions. Exceptions are the `kube-system` and `lumigo-system` namespaces, that will be always added to the default or provided exclusion list.

#### Opting out for specific resources

To prevent the Lumigo Kubernetes operator from injecting tracing to pods managed by some resource in a namespace that contains a `Lumigo` resource, add the `lumigo.auto-trace` label set to `false`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: hello-node
    lumigo.auto-trace: "false"  # <-- No injection will take place
  name: hello-node
  namespace: my-namespace
spec:
  selector:
    matchLabels:
      app: hello-node
```

In the logs of the Lumigo Kubernetes operator, you will see a message like the following:

```
1.67534267851615e+09    DEBUG   controller-runtime.webhook.webhooks   wrote response   {"webhook": "/v1alpha1/inject", "code": 200, "reason": "the resource has the 'lumigo.auto-trace' label set to 'false'; resource will not be mutated", "UID": "6d341941-c47b-4245-8814-1913cee6719f", "allowed": true}
```

#### Opting in for specific resources

Instead of monitoring an entire namespace using a Lumigo CR, you can selectively opt in individual resources by adding the `lumigo.auto-trace` label set to `true`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: hello-node
    lumigo.auto-trace: "true"  # <-- Enable tracing just for this resource
    lumigo.token-secret: "my-lumigo-secret"  # <-- Optional, defaults to "lumigo-credentials"
    lumigo.token-key: "my-token-key"  # <-- Optional, defaults to "token"
    lumigo.enable-traces: "true"  # <-- Optional, controls whether traces are sent (defaults to true)
    lumigo.enable-logs: "false"  # <-- Optional, controls whether logs are sent (defaults to false)
  name: hello-node
  namespace: my-namespace
spec:
  selector:
    matchLabels:
      app: hello-node
  template:
    metadata:
      labels:
        app: hello-node
    spec:
      containers:
      - command:
        - /agnhost
        - netexec
        - --http-port=8080
        image: registry.k8s.io/e2e-test-images/agnhost:2.39
        name: agnhost
```

This approach allows you to:
- Be selective about which resources to monitor without having to create a Lumigo CR
- Apply tracing to specific resources across different namespaces
- Have more granular control over your instrumentation strategy

When using resource labels for targeted tracing, you'll need a Kubernetes secret containing your Lumigo token in the same namespace. The following labels provide full control over the instrumentation:

- `lumigo.auto-trace`: When set to `"true"`, enables Lumigo instrumentation for the resource
- `lumigo.token-secret`: Specifies the name of the secret containing the Lumigo token (defaults to `lumigo-credentials`)
- `lumigo.token-key`: Specifies the key in the secret where the token is stored (defaults to `token`)
- `lumigo.enable-traces`: Controls whether traces are sent to Lumigo (defaults to `"true"`)
- `lumigo.enable-logs`: Controls whether logs are sent to Lumigo (defaults to `"false"`)

**Important notes:**
1. When a Lumigo CR exists in the namespace, it takes precedence over the `lumigo.auto-trace` label when set to `true`. The label will only be respected when set to `false` to opt out specific resources.
2. The secret referenced by the labels must exist in the same namespace as the labeled resource.
3. Events are not supported for injection via resource labels. If you're interested in collecting events for that resource, you can do so by creating a Lumigo CR in the same namespace, which will automatically collect events for all resources in that namespace.


### Settings

#### Inject existing resources

By default, when detecting a new Lumigo resource in a namespace, the Lumigo controller will instrument existing resources of the [supported types](#supported-resource-types).
The injection will cause new pods to be created for daemonsets, deployments, replicasets, statefulsets and jobs; cronjobs will spawn injected pods at the next iteration.
To turn off the automatic injection of existing resources, create the Lumigo resource as follows

```yaml
apiVersion: operator.lumigo.io/v1alpha1
kind: Lumigo
metadata:
  labels:
    app.kubernetes.io/name: lumigo
    app.kubernetes.io/instance: lumigo
    app.kubernetes.io/part-of: lumigo-operator
  name: lumigo
spec:
  lumigoToken: ...
  tracing:
    injection:
      injectLumigoIntoExistingResourcesOnCreation: false # Default: true
```

#### Remove injection from existing resources

To opt-out from Lumigo monitoring a namespace, you can delete the Lumigo resource from that namespace along with its corresponding secret:
```sh
kubectl delete lumigo --all --namespace <monitored namespace>
kubectl delete secret lumigo-credentials --namespace <monitored namespace>
```

By default, when detecting the deletion of the Lumigo resource in a namespace, the Lumigo controller will remove instrumentation from existing resources of the [supported types](#supported-resource-types).
The injection will cause new pods to be created for daemonsets, deployments, replicasets, statefulsets and jobs; cronjobs will spawn non-injected pods at the next iteration.
To turn off the automatic removal of injection from existing resources, create the Lumigo resource as follows

```yaml
apiVersion: operator.lumigo.io/v1alpha1
kind: Lumigo
metadata:
  labels:
    app.kubernetes.io/name: lumigo
    app.kubernetes.io/instance: lumigo
    app.kubernetes.io/part-of: lumigo-operator
  name: lumigo
spec:
  lumigoToken: ...
  tracing:
    injection:
      removeLumigoFromResourcesOnDeletion: false # Default: true
```

**Note:** The removal of injection from existing resources does not occur on uninstallation of the Lumigo Kubernetes operator, as the role-based access control is has likely already been deleted.

#### Collection of Kubernetes objects

The Lumigo Kubernetes operator will automatically collect Kubernetes object versions in the namespaces with a `Lumigo` resource in active state, and send them to Lumigo for issue detection (e.g., when you pods crash).
The collected object types are: `corev1.Events`, `corev1.Pods`, `appsv1.Deployments`, `apps/v1.DaemonSet`, `apps/v1.ReplicaSet`, `apps/v1.StatefulSet`, `batch/v1.CronJob`, and `batch/v1.Job`.
Besides events, the object versions, e.g., pods, replicasets and deployments, are needed to be able to correlate events across the [owner-reference chain](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/), e.g., the pod belongs to that replicaset, which belongs to that deployment.

To _disable_ the automated collection of Kubernetes events and object versions, you can configure your `Lumigo` resources as follows:

```yaml
apiVersion: operator.lumigo.io/v1alpha1
kind: Lumigo
metadata:
  labels:
    app.kubernetes.io/name: lumigo
    app.kubernetes.io/instance: lumigo
    app.kubernetes.io/part-of: lumigo-operator
  name: lumigo
spec:
  lumigoToken: ...
  infrastructure:
    kubeEvents:
      enabled: false # Default: true
```

When a `Lumigo` resource is deleted from a namespace, the collection of Kubernetes events and object versions is automatically halted.

#### Modify manager log level

By default, the manager will log all `INFO` level and above logs.

The current log level can be viewed by running:

```bash
kubectl -n lumigo-system get deploy lumigo-lumigo-operator-controller-manager -o=json | jq '.spec.template.spec.containers[0].args'
```

With the default settings, there will be no log level explicitly set and the above command will return:

```bash
[
  "--health-probe-bind-address=:8081",
  "--metrics-bind-address=127.0.0.1:8080",
  "--leader-elect"
]
```

To set the log level to only show `ERROR` level logs, run:

```bash
kubectl -n lumigo-system patch deploy lumigo-lumigo-operator-controller-manager --type=json -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--zap-log-level=error"}]'
```

If a log level is already set, instead of using the `add` operation we use `replace` and modify the path from `/args/-` to the index of containing the log level setting, such as `/args/3`:

```bash
kubectl -n lumigo-system patch deploy lumigo-lumigo-operator-controller-manager --type=json -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/args/3", "value": "--zap-log-level=info"}]'
```

NOTE: The container argument array is zero indexed, so the first argument is at index 0.

### Watchdog

The Lumigo Kubernetes operator includes a watchdog component that collects internal metrics and events for Lumigo's internal use. These metrics and events are not available to customers and are used solely by Lumigo for support, troubleshooting, and monitoring the operator's health. The watchdog is enabled by default and collects:

- OpenTelemetry collector internal metrics
- CPU and memory metrics from the operator containers in the `lumigo-system` namespace (or the namespace specified during installation)
- Events from the operator namespace for troubleshooting purposes

The collection frequency for both OpenTelemetry collector internal metrics and container CPU/memory metrics is set to 15 seconds by default. This can be adjusted using the following Helm settings:

```sh
helm upgrade -i lumigo lumigo/lumigo-operator \
  --set watchdog.otelCollector.internalMetricsFrequency=30s \
  --set watchdog.watchers.top.frequency=30s
```

While the watchdog can be disabled using the `watchdog.enabled` Helm setting, this is not recommended as it may impact Lumigo's ability to provide support and troubleshoot issues:

```sh
helm upgrade -i lumigo lumigo/lumigo-operator \
  --set watchdog.enabled=false
```

### Uninstall

The removal of the Lumigo Kubernetes operator is performed by:

```sh
helm delete lumigo --namespace lumigo-system
```

In namespaces with the Lumigo resource having `spec.tracing.injection.enabled` and `spec.tracing.injection.removeLumigoFromResourcesOnDeletion` both set to `true`, [supported resources](#supported-resource-types) that have been injected by the Lumigo Kubernetes operator will be updated to remove the injection, with the following caveat:

**Note:** The removal of injection from existing resources does not apply to `batchv1.Job` resources, as their `corev1.PodSpec` is immutable after the `batchv1.Job` resource has been created.

## TLS certificates

The Lumigo Kubernetes operator injector webhook uses a self-signed certificate that is automatically generate during the [installation of the Helm chart](#installing-the-lumigo-operator).
The generated certificate has a 365 days expiration, and a new certificate will be generated every time you upgrade Lumigo Kubernetes operator's helm chart.

## Events

The Lumigo Kubernetes operator will add events to the resources it instruments with the following reasons and in the following cases:

| Reason | Created on resource types | Under which conditions |
|--------|---------------------|------------------------|
| `LumigoAddedInstrumentation` | `apps/v1.Deployment`, `apps/v1.DaemonSet`, `apps/v1.ReplicaSet`, `apps/v1.StatefulSet`, `batch/v1.CronJob` | If a Lumigo resources exists in the namespace, and the resource is instrumented with Lumigo as a result |
| `LumigoCannotAddInstrumentation` | `apps/v1.Deployment`, `apps/v1.DaemonSet`, `apps/v1.ReplicaSet`, `apps/v1.StatefulSet`, `batch/v1.CronJob` | If a Lumigo resources exists in the namespace, and the resource _should_ be instrumented by Lumigo as a result, but an error occurs |
| `LumigoUpdatedInstrumentation` | `apps/v1.Deployment`, `apps/v1.DaemonSet`, `apps/v1.ReplicaSet`, `apps/v1.StatefulSet`, `batch/v1.CronJob` | If a Lumigo resources exists in the namespace, and the resource has the Lumigo instrumented updated as a result |
| `LumigoCannotUpdateInstrumentation` | `apps/v1.Deployment`, `apps/v1.DaemonSet`, `apps/v1.ReplicaSet`, `apps/v1.StatefulSet`, `batch/v1.CronJob` | If a Lumigo resources exists in the namespace, and the resource _should have_ the Lumigo instrumented updated as a result, but an error occurs |
| `LumigoRemovedInstrumentation` | `apps/v1.Deployment`, `apps/v1.DaemonSet`, `apps/v1.ReplicaSet`, `apps/v1.StatefulSet`, `batch/v1.CronJob` | If a Lumigo resources is deleted from the namespace, and the resource has the Lumigo instrumented removed as a result |
| `LumigoCannotRemoveInstrumentation` | `apps/v1.Deployment`, `apps/v1.DaemonSet`, `apps/v1.ReplicaSet`, `apps/v1.StatefulSet`, `batch/v1.CronJob` | If a Lumigo resources is deleted from the namespace, and the resource _should have_ the Lumigo instrumented removed as a result, but an error occurs |

[^1]: The user experience of having to install [Cert Manager](https://cert-manager.io/docs/installation/) is unnecessarily complex, and Kustomize layers, while they may be fine for one's own applications, are simply unsound for a batteries-included, rapidly-evolving product like the Lumigo Kubernetes operator.
Specifically, please expect your Kustomize layers to stop working with any release of the Lumigo Kubernetes operator.
