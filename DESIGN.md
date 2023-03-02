# Design of the Lumigo Operator

## Structure

```
                Cluser-scope, lumigo-system ns          ns: MyApp
            ┌─────────────────────────────────┐         ┌───────────────────────────────────────────────┐
            │ Lumigo Operator Deployment      │         │                                               │
            │  ┌───────────────────────────┐  │         │  ┌───────────────┐      ┌──────────────────┐  │
        ┌───┤  │ Lumigo Operator           │  │         │  │ v1 Secret     │      │ apps/v1 Depl.    │  │
        │   │  │                           │  │         │  │               │      │                  │  │
        │   │  │ - Managed Lumigo CRD      │  │         │  │ name: lumigo  │      │ Send traces      │  │
        │   │  │ - Expose Mutating Webhook │  │         │  │ data:         │      │ to OtelCollector │  │
        │   │  │ - Manage OtelCollector    │  │         │  │   token: t_*. │      │ Service          │  │
        │   │  │                           │  │         │  │               │      │                  │  │
        │   │  └───────────────────────────┘  │         │  └───────────────┘      └──────────────────┘  │
        │   │                                 │         │                                               │
        │   │  ┌───────────────────────────┐  │         │  ┌──────────────────────────────┐             │
        │   │  │ OtelCollector "Proxy"     │  │         │  │ operator.lumigo.io/v1 Lumigo │             │
        │   │  │                           │  │         │  │                              │             │
Manages │   │  │ - Rec. & frw. traces      │  │         │  │ lumigoToken                  │             │
        │   │  │ - Enrich K8S res.attr.    │  │         │  │   secretRef                  │             │
        │   │  │ - Clct Kube events        │  │         │  │     name: lumigo             │             │
        │   │  │                           │  │         │  │     key: token               │             │
        │   │  └───────────────────────────┘  │         │  └──────────────────────────────┘             │
        │   └─────────────────────────────────┘         └───────────────────────────────────────────────┘
        │   Deploy via Helm
        │
        │   CronJob, lumigo-system ns
        │   ┌─────────────────────────────────┐
        │   │ Updater container               │
        │   │ ┌─────────────────────────────┐ │
        │   │ │ Updates the Helm chart of   │ │
        └──►│ │ the Lumigo operator in the  │ │
            │ │ current namespace           │ │
            │ │                             │ │
            │ └─────────────────────────────┘ │
            └─────────────────────────────────┘
```

## Uninstallation process

When uninstalling the operator, [depending on settings in the Lumigo resource](README.md#remove-injection-from-existing-resources) in each namespace, the operator will uninstrument resources before it gets deleted.

The uninstallation process relies on a [`pre-delete`](./charts/lumigo-operator/templates/uninstallation/uninstall-hook.yaml) Helm hook, which starts a `batchv1.Job` running a controller container started with the `--uninstall` CLI command.
The `--uninstall` CLI command, implemented in [`main.go`](./main.go), instead of bringing up a new controller instance, triggers the deletion of all Lumigo resources in all namespaces.
The Lumigo resources are not deleted right away because the controller, the first time it sees a new Lumigo resource, adds a finalizer to it.
When the controller reconciles a Lumigo resource with a deletion timestamp (which is set on the Lumigo resource when the `pre-delete` hook triggers), it uninstruments all the resources in that Lumigo resource's namespace, and then it removes the finalizer, so that the Lumigo resource can be garbage collected.
The garbage collection of the Lumigo resource is observed by the uninstallation hook, and when all Lumigo resources have been deleted, the uninstallation hook completes, and Helm proceeds to remove the resources in the release (that is: the operator deployment, RBAC, etc.)