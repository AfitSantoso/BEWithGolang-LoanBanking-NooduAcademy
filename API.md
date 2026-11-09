# Dokumentasi API Loan Banking — Login Service

Dokumentasi ini mengikuti endpoint yang terdaftar di `internal/httpapi/router.go` dan aturan autentikasi di `internal/auth`. Contoh akun dan token di bawah merupakan ilustrasi; gunakan akun yang sudah dibuat pada database Anda.

## Base URL dan format

Database tujuan registrasi adalah **`Loan_Banking` → `public.users`** pada Neon. Setelah `DATABASE_URL` diubah, restart backend agar menggunakan konfigurasi baru.

Base URL lokal default: `http://127.0.0.1:8080`. Sesuaikan host dan port jika konfigurasi `HTTP_ADDRESS` diubah.

| Method | Endpoint | Autentikasi | Fungsi |
| --- | --- | --- | --- |
| `POST` | `/api/v1/auth/register` | Tidak | Registrasi akun dengan role `user` |
| `PATCH` | `/api/v1/admin/users/{userID}/role` | Bearer JWT admin | Mengubah role pengguna |
| `POST` | `/api/v1/auth/login` | Tidak | Login menggunakan email dan password |
| `GET` | `/api/v1/auth/me` | Bearer JWT | Mengambil profil akun aktif |
| `GET` | `/healthz` | Tidak | Memeriksa proses HTTP |
| `GET` | `/readyz` | Tidak | Memeriksa koneksi database |

Endpoint perubahan role menggunakan parameter path `userID` (integer positif). Endpoint lainnya tidak menggunakan parameter path; seluruh endpoint tidak memerlukan parameter query. Request `GET` tidak memerlukan body; jangan mengirim `{}` sebagai syarat request.

Respons JSON dari handler menggunakan `Content-Type: application/json; charset=utf-8` dan `Cache-Control: no-store`. Respons autentikasi dan perubahan role yang berhasil dibungkus dalam `data`, sedangkan health/readiness menggunakan `status`. Respons error JSON menggunakan bentuk:

```json
{
  "error": {
    "code": "unauthorized",
    "message": "Invalid credentials or access token"
  }
}
```

Gunakan status HTTP dan `error.code` untuk menangani error pada client. Pesan error mengikuti implementasi dalam bahasa Inggris.

## 1. Login

`POST /api/v1/auth/login`

Memverifikasi kredensial akun aktif dan mengembalikan access token JWT.

### Header request

| Header | Wajib | Nilai |
| --- | --- | --- |
| `Content-Type` | Ya | `application/json` (boleh dengan `charset=utf-8`) |

### Request JSON

```json
{
  "email": "alice@example.com",
  "password": "REPLACE_WITH_YOUR_PASSWORD"
}
```

| Field | Tipe | Wajib | Aturan |
| --- | --- | --- | --- |
| `email` | string | Ya | Format email valid; spasi di awal/akhir dihapus dan huruf diubah menjadi lowercase sebelum pencarian. Panjang setelah normalisasi maksimal 100 byte. |
| `password` | string | Ya | Panjang 1–72 byte; harus cocok dengan password akun. Spasi dan kapitalisasi password dipertahankan. |

Body dibatasi 4096 byte dan harus berisi satu objek JSON. Field tambahan, tipe field yang salah, JSON rusak, atau JSON tambahan setelah objek pertama ditolak. Field wajib yang hilang atau bernilai `null` menghasilkan validasi input gagal.

Batas minimal 12 karakter berlaku saat **registrasi atau membuat akun melalui CLI**, sedangkan endpoint login memvalidasi panjang password 1–72 byte. Panjang dalam byte dapat berbeda dari jumlah karakter untuk teks Unicode.

### Response berhasil — `200 OK`

```json
{
  "data": {
    "access_token": "REPLACE_WITH_ACCESS_TOKEN",
    "token_type": "Bearer",
    "expires_in": 900,
    "user": {
      "id": 1,
      "username": "alice",
      "email": "alice@example.com",
      "role": "user"
    }
  }
}
```

| Field | Tipe | Keterangan |
| --- | --- | --- |
| `data.access_token` | string | JWT untuk header `Authorization` pada request profil |
| `data.token_type` | string | Selalu `Bearer` |
| `data.expires_in` | integer | Durasi token dalam detik; default `900` (15 menit), mengikuti `JWT_TTL` |
| `data.user.id` | integer | ID pengguna |
| `data.user.username` | string | Username pengguna |
| `data.user.email` | string | Email pengguna |
| `data.user.role` | string | Role terkini: `user` atau `admin` |

`password`, `password_hash`, dan `is_active` tidak dikirim dalam respons pengguna.

### Response gagal

