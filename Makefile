BINARY_NAME := browser-ui
REGISTRY ?= localhost:5000
IMAGE_NAME := $(REGISTRY)/$(BINARY_NAME)

VERSION ?= develop
EXTRA_TAGS ?=
PLATFORM ?= linux/amd64
CONTAINER_TOOL ?= docker
NPM ?= npm
UI_DIR ?= src

.PHONY: fmt vet tidy test test-ui docker-build docker-push deploy clean show-vars

fmt:
	go fmt ./...

vet:
	go vet ./...

tidy:
	go mod tidy

test:
	go test -race -count=1 -cover ./...

test-ui:
	cd $(UI_DIR) && $(NPM) ci && $(NPM) run test

docker-build: tidy fmt vet test test-ui
	$(CONTAINER_TOOL) buildx build \
		--platform $(PLATFORM) \
		--build-arg VERSION=$(VERSION) \
		-t $(IMAGE_NAME):$(VERSION) \
		--load \
		.

docker-push:
	$(CONTAINER_TOOL) buildx build \
		--platform $(PLATFORM) \
		--build-arg VERSION=$(VERSION) \
		-t $(IMAGE_NAME):$(VERSION) \
		$(EXTRA_TAGS) \
		--push \
		.

deploy: docker-push

clean:
	$(CONTAINER_TOOL) rmi $(IMAGE_NAME):$(VERSION) 2>/dev/null || true

show-vars:
	@echo "BINARY_NAME: $(BINARY_NAME)"
	@echo "REGISTRY: $(REGISTRY)"
	@echo "IMAGE_NAME: $(IMAGE_NAME)"
	@echo "VERSION: $(VERSION)"
	@echo "PLATFORM: $(PLATFORM)"
	@echo "CONTAINER_TOOL: $(CONTAINER_TOOL)"
	@echo "NPM: $(NPM)"
	@echo "UI_DIR: $(UI_DIR)"
