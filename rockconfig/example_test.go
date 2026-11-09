package rockconfig_test

import (
	"errors"
	"log"
	"time"

	"github.com/arzab/gorock-kit/rockconfig"
)

// Compiled, not run: mirrors the README.

type Config struct {
	Host    string
	Port    int           `config:",default=8080"`
	Debug   bool          `config:",omitempty"`
	Secret  string        `config:",env=APP_SECRET"`
	Timeout time.Duration `config:",default=30s"`
	DB      struct {
		DSN string
	} `config:",omitempty"`
}

func ExampleInitFromFile() {
	cfg, err := rockconfig.InitFromFile[Config]("config.yaml")
	switch {
	case errors.Is(err, rockconfig.ErrMissingFields), errors.Is(err, rockconfig.ErrUnknownKeys):
		log.Fatalf("fix config.yaml: %v", err)
	case err != nil:
		log.Fatal(err)
	}
	log.Println(cfg.Host, cfg.Port)

	// A file shared with other programs:
	_, _ = rockconfig.InitFromFile[Config]("shared.yaml", rockconfig.AllowUnknownKeys())
}