| HTTP | `error.code` | Penyebab |
| --- | --- | --- |
| `400` | `invalid_json` | Body kosong, JSON rusak, tipe field salah, field tambahan, atau lebih dari satu nilai JSON |
| `400` | `invalid_input` | Email tidak valid/kosong atau password di luar batas 1–72 byte |
| `401` | `unauthorized` | Kredensial salah, akun tidak ditemukan, akun tidak aktif, atau hash password tidak valid |
| `413` | `body_too_large` | Body yang dibaca melebihi 4096 byte |
| `415` | `unsupported_media_type` | `Content-Type` hilang atau bukan `application/json` |
| `429` | `rate_limit_exceeded` | Batas percobaan login terlampaui |
| `500` | `internal_error` | Kegagalan internal, termasuk kegagalan akses database |

Contoh input tidak valid — `400 Bad Request`:

```json
{
  "error": {
    "code": "invalid_input",
    "message": "Provide a valid email and a password of 1 to 72 bytes"
  }
}
```

Contoh kredensial salah — `401 Unauthorized`:

```json
{
  "error": {
    "code": "unauthorized",
    "message": "Invalid credentials or access token"
  }
}
```

Login dibatasi **5 request per menit per IP**, termasuk request login yang gagal. Penghitung berada dalam memori masing-masing proses. Request berikutnya yang dibatasi menerima `429` beserta header `Retry-After`; tunggu sesuai nilai header sebelum mencoba kembali. IP mengikuti koneksi langsung; header `X-Forwarded-For` dari client tidak digunakan.

### Contoh cURL

```bash
curl -i -X POST 'http://127.0.0.1:8080/api/v1/auth/login' \
  -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"REPLACE_WITH_YOUR_PASSWORD"}'
```

## 2. Profil pengguna

`GET /api/v1/auth/me`

Memverifikasi JWT lalu membaca profil terbaru dari database. Akun yang sudah dihapus atau dinonaktifkan ditolak meskipun token belum kedaluwarsa.

### Header request

| Header | Wajib | Nilai |
| --- | --- | --- |
| `Authorization` | Ya | `Bearer REPLACE_WITH_ACCESS_TOKEN` |

Tidak ada request JSON/body. Token diambil dari `data.access_token` pada respons login; panjang token pada header maksimal 4096 byte.

### Response berhasil — `200 OK`

```json
{
  "data": {
    "id": 1,
    "username": "alice",
    "email": "alice@example.com",
    "role": "user"
  }
}
```

### Response gagal

`401 Unauthorized` jika header hilang/salah format, token tidak valid/kedaluwarsa, atau akun sudah tidak tersedia/tidak aktif. Respons menyertakan header `WWW-Authenticate: Bearer`:

```json
{
  "error": {
    "code": "unauthorized",
    "message": "Invalid credentials or access token"
  }
}
```

`500 Internal Server Error` jika terjadi kegagalan internal atau akses database:

```json
{
  "error": {
    "code": "internal_error",
    "message": "An internal error occurred"
  }
}
```

### Contoh cURL

```bash
curl -i 'http://127.0.0.1:8080/api/v1/auth/me' \
  -H 'Authorization: Bearer REPLACE_WITH_ACCESS_TOKEN'
```

## 3. Health check

`GET /healthz`

Tidak memerlukan autentikasi, header khusus, atau request JSON/body. Memeriksa apakah proses HTTP merespons, tanpa memeriksa koneksi database.

### Response berhasil — `200 OK`

```json
{
  "status": "ok"
}
```

### Contoh cURL

```bash
curl -i 'http://127.0.0.1:8080/healthz'
```

## 4. Readiness database

`GET /readyz`

Tidak memerlukan autentikasi, header khusus, atau request JSON/body. Menjalankan ping ke database dengan timeout 3 detik. Endpoint ini tidak memeriksa kelengkapan tabel, migrasi, atau keberadaan akun.

### Response berhasil — `200 OK`

```json
{
  "status": "ready"
}
```

### Response gagal — `503 Service Unavailable`

```json
{
  "error": {
    "code": "not_ready",
    "message": "Database unavailable"
  }
}
```

### Contoh cURL

```bash
curl -i 'http://127.0.0.1:8080/readyz'
```

## Referensi error JSON

Untuk setiap baris berikut, response body mengikuti bentuk `{"error":{"code":"...","message":"..."}}`. Teks `message` ditulis persis sesuai handler.

