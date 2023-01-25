# Lumigo Kubernetes Operator

![The Lumigo Logo](./images/lumigo.png)

The Kubernetes operator of Lumigo provides a one-click solution to monitoring Kubernetes clusters with Lumigo.

## Setup

### Installing the Lumigo operator

Install the Lumigo operator in your Kubernets cluster with [helm](https://helm.sh/):

```sh
helm install lumigo deploy/helm --namespace lumigo-system --create-namespace # TODO: Update when we release the Helm chart on ArtifactHub
```

You can customize the namespace name to use something other than `lumigo-system`, but this will make the rest of the instructions subtly wrong :-)

You can verify that the Lumigo Operator is up and running with:

```sh
$ kubectl get pods -n lumigo-system
NAME                                                         READY   STATUS    RESTARTS   AGE
lumigo-lumigo-operator-controller-manager-7fc8f67bcc-ffh5k   2/2     Running   0          56s
```

### Enabling automatic tracing

The Lumigo operator is capable of automatically add distributed tracing to pods created via:

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
  token: t_123456789012345678901 # Get the actual value from Lumigo following this documentation: https://docs.lumigo.io/docs/lumigo-tokens
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

__TODO__ show example

### Uninstall

__TODO__

## Troubleshooting

__TODO__

* How to detect if the operator is badly configured
* How to detect if the operator has problems
* How to list all resources traced by the Lumigo Operator