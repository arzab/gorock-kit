package rockservices_test

import (
	"context"
	"log"

	"github.com/arzab/gorock-kit/rockservices"
)

// Compiled, not run: mirrors the README.

// --- pkg/services/repository ---

const Name = "repository"

type Configs struct{ DSN string }

type Repository interface {
	rockservices.Service
	Find(ctx context.Context, id string) (string, error)
}

type repository struct{ cfg Configs }

func NewService(cfg Configs) Repository { return &repository{cfg: cfg} }

func (r *repository) Name() string                                        { return Name }
func (r *repository) Init(ctx context.Context) error                      { return nil } // open the pool here
func (r *repository) Stop() error                                         { return nil }
func (r *repository) Find(ctx context.Context, id string) (string, error) { return id, nil }

// --- pkg/services/services.go ---

type ServicesConfigs struct {
	Repository Configs
}

func Register(c *rockservices.Catalog, cfg ServicesConfigs) {
	rockservices.Add(c, NewService, cfg.Repository) // add order = init order
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
	repo := rockservices.Get[Repository](Name)
	_, _ = repo.Find(context.Background(), "42")
}