| HTTP | `error.code` | `error.message` |
| --- | --- | --- |
| `400` | `invalid_registration` | `Provide a username of 1 to 50 characters, a valid email, and a password of at least 12 characters and at most 72 bytes; password must not contain only whitespace` |
| `400` | `invalid_role` | `Provide a positive user ID and role user or admin` |
| `403` | `forbidden` | `Admin role required` |
| `404` | `user_not_found` | `User not found` |
| `409` | `user_exists` | `Email or username already exists` |
| `400` | `invalid_json` | `Provide exactly one JSON object with the expected fields` |
| `400` | `invalid_input` | `Provide a valid email and a password of 1 to 72 bytes` |
| `401` | `unauthorized` | `Invalid credentials or access token` |
| `403` | `origin_not_allowed` | `Origin is not allowed` |
| `404` | `not_found` | `Endpoint not found` |
| `405` | `method_not_allowed` | `Method not allowed` |
| `413` | `body_too_large` | `Request body must not exceed 4096 bytes` |
| `415` | `unsupported_media_type` | `Content-Type must be application/json` |
| `429` | `rate_limit_exceeded` | `Too many login attempts; try again later` |
| `500` | `internal_error` | `An internal error occurred` |
| `503` | `not_ready` | `Database unavailable` |

`404` berlaku untuk path yang tidak terdaftar. `405` berlaku untuk method yang tidak didukung pada path terdaftar, misalnya `PUT /api/v1/auth/login`.

Tabel ini mendokumentasikan error JSON yang ditulis handler aplikasi. Penolakan oleh middleware pembatas request bersamaan (32 request aktif) atau pemulihan panic dapat menghasilkan respons non-JSON; client perlu memeriksa `Content-Type` sebelum mengurai body.

## CORS dan preflight browser

Frontend pada origin berbeda harus menggunakan origin yang sama persis dengan konfigurasi `CORS_ORIGIN`, misalnya `http://localhost:3000`.

Browser dapat mengirim request berikut sebelum login:

```http
OPTIONS /api/v1/auth/login HTTP/1.1
Origin: http://localhost:3000
Access-Control-Request-Method: POST
Access-Control-Request-Headers: Content-Type
```

Preflight dengan origin yang diizinkan menghasilkan `204 No Content` tanpa body JSON dan header:

```http
Access-Control-Allow-Origin: http://localhost:3000
Access-Control-Allow-Headers: Authorization, Content-Type
Access-Control-Allow-Methods: GET, POST, PATCH, OPTIONS
```

Preflight dengan origin yang tidak diizinkan menghasilkan `403` dengan kode `origin_not_allowed`. Pada request biasa, origin yang tidak cocok tidak mendapatkan header izin CORS, sehingga browser memblokir akses respons. CORS bukan autentikasi endpoint.

## Alur penggunaan

1. Gunakan `DATABASE_URL` Neon proyek; skema registrasi/RBAC sudah diterapkan langsung pada database `Loan_Banking`, 7 November 2026, tanpa migrasi manual. Buat akun melalui endpoint registrasi atau CLI `go run ./cmd/create-user --username alice --email alice@example.com`. Untuk admin awal, gunakan CLI dengan `--role admin`.
2. Kirim email dan password ke endpoint login.
3. Gunakan `data.access_token` sebagai Bearer token untuk endpoint profil.
4. Jika token kedaluwarsa, login kembali. Default masa berlaku 15 menit; `JWT_TTL` dapat dikonfigurasi antara 1 detik dan 1 jam.
5. Saat logout di frontend, hapus token dari penyimpanan client. Service belum menyediakan endpoint logout, refresh token, reset password, atau pencabutan token individual.

Panduan konfigurasi dan menjalankan server tersedia di [README.md](README.md), dengan contoh PowerShell di [README-WINDOWS.md](README-WINDOWS.md).


## 5. Registrasi pengguna

`POST /api/v1/auth/register`

Endpoint publik ini membuat akun aktif dengan role **`user`**. Header wajib `Content-Type: application/json`; body maksimal 4096 byte, satu objek, tanpa field tambahan. Batas registrasi terpisah dari login: 5 request per menit per IP. Respons `429` menyertakan `Retry-After` dan pesan `Too many registration attempts; try again later`.

### Request JSON

```json
{
  "username": "afit",
  "email": "afitdesantos@gmail.com",
  "password": "afit123"
}
```

| Field | Tipe | Wajib | Aturan |
| --- | --- | --- | --- |
| `username` | string | Ya | Setelah trim: 1–50 karakter, tanpa karakter kontrol; unik tanpa membedakan kapitalisasi dan spasi awal/akhir |
| `email` | string | Ya | Email valid maksimal 100 byte setelah trim/lowercase; unik |
| `password` | string | Ya | Minimal 12 karakter, maksimal 72 byte, bukan hanya whitespace; disimpan sebagai hash bcrypt cost 12 |

Field `role`, `is_active`, `id`, dan `password_hash` tidak diterima. Request dengan `"role":"admin"` ditolak `400 invalid_json`, bukan membuat admin.

