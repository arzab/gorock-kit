package rockcron

// Config holds file-loadable App configuration.
type Config struct {
	// Location is the IANA timezone name for cron schedule evaluation.
	// Example: "Europe/Moscow". Default: UTC.
	Location string `config:",omitempty"`
}
