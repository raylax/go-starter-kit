-include .env
export

.PHONY: build-api build-worker dev-worker help dev build db-up db-down migrate-up migrate-down generate check-generated fmt lint test test-integration vuln check

help:
	@printf '%s\n' 'dev              使用本地 .env 启动 API' 'db-up            启动本地 PostgreSQL' 'migrate-up       应用数据库迁移' 'generate         生成 SQL 访问代码与 OpenAPI' 'check            检查格式、静态分析、单测、生成一致性与漏洞' 'test-integration 运行真实 PostgreSQL 集成测试（需要 Docker）' 'dev-worker       启动后台 Worker' 'build            构建 .bin/api 和 .bin/worker'

dev:
	go run ./cmd/api serve

build: build-api build-worker

build-api:
	CGO_ENABLED=0 go build -trimpath -o .bin/api ./cmd/api

build-worker:
	CGO_ENABLED=0 go build -trimpath -o .bin/worker ./cmd/worker

dev-worker:
	go run ./cmd/worker

db-up:
	docker compose up -d --wait db

db-down:
	docker compose down

migrate-up:
	go run ./cmd/api migrate up

# 显式回滚一个迁移版本，可能删除业务数据。
migrate-down:
	go run ./cmd/api migrate down

generate:
	@rm -f internal/db/sqlc/*.sql.go
	go tool sqlc generate
	gofmt -w -r 'interface{} -> any' internal/db/sqlc
	@mkdir -p api
	go run ./cmd/api openapi > api/openapi.json

check-generated:
	@sh scripts/check-generated.sh

fmt:
	gofmt -w -r 'interface{} -> any' .

lint:
	@test -z "$$(gofmt -l -r 'interface{} -> any' .)" || (gofmt -l -r 'interface{} -> any' .; exit 1)
	go vet ./...

test:
	go test -race -count=1 ./...

test-integration:
	go test -race -count=1 -tags=integration ./tests/integration/... -timeout 5m

vuln:
	go tool govulncheck ./...

check: lint test check-generated vuln
