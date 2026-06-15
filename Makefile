BINARY  := queue-bot
DOCKERHUB_REPO ?= hixichen/slack-queue-bot
TAG ?= latest
IMAGE := $(DOCKERHUB_REPO):$(TAG)
COMPOSE := docker compose -f deploy/docker-compose.yml

.PHONY: build test lint run clean docker-build docker-push image-release up down logs k8s-secret k8s-apply k8s-delete

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

## k8s-secret: create the prerequisite Secret from $SLACK_BOT_TOKEN / $SLACK_APP_TOKEN
k8s-secret:
	kubectl create secret generic slack-queue-bot \
	  --from-literal=SLACK_BOT_TOKEN=$(SLACK_BOT_TOKEN) \
	  --from-literal=SLACK_APP_TOKEN=$(SLACK_APP_TOKEN)

## k8s-apply: deploy the workload (the Secret 'slack-queue-bot' must already exist)
k8s-apply:
	@kubectl get secret slack-queue-bot >/dev/null 2>&1 || { \
	  echo "ERROR: secret 'slack-queue-bot' not found in the current namespace."; \
	  echo "Create it first, e.g.: make k8s-secret  (with tokens in your env)"; \
	  exit 1; }
	kubectl apply -f deploy/k8s/deployment.yaml

## k8s-delete: remove the workload (leaves the Secret untouched)
k8s-delete:
	kubectl delete -f deploy/k8s/deployment.yaml
