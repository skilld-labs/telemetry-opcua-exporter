package log

const (
	Debug = iota
	Info
	Warn
	Err
	Fatal
)

type LoggerConfiguration struct {
	Prefix    string
	Verbosity int
}

type Logger interface {
	Debug(string, ...interface{})
	Info(string, ...interface{})
	Warn(string, ...interface{})
	Err(string, ...interface{})
	Panic(string, ...interface{})
	Fatal(string, ...interface{})
	SetVerbosity(string)
	Shutdown() error
}

func GetVerbosityFromString(verbosity string) int {
	switch verbosity {
	case "debug":
		return Debug
	case "info":
		return Info
	case "warn":
		return Warn
	case "err":
		return Err
	case "fatal":
		return Fatal
	default:
		return Warn
	}
}
