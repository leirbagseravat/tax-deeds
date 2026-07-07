SHELL := /bin/bash

-include .env
export

.PHONY: run build test vet fmt up down migrate migrate-down migrate-status ocr

run:
	go run ./cmd/api

build:
	go build -o bin/api ./cmd/api
	go build -o bin/migrate ./cmd/migrate
	go build -o bin/ocr ./cmd/ocr

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

up:
	docker compose up -d

down:
	docker compose down

migrate:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down

migrate-status:
	go run ./cmd/migrate status

ocr:
	go run ./cmd/ocr
