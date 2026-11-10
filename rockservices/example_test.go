package rockservices_test

import (
	"context"
	"log"

	"github.com/arzab/gorock-kit/rockservices"
)

// Compiled, not run: mirrors the README.

// --- pkg/services/repository ---

type RepositoryConfigs struct{ DSN string }

type Repository interface {
	rockservices.Service
	Find(ctx context.Context, id string) (string, error)
}

type repository struct{ cfg RepositoryConfigs }

func NewRepository(cfg RepositoryConfigs) Repository { return &repository{cfg: cfg} } // no connections here

func (r *repository) Init(ctx context.Context) error                      { return nil } // open the pool here
func (r *repository) Stop() error                                         { return nil }
func (r *repository) Find(ctx context.Context, id string) (string, error) { return id, nil }

// --- pkg/services/services.go ---

const RepositoryName = "repository"

type Configs struct {
	Repository RepositoryConfigs
}

var catalog *rockservices.Catalog

// Init creates the services from their configs and initialises them in order.
func Init(ctx context.Context, cfg Configs) error {
	catalog = rockservices.New(map[string]rockservices.Service{
		RepositoryName: NewRepository(cfg.Repository),
	})
	return catalog.Init(ctx, RepositoryName)
}

// Stop stops the services in reverse init order.
func Stop() error { return catalog.Stop() }

// GetRepository is the getter every layer uses.
func GetRepository() Repository { return catalog.Get(RepositoryName).(Repository) }

func Example() {
	// cmd/main: services start before the apps and stop after them.
	if err := Init(context.Background(), Configs{Repository: RepositoryConfigs{DSN: "postgres://..."}}); err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := Stop(); err != nil {
			log.Print(err)
		}
	}()

	// anywhere: handlers, apps
	_, _ = GetRepository().Find(context.Background(), "42")
}
