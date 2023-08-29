# Lumigo Kubernetes Operator

The Kubernetes operator of Lumigo provides a one-click solution to monitoring Kubernetes clusters with Lumigo.

## Installing the Lumigo operator

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
lumigo-kubernetes-operator-7fc8f67bcc-ffh5k   2/2     Running   0          56s
```

## Enabling automatic tracing

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

## Uninstall

The removal of the Lumigo operator is performed by:

```sh
helm delete lumigo --namespace lumigo-system
```

All the `Lumigo` resources you have created in your namespaces are going to be automatically deleted.

# Detailed documentation

See the [repository documentation](https://github.com/lumigo-io/lumigo-kubernetes-operator) for more information on how to use the Lumigo Kubernetes Operator.
