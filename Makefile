BINARY  := queue-bot
DOCKERHUB_REPO ?= hixichen/slack-queue-bot
TAG ?= latest
IMAGE := $(DOCKERHUB_REPO):$(TAG)
COMPOSE := docker compose -f deploy/docker-compose.yml

.PHONY: build test lint run clean docker-build docker-push image-release up down logs k8s-apply k8s-delete

## build: compile the binary
build:
	CGO_ENABLED=1 go build -o $(BINARY) .

## test: run all unit tests
test:
	CGO_ENABLED=1 go test ./... -v

## lint: vet the code
lint:
	go vet ./...

## run: run locally (requires SLACK_BOT_TOKEN and SLACK_APP_TOKEN in env)
run: build
	./$(BINARY)

## clean: remove build artifacts and local db
clean:
	rm -f $(BINARY)
	rm -f data/bot.db data/bot.db-shm data/bot.db-wal

## docker-build: build the Docker image (override TAG=... as needed)
docker-build:
	docker build -f deploy/Dockerfile -t $(IMAGE) .

## docker-push: push the built image to Docker Hub
docker-push:
	docker push $(IMAGE)

## image-release: build for linux/amd64 and push in one step (works from any host arch)
image-release:
	docker buildx build --platform linux/amd64 -f deploy/Dockerfile -t $(IMAGE) --push .

## up: build and start the bot via docker compose
up:
	$(COMPOSE) up --build

## down: stop and remove compose containers
down:
	$(COMPOSE) down

## logs: tail compose logs
logs:
	$(COMPOSE) logs -f

## k8s-apply: deploy to Kubernetes (fill in deploy/k8s/secret.yaml first)
k8s-apply:
	kubectl apply -f deploy/k8s/secret.yaml
	kubectl apply -f deploy/k8s/deployment.yaml

## k8s-delete: remove from Kubernetes
k8s-delete:
	kubectl delete -f deploy/k8s/deployment.yaml
	kubectl delete -f deploy/k8s/secret.yaml
