# Codebase Rules - Backend Service Template

Aturan baku untuk setup codebase backend service baru. Dokumen ini sengaja dibuat **tidak terikat bahasa atau framework**, jadi bisa dipakai ulang untuk Go, Node.js, Java, Python, PHP, dan lain-lain.

## 0. Cara Pakai Dokumen Ini

- **WAJIB**: harus ada di codebase. Kalau tidak dipenuhi, codebase dianggap belum selesai.
- **DISARANKAN**: sebaiknya ada. Boleh dilewati kalau ada alasan jelas, dan alasannya ditulis di README.
- **OPSIONAL**: tambahan, dipakai kalau dibutuhkan.

Sebelum mulai setup, isi dulu **Tabel Keputusan Stack** di Lampiran A. Semua pilihan library harus mengikuti Bagian 1.

---

## 1. Prinsip Umum

1. **WAJIB** - Manfaatkan framework. Kalau framework atau library resminya sudah menyediakan fitur (router group, middleware, request binding, validator, health protocol, interceptor, annotation/decorator), pakai itu. Jangan tulis ulang dari nol.
2. **WAJIB** - Pilih library yang matang dan banyak dipakai. Kalau tim sudah punya standar library di project lain, ikuti standar itu supaya familiar.
3. **WAJIB** - Semua perilaku yang beda antar environment diatur lewat environment variable (`.env`), bukan diubah di kode.
4. **WAJIB** - Clone sampai running harus mulus. Kode hasil generate (dari file kontrak API, mock, dan sejenisnya) di-commit ke repo, atau dibuat otomatis oleh satu perintah yang tertulis di README.
5. **WAJIB** - Versi bahasa dan tools dikunci (pin) di file konfigurasi project, supaya semua developer dan CI memakai versi yang sama.
6. **DISARANKAN** - Pakai versi stabil terbaru dari bahasa dan framework saat setup dilakukan.

---

## 2. Arsitektur Layer

### 2.1 Daftar Layer

| Layer | Tanggung jawab | Tidak boleh |
|---|---|---|
| **Router** | Daftarkan route/RPC/queue ke controller, pasang middleware/interceptor per group | Berisi logic apa pun |
| **Controller** | Adapter per transport: terima input, bind + validasi, ubah DTO ke model, panggil service, ubah hasil/error jadi response transport | Berisi business logic, akses DB/cache langsung |
| **Service (Business Logic / Usecase)** | Aturan bisnis, orkestrasi repository, cache, dan publisher, kontrol transaksi | Tahu soal HTTP, gRPC, broker, atau library DB |
| **Data (Repository + Model)** | Akses DB/cache, mapping baris DB ke model, terjemahkan error DB ke domain error | Berisi aturan bisnis |
| **Platform / Infra** (pendukung) | Setup koneksi DB, cache, broker, logger, telemetry, config | Berisi aturan bisnis |
| **Middleware** (pendukung) | Hal yang berlaku lintas endpoint: request id, log, auth, recover, timeout | Berisi aturan bisnis |

### 2.2 Aturan Dependency

1. **WAJIB** - Arah dependency satu arah: Router -> Controller -> Service -> Repository. Tidak boleh ada import ke arah sebaliknya.
2. **WAJIB** - Service hanya bergantung pada **interface** (repository, cache, publisher). Implementasi konkret dipasang saat aplikasi start (dependency injection). Ini yang membuat service bisa di-unit test.
3. **WAJIB** - Interface didefinisikan di sisi pemakai (service mendefinisikan interface repository yang dia butuhkan), kecuali konvensi bahasanya berbeda.
4. **WAJIB** - Controller dibuat **per transport** (REST, gRPC, consumer), tapi semuanya memanggil service yang sama. Menambah transport baru cukup menambah controller, tanpa menyentuh business logic.
5. **WAJIB** - Pisahkan DTO (bentuk request/response transport) dari model/entity domain. Mapping dilakukan di controller.
6. **WAJIB** - Error domain (contoh: `NotFound`, `Conflict`, `InvalidInput`, `Unauthorized`) didefinisikan di layer domain/service. Repository menerjemahkan error DB ke error domain. Controller menerjemahkan error domain ke status transport (lihat Bagian 11).
7. **DISARANKAN** - Transaksi yang melibatkan lebih dari satu repository dikontrol oleh service lewat helper transaksi (unit of work), bukan oleh repository.

### 2.3 Gambaran Alur

