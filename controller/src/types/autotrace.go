package types

type AutoTraceSettings struct {
	IsAutoTraced  bool
	TracesEnabled bool
	LogsEnabled   bool
	SecretName    string
	SecretKey     string
}
