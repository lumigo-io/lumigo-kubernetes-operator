package internal

type ContextKey string

var (
	ContextKeyRunId                       = ContextKey("run-id")
	ContextKeyOtlpSinkConfigPath          = ContextKey("otlp-sink/config")
	ContextKeyOtlpSinkDataPath            = ContextKey("otlp-sink/data")
	ContextKeyLumigoEndpoint              = ContextKey("lumigo/endpoint")
	ContextKeyLumigoOperatorDebug         = ContextKey("lumigo/debug")
	ContextKeyLumigoToken                 = ContextKey("lumigo/token")
	ContextKeyOperatorControllerImage     = ContextKey("lumigo/operator/images/controller")
	ContextKeyOperatorTelemetryProxyImage = ContextKey("lumigo/operator/images/proxy")
	ContextKeySendDataToLumigo            = ContextKey("lumigo/upstream/send_data")
	ContextTestAppJsClientImageName       = ContextKey("test-apps/js/client/image/name")
	ContextTestAppJsServerImageName       = ContextKey("test-apps/js/server/image/name")
)

func (c ContextKey) String() string {
	return "ctx.key:" + string(c)
}