```
            REST request        gRPC call         Message dari broker
                 |                  |                     |
              [Router]          [Router]              [Router]
           (route + mw)    (service + interceptor)  (queue -> handler)
                 |                  |                     |
        [Controller REST]  [Controller gRPC]   [Controller Consumer]
                 \                  |                     /
                  +---------- [Service / Usecase] -------+
                                    |
                 +------------------+------------------+
                 |                  |                  |
          [Repository DB]    [Repository Cache]   [Publisher]
```

---

## 3. Transport dan Toggle

### 3.1 Flag Environment

| Flag | Default | Fungsi |
|---|---|---|
| `REST_ENABLED` | `true` | Aktifkan route bisnis REST (`/api/*`) |
| `GRPC_ENABLED` | `false` | Jalankan gRPC server dan daftarkan service gRPC |
| `MQ_ENABLED` | `false` | Buka koneksi ke broker dan aktifkan publisher |
| `CONSUMER_ENABLED` | `false` | Jalankan worker consumer dan daftarkan handler |
| `OTEL_ENABLED` | `false` | Aktifkan OpenTelemetry (trace, metric, log export) |

Nama flag boleh disesuaikan dengan konvensi bahasa, tapi maknanya harus sama. Nama broker spesifik (contoh `RABBITMQ_*`) boleh dipakai untuk config koneksinya.

### 3.2 Aturan

1. **WAJIB** - Default yang aktif hanya REST. Komponen yang tidak di-enable **tidak boleh** membuka koneksi atau mendaftarkan apa pun.
2. **WAJIB** - HTTP server untuk healthcheck **selalu menyala**, walaupun `REST_ENABLED=false`. Alasannya: instance yang hanya menjalankan consumer tetap butuh endpoint untuk probe orchestrator (contoh Kubernetes). `REST_ENABLED` hanya mengatur route bisnis.
3. **WAJIB** - Kombinasi flag yang tidak valid harus gagal saat start dengan pesan error yang jelas. Contoh: `CONSUMER_ENABLED=true` tapi `MQ_ENABLED=false`.
4. **WAJIB** - Satu artifact (image/binary) bisa dipakai untuk beberapa peran deployment hanya dengan beda env. Contoh: instance API (`REST=true, MQ=true, CONSUMER=false`) dan instance worker (`REST=false, MQ=true, CONSUMER=true`).
5. **WAJIB** - Graceful shutdown: saat menerima sinyal stop (SIGTERM/SIGINT), aplikasi berhenti menerima request baru, menunggu request dan pesan yang sedang diproses selesai (dengan batas waktu yang bisa diatur), flush telemetry, lalu menutup koneksi DB, cache, dan broker.

---

## 4. Healthcheck

1. **WAJIB** - REST:
   - `GET /healthz` (liveness): hanya memastikan proses hidup. Tidak mengecek dependency.
   - `GET /readyz` (readiness): mengecek dependency yang **aktif saja** (DB, cache, broker). Balas status gagal kalau salah satu tidak sehat, beserta detail per dependency.
2. **WAJIB** - gRPC: pakai **protokol health standar gRPC** (`grpc.health.v1.Health`) yang disediakan library gRPC, bukan RPC buatan sendiri. Dengan begitu langsung kompatibel dengan probe Kubernetes dan tools seperti `grpcurl`.
3. **WAJIB** - Endpoint healthcheck **tidak** melewati middleware auth dan **tidak** ditulis di request log, supaya log tidak penuh oleh probe.
4. **DISARANKAN** - gRPC reflection aktif di environment non-production supaya mudah dicoba dengan tools.

---

## 5. Middleware dan Interceptor

1. **WAJIB** - Semua endpoint bisnis (REST dan gRPC) melewati middleware/interceptor. Healthcheck dikecualikan.
2. **WAJIB** - Middleware auth disiapkan sebagai **placeholder kosong** (langsung meneruskan request) dengan komentar TODO yang jelas tempat logic auth nanti ditulis.
3. **WAJIB** - Urutan middleware yang disarankan:
   - Request ID (ambil dari header `X-Request-ID` kalau ada, kalau tidak buat baru, dan kembalikan di response header)
   - Recover dari panic
   - Tracing (kalau OTel aktif)
   - Request log
   - Auth (placeholder)
   - Timeout per request
