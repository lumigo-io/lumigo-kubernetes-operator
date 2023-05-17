package internal

type ContextKey string

var (
	ContextKeyRunId                   = ContextKey("run-id")
	ContextKeyOtlpSinkConfigPath      = ContextKey("otlp-sink/config")
	ContextKeyOtlpSinkDataPath        = ContextKey("otlp-sink/data")
	ContextKeyLumigoToken             = ContextKey("lumigo/token")
	ContextKeyOperatorControllerImage = ContextKey("lumigo/operator/images/controller")
	ContextKeyOperatorProxyImage      = ContextKey("lumigo/operator/images/proxy")
)

func (c ContextKey) String() string {
	return "ctx.key:" + string(c)
}
