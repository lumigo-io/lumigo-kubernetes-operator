package internal

type ContextKey string

var (
	ContextKeyRunId                             = ContextKey("run-id")
	ContextKeyKubernetesClusterName             = ContextKey("kubernetes/cluster/name")
	ContextKeyOtlpSinkConfigPath                = ContextKey("otlp-sink/config")
	ContextKeyOtlpSinkDataPath                  = ContextKey("otlp-sink/data")
	ContextKeyLumigoEndpoint                    = ContextKey("lumigo/endpoint")
	ContextKeyLumigoOperatorDebug               = ContextKey("lumigo/debug")
	ContextKeyLumigoToken                       = ContextKey("lumigo/token")
	ContextKeyOperatorControllerImage           = ContextKey("lumigo/operator/images/controller")
	ContextKeyOperatorTelemetryProxyImage       = ContextKey("lumigo/operator/images/proxy")
	ContextKeySendDataToLumigo                  = ContextKey("lumigo/upstream/send_data")
	ContextTestAppJsClientImageName             = ContextKey("test-apps/js/client/image/name")
	ContextTestAppJsServerImageName             = ContextKey("test-apps/js/server/image/name")
	ContextTestAppPythonImageName       	      = ContextKey("test-apps/python/image/name")
	ContextTestAppBusyboxIncludedContainerNamePrefix    = ContextKey("test-apps/busybox/included-container/name")
	ContextTestAppBusyboxExcludedContainerNamePrefix    = ContextKey("test-apps/busybox/excluded-container/name")
	ContextTestAppNamespacePrefix							  = ContextKey("test-apps/namespace/prefix")
)

func (c ContextKey) String() string {
	return "ctx.key:" + string(c)
}
