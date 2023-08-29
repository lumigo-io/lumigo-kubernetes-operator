# Lumigo Kubernetes Operator

![The Lumigo Logo](./images/lumigo.png)

[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/lumigo-operator)](https://artifacthub.io/packages/search?repo=lumigo-operator)

The Kubernetes operator of Lumigo provides a one-click solution to monitoring Kubernetes clusters with Lumigo.

## Setup

### Installation

Install the Lumigo Kubernetes operator in your Kubernets cluster with [helm](https://helm.sh/):

```sh
helm repo add lumigo https://lumigo-io.github.io/lumigo-kubernetes-operator
helm install lumigo lumigo/lumigo-operator --namespace lumigo-system --create-namespace --set cluster.name=<cluster_name>
```

You can customize the namespace name to use something other than `lumigo-system`, but this will make the rest of the instructions subtly wrong :-)

(The `cluster.name` is optional, but highly advised, see the [Naming your cluster](#naming-your-cluster) section.)


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

After creating the secret, deploy it in the desired namespace:

```sh
kubectl apply -f lumigo.yml -n <YOUR_NAMESPACE>
```

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

In the logs of the Lumigo Kubernetes operator, you will see a message like the following:

```
1.67534267851615e+09    DEBUG   controller-runtime.webhook.webhooks   wrote response   {"webhook": "/v1alpha1/inject", "code": 200, "reason": "the resource has the 'lumigo.auto-trace' label set to 'false'; resource will not be mutated", "UID": "6d341941-c47b-4245-8814-1913cee6719f", "allowed": true}
```

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