4. **WAJIB** - Pakai mekanisme bawaan framework untuk mengelompokkan route (route group) dan memasang middleware. Untuk gRPC, pakai interceptor chain dan mekanisme selector bawaan library untuk mengecualikan health service.
5. **WAJIB** - IP client hanya diambil dari header `X-Forwarded-For` / `X-Real-IP` kalau jumlah proxy tepercaya di depan aplikasi dikonfigurasi lewat env. Default-nya pakai alamat koneksi TCP, karena header tersebut bisa dipalsukan oleh client.
6. **WAJIB** - Request ID dari header client dibatasi panjangnya. Kalau tidak ada atau tidak valid, buat ID baru (contoh UUID). Jangan memakai ID yang membocorkan informasi internal seperti hostname.

---

## 6. Logging

### 6.1 Konfigurasi

| Env | Nilai | Keterangan |
|---|---|---|
| `LOG_LEVEL` | `debug`, `info`, `warn`, `error` | Default `info` |
| `LOG_FORMAT` | `pretty`, `json` | `pretty` untuk lokal (berwarna, satu baris, mudah dibaca), `json` untuk server dan log collector |
| `LOG_REDACT_KEYS` | daftar dipisah koma | Key yang nilainya disensor |
| `LOG_BODY_MAX_BYTES` | angka | Batas ukuran payload yang ditulis ke log |

### 6.2 Aturan

1. **WAJIB** - Pakai structured logging (log berbentuk pasangan key-value), bukan string bebas.
2. **WAJIB** - Setiap request masuk (REST, gRPC) dan setiap pesan yang di-consume ditulis ke log dengan field minimal:
   - waktu, level, request id, trace id (kalau OTel aktif)
   - method + path/route pattern (REST), nama method RPC (gRPC), atau nama queue/routing key (consumer)
   - status hasil dan durasi
   - payload request yang sudah di-redact
3. **WAJIB** - Level `info` ke atas: format ringkas, satu baris per request. Pada format `json`, data yang sama juga ditulis sebagai field terpisah (method, path, status, durasi) supaya bisa difilter di log collector.
4. **WAJIB** - Level `debug`: tambahkan detail, yaitu header (sudah di-redact), query string, response body, IP client, user agent, lokasi file:baris, dan query DB.
5. **WAJIB** - Redaction (penyensoran data sensitif):
   - Daftar key bisa diatur lewat env, tidak case-sensitive, dan dicek sampai ke object/array bertingkat.
   - Default minimal: `password`, `token`, `secret`, `authorization`, `pin`, `otp`, `cvv`, `card_number`.
   - Header `Authorization` dan `Cookie` selalu disensor.
   - Berlaku sama untuk payload REST, gRPC, dan pesan consumer.
   - Nilai diganti dengan penanda tetap, contoh `[REDACTED]`.
6. **WAJIB** - Payload dipotong sesuai `LOG_BODY_MAX_BYTES`. Payload biner atau multipart tidak ditulis isinya, cukup ukurannya.
7. **WAJIB** - Saat start, jangan pernah menulis nilai secret dari config ke log.
8. **WAJIB** - Request id dan trace id dibawa lewat context, supaya log di layer mana pun otomatis mencantumkannya.

---

## 7. Observability (OpenTelemetry)

