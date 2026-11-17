# go-metrics-practice

Агент собирает метрики среды выполнения Go и операционной системы и отправляет их по HTTP или батчами по gRPC. Сервер сохраняет метрики в файл или PostgreSQL. Список метрик доступен на [главной странице сервера](http://localhost:8080/).

## Запуск

Docker Compose запускает агент, сервер и PostgreSQL одной командой из корня проекта.

```bash
docker compose -f infra/compose.yaml up --build
```

Для локального запуска нужен Go 1.26.4 или новее. Выполните команды из корня проекта в разных терминалах, начиная с сервера.

```bash
go run ./cmd/server
go run ./cmd/agent
```

Для gRPC добавьте обеим командам `--grpc-address localhost:9090` (JSON: `grpc_address`). HTTP на сервере остаётся доступен.

При остановке агент отправляет оставшиеся метрики, а сервер завершает активные запросы в пределах таймаута и сохраняет данные.

## Примеры конфигурации

- [JSON-конфигурация агента](cmd/agent/config.example.json)
- [JSON-конфигурация сервера](cmd/server/config.example.json)

Скопируйте пример в соседний `config.json`. Например, сервер с локальной конфигурацией запускается командой `go run ./cmd/server -c cmd/server/config.json`.

## Параметры запуска

Приоритет от высшего к низшему — флаги, переменные окружения, JSON, значения по умолчанию. Для интервалов и таймаутов можно указывать длительность (`500ms`, `5s`, `1m`) или целое число секунд.

### Агент

| Флаг | Переменная окружения | По умолчанию | Назначение |
| --- | --- | --- | --- |
| `-c`, `--config` | `CONFIG` | `""` | Путь к JSON-конфигурации агента |
| `-a`, `--address` | `ADDRESS` | `localhost:8080` | Адрес HTTP-сервера |
| `--grpc-address` | `GRPC_ADDRESS` | `""` | Адрес gRPC-сервера; непустой выбирает gRPC вместо HTTP |
| `-p`, `--poll_interval` | `POLL_INTERVAL` | `2s` | Интервал сбора метрик |
| `-r`, `--report_interval` | `REPORT_INTERVAL` | `10s` | Интервал отправки метрик |
| `-t`, `--timeout` | `TIMEOUT` | `30s` | Таймаут отправки метрик |
| `-l`, `--rate-limit` | `RATE_LIMIT` | `1` | Максимальное число одновременных запросов |
| `-k`, `--key` | `KEY` | `""` | Общий ключ для подписи HMAC-SHA256 |
| `--crypto-key` | `CRYPTO_KEY` | `""` | Путь к публичному RSA-ключу сервера |

IP агента определяется по соединению и передаётся в HTTP-заголовке `X-Real-IP` или gRPC-метаданных `x-real-ip`.

### Сервер

| Флаг | Переменная окружения | По умолчанию | Назначение |
| --- | --- | --- | --- |
| `-c`, `--config` | `CONFIG` | `""` | Путь к JSON-конфигурации сервера |
| `-a`, `--address` | `ADDRESS` | `localhost:8080` | Адрес HTTP-сервера |
| `--grpc-address` | `GRPC_ADDRESS` | `""` | Дополнительный адрес gRPC; пустое значение отключает gRPC |
| `--shutdown-timeout` | `SHUTDOWN_TIMEOUT` | `5s` | Время на завершение HTTP- и gRPC-запросов |
| `-f`, `--file-storage-path` | `FILE_STORAGE_PATH`, затем `STORE_FILE` | `./temp/metrics-db.json` | Путь к файлу метрик |
| `-i`, `--store-interval` | `STORE_INTERVAL` | `5m` | Интервал записи в файл. При `0` запись после каждого обновления |
| `-r`, `--restore` | `RESTORE` | `true` | Загружать метрики из файла при старте |
| `-d`, `--database-dsn` | `DATABASE_DSN` | `""` | Строка подключения к PostgreSQL. При её наличии метрики хранятся в БД |
| `-k`, `--key` | `KEY` | `""` | Общий ключ для подписи HMAC-SHA256 |
| `--crypto-key` | `CRYPTO_KEY` | `""` | Путь к приватному RSA-ключу сервера |
| `-t`, `--trusted-subnet` | `TRUSTED_SUBNET` | `""` | Доверенная подсеть агентов в формате CIDR |
| `--audit-file` | `AUDIT_FILE` | `""` | Файл аудита успешных обновлений метрик |
| `--audit-url` | `AUDIT_URL` | `""` | URL для отправки событий аудита по HTTP |

В JSON-конфигурации подсеть задаётся строкой `trusted_subnet`, например `"192.168.1.0/24"` или `"2001:db8::/32"`. Пустая строка отключает проверку. Некорректный CIDR вызывает ошибку запуска; IP вне подсети — HTTP `403` или gRPC `PermissionDenied`.

## Шифрование HTTP-запросов

Для локального запуска один раз создайте пару RSA-ключей в формате PEM с помощью OpenSSL. Каталог `temp/` уже добавлен в `.gitignore`.

```bash
mkdir -p temp/keys
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out temp/keys/private.pem
openssl pkey -in temp/keys/private.pem -pubout -out temp/keys/public.pem
```

Приватный ключ остаётся на сервере. Агенту передайте публичный ключ. Запустите сервисы в разных терминалах.

```bash
go run ./cmd/server -crypto-key temp/keys/private.pem
go run ./cmd/agent -crypto-key temp/keys/public.pem
```

В JSON-конфигурации путь к ключу задаётся полем `crypto_key`.

## Проверки

```bash
go test -race ./...
go run ./cmd/staticlint ./...
```
