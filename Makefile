.PHONY: up down logs test lint tidy

up:
	docker compose -f deployments/docker-compose.yml --env-file .env up --build -d

down:
	docker compose -f deployments/docker-compose.yml down

logs:
	docker compose -f deployments/docker-compose.yml logs -f

test:
	go test -v -race ./...

tidy:
	go mod tidy