1. **WAJIB** - Mati secara default. Kalau `OTEL_ENABLED=true`, config endpoint collector wajib diisi. Kalau kosong, aplikasi gagal start dengan pesan jelas.
2. **WAJIB** - Pakai **nama env standar OpenTelemetry** (`OTEL_SERVICE_NAME`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG`, `OTEL_RESOURCE_ATTRIBUTES`) supaya dibaca otomatis oleh SDK, tanpa mapping manual.
3. **WAJIB** - Traces: pasang instrumentasi resmi/populer untuk HTTP server, gRPC, DB, cache, dan broker. Trace context ikut dikirim di header pesan broker, supaya trace nyambung dari publisher ke consumer.
4. **WAJIB** - Metrics: minimal metrik RED (Rate = jumlah request, Errors = jumlah error, Duration = latensi) per endpoint.
5. **WAJIB** - Logs: dikirim lewat OTLP ke collector dan tetap ditulis ke stdout. Setiap log membawa `trace_id` dan `span_id`, supaya di Grafana bisa lompat dari trace ke log dan sebaliknya.
6. **WAJIB** - Telemetry di-flush saat shutdown. Kalau backend telemetry mati, aplikasi **tidak boleh** ikut crash.

---

## 8. Messaging / Consumer

1. **WAJIB** - Broker default: **RabbitMQ**. Akses broker lewat interface (publisher, consumer) supaya bisa diganti broker lain (contoh Kafka) tanpa mengubah service.
2. **WAJIB** - Router consumer: registry yang memetakan queue/routing key ke handler controller. Menambah consumer baru cukup daftar satu baris di registry.
3. **WAJIB** - Perilaku consumer:
   - Manual ack: pesan baru dianggap selesai setelah handler sukses.
   - Prefetch: batas jumlah pesan yang diproses bersamaan, bisa diatur lewat env.
   - Retry dengan jeda (backoff) dan batas jumlah percobaan.
   - Dead letter queue (DLQ): antrian khusus penampung pesan yang tetap gagal setelah retry habis.
   - Auto reconnect saat koneksi broker putus.
   - Graceful drain: saat shutdown, berhenti ambil pesan baru dan selesaikan pesan yang sedang diproses.
4. **WAJIB** - Handler harus aman diproses lebih dari sekali untuk pesan yang sama (idempotent). Broker bisa mengirim ulang pesan, jadi hasil akhirnya harus tetap sama walau handler jalan dua kali. Contoh caranya: cek `message_id` yang sudah diproses, atau pakai operasi upsert.
5. **WAJIB** - Format pesan (envelope) seragam: `id`, `type`, `occurred_at`, `version`, `payload`.
6. **WAJIB** - Topologi (exchange, queue, binding, DLQ) dideklarasikan otomatis oleh aplikasi saat start, dan bisa diatur lewat config.
7. **WAJIB** - Mengubah config consumer (contoh jeda retry) tidak boleh mengharuskan hapus queue manual. Contoh di RabbitMQ: jeda retry diatur per pesan (expiration), bukan sebagai argumen queue.
8. **WAJIB** - Retry dan DLQ tetap menyimpan trace context dan penyebab error terakhir (di header pesan), supaya pesan yang gagal mudah ditelusuri.

---

## 9. Data Layer

1. **WAJIB** - Database default: **PostgreSQL**.
2. **WAJIB** - Akses DB memakai ORM atau query builder yang ditetapkan di Tabel Keputusan Stack. Jangan campur beberapa cara akses di satu codebase.
3. **WAJIB** - Perubahan schema hanya lewat **file migration berversi** (ada `up` dan `down`). Fitur auto-migrate dari ORM **tidak boleh** dipakai untuk mengubah schema, karena perubahan jadi tidak tercatat dan sulit di-rollback.
4. **WAJIB** - Seed data dummy dipisah dari migration schema (folder berbeda, perintah berbeda), dan hanya untuk lokal/dev. Tujuannya supaya data dummy tidak ikut masuk production.
5. **WAJIB** - Connection pool (max open, max idle, lifetime) diatur lewat env.
6. **WAJIB** - Repository mengembalikan model domain, bukan objek internal ORM/driver, dan menerjemahkan error DB (not found, duplicate key) ke error domain.
7. **DISARANKAN** - Primary key UUID, kolom `created_at` dan `updated_at` di setiap tabel. Soft delete (`deleted_at`) sesuai kebutuhan.
8. **DISARANKAN** - Data sensitif seperti password disimpan dalam bentuk hash (contoh bcrypt/argon2), termasuk di data seed.

---

## 10. Cache

1. **WAJIB** - Cache default: **Redis**, diakses lewat interface.
2. **WAJIB** - Pola cache-aside: service cek cache dulu. Kalau tidak ada, ambil dari DB lalu simpan ke cache dengan TTL.
3. **WAJIB** - TTL bisa diatur lewat env. Format key konsisten: `<nama-service>:<entity>:<id>`.
4. **WAJIB** - Data di cache dihapus atau diperbarui saat data aslinya berubah.
5. **WAJIB** - Kalau cache gagal (timeout, mati), request **tidak boleh** ikut gagal. Lanjut ambil dari DB dan tulis log level `warn`.

---

## 11. Error dan Response

1. **WAJIB** - Mapping error domain ke transport didefinisikan di satu tempat per transport:

| Error domain | HTTP | gRPC |
|---|---|---|
| InvalidInput | 400 | `INVALID_ARGUMENT` |
| Unauthorized | 401 | `UNAUTHENTICATED` |
| Forbidden | 403 | `PERMISSION_DENIED` |
| NotFound | 404 | `NOT_FOUND` |
| Conflict | 409 | `ALREADY_EXISTS` |
| Lainnya | 500 | `INTERNAL` |

2. **WAJIB** - Format response JSON REST seragam untuk sukses dan gagal. Contoh:
   - Sukses: `{ "data": { ... }, "meta": { "request_id": "..." } }`
   - Gagal: `{ "error": { "code": "NOT_FOUND", "message": "...", "details": [...] }, "meta": { "request_id": "..." } }`
3. **WAJIB** - Error internal (stack trace, pesan error DB) **tidak boleh** dikirim ke client. Cukup ditulis di log.
4. **WAJIB** - Error validasi mengembalikan detail per field.

---

## 12. Kontrak API

1. **WAJIB** - REST berversi di path: `/api/v1/...`.
2. **WAJIB** - gRPC didefinisikan di file `.proto` dengan package berversi (contoh `user.v1`). Kode hasil generate di-commit, dan perintah generate tertulis di README.
3. **WAJIB** - Validasi input dilakukan di batas controller, memakai validator bawaan/populer di ekosistem framework.
4. **WAJIB** - Spec OpenAPI/Swagger untuk REST, dibuat otomatis dari kode (anotasi atau definisi route) dan di-commit. Spec ini jadi kontrak untuk frontend (generate client TypeScript bertipe). UI dokumentasinya bisa dimatikan lewat env, dan default-nya mati di production.
5. **WAJIB** - Ada perintah untuk generate ulang spec dan pengecekan di CI yang gagal kalau spec tidak sesuai dengan kode.

---

## 13. Konfigurasi

1. **WAJIB** - Config dibaca dari env (dengan dukungan file `.env` untuk lokal) ke dalam struct/object bertipe.
2. **WAJIB** - Config divalidasi saat start (field wajib, format, kombinasi flag). Kalau tidak valid, aplikasi langsung berhenti dengan pesan jelas.
3. **WAJIB** - `.env.example` berisi **semua** variabel, lengkap dengan nilai default yang aman untuk lokal dan komentar penjelasan.
4. **WAJIB** - `.env` masuk `.gitignore`. Secret tidak pernah di-commit.

---

## 14. Testing

1. **WAJIB** - Unit test untuk **semua method di layer service**. Setiap jalur sukses dan setiap jalur error harus punya test case.
2. **WAJIB** - Dependency service (repository, cache, publisher) di-mock lewat interface. Unit test tidak boleh butuh DB, cache, atau broker sungguhan.
3. **WAJIB** - Pakai pola table-driven (daftar kasus input dan output yang diharapkan dalam satu tabel) kalau didukung bahasanya.
4. **WAJIB** - Unit test untuk redactor log.
5. **DISARANKAN** - Test controller REST dan gRPC memakai server in-memory (tanpa membuka port).
6. **DISARANKAN** - Coverage layer service minimal 80%.
7. **OPSIONAL** - Integration test repository memakai DB sungguhan di container (contoh Testcontainers), dipisah dari unit test supaya unit test tetap cepat.

---

## 15. Environment Lokal (Docker Compose)

1. **WAJIB** - `docker-compose.yml` (core): PostgreSQL dan Redis saja.
2. **WAJIB** - `docker-compose.optional.yml` memakai **compose profiles** supaya bisa dinyalakan sebagian:
   - Profile `mq`: RabbitMQ (dengan management UI).
   - Profile `otel`: OpenTelemetry Collector, penyimpanan log (contoh Loki), penyimpanan trace (contoh Tempo), penyimpanan metric (contoh Prometheus), dan Grafana.
3. **WAJIB** - Grafana sudah ter-provision otomatis: datasource log, trace, dan metric terpasang, plus link trace ke log dan log ke trace. Developer tidak perlu setting manual.
4. **WAJIB** - Setiap container punya healthcheck, volume bernama untuk data, dan port bisa diubah lewat env.
5. **WAJIB** - Aplikasi bisa dijalankan langsung di host (tanpa container) dan terhubung ke dependency di compose.
6. **DISARANKAN** - Dockerfile multi-stage untuk build image aplikasi yang kecil dan tidak berjalan sebagai root.

---

## 16. Contoh Fitur Wajib (Users)

Tujuannya memberi contoh nyata yang menyentuh **setiap layer**.

1. **WAJIB** - Migration tabel `users` dan seed beberapa user dummy (password sudah di-hash).
2. **WAJIB** - REST:
   - `GET /api/v1/users/{id}`: contoh cache-aside Redis.
   - `POST /api/v1/users`: contoh validasi, redaction password di log, dan publish event `user.created`.
3. **WAJIB** - gRPC: `UserService.GetUser` dan `UserService.CreateUser` yang memanggil service yang sama.
4. **WAJIB** - Consumer: handler untuk event `user.created` (contoh: simulasi kirim email selamat datang, cukup ditulis ke log).
5. **WAJIB** - Unit test service untuk semua kasus di atas.

---

## 17. Developer Tooling

1. **WAJIB** - Task runner standar di ekosistem bahasa tersebut, dengan perintah minimal: `run`, `test`, `lint`, `generate`, `migrate-up`, `migrate-down`, `seed`, `infra-up`, `infra-down`.
2. **WAJIB** - Konfigurasi linter dan formatter yang umum dipakai di ekosistem bahasa tersebut.
3. **WAJIB** - `.gitignore` sesuai bahasa, termasuk `.env`, hasil build, dan file coverage.
4. **DISARANKAN** - Pipeline CI (lint, test, build) sebagai contoh.

---

## 18. README

**WAJIB** berisi section berikut:

1. Ringkasan service dan fitur.
2. Arsitektur: penjelasan tiap layer dan diagram alurnya.
3. Struktur folder beserta fungsi tiap folder.
4. Prasyarat (versi bahasa, Docker, tools).
5. **Quick start**: langkah dari clone sampai service running dan endpoint bisa dipanggil.
6. Referensi konfigurasi: tabel semua env beserta default dan fungsinya.
7. Cara mengaktifkan gRPC, consumer, dan OpenTelemetry, termasuk cara membuka Grafana.
8. **Langkah membuat endpoint REST baru** (step by step, dari migration sampai router, termasuk update spec OpenAPI/Swagger).
9. **Langkah membuat endpoint gRPC baru** (step by step, dari file proto sampai registrasi).
10. **Langkah membuat consumer baru** (step by step, dari definisi event sampai registrasi handler, dan cara mengetesnya).
11. Testing: cara menjalankan unit test, coverage, dan integration test.
12. Migration dan seed.
13. Troubleshooting masalah umum.

---

## 19. Ringkasan Dependency

| Kategori | Dependency | Status |
|---|---|---|
| Default (selalu aktif) | REST, PostgreSQL, Redis, Logging, Healthcheck | WAJIB aktif |
| Bisa diaktifkan lewat env | gRPC, Consumer, Publisher, RabbitMQ, OpenTelemetry | Mati secara default |
| Infrastruktur lokal opsional | RabbitMQ, OTel Collector, Loki, Tempo, Prometheus, Grafana | Lewat compose profile |

---

## 20. Definition of Done

Codebase dianggap selesai kalau semua poin ini lolos:

- [ ] Build sukses tanpa warning dari linter.
- [ ] Semua unit test lulus, tanpa butuh dependency eksternal.
- [ ] Core compose naik, migration dan seed jalan, `GET /healthz` dan `GET /readyz` sehat.
- [ ] Endpoint contoh REST bisa dipanggil, request log muncul dengan payload sensitif ter-redact.
- [ ] Dengan `GRPC_ENABLED=true`: gRPC health dan RPC contoh bisa dipanggil.
- [ ] Dengan `MQ_ENABLED=true` dan `CONSUMER_ENABLED=true`: `POST` user menghasilkan event yang diproses consumer.
- [ ] Dengan `OTEL_ENABLED=true`: trace, metric, dan log muncul di Grafana dan saling terhubung.
- [ ] Graceful shutdown berjalan tanpa error.
- [ ] README lengkap sesuai Bagian 18.

---

## Lampiran A - Tabel Keputusan Stack

Isi tabel ini di awal setiap codebase baru, lalu salin hasilnya ke README project tersebut.

| Kebutuhan | Pilihan | Kriteria |
|---|---|---|
| Versi bahasa | | Stabil terbaru |
| Web framework / router | | Punya route group, middleware, binding |
| Validator | | Bawaan atau populer di ekosistem framework |
| gRPC library + middleware | | Mendukung health protocol standar dan interceptor chain |
| ORM / query builder | | Sesuai standar tim |
| Migration tool | | Migration berversi, ada up dan down |
| Redis client | | Mendukung instrumentasi OTel |
| Broker client | | Mendukung manual ack, prefetch, reconnect |
| Config loader | | Bertipe, ada default dan validasi |
| Logger | | Structured, mendukung level dan format JSON |
| OTel SDK + instrumentasi | | Resmi atau contrib resmi |
| Test framework + mocking | | Sesuai standar tim |
| Task runner | | Standar di ekosistem |
| Linter + formatter | | Standar di ekosistem |
