# Integrasi Zitadel: codebase-go (backend) + codebase-fe (frontend)

Panduan memasang Zitadel sebagai identity provider (layanan login) untuk backend Go (`codebase-go`, memakai [`zitadel-go`](https://github.com/zitadel/zitadel-go)) dan frontend Next.js (`codebase-fe`), lengkap dengan role per tenant (multi-tenant).

> **Sudah diuji end-to-end** pada 8 Oktober 2026 dengan: Zitadel v4.19.4 (self-host), zitadel-go v3.30.0, Go 1.27.1, Next.js 16.4.0, @zitadel/next-auth 1.1.6, @auth/core 0.41.3, openid-client 6.8.8. Detail hasil uji ada di [Bagian 10](#10-hasil-uji).

## Daftar Isi

1. [Gambaran Arsitektur](#1-gambaran-arsitektur)
2. [Konsep Zitadel yang Dipakai](#2-konsep-zitadel-yang-dipakai)
3. [Menjalankan Zitadel di Lokal](#3-menjalankan-zitadel-di-lokal)
4. [Bootstrap: Project, Role, App, Tenant](#4-bootstrap-project-role-app-tenant)
5. [Backend: codebase-go + zitadel-go](#5-backend-codebase-go--zitadel-go)
6. [Frontend: codebase-fe (Next.js)](#6-frontend-codebase-fe-nextjs)
7. [Operasional Multi-Tenant](#7-operasional-multi-tenant)
8. [Checklist Production](#8-checklist-production)
9. [Troubleshooting](#9-troubleshooting)
10. [Hasil Uji](#10-hasil-uji)
11. [Lampiran: Script](#11-lampiran-script)

---

## 1. Gambaran Arsitektur

```
Browser
  |  (1) klik Login
  v
codebase-fe (Next.js, port 3000)
  |  (2) redirect OIDC Authorization Code + PKCE
  v
Zitadel (port 8081) -- Login UI v2 --> user login
  |  (3) callback ke codebase-fe, tukar code jadi token (di server)
  v
codebase-fe menyimpan token di cookie session TERENKRIPSI (hanya bisa dibaca server)
  |  (4) browser memanggil /api/backend/* (BFF), Server Component memanggil backend langsung
  |      header: Authorization: Bearer <access token>, X-Tenant-ID: <org id tenant>
  v
codebase-go (port 8080 REST / 9090 gRPC)
  |  (5) zitadel-go: verifikasi JWT secara lokal pakai JWKS (tanpa panggil Zitadel per request)
  |  (6) cek role user di tenant aktif -> permission -> 403 kalau tidak boleh
  v
Service / database
```

Pembagian tanggung jawab:

| Komponen | Tanggung jawab |
|---|---|
| Zitadel | Login, MFA, user, organisasi (tenant), role yang dimiliki user di tiap tenant |
| codebase-fe | Alur login OIDC, menyimpan token di server, BFF ke backend, memilih tenant aktif |
| codebase-go | Verifikasi token, menentukan izin (role ke permission), menjalankan logic bisnis |

---

## 2. Konsep Zitadel yang Dipakai

| Konsep Zitadel | Arti di aplikasi kita |
|---|---|
| **Instance** | Satu instalasi Zitadel |
| **Organization** | **Tenant**. Setiap pelanggan atau perusahaan adalah satu organization. ID-nya dipakai sebagai `X-Tenant-ID`. |
| **Project** `codebase-go` | Aplikasi kita. Memiliki daftar role. Dibuat di organization milik kita (platform). |
| **Role** | `owner`, `admin`, `member`, `viewer`. Nama role saja, tanpa arti. Artinya (permission) ditentukan di backend. |
| **Project Grant** | Memberikan project (dan sebagian role-nya) ke organization tenant, supaya admin tenant bisa membagikan role ke user-nya sendiri. |
| **User Grant (authorization)** | User X punya role Y di tenant Z. |
| **Application (OIDC, tipe Web)** | `codebase-fe`. Client ID + client secret untuk login. |

Isi access token (JWT) setelah login, yang dibaca backend:

```json
{
  "iss": "http://localhost:8081",
  "sub": "394184817735892996",
  "aud": ["<client id codebase-fe>", "<project id>"],
  "urn:zitadel:iam:user:resourceowner:id": "394184817601675268",
  "urn:zitadel:iam:org:project:roles": {
    "admin": { "394184817601675268": "tenant-a.localhost" }
  }
}
```

- `aud` berisi **project ID**. Backend memvalidasi audience dengan project ID.
- `urn:zitadel:iam:org:project:roles` berisi role beserta organization (tenant) tempat role itu berlaku.
- `urn:zitadel:iam:user:resourceowner:id` adalah organization asal user. Dipakai sebagai tenant default kalau request tidak mengirim `X-Tenant-ID`.

---

## 3. Menjalankan Zitadel di Lokal

Zitadel v4 terdiri dari 2 container (API dan Login UI) yang harus tampil di **satu origin**, jadi ada proxy kecil (Traefik) di depannya. Database memakai PostgreSQL yang sudah ada di `docker-compose.yml` (Zitadel membuat database `zitadel` dan user-nya sendiri saat pertama kali start).

Pemakaian memori terukur di lokal: API sekitar 95 MiB, Login UI 92-177 MiB, proxy sekitar 23 MiB. Totalnya sekitar 210-300 MiB, kira-kira setara PostgreSQL + Redis.

### 3.1 Tambahkan profile `auth` ke `docker-compose.optional.yml`

Tempel blok ini di bagian `services:` (sebelum `volumes:`), lalu tambahkan `zitadel-bootstrap:` di bagian `volumes:`.

```yaml
  # ---------------------------------------------------------------- profile: auth
  # Zitadel v4 = API + Login UI, served on one origin by a small Traefik proxy.
  # Uses the PostgreSQL from docker-compose.yml (database "zitadel", created on first start).
  zitadel-proxy:
    profiles: [auth]
    image: traefik:v3.7.7
    command:
      - --providers.docker=true
      - --providers.docker.exposedbydefault=false
      - --entrypoints.web.address=:80
    ports:
      - "${ZITADEL_PORT:-8081}:80"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    depends_on:
      zitadel-api:
        condition: service_healthy
      zitadel-login:
        condition: service_healthy
    restart: unless-stopped

  zitadel-api:
    profiles: [auth]
    image: ghcr.io/zitadel/zitadel:v4.19.4
    user: "0"
    command: start-from-init --masterkey "${ZITADEL_MASTERKEY:-MasterkeyNeedsToHave32Characters}"
    environment:
      ZITADEL_PORT: 8080
      ZITADEL_EXTERNALDOMAIN: localhost
      ZITADEL_EXTERNALPORT: ${ZITADEL_PORT:-8081}
      ZITADEL_EXTERNALSECURE: "false"
      ZITADEL_TLS_ENABLED: "false"
      # Zitadel creates its own database and user with the admin account.
      ZITADEL_DATABASE_POSTGRES_HOST: postgres
      ZITADEL_DATABASE_POSTGRES_PORT: 5432
      ZITADEL_DATABASE_POSTGRES_DATABASE: zitadel
      ZITADEL_DATABASE_POSTGRES_USER_USERNAME: zitadel
      ZITADEL_DATABASE_POSTGRES_USER_PASSWORD: ${ZITADEL_DB_PASSWORD:-zitadel}
      ZITADEL_DATABASE_POSTGRES_USER_SSL_MODE: disable
      ZITADEL_DATABASE_POSTGRES_ADMIN_USERNAME: ${DB_USER:-postgres}
      ZITADEL_DATABASE_POSTGRES_ADMIN_PASSWORD: ${DB_PASSWORD:-postgres}
      ZITADEL_DATABASE_POSTGRES_ADMIN_SSL_MODE: disable
      ZITADEL_DATABASE_POSTGRES_ADMIN_EXISTINGDATABASE: postgres
      # Console admin: zitadel-admin@zitadel.localhost / Password1!
      ZITADEL_FIRSTINSTANCE_ORG_HUMAN_PASSWORDCHANGEREQUIRED: "false"
      # Machine user for the Login UI (token written to the shared volume).
      ZITADEL_FIRSTINSTANCE_LOGINCLIENTPATPATH: /zitadel/bootstrap/login-client.pat
      ZITADEL_FIRSTINSTANCE_ORG_LOGINCLIENT_MACHINE_USERNAME: login-client
      ZITADEL_FIRSTINSTANCE_ORG_LOGINCLIENT_MACHINE_NAME: Automatically Initialized IAM_LOGIN_CLIENT
      ZITADEL_FIRSTINSTANCE_ORG_LOGINCLIENT_PAT_EXPIRATIONDATE: "2099-01-01T00:00:00Z"
      # Admin machine user used by the bootstrap script (IAM owner).
      ZITADEL_FIRSTINSTANCE_PATPATH: /zitadel/bootstrap/admin.pat
      ZITADEL_FIRSTINSTANCE_ORG_MACHINE_MACHINE_USERNAME: admin-sa
      ZITADEL_FIRSTINSTANCE_ORG_MACHINE_MACHINE_NAME: Admin Service Account
      ZITADEL_FIRSTINSTANCE_ORG_MACHINE_PAT_EXPIRATIONDATE: "2099-01-01T00:00:00Z"
      ZITADEL_DEFAULTINSTANCE_FEATURES_LOGINV2_REQUIRED: "true"
      ZITADEL_DEFAULTINSTANCE_FEATURES_LOGINV2_BASEURI: http://localhost:${ZITADEL_PORT:-8081}/ui/v2/login/
      ZITADEL_OIDC_DEFAULTLOGINURLV2: http://localhost:${ZITADEL_PORT:-8081}/ui/v2/login/login?authRequest=
      ZITADEL_OIDC_DEFAULTLOGOUTURLV2: http://localhost:${ZITADEL_PORT:-8081}/ui/v2/login/logout?post_logout_redirect=
      ZITADEL_SAML_DEFAULTLOGINURLV2: http://localhost:${ZITADEL_PORT:-8081}/ui/v2/login/login?samlRequest=
    healthcheck:
      test: ["CMD", "/app/zitadel", "ready"]
      interval: 10s
      timeout: 30s
      retries: 12
      start_period: 20s
    volumes:
      - zitadel-bootstrap:/zitadel/bootstrap:rw
    labels:
      - traefik.enable=true
      - traefik.http.services.zitadel-api.loadbalancer.server.port=8080
      - traefik.http.services.zitadel-api.loadbalancer.server.scheme=h2c
      - traefik.http.routers.zitadel-api.rule=!PathPrefix(`/ui/v2/login`) && !Path(`/`)
      - traefik.http.routers.zitadel-api.entrypoints=web
      - traefik.http.routers.zitadel-api.service=zitadel-api
      - traefik.http.routers.zitadel-api.priority=100
    restart: unless-stopped

  zitadel-login:
    profiles: [auth]
    image: ghcr.io/zitadel/zitadel-login:v4.19.4
    user: "0"
    environment:
      ZITADEL_API_URL: http://zitadel-api:8080
      NEXT_PUBLIC_BASE_PATH: /ui/v2/login
      ZITADEL_SERVICE_USER_TOKEN_FILE: /zitadel/bootstrap/login-client.pat
      ZITADEL_SESSION_COOKIE_SECRET: ${ZITADEL_LOGIN_COOKIE_SECRET:-SessionCookieSecretNeedsAtLeast32Chars}
      CUSTOM_REQUEST_HEADERS: Host:localhost,X-Forwarded-Proto:http
    healthcheck:
      test: ["CMD", "/bin/sh", "-c", "node /app/healthcheck.mjs http://localhost:3000/ui/v2/login/healthy"]
      interval: 10s
      timeout: 30s
      retries: 12
      start_period: 20s
    volumes:
      - zitadel-bootstrap:/zitadel/bootstrap:ro
    depends_on:
      zitadel-api:
        condition: service_healthy
    labels:
      - traefik.enable=true
      - traefik.http.services.zitadel-login.loadbalancer.server.port=3000
      - traefik.http.middlewares.zitadel-root-rewrite.replacepath.path=/ui/v2/login/
      - traefik.http.routers.zitadel-root.rule=Path(`/`)
      - traefik.http.routers.zitadel-root.entrypoints=web
      - traefik.http.routers.zitadel-root.middlewares=zitadel-root-rewrite
      - traefik.http.routers.zitadel-root.service=zitadel-login
      - traefik.http.routers.zitadel-root.priority=400
      - traefik.http.routers.zitadel-login.rule=PathPrefix(`/ui/v2/login`)
      - traefik.http.routers.zitadel-login.entrypoints=web
      - traefik.http.routers.zitadel-login.service=zitadel-login
      - traefik.http.routers.zitadel-login.priority=250
    restart: unless-stopped
```

```yaml
volumes:
  # ...volume yang sudah ada...
  zitadel-bootstrap:
```

Catatan:

- Port 8081 dipakai karena 8080 sudah dipakai `codebase-go`. Bisa diganti lewat `ZITADEL_PORT` di `.env`.
- `ZITADEL_MASTERKEY` harus **tepat 32 karakter** dan tidak boleh berubah setelah instance dibuat (dipakai untuk mengenkripsi data di database).
- Nilai default di atas hanya untuk lokal. Lihat [Bagian 8](#8-checklist-production).

### 3.2 Target Makefile

```makefile
COMPOSE_AUTH := docker compose -f docker-compose.optional.yml --profile auth

.PHONY: infra-auth-up
infra-auth-up: ## Start Zitadel (needs `make infra-up` first for PostgreSQL)
	$(COMPOSE_AUTH) up -d --wait

.PHONY: zitadel-bootstrap
zitadel-bootstrap: ## Create project, roles, FE app and a demo tenant in Zitadel (run once)
	@mkdir -p .zitadel
	@$(COMPOSE_AUTH) exec -T zitadel-login cat /zitadel/bootstrap/admin.pat > .zitadel/admin.pat
	PAT_FILE=.zitadel/admin.pat ./scripts/zitadel-bootstrap.sh
```

Tambahkan `.zitadel/` ke `.gitignore` (berisi token admin).

### 3.3 Jalankan

```bash
make infra-up          # PostgreSQL + Redis
make infra-auth-up     # Zitadel, sekitar 1 menit pada start pertama
```

Cek:

- Discovery OIDC: http://localhost:8081/.well-known/openid-configuration
- Console admin: http://localhost:8081/ui/console (login `zitadel-admin@zitadel.localhost` / `Password1!`)

---

## 4. Bootstrap: Project, Role, App, Tenant

Simpan script di [Lampiran 11.1](#111-scriptszitadel-bootstrapsh) sebagai `scripts/zitadel-bootstrap.sh` (`chmod +x`), lalu:

```bash
make zitadel-bootstrap
```

Yang dibuat, beserta alasannya:

| Langkah | Objek | Setting penting | Alasan |
|---|---|---|---|
| 0 | Setting OIDC instance | Access token 15 menit, refresh token idle 30 hari | Default Zitadel 12 jam terlalu lama: role yang dicabut baru berlaku setelah token kedaluwarsa |
| 1 | Project `codebase-go` | `projectRoleAssertion: true` | Role dimasukkan ke dalam token |
| 1 | | `projectRoleCheck: true` | User tanpa role di project ini **tidak bisa login** (`GrantRequired`) |
| 2 | Role | `owner`, `admin`, `member`, `viewer` | |
| 3 | App OIDC `codebase-fe` | Tipe Web, auth method `BASIC` (client secret) | Next.js menyimpan secret di server, jadi termasuk confidential client. PKCE tetap dipakai oleh Auth.js. |
| 3 | | `accessTokenType: JWT`, `accessTokenRoleAssertion: true` | Backend bisa verifikasi token secara lokal dan membaca role dari token |
| 3 | | `devMode: true` | Mengizinkan redirect URI `http://`. **Matikan di production.** |
| 4 | Organization `Tenant A` | | Contoh tenant |
| 5 | Project Grant ke `Tenant A` | role `admin`, `member`, `viewer` | Admin tenant bisa membagikan role ini ke user-nya. `owner` tidak dibagikan. |
| 6 | User `alice@tenant-a.test` | username = email | Username unik **di seluruh instance**, bukan per organization, jadi email paling aman untuk multi-tenant |
| 7 | User Grant | `admin` di `Tenant A` | |

Output script berisi nilai env untuk kedua repo:

```
# codebase-go (.env)
AUTH_ENABLED=true
ZITADEL_DOMAIN=http://localhost:8081
ZITADEL_PROJECT_ID=394184817333239812

# codebase-fe (.env.local)
ZITADEL_DOMAIN=http://localhost:8081
ZITADEL_CLIENT_ID=394184817484300292
ZITADEL_CLIENT_SECRET=...
ZITADEL_PROJECT_ID=394184817333239812
```

Script dijalankan **sekali** pada Zitadel yang masih baru. Untuk mengulang dari awal, hapus database Zitadel (lihat [Bagian 9](#9-troubleshooting)).

**Lewat Console (alternatif manual).** Langkah yang sama bisa dilakukan di http://localhost:8081/ui/console: buat Project (centang *Assert Roles on Authentication* dan *Check authorization on Authentication*), tambah Roles, buat Application tipe Web (method *Code*, token type *JWT*, centang *User roles inside ID Token* dan *User Info inside ID Token*, aktifkan *Development Mode*), buat Organization, lalu buat Project Grant dan Authorization untuk user.

---

## 5. Backend: codebase-go + zitadel-go

Prinsip:

- Token diverifikasi **secara lokal** memakai JWKS (`oauth.DefaultJWTAuthorization`). Tidak ada panggilan ke Zitadel per request. Key di-cache, dan diambil ulang otomatis kalau ada key baru (rotasi).
- Role ada di token. **Permission** (contoh `users:create`) ditentukan di kode backend lewat map role ke permission.
- `AUTH_ENABLED=false` (default) tetap memakai placeholder lama, supaya dev lokal tanpa Zitadel tetap jalan.

Kenapa tidak memakai middleware bawaan `zitadel-go` secara langsung:

| Bawaan zitadel-go | Masalah untuk codebase kita | Solusi di panduan ini |
|---|---|---|
| `pkg/http/middleware` | Menulis error 401/403 sebagai teks biasa (`http.Error`), bukan envelope JSON kita | Pakai `Authorizer.CheckAuthorization` di middleware sendiri yang memakai `rest.WriteError` |
| `pkg/grpc/middleware` | Method yang **tidak terdaftar dianggap publik** | Pasang verifikasi di auth interceptor yang sudah ada (semua method wajib login), plus tabel permission yang menolak method tak terdaftar |
| `authorization.WithRole(role)` | Mengecek role di organization **mana saja**, tidak per tenant | Cek `IsGrantedRoleInOrganization(role, tenant)` |

### 5.1 Dependency

```bash
go get github.com/zitadel/zitadel-go/v3@v3.30.0
go mod tidy
```

### 5.2 Config (`internal/config/config.go`)

```go
type Config struct {
	// ...field yang sudah ada...
	Auth Auth
}

// Auth configures access token validation against Zitadel.
type Auth struct {
	Enabled   bool   `env:"AUTH_ENABLED" envDefault:"false"`
	Domain    string `env:"ZITADEL_DOMAIN"`     // e.g. http://localhost:8081
	ProjectID string `env:"ZITADEL_PROJECT_ID"` // must be in the token audience
}
```

Di `Validate()`:

```go
if c.Auth.Enabled && (c.Auth.Domain == "" || c.Auth.ProjectID == "") {
	errs = append(errs, errors.New("AUTH_ENABLED=true requires ZITADEL_DOMAIN and ZITADEL_PROJECT_ID"))
}
```

Tambahkan ke `.env.example`:

```bash
# ---------------------------------------------------------------- Auth (Zitadel)
# See docs/ZITADEL_INTEGRATION.md. When false, every request is allowed (placeholder).
AUTH_ENABLED=false
ZITADEL_DOMAIN=http://localhost:8081
ZITADEL_PROJECT_ID=                 # from `make zitadel-bootstrap`; checked as token audience
```

### 5.3 Package `internal/platform/auth`

`internal/platform/auth/zitadel.go`:

```go
// Package auth verifies Zitadel access tokens (via zitadel-go) and carries
// the caller (Principal) and the active tenant through the request context.
package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"

	"github.com/zitadel/zitadel-go/v3/pkg/authorization"
	"github.com/zitadel/zitadel-go/v3/pkg/authorization/oauth"
	"github.com/zitadel/zitadel-go/v3/pkg/zitadel"

	"github.com/z-alamsyah/codebase-go/internal/config"
	"github.com/z-alamsyah/codebase-go/internal/model"
)

// Verifier validates "Bearer <JWT>" access tokens locally with Zitadel's
// public keys (JWKS). Keys are fetched once and cached; there is no network
// call to Zitadel per request.
type Verifier struct {
	authz *authorization.Authorizer[*oauth.IntrospectionContext]
}

// NewVerifier runs OIDC discovery against Zitadel, so Zitadel must be
// reachable at startup. The token audience must contain the project ID.
func NewVerifier(ctx context.Context, cfg config.Auth, log *slog.Logger) (*Verifier, error) {
	z, err := newZitadel(cfg.Domain)
	if err != nil {
		return nil, err
	}
	authz, err := authorization.New(ctx, z,
		oauth.DefaultJWTAuthorization(cfg.ProjectID),
		authorization.WithLogger[*oauth.IntrospectionContext](log),
	)
	if err != nil {
		return nil, fmt.Errorf("init zitadel authorizer: %w", err)
	}
	return &Verifier{authz: authz}, nil
}

// Verify checks the Authorization header value and returns the caller.
// The cause is logged by zitadel-go; clients only get a generic message.
func (v *Verifier) Verify(ctx context.Context, authorizationHeader string) (*Principal, error) {
	c, err := v.authz.CheckAuthorization(ctx, authorizationHeader)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid or missing access token", model.ErrUnauthorized)
	}
	return NewPrincipal(c), nil
}

// newZitadel turns ZITADEL_DOMAIN (e.g. http://localhost:8081 or
// https://auth.example.com) into zitadel-go settings.
func newZitadel(raw string) (*zitadel.Zitadel, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid ZITADEL_DOMAIN %q", raw)
	}
	switch u.Scheme {
	case "http":
		port := u.Port()
		if port == "" {
			port = "80"
		}
		return zitadel.New(u.Hostname(), zitadel.WithInsecure(port)), nil
	case "https":
		if u.Port() == "" {
			return zitadel.New(u.Hostname()), nil
		}
		port, err := strconv.ParseUint(u.Port(), 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid port in ZITADEL_DOMAIN %q", raw)
		}
		return zitadel.New(u.Hostname(), zitadel.WithPort(uint16(port))), nil
	default:
		return nil, fmt.Errorf("ZITADEL_DOMAIN must start with http:// or https://, got %q", raw)
	}
}
```

`internal/platform/auth/principal.go`:

```go
package auth

import (
	"context"
	"slices"

	"github.com/zitadel/zitadel-go/v3/pkg/authorization/oauth"
)

// Principal is the authenticated caller.
type Principal struct {
	UserID string // Zitadel user ID (token "sub")
	OrgID  string // organization the user belongs to
	claims *oauth.IntrospectionContext
}

func NewPrincipal(c *oauth.IntrospectionContext) *Principal {
	return &Principal{UserID: c.UserID(), OrgID: c.OrganizationID(), claims: c}
}

// HasRole reports whether the caller holds role inside the tenant (Zitadel organization).
func (p *Principal) HasRole(role, tenantID string) bool {
	return p != nil && tenantID != "" && p.claims.IsGrantedRoleInOrganization(role, tenantID)
}

// Can reports whether one of the caller's roles in the tenant grants perm.
func (p *Principal) Can(perm, tenantID string) bool {
	for role, perms := range rolePermissions {
		if slices.Contains(perms, perm) && p.HasRole(role, tenantID) {
			return true
		}
	}
	return false
}

type principalKey struct{}
type tenantKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns the caller, or nil when the request is not authenticated.
func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}

func WithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenantID)
}

// TenantFrom returns the active tenant (Zitadel organization ID).
func TenantFrom(ctx context.Context) string {
	t, _ := ctx.Value(tenantKey{}).(string)
	return t
}
```

> Pakai tipe konkret `*oauth.IntrospectionContext` seperti di atas. Helper generik `authorization.Context[authorization.Ctx](ctx)` mengembalikan interface `nil` kalau request belum login, dan memanggil method di interface `nil` membuat aplikasi panic.

`internal/platform/auth/permissions.go`:

```go
package auth

// Permissions follow "<resource>:<action>".
const (
	PermUsersRead   = "users:read"
	PermUsersCreate = "users:create"
)

// rolePermissions maps Zitadel project roles to permissions. Roles are
// assigned in Zitadel (per tenant); what each role may do is decided here,
// in code that is reviewed and tested with the backend.
var rolePermissions = map[string][]string{
	"owner":  {PermUsersRead, PermUsersCreate},
	"admin":  {PermUsersRead, PermUsersCreate},
	"member": {PermUsersRead},
	"viewer": {PermUsersRead},
}
```

`internal/platform/auth/principal_test.go`:

```go
package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/zitadel-go/v3/pkg/authorization/oauth"
)

// principalWithRoles builds a caller whose token grants roles per tenant,
// shaped like Zitadel's "urn:zitadel:iam:org:project:roles" claim.
func principalWithRoles(roles map[string][]string) *Principal {
	claim := map[string]any{}
	for tenant, rs := range roles {
		for _, r := range rs {
			orgs, _ := claim[r].(map[string]any)
			if orgs == nil {
				orgs = map[string]any{}
				claim[r] = orgs
			}
			orgs[tenant] = tenant + ".localhost"
		}
	}
	c := &oauth.IntrospectionContext{IntrospectionResponse: oidc.IntrospectionResponse{
		Active:  true,
		Subject: "user-1",
		Claims:  map[string]any{"urn:zitadel:iam:org:project:roles": claim},
	}}
	return NewPrincipal(c)
}

func TestPrincipal_Can(t *testing.T) {
	p := principalWithRoles(map[string][]string{"tenant-a": {"admin"}, "tenant-b": {"viewer"}})

	tests := []struct {
		name   string
		perm   string
		tenant string
		want   bool
	}{
		{name: "admin can create in its tenant", perm: PermUsersCreate, tenant: "tenant-a", want: true},
		{name: "viewer cannot create", perm: PermUsersCreate, tenant: "tenant-b", want: false},
		{name: "viewer can read", perm: PermUsersRead, tenant: "tenant-b", want: true},
		{name: "no role in another tenant", perm: PermUsersRead, tenant: "tenant-c", want: false},
		{name: "empty tenant is denied", perm: PermUsersRead, tenant: "", want: false},
		{name: "unknown permission is denied", perm: "orders:delete", tenant: "tenant-a", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, p.Can(tt.perm, tt.tenant))
		})
	}

	var nobody *Principal
	assert.False(t, nobody.Can(PermUsersRead, "tenant-a"), "nil principal must be denied")
}
```

### 5.4 Middleware (`internal/middleware/auth.go`)

```go
package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/httplog/v3"
	grpcauth "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/auth"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/metadata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/z-alamsyah/codebase-go/internal/model"
	"github.com/z-alamsyah/codebase-go/internal/platform/auth"
)

// TenantHeader carries the active tenant (Zitadel organization ID). When it
// is missing, the organization the user belongs to is used.
const TenantHeader = "X-Tenant-ID"

// ErrorWriter renders an error response (rest.WriteError), so auth errors use
// the same JSON envelope as every other error.
type ErrorWriter func(w http.ResponseWriter, r *http.Request, err error)

// Authenticate verifies the bearer token and stores the caller and the
// active tenant in the request context.
func Authenticate(v *auth.Verifier, writeErr ErrorWriter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := v.Verify(r.Context(), r.Header.Get("Authorization"))
			if err != nil {
				writeErr(w, r, err)
				return
			}
			tenant := r.Header.Get(TenantHeader)
			if tenant == "" {
				tenant = p.OrgID
			}
			httplog.SetAttrs(r.Context(), slog.String("user_id", p.UserID), slog.String("tenant_id", tenant))

			ctx := auth.WithTenant(auth.WithPrincipal(r.Context(), p), tenant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequirePermission lets the request through only if the caller has perm in
// the active tenant. Mount it after Authenticate.
func RequirePermission(perm string, writeErr ErrorWriter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if !auth.FromContext(ctx).Can(perm, auth.TenantFrom(ctx)) {
				writeErr(w, r, fmt.Errorf("%w: missing permission %s", model.ErrForbidden, perm))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// GRPCAuthenticate is the go-grpc-middleware AuthFunc used when AUTH_ENABLED=true.
func GRPCAuthenticate(v *auth.Verifier) grpcauth.AuthFunc {
	return func(ctx context.Context) (context.Context, error) {
		md := metadata.ExtractIncoming(ctx)
		p, err := v.Verify(ctx, md.Get("authorization"))
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid or missing access token")
		}
		tenant := md.Get("x-tenant-id")
		if tenant == "" {
			tenant = p.OrgID
		}
		return auth.WithTenant(auth.WithPrincipal(ctx, p), tenant), nil
	}
}

// GRPCPermissions checks one permission per RPC. Every method must be listed:
// a method without an entry is denied (fail closed), so a new RPC cannot be
// exposed by accident.
func GRPCPermissions(perms map[string]string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		perm, ok := perms[info.FullMethod]
		if !ok {
			return nil, status.Errorf(codes.PermissionDenied, "no permission configured for %s", info.FullMethod)
		}
		if !auth.FromContext(ctx).Can(perm, auth.TenantFrom(ctx)) {
			return nil, status.Errorf(codes.PermissionDenied, "missing permission %s", perm)
		}
		return handler(ctx, req)
	}
}
```

Placeholder `Auth` (di `middleware/http.go`) dan `GRPCAuth` (di `middleware/grpc.go`) **tetap dipertahankan** untuk `AUTH_ENABLED=false`.

### 5.5 Error writer (`internal/controller/rest/response.go`)

```go
// WriteError renders err as the standard JSON error envelope. Middlewares
// use it so 401/403 responses look like every other error.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	respondError(w, r, err)
}
```

### 5.6 Router REST (`internal/router/http.go`)

```go
type HTTPDeps struct {
	Config   config.Config
	Logger   *slog.Logger
	Health   *rest.HealthHandler
	User     *rest.UserHandler
	Verifier *auth.Verifier // nil when AUTH_ENABLED=false
}
```

Ganti blok `if d.Config.HTTP.RESTEnabled { ... }` menjadi:

```go
	// AUTH_ENABLED=false keeps the no-op placeholder and skips permission checks.
	authenticate := middleware.Auth
	can := func(string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler { return next }
	}
	if d.Verifier != nil {
		authenticate = middleware.Authenticate(d.Verifier, rest.WriteError)
		can = func(perm string) func(http.Handler) http.Handler {
			return middleware.RequirePermission(perm, rest.WriteError)
		}
	}

	if d.Config.HTTP.RESTEnabled {
		r.Route("/api/v1", func(r chi.Router) {
			// Business endpoints: request log -> auth -> timeout.
			r.Use(
				middleware.RequestLogger(d.Logger, d.Config.Log),
				authenticate,
				chimw.Timeout(d.Config.HTTP.RequestTimeout),
			)

			r.Route("/users", func(r chi.Router) {
				r.With(can(auth.PermUsersCreate)).Post("/", d.User.Create)
				r.With(can(auth.PermUsersRead)).Get("/{id}", d.User.GetByID)
			})
		})
	}
```

Import `github.com/z-alamsyah/codebase-go/internal/platform/auth`. Setiap endpoint baru **wajib** diberi `r.With(can(<permission>))`.

### 5.7 Router gRPC (`internal/router/grpc.go`)

```go
	grpcauth "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/auth" // ganti alias import "auth" lama
	"github.com/z-alamsyah/codebase-go/internal/platform/auth"
```

```go
type GRPCDeps struct {
	Config   config.Config
	Logger   *slog.Logger
	User     *rpc.UserServer
	Verifier *auth.Verifier // nil when AUTH_ENABLED=false
}

// rpcPermissions lists the permission each RPC needs. With AUTH_ENABLED=true
// an RPC that is missing here is denied.
var rpcPermissions = map[string]string{
	userv1.UserService_GetUser_FullMethodName:    auth.PermUsersRead,
	userv1.UserService_CreateUser_FullMethodName: auth.PermUsersCreate,
}
```

Ganti penyusunan interceptor di `NewGRPC`:

```go
	authFn := middleware.GRPCAuth // no-op placeholder when AUTH_ENABLED=false
	if d.Verifier != nil {
		authFn = middleware.GRPCAuthenticate(d.Verifier)
	}
	unary := []grpc.UnaryServerInterceptor{
		middleware.UnaryRequestID,
		selector.UnaryServerInterceptor(logging.UnaryServerInterceptor(logger, logOpts...), middleware.SkipBuiltinServices),
		selector.UnaryServerInterceptor(grpcauth.UnaryServerInterceptor(authFn), middleware.SkipBuiltinServices),
	}
	if d.Verifier != nil {
		unary = append(unary, selector.UnaryServerInterceptor(middleware.GRPCPermissions(rpcPermissions), middleware.SkipBuiltinServices))
	}
	unary = append(unary, recovery.UnaryServerInterceptor(recoveryOpt))

	opts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(unary...),
		grpc.ChainStreamInterceptor(
			middleware.StreamRequestID,
			selector.StreamServerInterceptor(logging.StreamServerInterceptor(logger, logOpts...), middleware.SkipBuiltinServices),
			selector.StreamServerInterceptor(grpcauth.StreamServerInterceptor(authFn), middleware.SkipBuiltinServices),
			recovery.StreamServerInterceptor(recoveryOpt),
		),
	}
```

Health check dan reflection (`grpc.*`) tetap bisa diakses tanpa token. Kalau nanti ada RPC streaming, buat versi stream dari `GRPCPermissions`.

### 5.8 Wiring (`internal/app/app.go`)

Setelah blok RabbitMQ, sebelum `// Data layer.`:

```go
	var verifier *auth.Verifier
	if cfg.Auth.Enabled {
		if verifier, err = auth.NewVerifier(ctx, cfg.Auth, log); err != nil {
			return a, err
		}
	}
```

Lalu isi `Verifier: verifier` di `router.HTTPDeps{...}` dan `router.GRPCDeps{...}`.

### 5.9 Swagger

Tambahkan parameter header di anotasi setiap endpoint bisnis, lalu `make swagger`:

```go
//	@Param	X-Tenant-ID	header	string	false	"Active tenant (Zitadel organization ID). Default: the user's own organization."
```

`@Security BearerAuth` sudah ada. Di Swagger UI, klik **Authorize** dan isi `Bearer <access token>`.

### 5.10 Memakai identitas user di service

Service boleh membaca caller untuk audit (contoh `created_by`) atau untuk memfilter data per tenant:

```go
p := auth.FromContext(ctx)      // nil kalau AUTH_ENABLED=false
tenantID := auth.TenantFrom(ctx)
```

Untuk data milik tenant, simpan `tenant_id` di tabel dan **selalu** filter query dengan tenant aktif. Permission saja tidak cukup untuk mencegah tenant A membaca data tenant B.

### 5.11 Coba

```bash
# .env: AUTH_ENABLED=true, ZITADEL_DOMAIN, ZITADEL_PROJECT_ID (dari bootstrap)
make run
```

Ambil access token tanpa browser memakai script dev di [Lampiran 11.2](#112-scriptszitadel-tokensh) (hanya untuk lokal):

```bash
TOKEN=$(./scripts/zitadel-token.sh alice@tenant-a.test 'Password1!' | jq -r .access_token)

curl -H "Authorization: Bearer $TOKEN" localhost:8080/api/v1/users/01928f6a-0000-7000-8000-000000000001   # 200
curl -H "Authorization: Bearer $TOKEN" -H "X-Tenant-ID: 999" localhost:8080/api/v1/users/01928f6a-0000-7000-8000-000000000001   # 403
curl localhost:8080/api/v1/users/01928f6a-0000-7000-8000-000000000001   # 401

grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{"id":"01928f6a-0000-7000-8000-000000000001"}' localhost:9090 user.v1.UserService/GetUser
```

---

## 6. Frontend: codebase-fe (Next.js)

Prinsip (sesuai `codebase-fe/docs/CODEBASE_RULES.md`):

- Login memakai library yang dipakai **contoh resmi Zitadel untuk Next.js** ([zitadel/example-auth-nextjs](https://github.com/zitadel/example-auth-nextjs)): `@zitadel/next-auth` (pembungkus Auth.js) + `openid-client`.
- Token disimpan di cookie session yang terenkripsi dan **tidak pernah dikirim ke browser**.
- Browser memanggil backend lewat BFF `/api/backend/*`. Server Component memanggil backend langsung dari server.

Implementasi lengkap yang dipakai sebagai acuan ada di repo [`codebase-fe`](https://github.com/z-alamsyah/codebase-fe) (`src/lib/auth`, `src/proxy.ts`, `src/app/api/backend`, `src/app/api/auth/logout`). Potongan kode di bagian ini sama dengan repo tersebut, hanya membaca `process.env` langsung supaya mudah dibaca.

Ada beberapa hal di panduan ini yang **berbeda dari contoh resmi**, karena terbukti bermasalah saat diuji:

| Contoh resmi | Masalah | Di panduan ini |
|---|---|---|
| `accessToken` dan `idToken` dimasukkan ke object session | Session bisa dibaca browser lewat `GET /api/auth/session`, jadi token ikut terbaca JavaScript | Session hanya berisi data aman (nama, email, tenant + role). Token dibaca di server dengan `getToken()`. |
| Refresh token di callback `jwt`, dipanggil dari Server Component | Zitadel **langsung mematikan refresh token lama** begitu dipakai (rotasi). Server Component tidak bisa menyimpan cookie baru, jadi refresh berikutnya memakai token yang sudah mati, dan user ter-logout. | Refresh hanya terjadi di `proxy.ts` (bisa menulis cookie), dan hasil refresh dibagi ke request yang datang bersamaan |
| Auth.js menyusun `redirect_uri` dari URL request | Di server standalone (Docker) atau di belakang reverse proxy, URL request adalah URL internal, contoh `http://0.0.0.0:3000`. Zitadel menolak redirect URI tersebut. | Setiap request ke Auth.js dipindah ke origin `AUTH_URL` dulu (`onAppOrigin`, Bagian 6.5) |
| Logout lewat form POST yang redirect ke Zitadel lalu kembali | Chrome memblok langkah terakhir (kembali ke aplikasi) karena aturan CSP `form-action` | Logout memakai `fetch` lalu `window.location` (Bagian 6.10) |

### 6.1 Dependency

```bash
pnpm add @zitadel/next-auth@1.1.6 @auth/core@0.41.3 openid-client@6.8.8 server-only
```

### 6.2 Env (`.env.local`)

```bash
AUTH_URL=http://localhost:3000
AUTH_SECRET=                         # openssl rand -base64 32
ZITADEL_DOMAIN=http://localhost:8081
ZITADEL_CLIENT_ID=                   # dari make zitadel-bootstrap
ZITADEL_CLIENT_SECRET=               # dari make zitadel-bootstrap
ZITADEL_PROJECT_ID=                  # dari make zitadel-bootstrap
BACKEND_URL=http://localhost:8080
```

Auth.js menulis peringatan `env-url-basepath-redundant` di setiap request, karena `@zitadel/next-auth` menyetel `basePath` sendiri. Peringatan ini tidak berbahaya dan disaring lewat opsi `logger` di Bagian 6.5. `AUTH_URL` tetap wajib diisi.

### 6.3 Tipe session (`src/types/auth.d.ts`)

```ts
import '@auth/core/types';
import '@auth/core/jwt';

declare module '@auth/core/types' {
  // Sent to the browser (GET /api/auth/session): never put tokens here.
  interface Session {
    tenants?: Record<string, string[]>; // Zitadel org ID -> roles
    error?: string;
  }
}

declare module '@auth/core/jwt' {
  // Stored encrypted in the session cookie, readable only on the server.
  interface JWT {
    idToken?: string;
    accessToken?: string;
    refreshToken?: string;
    expiresAt?: number; // ms since epoch
    tenants?: Record<string, string[]>;
    error?: string;
  }
}
```

### 6.4 Scope (`src/lib/auth/scopes.ts`)

```ts
// https://zitadel.com/docs/apis/openidoauth/scopes
export function zitadelScopes(projectId: string): string {
  return [
    'openid',
    'profile',
    'email',
    'offline_access', // refresh token
    'urn:zitadel:iam:org:projects:roles', // roles per organization (tenant)
    'urn:zitadel:iam:user:resourceowner', // the organization the user belongs to
    `urn:zitadel:iam:org:project:id:${projectId}:aud`, // backend checks this audience
  ].join(' ');
}
```

### 6.5 Modul auth (`src/lib/auth/index.ts`)

```ts
import 'server-only';
import { NextAuth, type NextAuthConfig } from '@zitadel/next-auth';
import Zitadel from '@auth/core/providers/zitadel';
import { getToken, type JWT } from '@auth/core/jwt';
import { NextRequest } from 'next/server';
import * as oidc from 'openid-client';
import { zitadelScopes } from './scopes';

const env = {
  domain: process.env.ZITADEL_DOMAIN!, // e.g. http://localhost:8081
  clientId: process.env.ZITADEL_CLIENT_ID!,
  clientSecret: process.env.ZITADEL_CLIENT_SECRET!,
  projectId: process.env.ZITADEL_PROJECT_ID!,
  secret: process.env.AUTH_SECRET!,
  secureCookie: (process.env.AUTH_URL ?? '').startsWith('https://'),
};

const ROLES_CLAIM = 'urn:zitadel:iam:org:project:roles';
const REFRESH_BEFORE_EXPIRY_MS = 60_000;

/** Tenant (Zitadel org ID) -> role keys, from the roles claim of a token. */
function tenantsFrom(claims: Record<string, unknown> | undefined): Record<string, string[]> {
  const roles = (claims?.[ROLES_CLAIM] ?? {}) as Record<string, Record<string, string>>;
  const tenants: Record<string, string[]> = {};
  for (const [role, orgs] of Object.entries(roles)) {
    for (const orgId of Object.keys(orgs)) (tenants[orgId] ??= []).push(role);
  }
  return tenants;
}

let oidcConfig: Promise<oidc.Configuration> | undefined;
function getOidcConfig(): Promise<oidc.Configuration> {
  oidcConfig ??= oidc.discovery(
    new URL(env.domain),
    env.clientId,
    env.clientSecret,
    undefined,
    // Local Zitadel runs on plain http; never needed with https.
    env.domain.startsWith('http://') ? { execute: [oidc.allowInsecureRequests] } : undefined,
  );
  return oidcConfig;
}

/**
 * Zitadel rotates refresh tokens: a used refresh token stops working at once.
 * Requests that arrive together (or shortly after each other) with the same old
 * refresh token must therefore share ONE refresh result, otherwise all but the
 * first fail and the user is logged out. The cache only covers one Node.js
 * process; with several instances use sticky sessions or a shared store.
 */
const refreshResults = new Map<string, { at: number; result: Promise<JWT> }>();
const SHARE_REFRESH_MS = 30_000;

function refreshAccessToken(token: JWT): Promise<JWT> {
  const key = token.refreshToken;
  if (!key) return Promise.resolve({ ...token, error: 'RefreshAccessTokenError' });

  const now = Date.now();
  for (const [k, v] of refreshResults) if (now - v.at > SHARE_REFRESH_MS) refreshResults.delete(k);
  const shared = refreshResults.get(key);
  if (shared) return shared.result;

  const result = (async (): Promise<JWT> => {
    try {
      const res = await oidc.refreshTokenGrant(await getOidcConfig(), key);
      return {
        ...token,
        accessToken: res.access_token,
        idToken: res.id_token ?? token.idToken,
        refreshToken: res.refresh_token ?? token.refreshToken,
        expiresAt: Date.now() + (res.expires_in ?? 900) * 1000,
        tenants: res.id_token ? tenantsFrom(res.claims()) : token.tenants,
        error: undefined,
      };
    } catch (error) {
      console.error('token refresh failed', error);
      return { ...token, error: 'RefreshAccessTokenError' };
    }
  })();
  refreshResults.set(key, { at: now, result });
  return result;
}

export const authConfig: NextAuthConfig = {
  providers: [
    Zitadel({
      issuer: env.domain,
      clientId: env.clientId,
      clientSecret: env.clientSecret,
      authorization: { params: { scope: zitadelScopes(env.projectId) } },
    }),
  ],
  session: { strategy: 'jwt', maxAge: 30 * 24 * 60 * 60 },
  secret: env.secret,
  pages: { signIn: '/login' },
  logger: {
    error: (error) => console.error('auth error', error),
    warn: (code) => {
      // @zitadel/next-auth sets basePath itself, so Auth.js reports AUTH_URL as
      // redundant on every request. AUTH_URL is still needed (see onAppOrigin).
      if (code !== 'env-url-basepath-redundant') console.warn('auth warning', code);
    },
    debug: () => {},
  },
  callbacks: {
    async jwt({ token, account, profile }) {
      if (account) {
        // First request after login: keep the tokens on the server side of the cookie.
        return {
          ...token,
          idToken: account.id_token,
          accessToken: account.access_token,
          refreshToken: account.refresh_token,
          expiresAt: account.expires_at ? account.expires_at * 1000 : Date.now() + 900_000,
          tenants: tenantsFrom(profile as Record<string, unknown>),
        };
      }
      if (Date.now() < (token.expiresAt ?? 0) - REFRESH_BEFORE_EXPIRY_MS) return token;
      return refreshAccessToken(token);
    },
    async session({ session, token }) {
      // Only non-secret data goes to the browser.
      session.tenants = token.tenants;
      session.error = token.error;
      return session;
    },
  },
};

const auth = NextAuth(authConfig);

/**
 * Auth.js builds its callback URL from the incoming request URL. In the
 * standalone server or behind a reverse proxy that URL is the internal one
 * (e.g. http://0.0.0.0:3000), which Zitadel rejects as an unknown redirect
 * URI. Every request handed to Auth.js is therefore rebased on AUTH_URL.
 */
async function onAppOrigin(req: NextRequest): Promise<NextRequest> {
  const url = new URL(req.url);
  const app = new URL(process.env.AUTH_URL!);
  url.protocol = app.protocol;
  url.host = app.host;
  const body = req.method === 'GET' || req.method === 'HEAD' ? undefined : await req.arrayBuffer();
  return new NextRequest(url, { method: req.method, headers: req.headers, body });
}

export const handlers = {
  GET: async (req: NextRequest) => auth.handlers.GET(await onAppOrigin(req)),
  POST: async (req: NextRequest) => auth.handlers.POST(await onAppOrigin(req)),
};

export const getSession = auth.getSession;

/**
 * Server-only. Returns the access token of the request's session, or null.
 * Does not refresh: proxy.ts refreshes before pages and route handlers run.
 */
export async function getAccessToken(req: Request | { headers: Headers }): Promise<string | null> {
  const token = await getToken({ req, secret: env.secret, secureCookie: env.secureCookie });
  if (!token?.accessToken || token.error) return null;
  return token.accessToken;
}

/** URL that ends the session in Zitadel too, plus a state value to verify on return. */
export async function buildLogoutUrl(req: Request): Promise<{ url: string; state: string }> {
  const token = await getToken({ req, secret: env.secret, secureCookie: env.secureCookie });
  const state = crypto.randomUUID();
  const url = oidc.buildEndSessionUrl(await getOidcConfig(), {
    ...(token?.idToken ? { id_token_hint: token.idToken } : {}),
    post_logout_redirect_uri: `${process.env.AUTH_URL}/api/auth/logout/callback`,
    state,
  });
  return { url: url.toString(), state };
}
```

### 6.6 Route auth dan halaman login

`src/app/api/auth/[...nextauth]/route.ts`:

```ts
import { handlers } from '@/lib/auth';

export const { GET, POST } = handlers;
```

`src/app/login/page.tsx` (Auth.js memulai login lewat POST dengan CSRF token):

```tsx
'use client';

import { getCsrfToken } from '@zitadel/next-auth/react';
import { useSearchParams } from 'next/navigation';
import { Suspense, useEffect, useState } from 'react';

function LoginForm() {
  const callbackUrl = useSearchParams().get('callbackUrl') ?? '/';
  const [csrfToken, setCsrfToken] = useState('');

  useEffect(() => {
    void getCsrfToken().then((t) => setCsrfToken(t ?? ''));
  }, []);

  // Auth.js starts the OIDC flow on a POST with a CSRF token.
  return (
    <form method="post" action="/api/auth/signin/zitadel">
      <input type="hidden" name="csrfToken" value={csrfToken} />
      <input type="hidden" name="callbackUrl" value={callbackUrl} />
      <button type="submit" disabled={!csrfToken}>
        Login with Zitadel
      </button>
    </form>
  );
}

export default function LoginPage() {
  return (
    <Suspense>
      <LoginForm />
    </Suspense>
  );
}
```

### 6.7 Refresh dan proteksi halaman (`src/proxy.ts`)

Next.js 16 memakai `proxy.ts` (dulu `middleware.ts`).

```ts
import { NextRequest, NextResponse } from 'next/server';
import { handlers } from '@/lib/auth';

const PUBLIC_PATHS = ['/', '/login', '/api/health'];

/**
 * Runs before pages and the BFF. It asks Auth.js for the session the same way
 * the browser would, which refreshes the access token when it is close to
 * expiry. The updated cookie is sent to the browser AND passed on to the rest
 * of this request, so pages and route handlers always read a fresh token.
 *
 * This is the only place that refreshes tokens: Server Components cannot set
 * cookies, and Zitadel invalidates a refresh token as soon as it is used.
 */
export async function proxy(req: NextRequest) {
  const sessionRes = await handlers.GET(
    new NextRequest(new URL('/api/auth/session', req.url), { headers: req.headers }),
  );
  const setCookies = sessionRes.headers.getSetCookie();
  const session = (await sessionRes.json().catch(() => null)) as { user?: unknown; error?: string } | null;
  const loggedIn = Boolean(session?.user) && !session?.error;

  const { pathname, search } = req.nextUrl;
  if (!loggedIn && !PUBLIC_PATHS.includes(pathname)) {
    if (pathname.startsWith('/api/')) {
      return NextResponse.json(
        { error: { code: 'UNAUTHORIZED', message: 'login required' }, meta: {} },
        { status: 401 },
      );
    }
    const login = new URL('/login', req.url);
    login.searchParams.set('callbackUrl', pathname + search);
    return NextResponse.redirect(login);
  }

  const headers = new Headers(req.headers);
  headers.set('cookie', mergeCookies(req.headers.get('cookie'), setCookies));
  const res = NextResponse.next({ request: { headers } });
  for (const c of setCookies) res.headers.append('set-cookie', c);
  return res;
}

export const config = {
  // Everything except Auth.js's own endpoints and static files.
  matcher: ['/((?!api/auth|_next/static|_next/image|favicon.ico).*)'],
};

/** Applies Set-Cookie values to a Cookie header (expired cookies are removed). */
function mergeCookies(cookieHeader: string | null, setCookies: string[]): string {
  const jar = new Map<string, string>();
  for (const part of (cookieHeader ?? '').split(';')) {
    const [name, ...value] = part.trim().split('=');
    if (name) jar.set(name, value.join('='));
  }
  for (const sc of setCookies) {
    const [pair, ...attrs] = sc.split(';');
    const [name, ...value] = pair.trim().split('=');
    const expired = attrs.some((a) => /^\s*max-age=0\s*$/i.test(a) || /^\s*expires=thu, 01 jan 1970/i.test(a));
    if (expired) jar.delete(name);
    else jar.set(name, value.join('='));
  }
  return [...jar].map(([k, v]) => `${k}=${v}`).join('; ');
}
```

`proxy.ts` hanya redirect awal dan refresh token. Route Handler dan halaman yang dilindungi **tetap** memeriksa session sendiri (lihat 6.8 dan 6.9).

### 6.8 Pemanggil backend dan BFF

`src/lib/backend/index.ts`:

```ts
import 'server-only';

const BACKEND_URL = process.env.BACKEND_URL!; // e.g. http://localhost:8080

/** Calls the Go backend from the server with the user's access token. */
export function backendFetch(
  path: string,
  { accessToken, tenantId, requestId, init }: {
    accessToken: string;
    tenantId?: string;
    requestId?: string;
    init?: RequestInit;
  },
): Promise<Response> {
  const headers = new Headers(init?.headers);
  headers.set('Authorization', `Bearer ${accessToken}`);
  if (tenantId) headers.set('X-Tenant-ID', tenantId);
  headers.set('X-Request-Id', requestId ?? crypto.randomUUID());
  return fetch(new URL(path, BACKEND_URL), { ...init, headers, cache: 'no-store' });
}
```

`src/app/api/backend/[...path]/route.ts`:

```ts
import { getAccessToken, getSession } from '@/lib/auth';
import { backendFetch } from '@/lib/backend';

// Only these backend paths may be reached through the BFF.
const ALLOWED_PREFIXES = ['api/v1/'];

async function forward(req: Request, ctx: { params: Promise<{ path: string[] }> }) {
  const path = (await ctx.params).path.join('/');
  if (!ALLOWED_PREFIXES.some((p) => path.startsWith(p))) {
    return Response.json({ error: { code: 'NOT_FOUND', message: 'unknown path' }, meta: {} }, { status: 404 });
  }

  // Requests that change data must come from our own pages (CSRF protection).
  // Compare with AUTH_URL: req.url is the internal URL in the standalone server.
  if (req.method !== 'GET' && req.method !== 'HEAD') {
    const origin = req.headers.get('origin');
    if (origin && origin !== new URL(process.env.AUTH_URL!).origin) {
      return Response.json({ error: { code: 'FORBIDDEN', message: 'bad origin' }, meta: {} }, { status: 403 });
    }
  }

  const accessToken = await getAccessToken(req);
  if (!accessToken) {
    return Response.json({ error: { code: 'UNAUTHORIZED', message: 'login required' }, meta: {} }, { status: 401 });
  }

  // The tenant must be one the user belongs to; the backend checks roles again.
  const tenantId = req.headers.get('x-tenant-id') ?? undefined;
  if (tenantId) {
    const session = await getSession(req);
    if (!session?.tenants?.[tenantId]) {
      return Response.json({ error: { code: 'FORBIDDEN', message: 'not a member of this tenant' }, meta: {} }, { status: 403 });
    }
  }

  const url = new URL(req.url);
  const res = await backendFetch(`/${path}${url.search}`, {
    accessToken,
    tenantId,
    requestId: req.headers.get('x-request-id') ?? undefined,
    init: {
      method: req.method,
      headers: { 'Content-Type': req.headers.get('content-type') ?? 'application/json' },
      body: req.method === 'GET' || req.method === 'HEAD' ? undefined : await req.text(),
    },
  });

  return new Response(res.body, {
    status: res.status,
    headers: {
      'Content-Type': res.headers.get('content-type') ?? 'application/json',
      'X-Request-Id': res.headers.get('x-request-id') ?? '',
    },
  });
}

export { forward as GET, forward as POST, forward as PUT, forward as PATCH, forward as DELETE };
```

Di browser, client hasil generate Hey API diarahkan ke BFF, dan tenant diambil dari URL (`/[tenant]/...`):

```ts
// contoh konfigurasi client (detail mengikuti versi @hey-api/openapi-ts yang dipakai)
client.setConfig({ baseUrl: '/api/backend' });
// setiap request: header 'X-Tenant-ID' = params.tenant
```

### 6.9 Server Component

```tsx
import { headers } from 'next/headers';
import { getAccessToken, getSession } from '@/lib/auth';
import { backendFetch } from '@/lib/backend';

export default async function UserPage({ params }: { params: Promise<{ tenant: string; id: string }> }) {
  const { tenant, id } = await params;
  const req = new Request('http://internal', { headers: await headers() }); // only the cookie header is read
  const [session, accessToken] = await Promise.all([getSession(req), getAccessToken(req)]);
  if (!accessToken || !session?.tenants?.[tenant]) return <p>Tidak punya akses.</p>;

  const res = await backendFetch(`/api/v1/users/${id}`, { accessToken, tenantId: tenant });
  const body = await res.json();
  // ...render, atau prefetch ke TanStack Query lalu kirim lewat HydrationBoundary
}
```

### 6.10 Logout

Logout **tidak** memakai form POST yang langsung redirect ke Zitadel: Chrome memblok langkah terakhir (Zitadel kembali ke aplikasi) karena aturan CSP `form-action`. Browser meminta URL logout lewat `fetch`, lalu pindah halaman dengan `window.location`.

`src/app/api/auth/logout/route.ts`:

```ts
import { NextResponse } from 'next/server';
import { buildLogoutUrl } from '@/lib/auth';

/** Returns the Zitadel end-session URL; the browser then navigates there (see LogoutButton). */
export async function POST(req: Request) {
  // Only our own pages may log the user out (CSRF protection).
  if (req.headers.get('origin') !== new URL(process.env.AUTH_URL!).origin) {
    return NextResponse.json({ error: { code: 'FORBIDDEN', message: 'bad origin' }, meta: {} }, { status: 403 });
  }

  const { url, state } = await buildLogoutUrl(req);
  const res = NextResponse.json({ url });
  res.cookies.set('logout_state', state, {
    httpOnly: true,
    secure: process.env.AUTH_URL?.startsWith('https://'),
    sameSite: 'lax',
    path: '/api/auth/logout/callback',
  });
  return res;
}
```

`src/app/api/auth/logout/callback/route.ts`:

```ts
import { NextResponse, type NextRequest } from 'next/server';

// Zitadel redirects here after ending its session: verify state, then clear the local session.
export async function GET(req: NextRequest) {
  const state = req.nextUrl.searchParams.get('state');
  const expected = req.cookies.get('logout_state')?.value;
  const res = NextResponse.redirect(new URL(state && state === expected ? '/' : '/?logout=error', req.url));
  res.cookies.delete('logout_state');
  for (const c of req.cookies.getAll()) {
    if (c.name.includes('authjs.')) res.cookies.delete(c.name);
  }
  return res;
}
```

Tombol logout (Client Component):

```tsx
'use client';

import { useState } from 'react';

export function LogoutButton() {
  const [pending, setPending] = useState(false);

  async function logout() {
    setPending(true);
    try {
      const res = await fetch('/api/auth/logout', { method: 'POST' });
      const { url } = (await res.json()) as { url: string };
      window.location.assign(url);
    } catch {
      setPending(false);
    }
  }

  return (
    <button type="button" disabled={pending} onClick={() => void logout()}>
      {pending ? 'Logging out…' : 'Log out'}
    </button>
  );
}
```

### 6.11 Menampilkan UI berdasarkan role

Data `tenants` di session (contoh `{"<orgId>": ["admin"]}`) boleh dipakai untuk menyembunyikan menu atau tombol, dan untuk daftar pilihan tenant. Ini hanya untuk tampilan; keputusan izin tetap di backend.

---

## 7. Operasional Multi-Tenant

**Onboarding tenant baru** (lewat Console, atau otomatis dari backend memakai client Management API di `zitadel-go/pkg/client`):

1. Buat Organization baru (nama tenant).
2. Buat Project Grant dari project `codebase-go` ke organization tersebut, pilih role yang boleh dipakai tenant.
3. Buat user admin tenant (username = email), lalu beri role `admin` lewat grant tadi.
4. Admin tenant login ke Console Zitadel (atau ke halaman manajemen user yang kita buat) untuk menambah user dan membagikan role.

**Tenant aktif:** frontend menyimpan tenant di URL (`/[tenant]/...`) dan mengirimnya sebagai `X-Tenant-ID`. Kalau header kosong, backend memakai organization asal user.

**Mengubah izin:**

- Arti role (permission) diubah di `internal/platform/auth/permissions.go`, lalu deploy backend.
- Role user diubah di Zitadel. Berlaku setelah access token diperbarui (paling lama 15 menit dengan setting bootstrap).

---

## 8. Checklist Production

- [ ] Zitadel memakai HTTPS dan domain sendiri (`ZITADEL_EXTERNALDOMAIN`, `ZITADEL_EXTERNALSECURE=true`), mengikuti panduan resmi self-hosting Zitadel.
- [ ] `ZITADEL_MASTERKEY` (32 karakter acak), password database, dan `ZITADEL_LOGIN_COOKIE_SECRET` diambil dari secret manager. Masterkey **tidak boleh hilang**.
- [ ] Password `zitadel-admin` diganti. Token `admin.pat` disimpan aman atau dihapus setelah bootstrap.
- [ ] App OIDC: `devMode` dimatikan, redirect URI memakai `https://`.
- [ ] Frontend: `AUTH_SECRET` acak dan rahasia, `AUTH_URL` memakai `https://` (cookie jadi `Secure`).
- [ ] Frontend lebih dari 1 instance: pakai sticky session, atau pindahkan cache hasil refresh ke store bersama (contoh Redis), karena refresh token Zitadel dirotasi.
- [ ] Backend: `ZITADEL_DOMAIN` memakai `https://`. Backend perlu bisa menjangkau Zitadel saat start (OIDC discovery).
- [ ] Backup database Zitadel bersama backup database aplikasi.
- [ ] Versi image Zitadel di-pin. Baca catatan rilis sebelum upgrade.
- [ ] Data milik tenant di database aplikasi selalu difilter dengan `tenant_id` (Bagian 5.10).

---

## 9. Troubleshooting

| Gejala | Penyebab | Solusi |
|---|---|---|
| Login ditolak `Errors.User.GrantRequired` | User belum punya role di project (`projectRoleCheck: true`) | Beri user grant (role) di tenant-nya |
| Session API `User could not be found` | Login name salah. Login name = username, unik di seluruh instance (tidak ditambah domain organization). | Login dengan username persisnya, contoh `alice@tenant-a.test` |
| Backend 401 dan log `invalid audience` | `ZITADEL_PROJECT_ID` tidak ada di `aud` token | Pastikan FE meminta scope `urn:zitadel:iam:org:project:id:<project id>:aud` dan ID-nya sama |
| Backend 403 `missing permission` padahal user admin | Tenant aktif bukan tenant tempat role diberikan | Cek header `X-Tenant-ID` dan claim `urn:zitadel:iam:org:project:roles` |
| Backend gagal start: `OIDC discovery failed` | Zitadel belum jalan atau `ZITADEL_DOMAIN` salah | `make infra-auth-up`, cek `ZITADEL_DOMAIN/.well-known/openid-configuration` |
| User tiba-tiba logout, log FE `token refresh failed ... RefreshTokenInvalid` | Refresh token dipakai dua kali (rotasi) | Pastikan refresh hanya terjadi lewat `proxy.ts` dan cache hasil refresh aktif. Multi-instance: sticky session. |
| Token terlihat di browser | Token dimasukkan ke callback `session` | Hanya taruh data aman di session, baca token pakai `getAccessToken()` |
| Bootstrap gagal `HTTP 409` / `AlreadyExists` | Script dijalankan dua kali | Reset Zitadel (baris di bawah), lalu jalankan ulang |
| Update setting OIDC gagal `invalid google.protobuf.Duration` | Format durasi salah | Pakai detik, contoh `"900s"`, bukan `"15m"` |
| `curl: option --fail-with-body: is unknown` | curl versi lama | Script di lampiran sudah tidak memakai opsi itu |
| Zitadel menolak login: redirect URI `http://0.0.0.0:3000/...` | Auth.js memakai URL internal server (standalone/proxy) | Pakai `onAppOrigin` (Bagian 6.5) dan isi `AUTH_URL` dengan URL publik |
| BFF membalas 403 `bad origin` di Docker/standalone | Origin dibandingkan dengan `req.url` (URL internal) | Bandingkan dengan origin `AUTH_URL` (Bagian 6.8) |
| Logout tidak kembali ke aplikasi, console: `violates ... form-action` | Form POST yang redirect ke Zitadel lalu kembali diblok CSP di Chrome | Logout lewat `fetch` + `window.location` (Bagian 6.10) |

Reset Zitadel lokal (menghapus semua user, tenant, dan app):

```bash
docker compose -f docker-compose.optional.yml --profile auth down -v
docker compose exec postgres psql -U postgres -c 'DROP DATABASE IF EXISTS zitadel WITH (FORCE)' -c 'DROP ROLE IF EXISTS zitadel'
make infra-auth-up && make zitadel-bootstrap
```

---

## 10. Hasil Uji

Diuji pada 8 Oktober 2026 dengan Zitadel v4.19.4 lokal, backend `codebase-go` + perubahan di Bagian 5, dan aplikasi Next.js 16.4.0 dengan kode di Bagian 6.

| Skenario | Hasil |
|---|---|
| Zitadel start dari nol dengan PostgreSQL bersama | Sehat sekitar 1 menit, database `zitadel` dibuat sendiri |
| Script bootstrap pada Zitadel bersih | Semua objek terbuat, umur token 900 detik |
| Token user: `aud` berisi project ID, role per tenant ada di claim | Sesuai |
| REST tanpa token / token rusak | 401 (envelope JSON) |
| REST admin di tenant sendiri / tenant lain | 200 / 403 |
| REST viewer membuat user | 403 `missing permission users:create` |
| gRPC tanpa token / admin / tenant lain / viewer create | `Unauthenticated` / OK / `PermissionDenied` / `PermissionDenied` |
| gRPC health dan reflection tanpa token | Bisa diakses |
| Login lewat Next.js (Auth.js) sampai callback | Session terbentuk, redirect ke halaman tujuan |
| `GET /api/auth/session` dari browser | Hanya nama, email, tenant + role. Tidak ada token. |
| Server Component memanggil backend | 200 |
| BFF: GET, POST, tenant asing, Origin asing, path di luar allowlist | 200, 201, 403, 403, 404 |
| Refresh token: 3 request paralel + 1 request terlambat dengan cookie lama, lalu request setelah token awal kedaluwarsa | Semua 200, tidak ada error refresh |
| Logout | Session Zitadel diakhiri, state diverifikasi, cookie lokal dihapus, BFF kembali 401 |
| Unit test `internal/platform/auth` dan seluruh test backend | Lulus |
| E2E Playwright (Chromium) di `codebase-fe`, mode standalone dan `pnpm dev`: login lewat Login UI Zitadel, buat user, detail (prefetch server), conflict, tenant asing, logout, tanpa token di browser, tanpa pelanggaran CSP | Lulus |

---

## 11. Lampiran: Script

### 11.1 `scripts/zitadel-bootstrap.sh`

```bash
#!/usr/bin/env bash
# Creates the Zitadel objects codebase-go and codebase-fe need, for LOCAL development:
#   project + roles, OIDC app for the frontend, one tenant org with an admin user.
# Requires: curl, jq, and the admin PAT written by Zitadel at first start.
# Run it once on a fresh Zitadel. To start over, reset the Zitadel database.
set -euo pipefail

ZITADEL_URL="${ZITADEL_URL:-http://localhost:8081}"
PAT_FILE="${PAT_FILE:-./admin.pat}"
FE_URL="${FE_URL:-http://localhost:3000}"
TENANT_NAME="${TENANT_NAME:-Tenant A}"
# Usernames are unique across the whole instance, so use the email as username.
TENANT_ADMIN_EMAIL="${TENANT_ADMIN_EMAIL:-alice@tenant-a.test}"
TENANT_ADMIN_PASSWORD="${TENANT_ADMIN_PASSWORD:-Password1!}"

PAT="$(cat "$PAT_FILE")"

# api METHOD PATH [JSON] [ORG_ID] -> prints the JSON response, fails on HTTP errors.
api() {
  local method=$1 path=$2 body=${3:-} org=${4:-}
  local args=(-sS -w $'\n%{http_code}' -X "$method" "$ZITADEL_URL$path"
    -H "Authorization: Bearer $PAT" -H "Content-Type: application/json")
  [[ -n "$org" ]] && args+=(-H "x-zitadel-orgid: $org")
  [[ -n "$body" ]] && args+=(-d "$body")
  local out status
  out=$(curl "${args[@]}")
  status=${out##*$'\n'}
  out=${out%$'\n'*}
  if (( status >= 400 )); then
    echo "HTTP $status on $method $path: $out" >&2
    return 1
  fi
  printf '%s' "$out"
}

echo "0. Token lifetimes: short access token, long refresh token"
api PUT /admin/v1/settings/oidc '{
  "accessTokenLifetime": "900s",
  "idTokenLifetime": "900s",
  "refreshTokenIdleExpiration": "2592000s",
  "refreshTokenExpiration": "7776000s"
}' >/dev/null

echo "1. Project with role assertion (roles are put into the tokens)"
PROJECT_ID=$(api POST /management/v1/projects '{
  "name": "codebase-go",
  "projectRoleAssertion": true,
  "projectRoleCheck": true
}' | jq -r .id)

echo "2. Project roles"
api POST "/management/v1/projects/$PROJECT_ID/roles/_bulk" '{"roles": [
  {"key": "owner",  "displayName": "Owner"},
  {"key": "admin",  "displayName": "Admin"},
  {"key": "member", "displayName": "Member"},
  {"key": "viewer", "displayName": "Viewer"}
]}' >/dev/null

echo "3. OIDC web app for codebase-fe (code flow + refresh token, JWT access token)"
APP=$(api POST "/management/v1/projects/$PROJECT_ID/apps/oidc" "{
  \"name\": \"codebase-fe\",
  \"appType\": \"OIDC_APP_TYPE_WEB\",
  \"authMethodType\": \"OIDC_AUTH_METHOD_TYPE_BASIC\",
  \"responseTypes\": [\"OIDC_RESPONSE_TYPE_CODE\"],
  \"grantTypes\": [\"OIDC_GRANT_TYPE_AUTHORIZATION_CODE\", \"OIDC_GRANT_TYPE_REFRESH_TOKEN\"],
  \"redirectUris\": [\"$FE_URL/api/auth/callback/zitadel\"],
  \"postLogoutRedirectUris\": [\"$FE_URL/api/auth/logout/callback\"],
  \"accessTokenType\": \"OIDC_TOKEN_TYPE_JWT\",
  \"accessTokenRoleAssertion\": true,
  \"idTokenRoleAssertion\": true,
  \"idTokenUserinfoAssertion\": true,
  \"devMode\": true
}")
FE_CLIENT_ID=$(jq -r .clientId <<<"$APP")
FE_CLIENT_SECRET=$(jq -r .clientSecret <<<"$APP")

echo "4. Tenant organization"
TENANT_ORG_ID=$(api POST /management/v1/orgs "{\"name\": \"$TENANT_NAME\"}" | jq -r .id)

echo "5. Grant the project (and its roles) to the tenant"
GRANT_ID=$(api POST "/management/v1/projects/$PROJECT_ID/grants" "{
  \"grantedOrgId\": \"$TENANT_ORG_ID\",
  \"roleKeys\": [\"admin\", \"member\", \"viewer\"]
}" | jq -r .grantId)

echo "6. Tenant admin user"
USER_ID=$(api POST /management/v1/users/human/_import "{
  \"userName\": \"$TENANT_ADMIN_EMAIL\",
  \"profile\": {\"firstName\": \"Alice\", \"lastName\": \"Admin\"},
  \"email\": {\"email\": \"$TENANT_ADMIN_EMAIL\", \"isEmailVerified\": true},
  \"password\": \"$TENANT_ADMIN_PASSWORD\",
  \"passwordChangeRequired\": false
}" "$TENANT_ORG_ID" | jq -r .userId)

echo "7. Give the user the 'admin' role inside the tenant"
api POST "/management/v1/users/$USER_ID/grants" "{
  \"projectId\": \"$PROJECT_ID\",
  \"projectGrantId\": \"$GRANT_ID\",
  \"roleKeys\": [\"admin\"]
}" "$TENANT_ORG_ID" >/dev/null

cat <<OUT

Done. Copy these values:

# codebase-go (.env)
AUTH_ENABLED=true
ZITADEL_DOMAIN=$ZITADEL_URL
ZITADEL_PROJECT_ID=$PROJECT_ID

# codebase-fe (.env.local)
ZITADEL_DOMAIN=$ZITADEL_URL
ZITADEL_CLIENT_ID=$FE_CLIENT_ID
ZITADEL_CLIENT_SECRET=$FE_CLIENT_SECRET
ZITADEL_PROJECT_ID=$PROJECT_ID

# Test login
#   tenant org id : $TENANT_ORG_ID
#   login name    : $TENANT_ADMIN_EMAIL
#   password      : $TENANT_ADMIN_PASSWORD
OUT
```

### 11.2 `scripts/zitadel-token.sh`

**Hanya untuk lokal.** Mengambil token user tanpa browser dengan meniru Login UI (Session API v2 + OIDC Authorization Code + PKCE). Script ini memakai token `login-client`, yang punya hak besar. Jangan dipakai di luar lokal.

Simpan `ZITADEL_CLIENT_ID`, `ZITADEL_CLIENT_SECRET`, dan `ZITADEL_PROJECT_ID` hasil bootstrap di env, lalu salin token login client: `docker compose -f docker-compose.optional.yml --profile auth exec -T zitadel-login cat /zitadel/bootstrap/login-client.pat > .zitadel/login-client.pat`.

```bash
#!/usr/bin/env bash
# LOCAL ONLY: logs a user in like the Login UI would and prints the token response (JSON).
# Usage: ./scripts/zitadel-token.sh <login name> <password>
set -euo pipefail

Z="${ZITADEL_URL:-http://localhost:8081}"
LPAT=$(cat "${LOGIN_PAT_FILE:-.zitadel/login-client.pat}")
REDIRECT="${FE_URL:-http://localhost:3000}/api/auth/callback/zitadel"
LOGIN_NAME=$1
PASSWORD=$2

VERIFIER=$(python3 -c 'import secrets; print(secrets.token_urlsafe(48))')
CHALLENGE=$(python3 -c "import hashlib,base64,sys; print(base64.urlsafe_b64encode(hashlib.sha256(sys.argv[1].encode()).digest()).rstrip(b'=').decode())" "$VERIFIER")
SCOPE="openid profile email offline_access urn:zitadel:iam:org:projects:roles urn:zitadel:iam:user:resourceowner urn:zitadel:iam:org:project:id:${ZITADEL_PROJECT_ID}:aud"

# 1. Start the OIDC flow; Zitadel answers with a redirect to the Login UI carrying the auth request ID.
LOC=$(curl -s -o /dev/null -w '%{redirect_url}' -G "$Z/oauth/v2/authorize" \
  --data-urlencode "client_id=$ZITADEL_CLIENT_ID" --data-urlencode "redirect_uri=$REDIRECT" \
  --data-urlencode response_type=code --data-urlencode "scope=$SCOPE" \
  --data-urlencode "code_challenge=$CHALLENGE" --data-urlencode code_challenge_method=S256 \
  --data-urlencode state=local)
AUTHREQ=$(python3 -c "import sys,urllib.parse as u; print(u.parse_qs(u.urlparse(sys.argv[1]).query)['authRequest'][0])" "$LOC")

# 2. Check the user's password (what the Login UI does).
SESSION=$(curl -s -X POST "$Z/v2/sessions" -H "Authorization: Bearer $LPAT" -H 'Content-Type: application/json' \
  -d "{\"checks\":{\"user\":{\"loginName\":\"$LOGIN_NAME\"},\"password\":{\"password\":\"$PASSWORD\"}}}")

# 3. Link the session to the auth request and get the callback URL with the code.
CB=$(curl -s -X POST "$Z/v2/oidc/auth_requests/$AUTHREQ" -H "Authorization: Bearer $LPAT" -H 'Content-Type: application/json' \
  -d "$(jq -c '{session:{sessionId:.sessionId, sessionToken:.sessionToken}}' <<<"$SESSION")")
CBURL=$(jq -r '.callbackUrl // empty' <<<"$CB")
[[ -n "$CBURL" ]] || { echo "login failed: $CB $SESSION" >&2; exit 1; }
CODE=$(python3 -c "import sys,urllib.parse as u; print(u.parse_qs(u.urlparse(sys.argv[1]).query)['code'][0])" "$CBURL")

# 4. Exchange the code for tokens.
curl -s -u "$ZITADEL_CLIENT_ID:$ZITADEL_CLIENT_SECRET" -X POST "$Z/oauth/v2/token" \
  -d grant_type=authorization_code --data-urlencode "code=$CODE" \
  --data-urlencode "redirect_uri=$REDIRECT" --data-urlencode "code_verifier=$VERIFIER"
```
