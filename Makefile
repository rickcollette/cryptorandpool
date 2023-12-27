# Makefile for the cryptorandpool Go module

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Module name
MODULE=github.com/rickcollette/cryptorandpool

# Versioning
GIT_COMMIT=$(shell git rev-list -1 HEAD)
BUILD_DATE=$(shell date +%FT%T%z)

# Binary name for example if needed
BINARY_NAME=cryptorandpool

all: test build

build:
	$(GOBUILD) -o $(BINARY_NAME) -v

test:
	$(GOTEST) -v ./...

clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)

deps:
	$(GOGET) -v ./...

tidy:
	$(GOMOD) tidy

# Generate GoDoc documentation
godoc:
	go doc -all

# Example of a target to tag a new version
# Use `make tag version=v1.0.1` to create a new tag
tag:
	git tag $(version)
	git push origin $(version)

# Example of a target to update the module version
# Use `make version-update version=v1.0.1` to update the module
version-update:
	$(GOMOD) tidy -compat=$(version)
	$(GOMOD) edit -go=$(version)

# Example of a target for releasing a new version of the module
# Use `make release version=v1.0.1` for a new release
release: test tidy tag

.PHONY: all build test clean deps tidy tag version-update release godoc
