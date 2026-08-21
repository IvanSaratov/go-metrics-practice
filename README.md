# go-metrics-practice

Сервис сбора метрик: агент собирает runtime- и системные метрики, а сервер принимает и хранит их в файле или PostgreSQL.

## Быстрый запуск

```bash
docker compose -f infra/compose.yaml up --build
```

Сервер будет доступен на <http://localhost:8080>.

## Запуск локально

Нужен Go 1.26+. Запустите в разных терминалах:

```bash
go run ./cmd/server
go run ./cmd/agent
```

Параметры и переменные окружения: `go run ./cmd/server --help` и `go run ./cmd/agent --help`.

## Тесты

```bash
go test ./...
```
