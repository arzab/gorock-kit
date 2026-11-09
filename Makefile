# Thin wrapper over scripts/check.sh. Pass MODULES="rockengine rockbus" to limit scope.
MODULES ?=

.PHONY: all build vet test tidy-check lint

all: build vet test

build vet test tidy-check lint:
	bash scripts/check.sh $@ $(MODULES)
