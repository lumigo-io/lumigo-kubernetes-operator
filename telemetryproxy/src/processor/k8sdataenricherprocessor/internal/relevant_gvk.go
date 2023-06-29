package internal // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/k8sdataenricherprocessor/internal"

import "k8s.io/apimachinery/pkg/runtime/schema"

var (
	V1Namespace       = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
	V1Pod             = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	V1Event           = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Event"}
	AppsV1DaemonSet   = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DaemonSet"}
	AppsV1Deployment  = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	AppsV1ReplicaSet  = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}
	AppsV1StatefulSet = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"}
	BatchV1CronJob    = schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "CronJob"}
	BatchV1Job        = schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "Job"}

	relevantOwnerReferenceGroupVersionKinds = []schema.GroupVersionKind{
		V1Pod,
		AppsV1DaemonSet,
		AppsV1Deployment,
		AppsV1ReplicaSet,
		AppsV1StatefulSet,
		BatchV1CronJob,
		BatchV1Job,
	}
)
