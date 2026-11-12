# Belajar Alur Backend Go: dari Database sampai Endpoint

Panduan ini menjelaskan kode yang ada di proyek **BE-Golang**. Contoh utamanya registrasi akun, dilanjutkan login, profil, dan pengaturan role admin. Baca dari atas ke bawah untuk memahami hubungan antarfile sebelum mencoba menambah fitur sendiri.

Potongan yang diberi label **cuplikan** hanya menampilkan bagian yang sedang dibahas. Fungsi lengkap diambil dari kode proyek saat panduan dibuat. Potongan ini untuk dipelajari di lokasi file asalnya, bukan ditempel semuanya ke satu file Go.

Database tujuan proyek adalah **Neon → `Loan_Banking` → schema `public` → tabel `users`**. Nama database tetap mengikuti konfigurasi `DATABASE_URL`; nama tersebut tidak dipaksakan oleh kode repository.

## Daftar isi

1. [Gambaran alur dan pembagian tugas](#1-gambaran-alur-dan-pembagian-tugas)
2. [Peta file proyek](#2-peta-file-proyek)
3. [Dasar Go yang dipakai](#3-dasar-go-yang-dipakai)
4. [Konfigurasi: alamat database berasal dari mana?](#4-konfigurasi-alamat-database-berasal-dari-mana)
5. [Koneksi: Go menghubungi Neon](#5-koneksi-go-menghubungi-neon)
6. [Model dan kontrak repository](#6-model-dan-kontrak-repository)
7. [Validasi dan password](#7-validasi-dan-password)
8. [Repository: SQL menjadi data Go](#8-repository-sql-menjadi-data-go)
9. [Service: aturan registrasi](#9-service-aturan-registrasi)
10. [Handler: menerima request dan mengirim JSON](#10-handler-menerima-request-dan-mengirim-json)
11. [Router: fungsi menjadi endpoint](#11-router-fungsi-menjadi-endpoint)
12. [Main: merangkai dan menjalankan semuanya](#12-main-merangkai-dan-menjalankan-semuanya)
13. [Menelusuri satu registrasi dari awal sampai akhir](#13-menelusuri-satu-registrasi-dari-awal-sampai-akhir)
14. [Login dan JWT](#14-login-dan-jwt)
15. [Profil dan RBAC admin](#15-profil-dan-rbac-admin)
16. [Health check dan perintah CLI](#16-health-check-dan-perintah-cli)
17. [Urutan membuat backend ini](#17-urutan-membuat-backend-ini)
18. [Cara membaca pengujian](#18-cara-membaca-pengujian)
19. [Latihan dan cara mencari masalah](#19-latihan-dan-cara-mencari-masalah)

## 1. Gambaran alur dan pembagian tugas

Backend menerima request, menjalankan aturan aplikasi, membaca/menulis database jika diperlukan, lalu mengirim response. Dalam proyek ini, tugasnya dibagi sebagai berikut:

| Bagian | Pertanyaan yang dijawab | Contoh |
| --- | --- | --- |
| Config | Aplikasi harus memakai pengaturan apa? | Database, port, secret JWT |
| Database | Bagaimana mendapatkan koneksi PostgreSQL? | Membuka pool dan melakukan ping |
| Model | Data pengguna berbentuk apa? | Struct `User` |
| Repository | SQL apa yang harus dijalankan? | `INSERT` akun, `SELECT` profil |
| Service | Apakah operasi ini boleh dan valid? | Hash password, wajib role admin |
| Handler | Bagaimana membaca dan membalas HTTP? | Decode JSON, status `201` |
| Router | URL dan method ini masuk ke handler mana? | `POST /api/v1/auth/register` |
| Main | Bagaimana semua bagian dihubungkan? | Pool → repository → service → router |

**Saat server mulai**, komponen dibangun dari bawah ke atas:

```mermaid
flowchart LR
    A[main.go] --> B[Load konfigurasi]
    B --> C[Buka pool PostgreSQL]
    C --> D[Buat repository]
    D --> E[Buat service dan TokenManager]
    E --> F[Buat router dan handler]
    F --> G[Jalankan HTTP server]
```

**Saat request registrasi datang**, pemanggilannya berjalan seperti ini:

```mermaid
sequenceDiagram
    participant C as Thunder Client
    participant H as Router / Handler
    participant S as Auth Service
    participant R as Repository
    participant D as Neon Loan_Banking
    C->>H: POST register + JSON
    H->>S: Register(ctx, username, email, password)
    S->>S: Validasi + hash password
    S->>R: Create(ctx, username, email, hash, user)
    R->>D: INSERT INTO public.users ... RETURNING
    D-->>R: Baris akun baru
    R-->>S: User atau error
    S-->>H: User tanpa password hash atau error
    H-->>C: 201 JSON atau status error
```

Database tidak memanggil service. **Service memanggil repository**, dan repository mengirim SQL ke database. Hasilnya dikembalikan melalui urutan yang berlawanan.

Service dalam proyek ini adalah objek Go yang berisi aturan aplikasi. Endpoint adalah pintu HTTP untuk memanggil aturan tersebut. Membuat fungsi service saja belum membuat URL API.

## 2. Peta file proyek

```text
BE-Golang/
├── main.go
├── go.mod
├── go.sum
├── .env                  # Konfigurasi nyata, tidak untuk dibagikan
├── .env.example          # Contoh konfigurasi tanpa secret nyata
├── cmd/create-user/main.go
├── internal/
│   ├── config/
│   │   ├── config.go
│   │   └── config_test.go
│   ├── database/postgres.go
│   ├── auth/
│   │   ├── user.go
│   │   ├── repository.go
│   │   ├── password.go
│   │   ├── token.go
│   │   ├── service.go
│   │   ├── service_test.go
│   │   └── token_test.go
│   └── httpapi/
│       ├── router.go
│       └── router_test.go
├── migrations/
│   ├── 001_users.sql
│   └── 002_user_roles.sql
├── README.md
├── README-WINDOWS.md
└── API.md
```

| File | Isi dan perannya |
| --- | --- |
| [main.go](main.go) | Entry point, merangkai dependensi, menjalankan server, menangani shutdown |
| [go.mod](go.mod) / [go.sum](go.sum) | Nama modul, versi Go/dependensi, dan checksum dependensi |
| [config.go](internal/config/config.go) | Membaca `.env`/environment dan memvalidasi konfigurasi |
| [postgres.go](internal/database/postgres.go) | Membuka pool koneksi PostgreSQL dan memastikan koneksi berhasil |
| [user.go](internal/auth/user.go) | Model, role, error aplikasi, normalisasi, interface repository |
| [repository.go](internal/auth/repository.go) | SQL dan pemetaan baris database ke `User` |
| [password.go](internal/auth/password.go) | Validasi password baru dan pembuatan hash bcrypt |
| [token.go](internal/auth/token.go) | Penerbitan dan verifikasi JWT |
| [service.go](internal/auth/service.go) | Aturan registrasi, login, profil, dan pengaturan role |
| [router.go](internal/httpapi/router.go) | Route, handler HTTP, JSON, error response, CORS, rate limit |
| [CLI create-user](cmd/create-user/main.go) | Membuat akun melalui terminal, termasuk admin awal |
| [001_users.sql](migrations/001_users.sql) | Referensi tabel awal dan index unik |
| [002_user_roles.sql](migrations/002_user_roles.sql) | Catatan perubahan role/index dan penyelarasan sequence ID |
| File `*_test.go` | Pengujian konfigurasi, token, aturan service, dan respons HTTP |
| [API.md](API.md) | Kontrak endpoint: request, response, header, dan status HTTP |

Folder `internal` membatasi pemakaian package dari luar pohon proyek yang diizinkan Go. Semua file dalam folder `internal/auth` memakai `package auth`, sehingga dapat saling menggunakan deklarasi dalam package tersebut.

`go.mod` menggunakan nama modul `backend-loanbanking`. Karena itu import lokal ditulis seperti `backend-loanbanking/internal/auth`.

## 3. Dasar Go yang dipakai

Cuplikan berikut akan sering muncul:

```go
user, err := service.repository.FindByID(ctx, userID)
if err != nil {
    return User{}, err
}
```

- `:=` mendeklarasikan variabel sekaligus mengisi nilainya.
- Fungsi bisa mengembalikan dua hasil: data dan `error`.
- `err == nil` berarti tidak ada error yang dikembalikan.
- `User{}` adalah nilai awal struct `User`; dikembalikan bersama error karena datanya tidak berhasil diperoleh.
- `return` langsung keluar dari fungsi. Baris setelahnya tidak dijalankan.

Cuplikan method:

```go
func (service *Service) Register(ctx context.Context, username, email, password string) (User, error)
```

`(service *Service)` disebut **receiver**: fungsi ini dimiliki objek `Service`, sehingga dipanggil melalui `service.Register(...)`. Tanda `*` menunjukkan pointer, yaitu referensi ke objek, bukan salinan seluruh objek.

Cuplikan pengisian struct:

```go
return &Service{repository: repository, tokens: tokens, dummyHash: dummyHash}, nil
```

`Service{...}` mengisi field struct; `&` mengambil alamat objek tersebut. Fungsi mengembalikan pointer service dan `nil` sebagai tanda konstruksi berhasil.

Nama dengan huruf awal besar, seperti `Register` dan `User`, dapat digunakan package lain. Nama seperti `writeJSON` dan `scanUser` hanya dipakai dalam package masing-masing.

`defer cancel()` berarti `cancel()` dijalankan ketika fungsi selesai. Pola yang sama dipakai untuk menutup pool, menutup hasil query, dan membatalkan transaksi yang belum selesai.

## 4. Konfigurasi: alamat database berasal dari mana?

Sumber: [internal/config/config.go](internal/config/config.go).

Contoh konfigurasi **ilustrasi**, bukan kredensial nyata:

```dotenv
HTTP_ADDRESS=127.0.0.1:8080
DATABASE_URL=postgresql://USER:PASSWORD@YOUR-ENDPOINT.neon.tech:5432/Loan_Banking?sslmode=verify-full
JWT_SECRET=REPLACE_WITH_A_RANDOM_SECRET_OF_AT_LEAST_32_BYTES
JWT_ISSUER=loanbanking-api
JWT_AUDIENCE=loanbanking-web
JWT_TTL=15m
CORS_ORIGIN=http://localhost:3000
```

`DATABASE_URL` berisi informasi koneksi, termasuk **nama database setelah host/port**. Mengganti `/Loan_Banking` menjadi `/neondb` berarti aplikasi menulis ke database berbeda walaupun server Neon-nya sama.

Cuplikan pembacaan:

```go
if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
    return "", errors.New("cannot load .env file")
}

databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
parsed, err := url.Parse(databaseURL)
```

1. `godotenv.Load()` memuat `.env` dari working directory saat program dijalankan.
2. `os.Getenv()` membaca nilai environment setelah pemuatan tersebut.
3. Environment yang sebelumnya sudah diekspor **tidak ditimpa** oleh `.env`.
4. URL diparsing agar formatnya dapat diperiksa. URL PostgreSQL harus memiliki host dan nama database.
5. Untuk host Neon, konfigurasi aplikasi mewajibkan `sslmode=verify-full`.

`config.Load()` kemudian membaca konfigurasi lain. Cuplikannya:

```go
tokenTTL, err := time.ParseDuration(valueOrDefault("JWT_TTL", "15m"))
if err != nil || tokenTTL < time.Second || tokenTTL > time.Hour {
    return Config{}, errors.New("JWT_TTL must be between 1s and 1h")
}
```

`15m` diubah menjadi `time.Duration`. Default TTL adalah 15 menit; aplikasi membatasinya antara 1 detik dan 1 jam. Secret JWT minimal 32 byte tanpa spasi di tepi. Origin CORS, jika diisi, harus berupa origin HTTP(S) tanpa path.

Hasil akhirnya berupa struct `Config`, bukan koneksi database. `main.go` baru menggunakan `settings.DatabaseURL` untuk membuka koneksi.

**Setelah `.env` diubah, restart backend.** Proses yang sudah berjalan tetap memakai pengaturan yang dibaca saat startup. Memilih database di dashboard Neon atau DBCode tidak mengubah koneksi backend secara otomatis.

## 5. Koneksi: Go menghubungi Neon

Sumber: [internal/database/postgres.go](internal/database/postgres.go).

```go
func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// Driver errors can contain the connection string, including its password.
		return nil, errors.New("invalid PostgreSQL connection configuration")
	}
	poolConfig.MaxConns = 10
	poolConfig.MinConns = 0
	poolConfig.MaxConnLifetime = 30 * time.Minute
	poolConfig.MaxConnIdleTime = 5 * time.Minute
	poolConfig.ConnConfig.ConnectTimeout = 10 * time.Second
	poolConfig.ConnConfig.RuntimeParams["timezone"] = "UTC"
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "loanbanking-auth"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, errors.New("cannot create PostgreSQL connection pool")
	}
	pingContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := pool.Ping(pingContext); err != nil {
		pool.Close()
		return nil, errors.New("cannot connect to PostgreSQL; check DATABASE_URL and network access")
	}
	return pool, nil
}
```

Baca fungsi ini dalam empat bagian:

1. **Parse konfigurasi.** `pgxpool.ParseConfig()` mengubah URL menjadi pengaturan koneksi.
2. **Atur pool.** `MaxConns = 10` membatasi jumlah koneksi dalam pool proses ini. Koneksi yang menganggur dapat digunakan kembali, sehingga setiap request tidak perlu membangun pool baru. Neon juga bisa memiliki pooling pada endpoint-nya; pool di kode ini tetap berada di proses Go.
3. **Buat pool.** `pgxpool.NewWithConfig()` membuat objek pengelola koneksi. Pembuatan objek saja belum cukup membuktikan database dapat dihubungi.
4. **Ping.** `pool.Ping()` benar-benar memeriksa akses database. Jika gagal, pool ditutup dan startup dikembalikan sebagai error.

`context.Context` membawa sinyal pembatalan dan batas waktu. Ping startup dibatasi 15 detik; setiap koneksi juga memiliki batas waktu pembentukan koneksi 10 detik. Durasi koneksi dan idle mengatur umur koneksi, bukan masa hidup akun.

`timezone = UTC` mengatur sesi PostgreSQL. Penggunaan `TIMESTAMP` tanpa timezone pada tabel tetap berbeda dari `TIMESTAMPTZ`; pengaturan sesi ini tidak mengubah tipe kolom.

Fungsi ini **tidak membuat tabel dan tidak menjalankan migrasi**. File `.sql` adalah perintah/catatan skema. Menyimpannya di laptop tidak membuat database lokal. Saat dijalankan pada koneksi Neon, perubahannya terjadi di Neon.

## 6. Model dan kontrak repository

Sumber: [internal/auth/user.go](internal/auth/user.go).

### 6.1 Model User

```go
type User struct {
    ID           int64  `json:"id"`
    Username     string `json:"username"`
    Email        string `json:"email"`
    Role         string `json:"role"`
    PasswordHash string `json:"-"`
    IsActive     bool   `json:"-"`
}
```

Struct ini menampung data pengguna di Go. Tag `json` mengatur **serialisasi JSON**, bukan nama kolom SQL. `json:"-"` membuat field tidak ikut dikirim oleh JSON encoder.

| Database | Field Go | Response JSON |
| --- | --- | --- |
| `id` | `ID` | `id` |
| `username` | `Username` | `username` |
| `email` | `Email` | `email` |
| `role` | `Role` | `role` |
| `password_hash` | `PasswordHash` | Tidak dikirim |
| `is_active` | `IsActive` | Tidak dikirim |

Tidak semua kolom tabel harus masuk model ini. Contohnya `fcm_token`, `created_at`, dan `updated_at` belum menjadi bagian response pengguna.

### 6.2 Interface sebagai kontrak

```go
type UserRepository interface {
    FindByEmail(context.Context, string) (User, error)
    FindByID(context.Context, int64) (User, error)
    Create(context.Context, string, string, string, string) (User, error)
    SetRole(context.Context, int64, int64, string) (User, error)
}
```

Interface berisi daftar kemampuan, tanpa SQL. Pada signature `Create`, empat string berurutan berarti `username`, `email`, `passwordHash`, dan `role`. Pada `SetRole`, dua angka berarti ID admin yang melakukan operasi dan ID akun tujuan.

`PostgresRepository` memenuhi kontrak ini karena memiliki semua method dengan signature yang sesuai. Go tidak memerlukan kata `implements`.

Service menyimpan `UserRepository`, sehingga dapat bekerja dengan repository PostgreSQL nyata maupun repository pengganti saat test. Interface tidak otomatis memilih database; objek nyata tetap diberikan dari `main.go`.

### 6.3 Role dan error aplikasi

```go
const (
    RoleUser  = "user"
    RoleAdmin = "admin"
)
```

Role dibatasi ke dua nilai tersebut. Error seperti `ErrUserExists`, `ErrInvalidCredentials`, dan `ErrForbidden` dipakai untuk meneruskan hasil antarbagian tanpa membawa detail HTTP atau membocorkan detail database.

## 7. Validasi dan password

### 7.1 Normalisasi username

Sumber: [user.go](internal/auth/user.go).

```go
func NormalizeUsername(username string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" || !utf8.ValidString(username) || utf8.RuneCountInString(username) > 50 || strings.ContainsFunc(username, unicode.IsControl) {
		return "", ErrInvalidRegistration
	}
	return username, nil
}

// User follows the existing public.users table. Password hashes never enter JSON responses.
type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	PasswordHash string `json:"-"`
	IsActive     bool   `json:"-"`
}

type UserRepository interface {
	FindByEmail(context.Context, string) (User, error)
	FindByID(context.Context, int64) (User, error)
	Create(context.Context, string, string, string, string) (User, error)
	SetRole(context.Context, int64, int64, string) (User, error)
}
```

Spasi di awal/akhir dihapus. Username harus berisi 1–50 karakter Unicode, berupa UTF-8 valid, dan tidak mengandung karakter kontrol seperti newline. Username tidak dipaksa lowercase; keunikan tanpa membedakan kapitalisasi dijamin index database.

### 7.2 Normalisasi email

```go
func NormalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 100 {
		return "", ErrInvalidInput
	}
	return email, nil
}
```

Contoh `" Alice@Example.com "` menjadi `"alice@example.com"`. Email harus valid, tidak memakai bentuk nama tampilan seperti `Alice <alice@example.com>`, dan panjang setelah normalisasi maksimal 100 byte.

### 7.3 Password menjadi hash

Sumber: [password.go](internal/auth/password.go). Konstanta `passwordCost` pada file ini bernilai `12`.

```go
func HashPassword(password string) (string, error) {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || len(password) > 72 {
		return "", errors.New("password must contain at least 12 characters and at most 72 bytes")
	}
	var hasNonSpace bool
	for _, character := range password {
		if !unicode.IsSpace(character) {
			hasNonSpace = true
			break
		}
	}
	if !hasNonSpace {
		return "", errors.New("password cannot contain only whitespace")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	return string(hash), err
}
```

Urutannya:

1. Tolak UTF-8 tidak valid, password di bawah 12 karakter, atau di atas 72 byte.
2. Pastikan ada karakter selain whitespace.
3. Bcrypt membuat hash menggunakan cost 12.
4. Ubah hasil `[]byte` menjadi `string` untuk disimpan repository.

Karakter dan byte berbeda: satu karakter Unicode dapat memakai beberapa byte. Password tidak di-trim atau diubah lowercase karena spasi dan kapitalisasi merupakan bagian dari password.

Hash bukan password yang dienkripsi untuk dibuka kembali. Login memeriksa kecocokan menggunakan bcrypt. Dua pemanggilan hash untuk password sama dapat menghasilkan string berbeda karena bcrypt memakai salt.

## 8. Repository: SQL menjadi data Go

Sumber: [internal/auth/repository.go](internal/auth/repository.go).

### 8.1 Repository memegang pool

```go
type PostgresRepository struct{ pool *pgxpool.Pool }

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
    return &PostgresRepository{pool: pool}
}
```

Fungsi constructor ini hanya menyimpan pool yang sudah dibuat. Ia tidak membuka database baru setiap kali `Create` dipanggil.

### 8.2 Urutan kolom dan Scan

```go
const userColumns = `id, username, email, COALESCE(password_hash, ''), COALESCE(is_active, false), role`
```

```go
func scanUser(row pgx.Row) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.IsActive, &user.Role)
	return user, err
}
```

`Scan()` memindahkan hasil kolom SQL ke field Go **berdasarkan urutan**, bukan berdasarkan tag JSON. Kolom pertama masuk `ID`, kedua `Username`, dan seterusnya.

`&user.ID` memberikan alamat field agar `Scan` dapat mengisinya. `COALESCE(password_hash, '')` mengubah SQL `NULL` menjadi string kosong. `COALESCE(is_active, false)` membuat status `NULL` diperlakukan sebagai tidak aktif.

### 8.3 Menulis pengguna baru

```go
func (repository *PostgresRepository) Create(ctx context.Context, username, email, passwordHash, role string) (User, error) {
	if !ValidRole(role) {
		return User{}, ErrInvalidRole
	}
	user, err := scanUser(repository.pool.QueryRow(ctx, `
		INSERT INTO public.users (username, email, password_hash, is_active, role, created_at, updated_at)
		VALUES ($1, $2, $3, true, $4, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING `+userColumns, username, email, passwordHash, role))
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return User{}, ErrUserExists
	}
	if err != nil {
		return User{}, errors.New("cannot create user; verify the users table and its unique indexes")
	}
	return user, nil
}
```

SQL `INSERT` inilah yang benar-benar menyimpan akun. Penjelasan bagian penting:

| Potongan | Maksud |
| --- | --- |
| `public.users` | Schema dan tabel tujuan di database yang dipilih koneksi |
| `$1` | Nilai `username` |
| `$2` | Nilai `email` |
| `$3` | Hash password, bukan password asli |
| `$4` | Role yang diberikan service |
| `true` | Akun baru aktif |
| `CURRENT_TIMESTAMP` | Waktu database saat transaksi berjalan |
| `RETURNING` | Meminta database mengembalikan data baris yang baru ditulis |
| `QueryRow` + `scanUser` | Mengeksekusi query satu baris dan membaca hasil/error-nya |

Nilai dikirim sebagai parameter, bukan digabungkan ke string SQL. Ini mencegah input pengguna diperlakukan sebagai bagian perintah SQL.

`id` tidak dikirim pada INSERT normal. Database membuatnya lewat default/sequence. Sequence tidak menjanjikan ID selalu berurutan tanpa celah; percobaan INSERT gagal juga bisa memakai nomor sequence.

PostgreSQL `23505` berarti pelanggaran unique constraint. Kode saat ini menerjemahkan seluruh error `23505` menjadi `ErrUserExists`, termasuk jika benturan terjadi pada primary key ID. Karena itu sequence ID juga perlu benar; catatan SQL `002` menyelaraskannya agar tidak tertinggal dari ID lama.

Setelah fungsi sukses, INSERT satu statement ini sudah selesai dalam transaksi implisit database. Tidak ada antrean akun tersimpan di file Go atau file migrasi. Penyimpanan terjadi pada database tujuan pool.

### 8.4 Membaca pengguna

```go
func (repository *PostgresRepository) FindByEmail(ctx context.Context, email string) (User, error) {
	rows, err := repository.pool.Query(ctx, `SELECT `+userColumns+` FROM public.users WHERE lower(btrim(email)) = $1 LIMIT 2`, email)
	if err != nil {
		return User{}, fmt.Errorf("find user: %w", err)
	}
	defer rows.Close()
	users, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (User, error) { return scanUser(row) })
	if err != nil {
		return User{}, fmt.Errorf("scan user: %w", err)
	}
	// Older schemas may lack a unique index. Ambiguous identities must fail closed.
	if len(users) != 1 {
		return User{}, ErrUserNotFound
	}
	return users[0], nil
}
```

Pencarian email memakai `lower(btrim(email))`, sehingga sesuai dengan normalisasi dan index unik. `LIMIT 2` digunakan untuk mendeteksi data lama yang ambigu. Jika hasil nol atau lebih dari satu akun, login menolak identitas tersebut.

`rows.Close()` mengembalikan resource hasil query; `pgx.CollectRows` memanggil `scanUser` untuk setiap baris.

```go
func (repository *PostgresRepository) FindByID(ctx context.Context, userID int64) (User, error) {
	user, err := scanUser(repository.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM public.users WHERE id = $1`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("find user: %w", err)
	}
	return user, nil
}
```

`pgx.ErrNoRows` diubah menjadi error aplikasi `ErrUserNotFound`. Service tidak perlu mengetahui nama error khusus driver PostgreSQL.

## 9. Service: aturan registrasi

Sumber: [internal/auth/service.go](internal/auth/service.go).

### 9.1 Isi Service dan constructor

```go
type Service struct {
    repository UserRepository
    tokens     *TokenManager
    dummyHash  []byte
}
```

```go
func NewService(repository UserRepository, tokens *TokenManager) (*Service, error) {
	// Unknown users still incur the same bcrypt work as a normal login attempt.
	dummyHash, err := bcrypt.GenerateFromPassword([]byte("dummy-password-never-used-for-login"), passwordCost)
	if err != nil {
		return nil, fmt.Errorf("initialize password verifier: %w", err)
	}
	return &Service{repository: repository, tokens: tokens, dummyHash: dummyHash}, nil
}
```

Service membutuhkan repository untuk akses data dan `TokenManager` untuk JWT. `dummyHash` dibuat satu kali saat service dibangun, untuk menjalankan pembandingan bcrypt juga ketika login memakai email yang tidak ditemukan. Ini mengurangi perbedaan waktu yang mudah diamati; bukan password akun atau akun default.

### 9.2 Aturan Register

```go
func (service *Service) Register(ctx context.Context, username, email, password string) (User, error) {
	username, err := NormalizeUsername(username)
	if err != nil {
		return User{}, ErrInvalidRegistration
	}
	email, err = NormalizeEmail(email)
	if err != nil {
		return User{}, ErrInvalidRegistration
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, ErrInvalidRegistration
	}
	user, err := service.repository.Create(ctx, username, email, hash, RoleUser)
	if err != nil {
		return User{}, err
	}
	user.PasswordHash = ""
	return user, nil
}
```

Baca dari atas ke bawah:

1. Rapikan dan validasi username.
2. Rapikan dan validasi email.
3. Validasi password baru dan buat hash.
4. Minta repository menyimpan akun dengan **`RoleUser`**.
5. Jika database gagal/duplikat, teruskan error; jangan mengirim sukses.
6. Kosongkan hash pada objek hasil, kemudian kembalikan pengguna.

Service ini menerima string, bukan `http.Request`. Ia tidak memilih status `201` atau menulis JSON. Itulah alasan aturan aplikasi bisa diuji tanpa HTTP server.

Tidak ada parameter role publik pada fungsi registrasi. Client tidak dapat menjadikan dirinya admin melalui body register.

## 10. Handler: menerima request dan mengirim JSON

Sumber: [internal/httpapi/router.go](internal/httpapi/router.go).

### 10.1 Kontrak service yang diperlukan HTTP

```go
type AuthService interface {
    Login(context.Context, string, string) (auth.LoginResult, error)
    CurrentUser(context.Context, string) (auth.User, error)
    Register(context.Context, string, string, string) (auth.User, error)
    SetUserRole(context.Context, string, int64, string) (auth.User, error)
}

type handler struct {
    service AuthService
    logger  *slog.Logger
    ping    func(context.Context) error
}
```

Ini interface kedua: HTTP membutuhkan kemampuan service, sedangkan service membutuhkan kemampuan repository. `ping` adalah fungsi pemeriksa database, dipakai readiness check.

### 10.2 Handler registrasi

```go
func (handlers *handler) register(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	defer cancel()
	user, err := handlers.service.Register(ctx, input.Username, input.Email, input.Password)
	if err != nil {
		handlers.serviceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{"data": user})
}
```

- `writer` dipakai menulis status, header, dan body response.
- `request` berisi method, URL, header, body, dan context request.
- `input` adalah struct khusus data masuk; bentuknya berbeda dari model `User` hasil database.
- `&input` membuat decoder dapat mengisi field struct tersebut.
- `context.WithTimeout(..., 5*time.Second)` meneruskan batas waktu operasi ke service/repository.
- `defer cancel()` membersihkan resource context ketika handler selesai.
- Saat sukses, handler mengirim `201 Created` dengan pembungkus `data`.

Context tidak otomatis menghentikan semua pekerjaan CPU. Query yang menerima context dapat dibatalkan, tetapi `bcrypt.GenerateFromPassword` tidak menerima context dan tidak terputus seketika hanya karena deadline berakhir.

### 10.3 Membaca JSON dengan aturan yang jelas

```go
func decodeJSON(writer http.ResponseWriter, request *http.Request, input any) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return false
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 4096)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		writeDecodeError(writer, err)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeDecodeError(writer, err)
		return false
	}
	return true
}
```

Fungsi ini mengerjakan tiga pemeriksaan utama:

1. Header harus memakai media type `application/json`; parameter seperti `charset=utf-8` diperbolehkan.
2. Body dibatasi 4096 byte dan field tak dikenal ditolak oleh `DisallowUnknownFields()`.
3. Setelah objek pertama, decoder mencoba membaca lagi. Hasil yang diharapkan adalah `io.EOF`, yaitu tidak ada nilai JSON tambahan.

Contoh `{"role":"admin"}` pada request register ditolak karena `input` register tidak memiliki field role. Validasi JSON memeriksa bentuk data; kelengkapan dan nilai bisnisnya tetap diperiksa service. Misalnya field wajib hilang menjadi string kosong dan kemudian ditolak service.

### 10.4 Mengubah data Go menjadi response

```go
func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
```

`json.NewEncoder(writer).Encode(value)` melakukan serialisasi Go → JSON. `map[string]any{"data": user}` menghasilkan objek dengan properti `data` yang berisi representasi JSON `User`. Field bertag `json:"-"` tidak disertakan.

Cuplikan pemetaan error:

```go
case errors.Is(err, auth.ErrUserExists):
    writeError(writer, http.StatusConflict, "user_exists", "Email or username already exists")
```

Ini cabang di `serviceError`. `errors.Is` mengenali jenis error aplikasi, termasuk error yang dibungkus. `writeError` membentuk `{"error":{"code":"...","message":"..."}}`, lalu memanggil `writeJSON`.

| Error aplikasi | Status handler |
| --- | --- |
| `ErrInvalidRegistration` | `400` |
| `ErrInvalidCredentials` | `401` |
| `ErrForbidden` | `403` |
| `ErrUserNotFound` untuk akun tujuan perubahan role | `404` |
| `ErrUserExists` | `409` |
| Error internal yang tidak dikenali | `500` dengan pesan umum |

Detail teknis rahasia tidak perlu menjadi response pengguna. Daftar error lengkap ada di [API.md](API.md).

## 11. Router: fungsi menjadi endpoint

Sumber: fungsi `NewRouter` pada [router.go](internal/httpapi/router.go).

Cuplikan route registrasi asli:

```go
router.With(httprate.LimitBy(5, time.Minute, clientIP, httprate.WithLimitHandler(func(writer http.ResponseWriter, request *http.Request) {
    writeError(writer, http.StatusTooManyRequests, "rate_limit_exceeded", "Too many registration attempts; try again later")
}))).Post("/register", handlers.register)
```

Baris ini berada di dalam `router.Route("/api/v1/auth", ...)`. Prefix dan path digabung menjadi **`POST /api/v1/auth/register`**. `handlers.register` diberikan sebagai fungsi yang akan dipanggil ketika request cocok, bukan dipanggil saat route didaftarkan.

| Method dan path lengkap | Handler | Pemanggilan service |
| --- | --- | --- |
| `POST /api/v1/auth/register` | `register` | `Register` |
| `POST /api/v1/auth/login` | `login` | `Login` |
| `GET /api/v1/auth/me` | `currentUser` | `CurrentUser` |
| `PATCH /api/v1/admin/users/{userID}/role` | `setUserRole` | `SetUserRole` |
| `GET /healthz` | Fungsi langsung di router | Tidak memakai auth service |
| `GET /readyz` | `ready` | Memanggil fungsi ping database |

Path `{userID}` diambil menggunakan `chi.URLParam(request, "userID")`, lalu diubah ke `int64` oleh `strconv.ParseInt`. Nilainya harus positif.

### Middleware yang membungkus handler

Cuplikan middleware global:

```go
router.Use(middleware.RequestID, middleware.Recoverer, middleware.Throttle(32))
router.Use(securityHeaders, cors(corsOrigin))
```

Middleware adalah fungsi yang membungkus jalannya request sebelum/sesudah handler:

| Middleware | Peran pada proyek ini |
| --- | --- |
| `RequestID` | Menyediakan penanda request untuk membantu penelusuran log |
| `Recoverer` | Menangani panic agar tidak langsung menjatuhkan seluruh proses |
| `Throttle(32)` | Membatasi request yang aktif bersamaan |
| `securityHeaders` | Menambahkan `nosniff` dan `Cache-Control: no-store` |
| `cors` | Memberikan izin baca response browser untuk origin yang dikonfigurasi |
| Rate limiter pada login/register | Masing-masing membatasi 5 request per menit per IP |

Hitungan rate limit ada di memori proses. Restart mereset hitungan itu, tetapi tidak menghapus akun database. CORS memengaruhi akses oleh browser; Thunder Client tetap perlu autentikasi untuk endpoint privat, tetapi tidak mengikuti pembatasan CORS seperti halaman browser.

RBAC proyek ini diperiksa dalam service dan transaksi repository. Prefix `/admin` saja tidak otomatis memberikan perlindungan admin.

## 12. Main: merangkai dan menjalankan semuanya

Sumber: [main.go](main.go).

```go
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err.Error())
		os.Exit(1)
	}
}
```

`main()` membuat logger, lalu memanggil `run`. Jika startup/server gagal, program mencatat pesan dan keluar dengan status kegagalan.

Fungsi `run` lengkap:

```go
func run(logger *slog.Logger) error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := database.Open(ctx, settings.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	logger.Info("database connected", "database", pool.Config().ConnConfig.Database)
	tokens := auth.NewTokenManager(settings.JWTSecret, settings.JWTIssuer, settings.JWTAudience, settings.JWTTTL)
	service, err := auth.NewService(auth.NewPostgresRepository(pool), tokens)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              settings.HTTPAddress,
		Handler:           httpapi.NewRouter(service, logger, pool.Ping, settings.CORSOrigin),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("HTTP server starting", "address", settings.HTTPAddress)
		serverErrors <- server.ListenAndServe()
	}()
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		stop()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			_ = server.Close()
			return err
		}
		logger.Info("HTTP server stopped")
		return nil
	}
}
```

Baca dalam urutan berikut:

1. **`config.Load()`** mengambil pengaturan aplikasi.
2. **`signal.NotifyContext`** membuat context yang dibatalkan saat Ctrl+C atau sinyal penghentian diterima.
3. **`database.Open`** membuka pool dan memeriksa Neon. Jika gagal, server belum menerima request.
4. **`defer pool.Close()`** memastikan pool ditutup saat `run` selesai.
5. **Log nama database** membantu memastikan proses benar-benar mengarah ke `Loan_Banking`.
6. **`NewTokenManager`** membuat pengelola JWT.
7. **`NewPostgresRepository(pool)`** membuat repository dengan pool yang sama.
8. **`NewService(repository, tokens)`** menyuntikkan repository dan pengelola JWT ke service.
9. **`NewRouter(service, logger, pool.Ping, ...)`** memberikan service ke handler/router.
10. **`http.Server`** memegang alamat, router sebagai `Handler`, serta batas waktu HTTP.
11. **`ListenAndServe()`** membuka listener dan melayani request. Goroutine `go func()` membuat `run` tetap bisa menunggu sinyal berhenti.
12. **`select`** menunggu server gagal atau context dibatalkan. Channel `serverErrors` mengirim hasil server kembali ke goroutine utama.
13. **`Shutdown`** memberi kesempatan request yang berjalan selesai, dibatasi 10 detik. Jika gagal, `Close` menutup server.

Hubungan objeknya, ditulis terpisah agar mudah dibaca (ilustrasi setara dengan konstruksi asli):

```go
repository := auth.NewPostgresRepository(pool)
service, err := auth.NewService(repository, tokens)
if err != nil {
    return err
}
router := httpapi.NewRouter(service, logger, pool.Ping, settings.CORSOrigin)
```

Inilah **dependency injection**: objek yang dibutuhkan diberikan sebagai parameter. `main.go` menjadi tempat perakitan; repository tidak membuat service, dan service tidak menjalankan HTTP server.

## 13. Menelusuri satu registrasi dari awal sampai akhir

Contoh ini memakai akun ilustrasi, bukan bukti akun sudah tersimpan.

### Langkah A — Client mengirim request

Di Thunder Client:

```http
POST http://127.0.0.1:8080/api/v1/auth/register
Content-Type: application/json
```

```json
{
  "username": "belajar",
  "email": "belajar@example.com",
  "password": "valid-password-123"
}
```

### Langkah B — Router dan handler

Chi mencocokkan method/path. Setelah middleware, `register()` menerima request. `decodeJSON()` mengisi `input.Username`, `input.Email`, dan `input.Password`.

### Langkah C — Service

`Register()` melakukan normalisasi dan menghasilkan hash bcrypt. Bentuk data berubah:

```text
Input:   username, email, password asli
Service: username normal, email normal, hash bcrypt, role user
```

Password asli hanya dipakai untuk menghasilkan hash; bukan isi yang diberikan sebagai `password_hash` ke database.

### Langkah D — Repository dan database

`repository.Create()` mengirim INSERT ke `public.users` pada database pool, yaitu konfigurasi yang dipilih saat startup. Database membuat ID dan mengembalikan baris melalui `RETURNING`.

### Langkah E — Hasil kembali

`scanUser()` membuat `User`; service membersihkan field hash pada objek hasil; handler mengubahnya menjadi JSON dengan status `201`.

Contoh response (ID ilustrasi):

```json
{
  "data": {
    "id": 10,
    "username": "belajar",
    "email": "belajar@example.com",
    "role": "user"
  }
}
```

Registrasi **tidak menerbitkan token**. Untuk mendapatkan access token, lakukan login.

### Langkah F — Lihat hasil di Neon

Pilih branch yang sesuai endpoint koneksi, database `Loan_Banking`, schema `public`, tabel `users`, lalu refresh. Jika perlu, jalankan query baca-saja ini pada database yang sama:

```sql
SELECT id, username, email, role, is_active
FROM public.users
WHERE lower(btrim(email)) = 'belajar@example.com';
```

Jika akun sama didaftarkan lagi, index unik menolak INSERT. Error bergerak dari PostgreSQL → repository → service → handler dan menjadi `409 user_exists`. Request kedua tidak membuat akun baru.

## 14. Login dan JWT

### 14.1 Handler login

Sumber: [router.go](internal/httpapi/router.go).

```go
func (handlers *handler) login(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	defer cancel()
	result, err := handlers.service.Login(ctx, input.Email, input.Password)
	if err != nil {
		handlers.serviceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": result})
}
```

Pola handler sama dengan register: decode input → buat context → panggil service → tangani error → kirim response. Perbedaannya field input hanya email/password, dan status sukses `200`.

### 14.2 Service login

Sumber: [service.go](internal/auth/service.go).

```go
func (service *Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	email, err := NormalizeEmail(email)
	if err != nil || len(password) == 0 || len(password) > 72 {
		return LoginResult{}, ErrInvalidInput
	}
	user, err := service.repository.FindByEmail(ctx, email)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return LoginResult{}, err
	}
	hash := []byte(user.PasswordHash)
	if err != nil || !user.IsActive || len(hash) == 0 {
		hash = service.dummyHash
	}
	passwordError := bcrypt.CompareHashAndPassword(hash, []byte(password))
	if err != nil || !user.IsActive || passwordError != nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	token, expiresIn, err := service.tokens.Issue(user.ID)
	if err != nil {
		return LoginResult{}, fmt.Errorf("issue access token: %w", err)
	}
	user.PasswordHash = ""
	return LoginResult{AccessToken: token, TokenType: "Bearer", ExpiresIn: expiresIn, User: user}, nil
}
```

Alurnya:

1. Normalisasi email dan periksa panjang password login (1–72 byte).
2. Ambil akun dengan `FindByEmail`.
3. Pilih hash akun, atau `dummyHash` jika akun tidak ditemukan/tidak aktif/tidak memiliki hash.
4. Jalankan `bcrypt.CompareHashAndPassword`.
5. Tolak kredensial salah/akun tidak aktif dengan error yang sama.
6. Terbitkan JWT jika semuanya valid.
7. Kembalikan token dan profil tanpa hash.

Login tidak memakai `HashPassword` lalu membandingkan dua string hash, karena salt bcrypt dapat membuat hasil string berbeda. Aturan minimal 12 karakter dipakai saat membuat password baru, bukan saat login akun lama.

### 14.3 Menerbitkan token

Sumber: [token.go](internal/auth/token.go). `TokenManager` menyimpan secret, issuer, audience, dan TTL yang diberikan `main.go`.

```go
func (manager *TokenManager) Issue(userID int64) (string, int64, error) {
	now := time.Now().UTC()
	claims := jwt.RegisteredClaims{
		Issuer:    manager.issuer,
		Subject:   strconv.FormatInt(userID, 10),
		Audience:  jwt.ClaimStrings{manager.audience},
		ExpiresAt: jwt.NewNumericDate(now.Add(manager.ttl)),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(manager.secret)
	return token, int64(manager.ttl.Seconds()), err
}
```

| Claim | Isi |
| --- | --- |
| `sub` | ID pengguna, diubah menjadi string |
| `iss` | Penerbit token yang dikonfigurasi |
| `aud` | Penerima token yang diharapkan |
| `exp` | Batas kedaluwarsa |
| `iat` | Waktu penerbitan |
| `nbf` | Waktu paling awal token boleh digunakan |

Token ditandatangani dengan HS256. JWT bukan penyimpanan password, dan payload bertanda tangan tidak otomatis terenkripsi. Proyek ini tidak memasukkan role dalam claim; hak akses dibaca kembali dari database.

Cuplikan bentuk hasil Go:

```go
type LoginResult struct {
    AccessToken string `json:"access_token"`
    TokenType   string `json:"token_type"`
    ExpiresIn   int64  `json:"expires_in"`
    User        User   `json:"user"`
}
```

`expires_in` adalah durasi dalam detik, default `900`. Token tidak disimpan dalam tabel sesi oleh implementasi ini.

### 14.4 Memverifikasi token

```go
func (manager *TokenManager) Verify(rawToken string) (int64, error) {
	var claims jwt.RegisteredClaims
	token, err := jwt.ParseWithClaims(rawToken, &claims, func(token *jwt.Token) (any, error) {
		return manager.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(manager.issuer), jwt.WithAudience(manager.audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || !token.Valid {
		return 0, errors.New("invalid access token")
	}
	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || userID <= 0 {
		return 0, errors.New("invalid access token subject")
	}
	return userID, nil
}
```

Verifikasi memeriksa algoritma, signature, issuer, audience, waktu token, serta expiration yang wajib ada. Claim subject kemudian harus dapat diubah menjadi ID integer positif. Decode payload tanpa verifikasi signature tidak cukup untuk menganggap token valid.

## 15. Profil dan RBAC admin

### 15.1 Membaca Bearer token

Sumber: [router.go](internal/httpapi/router.go).

```go
func bearerToken(request *http.Request) (string, bool) {
	parts := strings.Fields(request.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) > 4096 {
		return "", false
	}
	return parts[1], true
}
```

Client mengirim:

```http
Authorization: Bearer REPLACE_WITH_ACCESS_TOKEN
```

Fungsi ini hanya memeriksa bentuk header dan batas panjang token. Verifikasi JWT sesungguhnya dilakukan service melalui `TokenManager.Verify`.

Handler `currentUser` memanggil `CurrentUser`; handler `setUserRole` membaca `userID` dan body `{"role":"admin"}`, kemudian memanggil `SetUserRole`. Keduanya mengembalikan `data` dan menggunakan `serviceError` jika gagal.

### 15.2 Mengambil profil terbaru

Sumber: [service.go](internal/auth/service.go).

```go
func (service *Service) CurrentUser(ctx context.Context, accessToken string) (User, error) {
	userID, err := service.tokens.Verify(accessToken)
	if err != nil {
		return User{}, ErrInvalidCredentials
	}
	user, err := service.repository.FindByID(ctx, userID)
	if errors.Is(err, ErrUserNotFound) {
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	if !user.IsActive {
		return User{}, ErrInvalidCredentials
	}
	user.PasswordHash = ""
	return user, nil
}
```

Token membuktikan identitas akun melalui ID. Database menentukan apakah akun masih ada dan aktif. Karena itu akun yang dinonaktifkan/dihapus tidak dapat terus memakai endpoint ini walaupun token belum kedaluwarsa.

### 15.3 Memeriksa izin admin di service

```go
func (service *Service) SetUserRole(ctx context.Context, accessToken string, userID int64, role string) (User, error) {
	actor, err := service.CurrentUser(ctx, accessToken)
	if err != nil {
		return User{}, err
	}
	if actor.Role != RoleAdmin {
		return User{}, ErrForbidden
	}
	if userID <= 0 || !ValidRole(role) {
		return User{}, ErrInvalidRole
	}
	user, err := service.repository.SetRole(ctx, actor.ID, userID, role)
	if err != nil {
		return User{}, err
	}
	user.PasswordHash = ""
	return user, nil
}
```

**Autentikasi** menjawab “siapa pengguna ini?” melalui token dan akun aktif. **Otorisasi/RBAC** menjawab “apakah pengguna ini boleh mengubah role?” melalui `actor.Role`.

`actor` adalah pengguna yang mengirim request. `userID` adalah pengguna yang akan diubah. Kedua ID itu bisa berbeda.

- Tidak ada token valid/akun aktif → `401`.
- Login sebagai `user` lalu mengubah role → `403`.
- Admin dengan ID/role tujuan tidak valid → `400`.
- Admin dengan akun tujuan tidak ditemukan → `404`.
- Admin berhasil → `200` dengan pengguna yang sudah diperbarui.

### 15.4 Mengubah role dalam transaksi

Sumber: [repository.go](internal/auth/repository.go).

```go
func (repository *PostgresRepository) SetRole(ctx context.Context, actorID, userID int64, role string) (User, error) {
	if !ValidRole(role) || userID <= 0 {
		return User{}, ErrInvalidRole
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)
	actor, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM public.users WHERE id = $1 FOR UPDATE`, actorID))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !actor.IsActive) {
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	if actor.Role != RoleAdmin {
		return User{}, ErrForbidden
	}
	user, err := scanUser(tx.QueryRow(ctx, `UPDATE public.users SET role = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 RETURNING `+userColumns, role, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return user, nil
}
```

Transaksi membuat pemeriksaan database dan perubahan role menjadi satu kesatuan:

1. `Begin` membuka transaksi.
2. `SELECT ... FOR UPDATE` membaca sekaligus mengunci baris admin pemanggil sampai transaksi selesai.
3. Status aktif dan role diperiksa lagi, karena bisa berubah sejak service memeriksanya.
4. `UPDATE` mengubah pengguna tujuan dan mengembalikan profilnya.
5. `Commit` menyimpan perubahan transaksi.
6. `defer Rollback` membersihkan transaksi jika ada jalan keluar sebelum commit. Setelah commit berhasil, rollback tertunda tidak membatalkan perubahan yang sudah disimpan.

Penguncian mencegah perubahan role berjalan berdasarkan hak admin lama jika admin tersebut didemosi bersamaan. Transaksi masih bisa gagal karena timeout/deadlock; kegagalan itu tidak dianggap sukses.

Hak admin dibaca dari database, sehingga token yang sama kehilangan hak admin pada request berikutnya setelah role menjadi `user`. Admin boleh menurunkan dirinya sendiri; implementasi belum melindungi admin terakhir.

## 16. Health check dan perintah CLI

### 16.1 Health dan readiness

Cuplikan route health:

```go
router.Get("/healthz", func(writer http.ResponseWriter, request *http.Request) {
    writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
})
```

Health menunjukkan proses HTTP dapat menjawab. Ia tidak mengakses repository atau database.

Readiness menggunakan fungsi ping yang sebelumnya diberikan oleh `main.go`:

```go
func (handlers *handler) ready(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
	defer cancel()
	if err := handlers.ping(ctx); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "not_ready", "Database unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
}
```

Jika ping gagal, hasilnya `503 not_ready`; jika berhasil, `200` dengan status `ready`. Ping tidak membuktikan kolom/tabel lengkap atau registrasi pasti berhasil.

### 16.2 CLI create-user

Sumber: [cmd/create-user/main.go](cmd/create-user/main.go).

Cuplikan bagian penyimpanan:

```go
user, err := auth.NewPostgresRepository(pool).Create(ctx, username, email, passwordHash, *roleFlag)
if err != nil {
    return err
}
fmt.Printf("User created successfully (id=%d, role=%s).\n", user.ID, user.Role)
```

Alur CLI berbeda dari endpoint publik:

```text
Terminal → baca flags → validasi username/email/role
         → baca password tersembunyi dan konfirmasi
         → HashPassword → buka pool → repository.Create → Neon
```

CLI tidak melewati router/handler/service registrasi. Karena itu ia melakukan validasinya sendiri, memakai helper password yang sama, dan dapat diberi `--role admin` oleh pengelola server.

```bash
go run ./cmd/create-user --username administrator --email admin@example.com --role admin
```

Perintah tersebut **benar-benar membuat akun** jika dijalankan dengan input valid. Untuk memakai API dari Thunder Client, CLI tidak diperlukan. CLI ini berguna untuk admin pertama karena registrasi publik selalu membuat role `user`.

## 17. Urutan membuat backend ini

Berikut urutan yang disarankan ketika membangun ulang backend serupa. Ini urutan belajar/implementasi, bukan klaim urutan historis penulisan setiap file.

| Urutan | Yang dibuat | Hasil yang harus dipahami |
| --- | --- | --- |
| 1 | Modul Go (`go.mod`) | Nama import dan dependensi yang dipakai |
| 2 | Skema tabel dan index | Kolom apa yang disimpan, ID, unique email/username, role |
| 3 | `.env.example`, `.env`, `config.go` | Sumber alamat database/port/secret dan validasinya |
| 4 | `database/postgres.go` | Pool bisa terhubung dan ping ke database tujuan |
| 5 | `auth/user.go` | Struct data, role, error aplikasi, kontrak repository |
| 6 | `auth/repository.go` | Operasi SQL dan pemetaan hasilnya |
| 7 | `auth/password.go`, `auth/token.go` | Pembuatan/verifikasi kredensial dan token |
| 8 | `auth/service.go` | Aturan bisnis di atas repository dan helper |
| 9 | Handler pada `httpapi/router.go` | Input HTTP → pemanggilan service → output HTTP |
| 10 | Route dan middleware | Method/path dapat diakses dengan batas request yang sesuai |
| 11 | `main.go` | Seluruh dependensi terhubung dan server berjalan |
| 12 | CLI administratif | Jalan membuat admin pertama melalui terminal |
| 13 | Pengujian dan dokumentasi | Perilaku sesuai kontrak dan mudah dicoba |

Pengujian sebaiknya dibuat sambil mengerjakan bagian terkait, bukan menunggu semua fitur selesai. Contohnya uji hash setelah helper password selesai, lalu uji service setelah aturan registrasi tersedia.

Urutan dependensi saat program disusun dan saat request berjalan berbeda:

```text
Membangun objek: pool → repository → service → handler/router → server
Request masuk:  server → router → handler → service → repository → database
Data kembali:   database → repository → service → handler → client
```

Jika menambah fitur baru, tentukan data dan izin terlebih dahulu. Tambahkan operasi repository bila perlu SQL baru, aturan service, handler, route, perakitan jika ada dependensi baru, lalu pengujian dan dokumentasi. Endpoint baru yang memakai service lama tidak selalu memerlukan pool atau service baru.

## 18. Cara membaca pengujian

Pengujian otomatis proyek ini memakai pengganti repository/service dan HTTP recorder. Ia tidak membuat akun pada database Neon aktif.

| File test | Yang diperiksa |
| --- | --- |
| `internal/auth/service_test.go` | Normalisasi, hash, login, akun nonaktif, register, duplikasi, hak admin dan demosi |
| `internal/auth/token_test.go` | Signature, algoritma, expiration, issuer/audience, subject JWT |
| `internal/httpapi/router_test.go` | JSON, status/header HTTP, larangan field role publik, rate limit, CORS |
| `internal/config/config_test.go` | Konfigurasi valid dan penolakan nilai yang tidak sesuai |

Cuplikan repository pengganti dari `service_test.go`:

```go
func (repository *stubRepository) Create(ctx context.Context, username, email, hash, role string) (User, error) {
    repository.createdRole, repository.createdHash = role, hash
    return User{ID: 43, Username: username, Email: email, Role: role, PasswordHash: hash, IsActive: true}, repository.err
}
```

Stub menyimpan nilai yang diterima agar test dapat memeriksa apakah service benar-benar mengirim hash dan role `user`. Semua method kontrak tetap perlu disediakan agar stub memenuhi interface `UserRepository`.

Cuplikan pemeriksaan peran dan hash dalam `TestRegisterAndRBAC`:

```go
if repository.createdRole != RoleUser || bcrypt.CompareHashAndPassword([]byte(repository.createdHash), []byte("valid-password-123")) != nil {
    t.Fatal("registration must persist user role and bcrypt hash")
}
```

Cuplikan alur test handler:

```go
request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(testCase.body))
request.Header.Set("Content-Type", testCase.contentType)
response := httptest.NewRecorder()
router.ServeHTTP(response, request)
```

`httptest.NewRequest` membuat request dalam memori. `NewRecorder` menampung response. `ServeHTTP` menjalankan router tanpa membuka port server. Test kemudian membandingkan `response.Code`, body, atau header dengan hasil yang diharapkan.

Jalankan dari folder proyek:

```bash
go test ./...
go test -race ./...
go vet ./...
go build -o bin/server .
```

`go test` menjalankan pengujian; `-race` membantu mendeteksi akses memori bersamaan yang bermasalah jika platform/toolchain mendukungnya; `go vet` memeriksa pola kode mencurigakan; `go build` membangun executable. Lolos test dengan stub belum membuktikan skema/database aktif benar, sehingga koneksi dan operasi SQL perlu diverifikasi secara terpisah ketika skema berubah.

## 19. Latihan dan cara mencari masalah

### 19.1 Urutan latihan membaca kode

1. Buka `User`, lalu cari semua pemakaian `UserRepository`.
2. Ikuti `Register` dari handler ke service dan repository. Temukan baris INSERT sebenarnya.
3. Periksa mengapa request register tidak dapat menyertakan role admin.
4. Ikuti `Login`, lalu cari dari mana `AccessToken` diperoleh.
5. Ikuti `CurrentUser` dan jelaskan mengapa token saja tidak cukup jika akun sudah nonaktif.
6. Ikuti `SetUserRole` dan bedakan ID pemanggil dengan ID pengguna tujuan.
7. Buka `main.go` dan tunjukkan tempat pool, repository, service, serta router dihubungkan.

Untuk latihan HTTP, siapkan satu akun uji milik Anda. Registrasi akan menulis ke Neon; penggunaan email sama dua kali memang diharapkan menghasilkan `409`.

### 19.2 Menentukan file yang perlu diperiksa

| Gejala | Bagian pertama yang diperiksa | Alasannya |
| --- | --- | --- |
| Akun tidak terlihat pada tabel yang dibuka | `DATABASE_URL`, restart backend, log nama database, branch/schema dashboard | Pool mungkin menunjuk database atau branch berbeda |
| `400 invalid_json` | Header/body Thunder Client dan `decodeJSON` | JSON atau field tidak sesuai |
| `400 invalid_registration` | Helper normalisasi dan `HashPassword` | Nilai input tidak memenuhi aturan |
| `401` saat login | `Login`, hash bcrypt, `is_active` | Email/password atau status akun tidak diterima |
| `401` saat `/me` | Header Bearer, `Verify`, akun database | Token/akun tidak valid |
| `403` saat mengubah role | `SetUserRole`, role pemanggil pada database | Identitas valid, tetapi hak admin tidak ada |
| `409 user_exists` | Index unik, data yang sudah ada, sequence jika benturan ID | INSERT ditolak unique constraint |
| `429` | Rate limiter login/register | Batas request per IP terlampaui |
| `500` | Log request, service/repository, kecocokan kolom SQL | Ada error internal, termasuk skema tidak cocok |
| `/readyz` gagal | Koneksi di `postgres.go` dan konfigurasi Neon | Ping database gagal |

Saat membandingkan tampilan dashboard dengan backend, cocokan **endpoint/branch, nama database, schema, dan tabel**. Koneksi DBCode yang dipilih tidak otomatis digunakan aplikasi.

Data akun disimpan di `Loan_Banking.public.users` sesuai koneksi, objek Go hanya membawa salinan hasil selama pemrosesan, dan response JSON hanya menampilkan field yang diizinkan. Mengubah file model atau file SQL tidak otomatis mengubah database. Mengubah kode service tidak langsung memengaruhi proses lama sebelum aplikasi dibangun/dijalankan ulang.

Untuk mencoba endpoint, lanjutkan ke [API.md](API.md). Untuk konfigurasi awal, lihat [README.md](README.md) atau [panduan Windows](README-WINDOWS.md).
