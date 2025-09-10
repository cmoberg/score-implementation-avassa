# Disable all the default make stuff
MAKEFLAGS += --no-builtin-rules
.SUFFIXES:

## Display a list of the documented make targets
.PHONY: help
help:
	@echo Documented Make targets:
	@perl -e 'undef $$/; while (<>) { while ($$_ =~ /## (.*?)(?:\n# .*)*\n.PHONY:\s+(\S+).*/mg) { printf "\033[36m%-30s\033[0m %s\n", $$2, $$1 } }' $(MAKEFILE_LIST) | sort

.PHONY: .FORCE
.FORCE:

build:
	go build ./cmd/score-implementation-avassa/

test:
	go vet ./...
	go test ./... -cover -race


test-app: build
	./score-implementation-avassa --version
	./score-implementation-avassa init
	cat score.yaml
	./score-implementation-avassa generate score.yaml
	cat manifests.yaml

build-container:
	docker build -t score-implementation-avassa:local .


test-container: build-container
	docker run --rm score-implementation-avassa:local --version
	docker run --rm -v .:/score-implementation-avassa score-implementation-avassa:local init
	cat score.yaml
	docker run --rm -v .:/score-implementation-avassa score-implementation-avassa:local generate score.yaml
	cat manifests.yaml
