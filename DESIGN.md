

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