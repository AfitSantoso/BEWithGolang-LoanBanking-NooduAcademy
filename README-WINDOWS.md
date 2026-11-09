# Menjalankan Login Service di Windows - Updatex

Panduan ini menggunakan **Windows PowerShell 5.1 atau PowerShell 7**. Semua perintah dijalankan dari folder `BE-Golang`, kecuali disebutkan lain. Penjelasan struktur folder dan arsitektur tersedia di [README.md](README.md#struktur-folder).

## 1. Install Go

1. Unduh installer Go untuk Windows (`.msi`) dari [situs resmi Go](https://go.dev/dl/), sesuai arsitektur komputer Anda.
2. Install **Go 1.26 atau lebih baru**, mengikuti versi minimum di `go.mod`.
3. Buka ulang PowerShell dan VS Code setelah instalasi, kemudian periksa:

   ```powershell
   go version
   ```

Jika `go` belum dikenali, periksa PATH instalasi Go, biasanya `C:\Program Files\Go\bin`.

## 2. Buka folder proyek

Ganti path berikut dengan lokasi proyek Anda di Windows:

```powershell
Set-Location 'C:\Projects\BE-Golang'
Get-ChildItem
```

Pastikan folder tersebut berisi `main.go`, `go.mod`, dan `.env.example`. Alternatifnya, buka folder proyek di VS Code, lalu buka terminal dengan profil **PowerShell**.

## 3. Siapkan file .env

Untuk konfigurasi pertama kali:

```powershell
Copy-Item .env.example .env
notepad .env
```

Jika `.env` sudah ada dan sudah diisi, langsung edit file tersebut tanpa menyalin ulang template.

Buat secret JWT acak menggunakan fasilitas kriptografi bawaan Windows/.NET; OpenSSL tidak diperlukan:

```powershell
$secretBytes = New-Object byte[] 32
$randomGenerator = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try {
    $randomGenerator.GetBytes($secretBytes)
} finally {
    $randomGenerator.Dispose()
}
$jwtSecret = [System.BitConverter]::ToString($secretBytes).Replace('-', '').ToLowerInvariant()
$jwtSecret
```

Salin hasil 64 karakter tersebut ke `JWT_SECRET` pada `.env`. Isi konfigurasi berikut:

| Variabel | Nilai |
| --- | --- |
| `DATABASE_URL` | Connection string PostgreSQL dari dashboard Neon, menggunakan `sslmode=verify-full` |
| `JWT_SECRET` | Secret acak yang baru dibuat |
| `HTTP_ADDRESS` | `127.0.0.1:8080` untuk pengembangan lokal |
| `CORS_ORIGIN` | Origin frontend, misalnya `http://localhost:3000` |
| `JWT_TTL` | `15m` |

Nilai `JWT_ISSUER` dan `JWT_AUDIENCE` dapat mengikuti template. Simpan `.env` sebagai **UTF-8 tanpa BOM**, dengan nama persis `.env`, bukan `.env.txt`. Di VS Code, gunakan menu encoding pada status bar jika perlu.

Password koneksi DBCode dari Mac tidak otomatis tersedia pada Windows. Ambil connection string dari Neon; aplikasi tidak membaca Secret Storage DBCode. URL-encode karakter khusus password jika merangkai URL secara manual. `.env` sudah diabaikan Git. Environment yang sudah diekspor mengalahkan nilai dari `.env`.

## 4. Gunakan database Loan_Banking di Neon

Pastikan `DATABASE_URL` pada `.env` memilih database **`Loan_Banking`**, bukan `neondb`. Skema registrasi/RBAC sudah diterapkan langsung ke `Loan_Banking` pada 7 November 2026; Anda tidak perlu menjalankan migrasi manual untuk database tersebut. Tabel tujuan adalah `public.users`, dengan kolom `password_hash`, `username`, dan `role`.

Pilih database serta branch yang sama pada dashboard Neon atau DBCode untuk melihat akun yang baru didaftarkan. Setelah mengubah `.env`, hentikan backend dengan Ctrl+C lalu jalankan kembali `go run .`. Log `database connected` harus menampilkan `Loan_Banking`. Akun yang dahulu dibuat di `neondb` tidak dipindahkan atau dihapus otomatis.

Tidak perlu memasang PostgreSQL lokal. File SQL di `migrations/` disimpan sebagai catatan perubahan dan referensi database baru.

## 5. Unduh dependensi dan siapkan akun

```powershell
go mod download
go mod verify
go run ./cmd/create-user --username alice --email alice@example.com
```

Jalankan perintah pembuatan akun dalam terminal interaktif. Masukkan password lalu konfirmasinya; karakter password tidak ditampilkan. Password akun baru minimal 12 karakter dan maksimal 72 byte. Database menyimpan hash bcrypt.

Jika akun sudah dibuat dari Mac pada database Neon yang sama, gunakan akun tersebut dan lewati perintah `create-user`.

## 6. Jalankan backend

```powershell
go run .
```

Biarkan terminal ini berjalan. Buka terminal PowerShell kedua untuk mencoba API. Gunakan **Ctrl+C** untuk menghentikan backend.

## 7. Coba health check, login, dan profil

Di terminal kedua:

```powershell
$baseUrl = 'http://127.0.0.1:8080'
Invoke-RestMethod -Method Get -Uri "$baseUrl/healthz"
Invoke-RestMethod -Method Get -Uri "$baseUrl/readyz"
```

Hasil yang diharapkan adalah `ok` dan `ready`. Selanjutnya, masukkan password akun melalui prompt kredensial agar tidak ditulis langsung dalam riwayat perintah:

```powershell
$credentials = Get-Credential -UserName 'alice@example.com' -Message 'Masukkan password akun login service'
$loginBody = @{
    email = $credentials.UserName
    password = $credentials.GetNetworkCredential().Password
} | ConvertTo-Json

$loginParameters = @{
    Method = 'Post'
    Uri = "$baseUrl/api/v1/auth/login"
    ContentType = 'application/json; charset=utf-8'
    Body = [System.Text.Encoding]::UTF8.GetBytes($loginBody)
}

try {
    $loginResponse = Invoke-RestMethod @loginParameters
} finally {
    Remove-Variable credentials, loginBody, loginParameters -ErrorAction SilentlyContinue
}

$loginResponse.data.user | Format-List
```

Jika berhasil, hasil menampilkan `id`, `username`, dan `email`. Gunakan token dari hasil login untuk mengambil profil:

```powershell
$profileParameters = @{
    Method = 'Get'
    Uri = "$baseUrl/api/v1/auth/me"
    Headers = @{ Authorization = "Bearer $($loginResponse.data.access_token)" }
}

$profileResponse = Invoke-RestMethod @profileParameters
$profileResponse.data | Format-List
```

Contoh ini memakai `Invoke-RestMethod` agar penulisan JSON dan header sesuai PowerShell. Token berlaku default 15 menit; login ulang jika kedaluwarsa.

## 8. Jalankan pemeriksaan dan buat executable

Dari folder proyek:

```powershell
go test ./...
go vet ./...
New-Item -ItemType Directory -Path .\bin -Force | Out-Null
go build -o .\bin\server.exe .
.\bin\server.exe
```

Hentikan `go run .` terlebih dahulu agar port tidak digunakan dua proses. Jalankan executable dari folder proyek agar `.env` ditemukan. Pada deployment, konfigurasi juga dapat disediakan melalui environment.

Untuk pengujian race detector pada Windows/amd64:

```powershell
go test -race ./...
```

Race detector memerlukan CGO dan compiler C yang kompatibel, dengan runtime mingw-w64 versi 8 atau lebih baru. Jika toolchain tersebut belum tersedia, `go test ./...` tetap dapat digunakan untuk pengujian biasa; pemeriksaan race dapat dijalankan pada Mac atau CI yang mendukungnya. Lihat [persyaratan resmi race detector Go](https://go.dev/doc/articles/race_detector#Requirements).

## Kendala umum

| Kendala | Langkah pengecekan |
| --- | --- |
| `go` tidak dikenali | Buka ulang terminal/VS Code dan periksa PATH Go |
| `.env` tidak terbaca | Jalankan dari folder proyek; periksa nama file dan encoding tanpa BOM |
| `JWT_SECRET` tidak valid | Isi secret acak 64 karakter dari langkah 3 |
| Tidak terhubung ke database | Periksa connection string, password, akses jaringan, dan `sslmode=verify-full` |
| Tabel `users` tidak ditemukan | Jalankan migrasi pada database yang sama dengan `DATABASE_URL` |
| `401 Unauthorized` | Periksa email/password, `is_active`, hash bcrypt, atau masa berlaku token |
| `429 Too Many Requests` | Tunggu sekitar satu menit sebelum mencoba login lagi |
| Port `8080` sudah digunakan | Hentikan proses sebelumnya atau ubah `HTTP_ADDRESS` dan `$baseUrl` |

Referensi perintah HTTP: [Microsoft — Invoke-RestMethod](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.utility/invoke-restmethod?view=powershell-5.1).
