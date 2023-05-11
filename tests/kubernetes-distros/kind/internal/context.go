package internal

type ContextKey string

var (
	ContextKeyRunId              = ContextKey("run-id")
	ContextKeyOtlpSinkConfigPath = ContextKey("otlp-sink/config")
	ContextKeyOtlpSinkDataPath   = ContextKey("otlp-sink/data")
)

func (c ContextKey) String() string {
	return "ctx.key:" + string(c)
}
