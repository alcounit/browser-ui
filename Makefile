BINARY_NAME := browser-ui
DOCKER_REGISTRY ?= 192.168.1.101:30000
VERSION ?= v1
IMAGE := $(DOCKER_REGISTRY)/$(BINARY_NAME):$(VERSION)
PLATFORM ?= linux/amd64
CONTAINER_TOOL ?= docker

.PHONY: fmt vet tidy docker-build docker-push deploy clean show-vars

fmt:
	go fmt ./...

vet:
	go vet ./...

tidy:
	go mod tidy

docker-build: tidy fmt vet
	$(CONTAINER_TOOL) build --platform $(PLATFORM) -t $(IMAGE) .

docker-push:
	$(CONTAINER_TOOL) push $(IMAGE)

deploy: docker-build docker-push

clean:
	$(CONTAINER_TOOL) rmi $(IMAGE) 2>/dev/null || true

show-vars:
	@echo "BINARY_NAME: $(BINARY_NAME)"
	@echo "DOCKER_REGISTRY: $(DOCKER_REGISTRY)"
	@echo "VERSION: $(VERSION)"
	@echo "IMAGE: $(IMAGE)"
	@echo "PLATFORM: $(PLATFORM)"
	@echo "CONTAINER_TOOL: $(CONTAINER_TOOL)"
