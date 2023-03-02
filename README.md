# Lumigo Kubernetes Operator

![The Lumigo Logo](./images/lumigo.png)

[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/lumigo-operator)](https://artifacthub.io/packages/search?repo=lumigo-operator)

The Kubernetes operator of Lumigo provides a one-click solution to monitoring Kubernetes clusters with Lumigo.

## Setup

### Installing the Lumigo operator

Install the Lumigo operator in your Kubernets cluster with [helm](https://helm.sh/):

```sh
helm repo add lumigo https://lumigo-io.github.io/lumigo-kubernetes-operator
helm install lumigo lumigo/lumigo-operator --namespace lumigo-system --create-namespace
```

You can customize the namespace name to use something other than `lumigo-system`, but this will make the rest of the instructions subtly wrong :-)

You can verify that the Lumigo Operator is up and running with:

```sh
$ kubectl get pods -n lumigo-system
NAME                                                         READY   STATUS    RESTARTS   AGE
lumigo-lumigo-operator-controller-manager-7fc8f67bcc-ffh5k   2/2     Running   0          56s
```

### Enabling automatic tracing

#### Supported resource types

The Lumigo operator automatically adds distributed tracing to pods created via:

* Deployments ([`apps/v1.Deployment`](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/))
* Daemonsets ([`apps/v1.DaemonSet`](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/))
* ReplicaSets ([`apps/v1.ReplicaSet`](https://kubernetes.io/docs/concepts/workloads/controllers/replicaset/))
* StatefulSets ([`apps/v1.StatefulSet`](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/))
* CronJobs ([`batch/v1.CronJob`](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/))
* Jobs ([`batch/v1.Job`](https://kubernetes.io/docs/concepts/workloads/controllers/job/))

The distributed tracing is provided by the [Lumigo OpenTelemetry distribution for Python](https://github.com/lumigo-io/opentelemetry-python-distro) and [Lumigo OpenTelemetry distribution for JS](https://github.com/lumigo-io/opentelemetry-js-distro).

The Lumigo Operator will automatically trace all Python and Node.js processes found in the containers of pods created in the namespaces that Lumigo traces.
To activate automatic tracing for resources in a namespace, create in that namespace a Kubernetes secret containing your [Lumigo token](https://docs.lumigo.io/docs/lumigo-tokens), and reference it from a `Lumigo` (`operator.lumigo.io/v1alpha1.Lumigo`) custom resource:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: lumigo-credentials
stringData:
  # Kubectl won't allow you to deploy this dangling anchor.
  # Get the actual value from Lumigo following this documentation: https://docs.lumigo.io/docs/lumigo-tokens
  token: *lumigo-token # Example: t_123456789012345678901
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
      key: token # This must match the key in the Kubernetes secret
```

#### Opting out for specific resources

To prevent the Lumigo operator from injecting tracing to pods managed by some resource in a namespace that contains a `Lumigo` resource, add the `lumigo.auto-trace` label set to `false`:

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

In the logs of the Lumigo operator, you will see a message like the following:

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

By default, when detecting the deletion of a new Lumigo resource in a namespace, the Lumigo controller will remove instrumentation from existing resources of the [supported types](#supported-resource-types).
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

**Note:** The removal of injection from existing resources does not occur on uninstallation of the Lumigo operator, as the role-based access control is has likely already been deleted.

### Uninstall

The removal of the Lumigo operator is performed by:

```sh
helm delete lumigo --namespace lumigo-system
```

In namespaces with the Lumigo resource having `spec.tracing.injection.enabled` and `spec.tracing.injection.removeLumigoFromResourcesOnDeletion` both set to `true`, [supported resources](#supported-resource-types) that have been injected by the Lumigo operator will be updated to remove the injection, with the following caveat:

**Note:** The removal of injection from existing resources does not apply to `batchv1.Job` resources, as their `corev1.PodSpec` is immutable after the `batchv1.Job` resource has been created.

## TLS certificates

The Lumigo operator injector webhook uses a self-signed certificate that is automatically generate during the [installation of the Helm chart](#installing-the-lumigo-operator).
The generated certificate has a 365 days expiration, and a new certificate will be generated every time you upgrade Lumigo operator's helm chart.
