# Lumigo Kubernetes operator: Telemetry-Proxy image

This image is part of the [Lumigo Kubernetes Operator](https://docs.lumigo.io/docs/lumigo-kubernetes-operator).
The Lumigo Kubernetes operator is best way to monitor Kubernetes applications with Lumigo.

Using the Lumigo Kubernetes operator is as simple as:

1. Install the Lumigo Kubernetes operator on your cluster via Helm

   ```
   helm repo add lumigo https://lumigo-io.github.io/lumigo-kubernetes-operator
   helm install lumigo lumigo/lumigo-operator --namespace lumigo-system --create-namespace
   ```

2. In the Kubernetes namespaces with applications you want to trace, create a Kubernetes secret containing the Lumigo Token, and a Lumigo resource pointing to it:

   ```
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

The Lumigo Kubernetes operator will update your deployments, daemonsets, replicasets, statefulsets, cronjobs and jobs to be traced with the Lumigo OpenTelemetry Distro for Node.js and Lumigo OpenTelemetry Distro for Python.

More detailed instructions are available in the Lumigo Kubernetes operator repository on GitHub.