### Response berhasil — `201 Created`

```json
{
  "data": {
    "id": 1,
    "username": "alice",
    "email": "alice@example.com",
    "role": "user"
  }
}
```

Registrasi tidak menerbitkan token. Login setelah berhasil mendaftar.

### Response gagal

| HTTP | `error.code` | Penyebab |
| --- | --- | --- |
| `400` | `invalid_registration` | Username/email/password tidak memenuhi aturan |
| `400` | `invalid_json` | JSON tidak sesuai, termasuk field tambahan `role` |
| `409` | `user_exists` | Email atau username sudah digunakan |
| `413` | `body_too_large` | Body melebihi batas |
| `415` | `unsupported_media_type` | Content-Type bukan JSON |
| `429` | `rate_limit_exceeded` | Terlalu banyak percobaan registrasi |
| `500` | `internal_error` | Kesalahan internal/database |

Contoh duplikasi — `409 Conflict`:

```json
{
  "error": {
    "code": "user_exists",
    "message": "Email or username already exists"
  }
}
```

```bash
curl -i -X POST 'http://127.0.0.1:8080/api/v1/auth/register' \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","email":"alice@example.com","password":"valid-password-123"}'
```

## 6. Mengatur role pengguna (admin)

`PATCH /api/v1/admin/users/{userID}/role`

Memerlukan `Authorization: Bearer REPLACE_WITH_ADMIN_ACCESS_TOKEN` dan `Content-Type: application/json`. Parameter `userID` harus integer positif. Body dibatasi 4096 byte dan menolak field tambahan.

### Request JSON

```json
{
  "role": "admin"
}
```

`role` wajib string dengan nilai persis `user` atau `admin` (case-sensitive). Gunakan `user` untuk menurunkan hak admin.

### Response berhasil — `200 OK`

```json
{
  "data": {
    "id": 2,
    "username": "bob",
    "email": "bob@example.com",
    "role": "admin"
  }
}
```

### Response gagal

| HTTP | `error.code` | Penyebab |
| --- | --- | --- |
| `400` | `invalid_role` | ID pengguna atau role tidak valid |
| `400` | `invalid_json` | JSON tidak sesuai |
| `401` | `unauthorized` | Token hilang/tidak valid/kedaluwarsa atau akun pemanggil tidak aktif/dihapus |
| `403` | `forbidden` | Akun pemanggil bukan admin |
| `404` | `user_not_found` | ID pengguna tujuan tidak ditemukan |
| `413` | `body_too_large` | Body melebihi batas |
| `415` | `unsupported_media_type` | Content-Type bukan JSON |
| `500` | `internal_error` | Kesalahan internal/database |

Contoh ditolak untuk role user — `403 Forbidden`:

```json
{
  "error": {
    "code": "forbidden",
    "message": "Admin role required"
  }
}
```

```bash
curl -i -X PATCH 'http://127.0.0.1:8080/api/v1/admin/users/2/role' \
  -H 'Authorization: Bearer REPLACE_WITH_ADMIN_ACCESS_TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{"role":"admin"}'
```

### Matriks RBAC

| Operasi | Publik | User aktif | Admin aktif |
| --- | --- | --- | --- |
| Registrasi, login, health/readiness | Ya | Ya | Ya |
| Profil sendiri (`/me`) | Tidak | Ya | Ya |
| Mengubah role pengguna | Tidak | Tidak | Ya |

Role dibaca dari database saat otorisasi, bukan dari role yang dikirim client atau claim role dalam JWT. Perubahan role berlaku pada request berikutnya tanpa login ulang. JWT tetap menggunakan subject ID pengguna. Saat perubahan role, hak admin diperiksa ulang dan baris admin dikunci dalam transaksi database untuk mencegah pembaruan dengan otorisasi lama saat demosi bersamaan.

Admin dapat mengubah role sendiri, termasuk menurunkan diri menjadi user. Tidak ada perlindungan admin terakhir; pastikan ada admin lain sebelum demosi diri. Admin pertama dibuat melalui CLI pengelola server:

```bash
go run ./cmd/create-user --username administrator --email admin@example.com --role admin
```

CLI meminta password tersembunyi dan konfirmasi. Role default CLI adalah `user`. Skema registrasi/RBAC sudah diterapkan langsung ke Neon pada database `Loan_Banking`, 7 November 2026. Username akun lama dipertahankan dan role awalnya `user`; tidak ada akun yang diangkat otomatis menjadi admin. Repository memakai kolom `password_hash` yang sudah ada di Neon. File `migrations/002_user_roles.sql` disimpan sebagai catatan perubahan, bukan langkah manual yang perlu Anda jalankan.
