package rocktelebot

// Config holds file-loadable App configuration.
type Config struct {
	// Token is the Telegram Bot API token from @BotFather. Required.
	Token string

	// Webhook configures webhook mode. If empty, long polling is used.
	Webhook WebhookConfig `config:",omitempty"`
}

// WebhookConfig configures webhook mode.
// If Listen is empty, long polling is used instead.
type WebhookConfig struct {
	// Listen is the address the webhook server listens on, e.g. ":8443".
	Listen string `config:",omitempty"`

	// PublicURL is the public HTTPS URL Telegram will send updates to,
	// e.g. "https://example.com/bot".
	PublicURL string `config:"public_url,omitempty"`
}
