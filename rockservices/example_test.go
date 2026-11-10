package rockservices_test

import (
	"context"
	"log"

	"github.com/arzab/gorock-kit/rockservices"
)

// Compiled, not run: mirrors the README.

// --- pkg/services/repository ---

type Configs struct{ DSN string }

type Repository interface {
	rockservices.Service
	Find(ctx context.Context, id string) (string, error)
}

type repository struct{ cfg Configs }

func NewService(cfg Configs) Repository { return &repository{cfg: cfg} }

func (r *repository) Init(ctx context.Context) error                      { return nil } // open the pool here
func (r *repository) Stop() error                                         { return nil }
func (r *repository) Find(ctx context.Context, id string) (string, error) { return id, nil }

// --- pkg/services/services.go ---

// Keys are declared once, next to the registration.
var RepositoryKey = rockservices.NewKey[Repository]("repository")

type ServicesConfigs struct {
	Repository Configs
}

func Register(c *rockservices.Catalog, cfg ServicesConfigs) {
	rockservices.Add(c, RepositoryKey, NewService, cfg.Repository) // add order = init order
}

func Example() {
	// cmd/main: services start before the apps and stop after them.
	Register(rockservices.Default(), ServicesConfigs{Repository: Configs{DSN: "postgres://..."}})
	if err := rockservices.Init(context.Background()); err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := rockservices.Stop(); err != nil {
			log.Print(err)
		}
	}()

	// anywhere: delivery, apps, other services' constructors
	_, _ = RepositoryKey.Get().Find(context.Background(), "42")
}
