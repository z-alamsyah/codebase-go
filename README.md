# codebase-go

Template backend service berbasis **Go 1.27** dan **chi**, siap pakai untuk REST, gRPC, dan message consumer (RabbitMQ), lengkap dengan logging yang aman (redaction), OpenTelemetry, dan stack observability lokal (Grafana, Tempo, Loki, Prometheus).

Aturan umum yang dipakai template ini (tidak terikat bahasa) ada di [`docs/CODEBASE_RULES.md`](docs/CODEBASE_RULES.md).

## Daftar Isi

1. [Fitur](#1-fitur)
2. [Arsitektur](#2-arsitektur)
3. [Struktur Folder](#3-struktur-folder)
4. [Prasyarat](#4-prasyarat)
5. [Quick Start: dari Clone sampai Running](#5-quick-start-dari-clone-sampai-running)
6. [Konfigurasi (.env)](#6-konfigurasi-env)
7. [Mengaktifkan gRPC, Consumer, dan OpenTelemetry](#7-mengaktifkan-grpc-consumer-dan-opentelemetry)
8. [Logging](#8-logging)
9. [Membuat Endpoint REST Baru](#9-membuat-endpoint-rest-baru)
10. [Membuat Endpoint gRPC Baru](#10-membuat-endpoint-grpc-baru)
11. [Membuat dan Menggunakan Consumer](#11-membuat-dan-menggunakan-consumer)
12. [Testing](#12-testing)
13. [Migration dan Seed](#13-migration-dan-seed)
14. [Perintah Make](#14-perintah-make)
15. [Troubleshooting](#15-troubleshooting)
16. [Catatan untuk Production](#16-catatan-untuk-production)

---

## 1. Fitur

| Fitur | Default | Cara aktifkan |
|---|---|---|
| REST API (chi) | Aktif | `REST_ENABLED=true` |
| Healthcheck `/healthz` + `/readyz` | Selalu aktif | - |
| PostgreSQL (GORM) | Aktif | - |
| Redis (cache + idempotency) | Aktif | - |
| Logging (redaction, pretty/JSON) | Aktif | `LOG_LEVEL`, `LOG_FORMAT` |
| gRPC + health `grpc.health.v1` + reflection | Mati | `GRPC_ENABLED=true` |
| Publisher RabbitMQ | Mati | `MQ_ENABLED=true` |
| Consumer RabbitMQ (retry, DLQ, reconnect) | Mati | `CONSUMER_ENABLED=true` (butuh `MQ_ENABLED=true`) |
| OpenTelemetry (trace, metric, log) | Mati | `OTEL_ENABLED=true` + `OTEL_EXPORTER_OTLP_ENDPOINT` |

Contoh fitur yang sudah jadi (menyentuh semua layer):

| Transport | Endpoint | Yang dicontohkan |
|---|---|---|
| REST | `GET /api/v1/users/{id}` | Cache-aside Redis |
| REST | `POST /api/v1/users` | Validasi, redaction password di log, publish event `user.created` |
| gRPC | `user.v1.UserService/GetUser` | Service yang sama dengan REST |
| gRPC | `user.v1.UserService/CreateUser` | Validasi dengan detail error `google.rpc.BadRequest` |
| Consumer | queue `codebase-go.user.welcome-email` | Consume `user.created`, idempotent, retry, DLQ |

### Tech Stack

| Kebutuhan | Library |
|---|---|
| Router REST | [go-chi/chi v5](https://github.com/go-chi/chi) + `chi/middleware` |
| Bind/render JSON | [go-chi/render](https://github.com/go-chi/render) |
| Request log | [go-chi/httplog v3](https://github.com/go-chi/httplog) (berbasis `log/slog`) |
| Validasi | [go-playground/validator v10](https://github.com/go-playground/validator) |
| gRPC | [grpc-go](https://github.com/grpc/grpc-go) + [go-grpc-middleware v2](https://github.com/grpc-ecosystem/go-grpc-middleware) |
| Protobuf | [buf](https://buf.build) |
| Database | [GORM](https://gorm.io) di atas driver pgx v5 |
| Migration | [golang-migrate](https://github.com/golang-migrate/migrate) (file SQL di-embed) |
| Cache | [go-redis v9](https://github.com/redis/go-redis) |
| Message broker | [amqp091-go](https://github.com/rabbitmq/amqp091-go) |
| Config | [caarlos0/env](https://github.com/caarlos0/env) + [godotenv](https://github.com/joho/godotenv) |
| Logger | `log/slog` + [tint](https://github.com/lmittmann/tint) (mode pretty) |
| Telemetry | OpenTelemetry SDK, `otelhttp`, `otelgrpc`, `otelpgx`, `redisotel`, `otelslog` |
| Test | [testify](https://github.com/stretchr/testify) + [go.uber.org/mock](https://github.com/uber-go/mock) |
| Lint | [golangci-lint v2](https://golangci-lint.run) |

---

## 2. Arsitektur

### 2.1 Layer

```
            REST request         gRPC call          Message RabbitMQ
                 |                   |                     |
            [ROUTER]             [ROUTER]              [ROUTER]
     internal/router/http.go  router/grpc.go      router/consumer.go
       (route + middleware)  (service + interceptor) (queue -> handler)
                 |                   |                     |
          [CONTROLLER]         [CONTROLLER]          [CONTROLLER]
     controller/rest         controller/rpc        controller/consumer
                  \                  |                    /
                   +-------- [BUSINESS LOGIC] -----------+
                             internal/service/user
                                     |
                +--------------------+--------------------+
                |                    |                    |
           [DATA LAYER]         [DATA LAYER]         [PLATFORM]
     repository/postgres     repository/redis     messaging/rabbitmq
          (GORM)           (cache, idempotency)      (publisher)
```

| Layer | Folder | Tanggung jawab | Tidak boleh |
|---|---|---|---|
| **Router** | `internal/router` | Mendaftarkan route REST, service gRPC, dan queue consumer ke controller. Memasang middleware/interceptor per group. | Berisi logic |
| **Controller** | `internal/controller/{rest,rpc,consumer}` | Adapter per transport: bind + validasi input, ubah DTO ke input service, panggil service, ubah hasil/error jadi response transport. | Business logic, akses DB/cache langsung |
| **Business Logic** | `internal/service/<domain>` | Aturan bisnis dan orkestrasi repository, cache, publisher. Hanya bergantung pada **interface** (`ports.go`). | Import chi, gRPC, GORM, RabbitMQ |
| **Data** | `internal/repository/*`, `internal/model` | Akses PostgreSQL (GORM), Redis, dan sistem luar. Mengembalikan `model.*` dan menerjemahkan error DB ke error domain. | Aturan bisnis |
| Model | `internal/model` | Entity domain, event, dan error domain. Dipakai semua layer. | Import package framework |
| Platform | `internal/platform` | Setup koneksi DB, Redis, RabbitMQ, logger, telemetry. | Aturan bisnis |
| Middleware | `internal/middleware` | Request ID, request log, auth (placeholder), tracing helper. | Aturan bisnis |
| Wiring | `internal/app` | Membuat semua dependency dan menyambungkan layer (dependency injection), start/stop server. | Aturan bisnis |

### 2.2 Aturan dependency

- Arah import selalu: router -> controller -> service -> repository. Tidak ada import ke arah sebaliknya.
- Service mendefinisikan interface yang ia butuhkan di `ports.go` (contoh `user.Repository`, `user.Cache`). Implementasinya di `internal/repository`, dipasang di `internal/app/app.go`. Inilah yang membuat service bisa di-unit test dengan mock.
- REST, gRPC, dan consumer memanggil **service yang sama**. Menambah transport baru cukup menambah controller.
- Error domain (`model.ErrNotFound`, `ErrConflict`, `ErrInvalidInput`, ...) diterjemahkan ke HTTP status di `controller/rest/response.go` dan ke gRPC code di `controller/rpc/errors.go`.

### 2.3 Alur contoh `POST /api/v1/users`

1. **Router** (`router/http.go`): request masuk group `/api/v1`, lewat middleware: request ID -> request log -> auth (placeholder) -> timeout.
2. **Controller** (`controller/rest/user_handler.go`): `render.Bind` decode JSON lalu validasi DTO (`controller/dto`). Kalau gagal, balas 400 dengan detail per field.
3. **Service** (`service/user/service.go`): normalisasi email, cek email unik, hash password, simpan, lalu publish `user.created`.
4. **Data** (`repository/postgres/user_repository.go`): insert lewat GORM. Hook `BeforeCreate` membuat UUID v7. Duplicate key diterjemahkan ke `model.ErrConflict`.
5. **Consumer** (kalau aktif): `controller/consumer/user_handler.go` menerima event, service mengirim welcome email (simulasi), dengan idempotency key di Redis supaya email tidak terkirim dua kali.

### 2.4 Format response REST

```json
// sukses
{ "data": { "id": "...", "name": "Andi" }, "meta": { "request_id": "..." } }

// gagal
{
  "error": {
    "code": "INVALID_INPUT",
    "message": "request validation failed",
    "details": [{ "field": "email", "message": "must be a valid email address" }]
  },
  "meta": { "request_id": "..." }
}
```

| Error domain | HTTP | gRPC |
|---|---|---|
| `ErrInvalidInput` / `ValidationError` | 400 `INVALID_INPUT` | `InvalidArgument` (+ `BadRequest` details) |
| `ErrUnauthorized` | 401 | `Unauthenticated` |
| `ErrForbidden` | 403 | `PermissionDenied` |
| `ErrNotFound` | 404 | `NotFound` |
| `ErrConflict` | 409 | `AlreadyExists` |
| lainnya | 500 `INTERNAL` (detail tidak dikirim ke client) | `Internal` |

---

## 3. Struktur Folder

```
.
├── api/proto/user/v1/          # Kontrak gRPC (.proto)
├── cmd/
│   ├── app/                    # Entrypoint service
│   └── migrate/                # CLI migration + seed (file SQL di-embed)
├── deployments/                # Config OTel Collector, Tempo, Loki, Prometheus, Grafana
├── docs/CODEBASE_RULES.md      # Aturan codebase (generic, lintas bahasa)
├── gen/proto/                  # Hasil generate buf (di-commit, jangan diedit manual)
├── internal/
│   ├── app/                    # Wiring dependency + lifecycle (start, graceful shutdown)
│   ├── config/                 # Struct config dari env + validasi
│   ├── router/                 # [Router] http.go, grpc.go, consumer.go
│   ├── controller/
│   │   ├── dto/                # Request/response + validasi, dipakai REST dan gRPC
│   │   ├── rest/               # [Controller] REST
│   │   ├── rpc/                # [Controller] gRPC
│   │   └── consumer/           # [Controller] message consumer
│   ├── service/user/           # [Business logic] + ports.go (interface) + unit test
│   ├── repository/             # [Data] postgres/ (GORM), redis/, mailer/
│   ├── model/                  # Entity, event, error domain
│   ├── middleware/             # HTTP middleware + gRPC interceptor
│   └── platform/               # logger, telemetry, database, cache, messaging, password
├── migrations/                 # Migration schema (up/down)
├── seeds/                      # Data dummy, khusus lokal
├── docker-compose.yml          # PostgreSQL + Redis
├── docker-compose.optional.yml # Profile mq (RabbitMQ) dan otel (observability)
├── Dockerfile                  # Multi-stage, distroless, non-root
├── Makefile
└── .env.example
```

`mocks/` di setiap package hasil `go generate` (mockgen), di-commit supaya test langsung jalan setelah clone.

---

## 4. Prasyarat

| Tool | Versi | Keterangan |
|---|---|---|
| Go | 1.27.1+ | Versi dikunci di `go.mod`. Kalau Go lokal lebih lama, Go otomatis mengunduh toolchain yang sesuai (`GOTOOLCHAIN=auto`). |
| Docker + Docker Compose v2 | Docker 24+ | Untuk PostgreSQL, Redis, RabbitMQ, dan stack observability |
| make | bawaan macOS/Linux | Opsional, semua perintah juga bisa dijalankan manual |

Tidak perlu install `buf`, `protoc`, `mockgen`, atau `golangci-lint` secara global:

- `protoc-gen-go`, `protoc-gen-go-grpc`, dan `mockgen` dikunci di `go.mod` (blok `tool`) dan dijalankan lewat `go tool`.
- `buf` dan `golangci-lint` dijalankan lewat `go run` dengan versi yang dikunci di `Makefile`.

Opsional untuk mencoba gRPC: [grpcurl](https://github.com/fullstorydev/grpcurl) (`brew install grpcurl`).

---

## 5. Quick Start: dari Clone sampai Running

```bash
# 1. Clone
git clone git@github.com:z-alamsyah/codebase-go.git
cd codebase-go

# 2. Buat file .env (dibaca oleh aplikasi DAN docker compose)
cp .env.example .env

# 3. Nyalakan PostgreSQL + Redis, tunggu sampai healthy
make infra-up

# 4. Buat tabel, lalu isi 10 user dummy (password semua: Password123!)
make migrate-up
make seed

# 5. Jalankan service
make run
```

Output yang diharapkan:

```
12:31:27 INF service started env=local http_port=8080 rest=true grpc=false mq=false consumer=false otel=false log_level=info
```

Cek dari terminal lain:

```bash
curl localhost:8080/healthz
# {"status":"ok"}

curl localhost:8080/readyz
# {"status":"ok","checks":{"postgres":"ok","redis":"ok"}}

# Ambil user hasil seed
curl localhost:8080/api/v1/users/01928f6a-0000-7000-8000-000000000001

# Buat user baru
curl -X POST localhost:8080/api/v1/users \
  -H 'Content-Type: application/json' \
  -d '{"name":"Rina","email":"rina@example.com","phone":"+6281234567890","password":"Password123!"}'
```

Di terminal service akan muncul request log dengan password yang sudah disensor:

```
12:36:38 INF POST /api/v1/users => HTTP 201 (91ms) body="{\"email\":\"rina@example.com\",\"name\":\"Rina\",\"password\":\"[REDACTED]\",...}" request_id=...
```

Berhenti: `Ctrl+C` untuk service (graceful shutdown), `make infra-down` untuk container.

---

## 6. Konfigurasi (.env)

Semua config dibaca dari environment variable. File `.env` hanya untuk lokal dan tidak di-commit. Daftar lengkap dengan komentar ada di [`.env.example`](.env.example). Config divalidasi saat start; kombinasi yang salah langsung gagal dengan pesan jelas.

| Variable | Default | Fungsi |
|---|---|---|
| `APP_NAME` | `codebase-go` | Nama service: prefix key Redis, nama queue, nama koneksi RabbitMQ |
| `APP_ENV` | `local` | `production` menolak perintah seed |
| `APP_SHUTDOWN_TIMEOUT` | `15s` | Batas waktu graceful shutdown |
| `REST_ENABLED` | `true` | Route bisnis `/api/*`. Server HTTP tetap jalan untuk healthcheck walau `false`. |
| `HTTP_PORT` | `8080` | Port HTTP |
| `HTTP_REQUEST_TIMEOUT` | `10s` | Timeout per request REST |
| `HTTP_TRUSTED_PROXIES` | `0` | Jumlah proxy di depan app. `0` = abaikan `X-Forwarded-For` (tidak bisa dipalsukan client). |
| `GRPC_ENABLED` | `false` | Jalankan gRPC server |
| `GRPC_PORT` | `9090` | Port gRPC |
| `GRPC_REFLECTION_ENABLED` | `true` | Supaya `grpcurl` bisa melihat daftar service |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `pretty` | `pretty` (lokal) atau `json` (server, log collector) |
| `LOG_REDACT_KEYS` | `password,token,...` | Key yang disensor di log (tidak case-sensitive, sampai nested) |
| `LOG_BODY_MAX_BYTES` | `4096` | Batas ukuran payload di log |
| `DB_*` | lihat `.env.example` | Koneksi dan pool PostgreSQL |
| `DB_SLOW_QUERY_THRESHOLD` | `200ms` | Query lebih lambat dari ini ditulis sebagai warning |
| `REDIS_ADDR` | `localhost:6379` | Alamat Redis |
| `CACHE_TTL` | `5m` | TTL cache user |
| `MQ_ENABLED` | `false` | Koneksi RabbitMQ + publisher event |
| `RABBITMQ_URL` | `amqp://guest:guest@localhost:5672/` | URL broker |
| `RABBITMQ_EXCHANGE` | `codebase-go.events` | Topic exchange untuk event |
| `CONSUMER_ENABLED` | `false` | Jalankan consumer (butuh `MQ_ENABLED=true`) |
| `CONSUMER_PREFETCH` | `10` | Jumlah pesan yang diproses paralel per queue |
| `CONSUMER_MAX_RETRIES` | `3` | Setelah itu pesan masuk `<queue>.dlq` |
| `CONSUMER_RETRY_DELAY` | `5s` | Jeda sebelum retry |
| `OTEL_ENABLED` | `false` | Aktifkan OpenTelemetry |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4317` | Wajib kalau OTel aktif. `http://` = tanpa TLS. |
| `OTEL_SERVICE_NAME` | `codebase-go` | Nama service di Grafana |
| `OTEL_TRACES_SAMPLER_ARG` | `1.0` | Rasio trace yang disimpan |
| `OTEL_METRIC_EXPORT_INTERVAL` | `15000` | Interval kirim metric (ms) |

Variabel `OTEL_*` memakai nama standar OpenTelemetry, jadi variabel standar lain (contoh `OTEL_RESOURCE_ATTRIBUTES`, `OTEL_EXPORTER_OTLP_HEADERS`) langsung didukung SDK tanpa perubahan kode.

---

## 7. Mengaktifkan gRPC, Consumer, dan OpenTelemetry

### 7.1 gRPC

```bash
# .env
GRPC_ENABLED=true
```

```bash
make run

grpcurl -plaintext localhost:9090 list
grpcurl -plaintext localhost:9090 grpc.health.v1.Health/Check
grpcurl -plaintext -d '{"id":"01928f6a-0000-7000-8000-000000000002"}' \
  localhost:9090 user.v1.UserService/GetUser
grpcurl -plaintext -d '{"name":"Sari","email":"sari@example.com","password":"Password123!"}' \
  localhost:9090 user.v1.UserService/CreateUser
```

Health check memakai protokol standar `grpc.health.v1`, jadi bisa langsung dipakai sebagai gRPC probe di Kubernetes.

### 7.2 RabbitMQ (publisher + consumer)

```bash
make infra-mq-up        # RabbitMQ + management UI http://localhost:15672 (guest/guest)
```

```bash
# .env
MQ_ENABLED=true         # publisher aktif: POST /users akan publish user.created
CONSUMER_ENABLED=true   # worker consumer aktif
```

Saat start, consumer membuat sendiri exchange dan queue-nya. Lihat [Bagian 11](#11-membuat-dan-menggunakan-consumer).

**Peran deployment dengan satu image:**

| Peran | `REST_ENABLED` | `GRPC_ENABLED` | `MQ_ENABLED` | `CONSUMER_ENABLED` |
|---|---|---|---|---|
| API | `true` | sesuai kebutuhan | `true` | `false` |
| Worker | `false` | `false` | `true` | `true` |

Instance worker tetap melayani `/healthz` dan `/readyz` untuk probe.

### 7.3 OpenTelemetry + Grafana

```bash
make infra-otel-up      # OTel Collector, Tempo, Loki, Prometheus, Grafana
```

```bash
# .env
OTEL_ENABLED=true
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
```

Alur data:

```
app --OTLP--> OTel Collector --traces--> Tempo      \
                             --logs----> Loki        > Grafana (http://localhost:3000)
                             --metrics-> Prometheus /
```

- Buka Grafana di http://localhost:3000 (login `admin` / `admin`, boleh skip ganti password).
- Datasource Prometheus, Tempo, dan Loki sudah terpasang otomatis.
- Dashboard **codebase-go / codebase-go - Service Overview** berisi request rate, error, latency p95 (REST dan gRPC), latency query DB, memori, goroutine, log, dan trace terbaru.
- **Dari log ke trace:** di panel log, buka detail baris log, klik link `trace_id`.
- **Dari trace ke log:** di Explore > Tempo, buka trace, klik ikon log di span.
- Trace menyambung dari request REST -> query DB -> publish -> consumer dalam satu trace.

| Komponen | Port host |
|---|---|
| Grafana | 3000 |
| OTel Collector | 4317 (gRPC), 4318 (HTTP) |
| Prometheus | 9091 (9090 dipakai gRPC app) |

---

## 8. Logging

### 8.1 Level

| Level | Isi request log |
|---|---|
| `info` (default) | Satu baris per request: method, path, status, durasi, payload request (sudah disensor), request_id, trace_id |
| `debug` | Semua info di atas, ditambah header (sudah disensor), IP client, user agent, response body, query SQL, lokasi file:baris, dan event gRPC per payload |

Contoh `LOG_LEVEL=info`:

```
INF POST /api/v1/users => HTTP 201 (82ms) body="{\"email\":\"a@b.c\",\"password\":\"[REDACTED]\"}" request_id=...
WRN GET /api/v1/users/... => HTTP 404 (2ms) error="not found: user ..." request_id=...
INF gRPC /user.v1.UserService/GetUser => OK (10ms) body="{\"id\":\"...\"}" request_id=...
INF CONSUME codebase-go.user.welcome-email => ack (1ms) message_id=... attempt=0 payload="{...}"
```

Status 4xx ditulis sebagai `WRN`, 5xx sebagai `ERR`. Healthcheck tidak ditulis ke log.

### 8.2 Format

- `LOG_FORMAT=pretty`: berwarna dan ringkas, untuk lokal.
- `LOG_FORMAT=json`: untuk server dan log collector. Field memakai nama OpenTelemetry (`http.request.method`, `http.response.status_code`, ...) sehingga bisa difilter.

### 8.3 Redaction (sensor data sensitif)

- Key di `LOG_REDACT_KEYS` disensor di mana pun berada: payload REST, payload gRPC, payload pesan consumer, header, dan atribut log biasa. Pencocokan tidak case-sensitive dan masuk sampai object/array bertingkat.
- Header `Authorization` dan `Cookie` selalu disensor.
- Payload yang bukan JSON tidak pernah ditulis isinya (hanya ukurannya), karena tidak bisa disensor dengan aman.
- Payload dipotong setelah disensor (`LOG_BODY_MAX_BYTES`), jadi pemotongan tidak membuat data sensitif lolos.
- Query SQL ditulis dengan placeholder (`$1`), nilai parameternya tidak pernah ditulis.

Menambah key sensitif cukup ubah env, contoh: `LOG_REDACT_KEYS=password,token,secret,authorization,cookie,pin,otp,cvv,card_number,nik`.

### 8.4 Menulis log dari kode

Selalu pakai versi `...Context` supaya `request_id` dan `trace_id` ikut tertulis:

```go
s.log.InfoContext(ctx, "order paid", slog.String("order_id", id))
```

---

## 9. Membuat Endpoint REST Baru

Contoh: **`PATCH /api/v1/users/{id}`** untuk mengubah nama user. Ikuti urutan dari layer paling dalam ke paling luar.

**Langkah 1 - Migration (kalau schema berubah).** Contoh ini tidak butuh kolom baru. Kalau butuh:

```bash
make migrate-create name=add_users_nickname   # membuat migrations/00000N_add_users_nickname.{up,down}.sql
# isi SQL up dan down, lalu:
make migrate-up
```

**Langkah 2 - Model** (`internal/model`). Tambah field atau error domain baru kalau perlu. Contoh ini tidak perlu.

**Langkah 3 - Repository** (`internal/repository/postgres/user_repository.go`). `clause.Returning{}` (import `gorm.io/gorm/clause`) membuat PostgreSQL mengembalikan baris yang sudah di-update:

```go
func (r *UserRepository) UpdateName(ctx context.Context, id uuid.UUID, name string) (model.User, error) {
	var e userEntity
	res := r.db.WithContext(ctx).Model(&e).Clauses(clause.Returning{}).
		Where("id = ?", id).Update("name", name)
	if res.Error != nil {
		return model.User{}, fmt.Errorf("update user: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return model.User{}, fmt.Errorf("%w: user %s", model.ErrNotFound, id)
	}
	return e.toModel(), nil
}
```

**Langkah 4 - Interface di service** (`internal/service/user/ports.go`), lalu generate ulang mock:

```go
type Repository interface {
	// ...
	UpdateName(ctx context.Context, id uuid.UUID, name string) (model.User, error)
}
```

```bash
make mocks
```

**Langkah 5 - Business logic** (`internal/service/user/service.go`):

```go
func (s *Service) UpdateName(ctx context.Context, id uuid.UUID, name string) (model.User, error) {
	u, err := s.repo.UpdateName(ctx, id, strings.TrimSpace(name))
	if err != nil {
		return model.User{}, err
	}
	// Data berubah: hapus cache supaya GET berikutnya tidak membaca data lama.
	if err := s.cache.Delete(ctx, id); err != nil {
		s.log.WarnContext(ctx, "cache delete failed", slog.Any("error", err))
	}
	return u, nil
}
```

**Langkah 6 - Unit test service** (`internal/service/user/service_test.go`). Tambah fungsi `TestService_UpdateName` dengan pola table-driven yang sama: kasus sukses, not found, error repository, dan error cache yang diabaikan.

**Langkah 7 - DTO** (`internal/controller/dto/user.go`):

```go
type UpdateUserRequest struct {
	Name string `json:"name" validate:"required,min=2,max=100"`
}
```

**Langkah 8 - Controller REST** (`internal/controller/rest`):

```go
// ports.go: tambah method ke interface, lalu `make mocks`
UpdateName(ctx context.Context, id uuid.UUID, name string) (model.User, error)

// user_handler.go
type updateUserRequest struct{ dto.UpdateUserRequest }

func (u *updateUserRequest) Bind(*http.Request) error { return dto.Validate(u.UpdateUserRequest) }

func (h *UserHandler) UpdateName(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, r, model.NewValidationError("id", "must be a valid UUID"))
		return
	}
	var req updateUserRequest
	if err := bind(r, &req); err != nil { // decode + validasi, JSON rusak jadi 400
		respondError(w, r, err)
		return
	}
	u, err := h.svc.UpdateName(r.Context(), id, req.Name)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respond(w, r, http.StatusOK, dto.NewUserResponse(u))
}
```

Tambahkan juga test di `user_handler_test.go`.

**Langkah 9 - Router** (`internal/router/http.go`). Route di dalam group `/api/v1` otomatis mendapat middleware request log, auth, dan timeout:

```go
r.Route("/users", func(r chi.Router) {
	r.Post("/", d.User.Create)
	r.Get("/{id}", d.User.GetByID)
	r.Patch("/{id}", d.User.UpdateName) // baru
})
```

Untuk domain baru (contoh `orders`), buat `service/order`, `repository/postgres/order_repository.go`, `controller/rest/order_handler.go`, sambungkan di `internal/app/app.go`, lalu tambah `r.Route("/orders", ...)`.

**Langkah 10 - Cek:**

```bash
make test lint
make run
curl -X PATCH localhost:8080/api/v1/users/01928f6a-0000-7000-8000-000000000001 \
  -H 'Content-Type: application/json' -d '{"name":"Andi P."}'
```

---

## 10. Membuat Endpoint gRPC Baru

Contoh: RPC **`UpdateUserName`** yang memakai service dari Bagian 9.

**Langkah 1 - Kontrak** (`api/proto/user/v1/user.proto`):

```proto
service UserService {
  // ...
  rpc UpdateUserName(UpdateUserNameRequest) returns (UpdateUserNameResponse);
}

message UpdateUserNameRequest {
  string id = 1;
  string name = 2;
}

message UpdateUserNameResponse {
  User user = 1;
}
```

**Langkah 2 - Generate kode Go** (hasil di `gen/proto`, di-commit):

```bash
make proto      # buf generate
make lint       # termasuk buf lint untuk aturan penamaan proto
```

**Langkah 3 - Interface controller** (`internal/controller/rpc/ports.go`), lalu `make mocks`:

```go
UpdateName(ctx context.Context, id uuid.UUID, name string) (model.User, error)
```

**Langkah 4 - Implementasi** (`internal/controller/rpc/user_server.go`):

```go
func (s *UserServer) UpdateUserName(ctx context.Context, req *userv1.UpdateUserNameRequest) (*userv1.UpdateUserNameResponse, error) {
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return nil, toStatus(ctx, model.NewValidationError("id", "must be a valid UUID"))
	}
	in := dto.UpdateUserRequest{Name: req.GetName()}
	if err := dto.Validate(in); err != nil {
		return nil, toStatus(ctx, err) // InvalidArgument + detail per field
	}
	u, err := s.svc.UpdateName(ctx, id, in.Name)
	if err != nil {
		return nil, toStatus(ctx, err)
	}
	return &userv1.UpdateUserNameResponse{User: toProtoUser(u)}, nil
}
```

**Langkah 5 - Registrasi.** RPC baru di service yang sudah ada tidak perlu registrasi lagi. Untuk **service gRPC baru** (contoh `order.v1.OrderService`), daftarkan di `internal/router/grpc.go`:

```go
orderv1.RegisterOrderServiceServer(srv, d.Order)
hs.SetServingStatus(orderv1.OrderService_ServiceDesc.ServiceName, healthpb.HealthCheckResponse_SERVING)
```

Interceptor request ID, request log, auth (placeholder), dan recovery otomatis berlaku untuk semua service kecuali `grpc.*` (health, reflection).

**Langkah 6 - Test dan coba:**

```bash
make test
GRPC_ENABLED=true make run
grpcurl -plaintext -d '{"id":"01928f6a-0000-7000-8000-000000000001","name":"Andi P."}' \
  localhost:9090 user.v1.UserService/UpdateUserName
```

Contoh test gRPC in-memory (bufconn) ada di `internal/controller/rpc/user_server_test.go`.

---

## 11. Membuat dan Menggunakan Consumer

### 11.1 Cara kerja

```
publisher --(routing key)--> exchange codebase-go.events (topic)
                                   |
                                   v
                          <queue>  ---- handler sukses ---------------> ack
                             ^      \--- handler gagal, attempt < max -> <queue>.retry (tunggu RETRY_DELAY)
                             |                                               |
                             +--------------- kembali otomatis --------------+
                                    \--- gagal, attempt >= max
                                         atau error Permanent -------> <queue>.dlq
```

- **Manual ack:** pesan baru dihapus dari queue setelah handler sukses.
- **Prefetch:** maksimal `CONSUMER_PREFETCH` pesan diproses paralel per queue.
- **Retry:** pesan gagal dikirim ke `<queue>.retry`, menunggu `CONSUMER_RETRY_DELAY`, lalu kembali ke queue utama. Header `x-retry-count` mencatat percobaan ke berapa.
- **Dead letter queue (DLQ):** setelah `CONSUMER_MAX_RETRIES` kali gagal, pesan masuk `<queue>.dlq` dengan header `x-last-error`.
- **Error permanen:** handler mengembalikan `messaging.Permanent(err)` untuk pesan yang pasti gagal kalau diulang (contoh payload rusak). Pesan langsung ke DLQ tanpa retry.
- **Auto reconnect:** kalau koneksi RabbitMQ putus, consumer menyambung ulang dengan jeda bertambah (1s sampai 30s).
- **Graceful drain:** saat shutdown, consumer berhenti mengambil pesan baru dan menyelesaikan pesan yang sedang diproses.
- **Trace:** trace context dikirim di header pesan, jadi trace REST -> publish -> consume tersambung.

**Penting: handler harus idempotent**, artinya aman kalau pesan yang sama diproses dua kali (RabbitMQ menjamin pesan terkirim *minimal* sekali, bukan *tepat* sekali). Contohnya ada di `service.SendWelcomeEmail`: memakai `IdempotencyStore` (Redis `SET NX`) dengan key per user.

### 11.2 Mencoba consumer contoh

```bash
make infra-mq-up
MQ_ENABLED=true CONSUMER_ENABLED=true make run
```

```bash
# Buat user -> service publish user.created -> consumer kirim welcome email (simulasi)
curl -X POST localhost:8080/api/v1/users -H 'Content-Type: application/json' \
  -d '{"name":"Tono","email":"tono@example.com","password":"Password123!"}'
```

Log yang muncul:

```
INF welcome email sent (simulated) to=tono@example.com name=Tono ...
INF CONSUME codebase-go.user.welcome-email => ack (1.1ms) message_id=... attempt=0 payload="{...}"
```

Publish pesan manual tanpa lewat API: buka http://localhost:15672 > **Exchanges** > `codebase-go.events` > **Publish message**, isi routing key `user.created` dan payload:

```json
{"id":"manual-1","type":"user.created","version":1,"payload":{"user_id":"u-1","name":"Test","email":"test@example.com"}}
```

Pesan yang gagal bisa dilihat di queue `codebase-go.user.welcome-email.dlq` (tombol **Get messages**).

### 11.3 Langkah membuat consumer baru

Contoh: saat **order dibayar**, kirim notifikasi.

**Langkah 1 - Definisikan event** di `internal/model` (nama event = routing key):

```go
const EventOrderPaid = "order.paid"

type OrderPaidEvent struct {
	OrderID string `json:"order_id"`
	UserID  string `json:"user_id"`
	Amount  int64  `json:"amount"`
}
```

**Langkah 2 - Publish dari service** yang memiliki kejadiannya, memakai interface `Publisher` (sudah otomatis no-op kalau `MQ_ENABLED=false`):

```go
if err := s.pub.Publish(ctx, model.EventOrderPaid, model.OrderPaidEvent{...}); err != nil {
	s.log.ErrorContext(ctx, "publish event failed", slog.Any("error", err))
}
```

**Langkah 3 - Business logic consumer** di service tujuan (contoh `service/notification`). Buat idempotent, contoh dengan `IdempotencyStore.Acquire(ctx, "order-paid:"+ev.OrderID, ttl)`. Tambahkan unit test-nya.

**Langkah 4 - Controller consumer** (`internal/controller/consumer/order_handler.go`):

```go
func (h *OrderHandler) HandleOrderPaid(ctx context.Context, msg messaging.Message) error {
	var ev model.OrderPaidEvent
	if err := json.Unmarshal(msg.Payload, &ev); err != nil {
		return messaging.Permanent(fmt.Errorf("decode payload: %w", err)) // langsung ke DLQ
	}
	return h.svc.NotifyOrderPaid(ctx, ev) // error biasa = retry
}
```

**Langkah 5 - Daftarkan route** di `internal/router/consumer.go` (satu baris per consumer):

```go
{Queue: d.App.Name + ".order.paid-notification", RoutingKey: model.EventOrderPaid, Handler: d.Order.HandleOrderPaid},
```

Lalu buat handler-nya di `internal/app/app.go` dan masukkan ke `router.ConsumerDeps`. Exchange, queue, retry queue, dan DLQ dibuat otomatis saat start. Satu routing key boleh dipakai beberapa queue (setiap queue mendapat salinan pesannya sendiri).

**Langkah 6 - Coba:** jalankan dengan `MQ_ENABLED=true CONSUMER_ENABLED=true`, publish event, lalu cek log `CONSUME ... => ack` dan isi DLQ.

---

## 12. Testing

```bash
make test       # go test -race ./...
make cover      # + ringkasan coverage dan coverage.html
make lint       # golangci-lint + buf lint
```

| Yang dites | Lokasi | Cara |
|---|---|---|
| Business logic (wajib) | `internal/service/user/service_test.go` | Table-driven, semua dependency di-mock (mockgen). Coverage 100%. |
| Controller REST | `internal/controller/rest/*_test.go` | `httptest`, tanpa membuka port |
| Controller gRPC | `internal/controller/rpc/user_server_test.go` | Server in-memory (`bufconn`) |
| Controller consumer | `internal/controller/consumer/user_handler_test.go` | Mock service |
| Redaction log | `internal/platform/logger/redact_test.go` | Termasuk nested, header, proto |
| Keputusan retry/DLQ | `internal/platform/messaging/rabbitmq/consumer_test.go` | Fungsi murni `decide` |
| Validasi config | `internal/config/config_test.go` | Kombinasi flag tidak valid |

Unit test tidak butuh PostgreSQL, Redis, atau RabbitMQ. Mock dibuat dari `ports.go` lewat `//go:generate` (`make mocks`).

---

## 13. Migration dan Seed

- **Migration** (`migrations/`): perubahan schema, aman untuk production, versinya dicatat di tabel `schema_migrations`.
- **Seed** (`seeds/`): data dummy untuk lokal, dicatat terpisah di tabel `seed_migrations`. Ditolak kalau `APP_ENV=production`.
- File SQL di-embed ke binary `migrate`, jadi image Docker bisa menjalankan migration tanpa file tambahan.
- Fitur `AutoMigrate` GORM sengaja **tidak** dipakai, supaya setiap perubahan schema tercatat dan bisa di-rollback.

```bash
make migrate-up                         # terapkan semua migration
make migrate-down                       # rollback 1 migration
make migrate-version                    # versi sekarang
make migrate-create name=add_orders     # buat pasangan file up/down baru
make seed                               # isi data dummy
make seed-down                          # hapus data dummy
go run ./cmd/migrate force 1            # perbaiki status "dirty" setelah migration gagal
```

User dummy (password semua `Password123!`): `andi@example.com` sampai `joko@example.com`, dengan id `01928f6a-0000-7000-8000-000000000001` sampai `...000000000010`.

---

## 14. Perintah Make

Jalankan `make` atau `make help` untuk daftar lengkap.

| Perintah | Fungsi |
|---|---|
| `make run` | Jalankan service |
| `make build` | Build binary ke `./bin` |
| `make test` / `make cover` | Unit test / dengan coverage |
| `make lint` / `make fmt` | Lint / format (Go + proto) |
| `make generate` | `make proto` + `make mocks` |
| `make migrate-up` / `migrate-down` / `seed` | Database |
| `make infra-up` | PostgreSQL + Redis |
| `make infra-mq-up` | RabbitMQ |
| `make infra-otel-up` | Stack observability |
| `make infra-all-up` | Semua dependency |
| `make infra-down` | Stop semua container (data tetap ada) |
| `make infra-reset` | Stop dan **hapus semua data** container |
| `make docker-build` | Build image aplikasi |

---

## 15. Troubleshooting

| Gejala | Penyebab | Solusi |
|---|---|---|
| `invalid config: CONSUMER_ENABLED=true requires MQ_ENABLED=true` | Kombinasi flag salah | Set `MQ_ENABLED=true` |
| `ping postgres: ... connection refused` | PostgreSQL belum jalan | `make infra-up`, cek `docker compose ps` |
| `bind: address already in use` | Port dipakai proses lain | Ganti `HTTP_PORT` / `GRPC_PORT` / port di `.env` |
| `/readyz` balas 503 | Salah satu dependency mati | Lihat field `checks` di response |
| Log `redis not reachable at startup` | Redis mati | Service tetap jalan tanpa cache; nyalakan Redis |
| `Dirty database version N` | Migration gagal di tengah | Perbaiki SQL, lalu `go run ./cmd/migrate force <versi terakhir yang sukses>` |
| `PRECONDITION_FAILED - inequivalent arg` di RabbitMQ | Queue sudah ada dengan argumen berbeda | Hapus queue tersebut di UI RabbitMQ, lalu start ulang |
| Trace/log tidak muncul di Grafana | OTel mati atau collector belum jalan | Cek `OTEL_ENABLED=true`, `make infra-otel-up`, `docker compose -f docker-compose.optional.yml logs otel-collector` |
| Metric baru muncul setelah beberapa detik | Metric dikirim berkala | Tunggu `OTEL_METRIC_EXPORT_INTERVAL` (default 15 detik) |
| `the Go language version ... is lower than the targeted Go version` saat lint | golangci-lint dibangun dengan Go lama | Jalankan lewat `make lint` (memakai versi Go dari `go.mod`) |
| Port 9090 bentrok dengan Prometheus | - | Prometheus lokal sengaja di port host 9091 |

---

## 16. Catatan untuk Production

Template ini fokus ke struktur dan pola. Sebelum production, pertimbangkan:

- **Auth:** isi placeholder di `internal/middleware/http.go` (`Auth`) dan `internal/middleware/grpc.go` (`GRPCAuth`).
- **Event tidak boleh hilang:** sekarang event dipublish setelah data tersimpan; kalau broker mati saat itu, event hilang (tercatat di log error). Untuk jaminan penuh pakai *transactional outbox*: simpan event di tabel yang sama dalam satu transaksi DB, lalu kirim ke broker oleh proses terpisah.
- **Queue RabbitMQ:** template memakai classic durable queue. Untuk cluster, pertimbangkan *quorum queue* (`x-queue-type: quorum`).
- **Log:** pakai `LOG_FORMAT=json` dan `LOG_LEVEL=info` di server. Level `debug` menulis header dan response body.
- **Sampling trace:** turunkan `OTEL_TRACES_SAMPLER_ARG` (contoh `0.1`) untuk traffic besar.
- **Proxy:** set `HTTP_TRUSTED_PROXIES` sesuai jumlah load balancer/ingress di depan app supaya IP client di log benar.
- **gRPC reflection:** matikan (`GRPC_REFLECTION_ENABLED=false`) kalau tidak ingin daftar service terlihat publik.
- **Secret:** isi `DB_PASSWORD`, `RABBITMQ_URL`, dan lainnya dari secret manager, bukan dari file `.env`.
- **Grafana lokal** memakai login `admin/admin` dan bukan konfigurasi production.
