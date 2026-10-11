package ui

// Config contains TUI-specific configuration.
type Config struct {
	ShowLineNumbers  bool
	GlamourMaxWidth  uint
	GlamourStyle     string `env:"GLAMOUR_STYLE"`
	EnableMouse      bool
	PreserveNewLines bool

	// File path of the markdown document to display.
	Path string

	// For debugging the UI
	GlamourEnabled bool `env:"GLOW_ENABLE_GLAMOUR" envDefault:"true"`
}
