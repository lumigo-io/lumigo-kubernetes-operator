package internal

type ContextKey string

var (
	ContextKeyRunId                       = ContextKey("run-id")
	ContextKeyKubernetesClusterName       = ContextKey("kubernetes/cluster/name")
	ContextKeyLumigoOperatorDebug         = ContextKey("lumigo/debug")
	ContextKeyLumigoNamespace             = ContextKey("lumigo/namespace")
	ContextKeyLumigoToken                 = ContextKey("lumigo/token")
	ContextKeyOperatorControllerImage     = ContextKey("lumigo/operator/images/controller")
	ContextKeyOperatorTelemetryProxyImage = ContextKey("lumigo/operator/images/proxy")
	ContextKeySendDataToLumigo            = ContextKey("lumigo/upstream/send_data")
	ContextQuickstartNamespaces           = ContextKey("test-apps/namespace/quickstart")
)

func (c ContextKey) String() string {
	return "ctx.key:" + string(c)
}
