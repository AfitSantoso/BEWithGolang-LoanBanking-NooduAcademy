# Login Google OAuth

Login Google memakai tabel **Neon `Loan_Banking.public.users`** yang sama dengan registrasi email/password. Setelah Google diverifikasi, backend mengembalikan JWT aplikasi dengan bentuk yang sama seperti login biasa. JWT tersebut dapat dipakai untuk profil pelanggan, upload gambar, dan endpoint lain yang sudah ada.

## Endpoint

| Method | Endpoint | Hasil |
| --- | --- | --- |
| `GET` | `/auth/google` | `302` menuju halaman login/pemilihan akun Google |
| `GET` | `/auth/google/callback` | Menerima callback, memverifikasi identitas, menyimpan/menautkan akun, lalu mengembalikan JWT JSON |
| `PUT` | `/api/v1/auth/password` | Membuat password lokal pertama atau mengubah password lokal setelah login |

Endpoint Google berada langsung pada `/auth`, sesuai redirect URL konfigurasi, bukan `/api/v1/auth/google`.

## Konfigurasi

Backend membaca tiga variabel yang sudah ada di `.env` Anda:

```dotenv
GOOGLE_CLIENT_ID=YOUR_CLIENT_ID.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=YOUR_CLIENT_SECRET
GOOGLE_REDIRECT_URL=http://127.0.0.1:8080/auth/google/callback
```

Nilai ID/secret di atas hanya placeholder. Kredensial nyata tidak ditulis ke source atau dokumentasi. Ketiganya harus diisi bersama. Jika semuanya kosong, endpoint Google menjawab `503 google_oauth_unavailable`; endpoint lain tetap berjalan.

Di Google Cloud Console, OAuth client harus bertipe **Web application** dan **Authorized redirect URI** harus sama persis dengan `GOOGLE_REDIRECT_URL`. `localhost` dan `127.0.0.1` adalah host berbeda. Gunakan host yang sama saat membuka awal login dan menerima callback supaya cookie browser cocok. Jika consent screen masih dalam mode pengujian, akun yang mencoba harus diizinkan sebagai test user. Ikuti [panduan resmi OpenID Connect Google](https://developers.google.com/identity/openid-connect/openid-connect).

Konfigurasi produksi wajib memakai HTTPS; HTTP hanya diizinkan untuk loopback (`127.0.0.1`, `localhost`, `::1`). URL callback harus memakai path `/auth/google/callback`, tanpa query/fragment/kredensial URL.

Restart backend setelah konfigurasi berubah:

```bash
go run .
```

## Mencoba dari browser

Buka:

```text
http://127.0.0.1:8080/auth/google
```

Backend membuat state sekali pakai dan cookie browser, kemudian mengarahkan pengguna ke Google. Google mengembalikan browser ke callback dengan parameter `code` dan `state`. Parameter tersebut dibuat oleh proses OAuth; jangan mengisi kode Google secara manual atau memanggil callback langsung tanpa memulai login.

Setelah sukses, callback mengembalikan `200 OK`:

```json
{
  "data": {
    "access_token": "REPLACE_WITH_APPLICATION_JWT",
    "token_type": "Bearer",
    "expires_in": 900,
    "user": {
      "id": 10,
      "username": "google_0123456789abcdef0123456789abcdef",
      "email": "alice@gmail.com",
      "role": "user"
    }
  }
}
```

ID, username dan token pada contoh merupakan ilustrasi. Username akun baru dibuat dengan prefix `google_` dan nilai acak agar tidak bertabrakan dengan username lain. Backend tidak mengambil password Google dan tidak mengembalikan access token/ID token Google kepada frontend.

Callback saat ini adalah **endpoint JSON backend**, bukan halaman yang otomatis kembali ke dashboard frontend. Tombol di frontend dapat membuka URL login; setelah alur browser sukses, callback menampilkan JSON tersebut. Mekanisme penyerahan JWT dari halaman callback ke UI frontend (misalnya alur popup khusus) belum disediakan. JWT tidak ditempatkan pada query redirect URL.

Untuk pengujian backend, token hasil callback dapat digunakan di Thunder Client:

```http
GET /api/v1/auth/me
Authorization: Bearer REPLACE_WITH_APPLICATION_JWT
```

Jangan hanya memakai `fetch()` yang mengikuti redirect lintas-origin menuju halaman Google. Awali login dengan navigasi browser, misalnya:

```javascript
window.location.assign("http://127.0.0.1:8080/auth/google");
```

## Akun dan email tidak ganda

| Keadaan | Perilaku |
| --- | --- |
| Google pertama kali, email belum ada | Membuat satu baris `users`, role `user`, aktif, `password_hash = NULL` |
| Google berikutnya dengan subject sama | Menggunakan kembali user ID yang sama |
| Registrasi biasa dengan email akun Google | `409 user_exists`; tidak menambahkan akun baru |
| Email/password lama kemudian login Google | Menautkan ke ID yang sama setelah aturan kepemilikan email dipenuhi; password dan role dipertahankan |
| Akun dinonaktifkan | Login Google ditolak; akun tidak diaktifkan ulang otomatis |
| Identitas Google sudah terhubung ke akun lain | `409 google_account_conflict`; akun tidak dipindahkan |

Email dinormalisasi trim/lowercase dan tetap dilindungi index unik `users_email_normalized_uidx`. Penyimpanan Google menggunakan INSERT dengan penanganan konflik dan transaksi penautan; tidak menghapus atau mengganti akun lama. Relasi `customers.user_id` serta role admin yang sudah ada tetap memakai user ID sebelumnya.

Identitas utama Google disimpan pada **`users.google_subject`**, berdasarkan claim `sub` yang unik. Login berulang mencari subject terlebih dahulu sehingga tidak membuat akun baru hanya karena email Google berubah. Email lokal pada akun yang sudah terhubung tidak diganti otomatis. [Verifikasi identitas Google](https://developers.google.com/identity/gsi/web/guides/verify-google-id-token).

## Penautan akun lama

Penautan otomatis berbasis email hanya dilakukan jika email Google terverifikasi dan Google bersifat otoritatif: Gmail, atau Workspace dengan claim `hd` yang cocok dengan domain email. Untuk email pihak ketiga pada akun Google, kecocokan email saja tidak cukup membuktikan akses ke akun lokal lama; backend mengembalikan `409 google_link_required`. Aturan ini mengikuti [penjelasan kepemilikan email oleh Google](https://developers.google.com/identity/gsi/web/guides/verify-google-id-token).

Untuk menautkan secara eksplisit:

1. Login ke akun lama menggunakan endpoint email/password yang sudah ada.
2. Dalam browser frontend, minta URL OAuth menggunakan JWT akun lama:

   ```javascript
   const response = await fetch(
     "http://127.0.0.1:8080/auth/google?format=json",
     {
       credentials: "include",
       headers: { Authorization: `Bearer ${accessToken}` },
     }
   );
   const result = await response.json();
   if (!response.ok) throw new Error(result.error?.message || "Gagal memulai Google login");
   window.location.assign(result.data.authorization_url);
   ```

3. Pilih akun Google dengan email yang sama persis setelah normalisasi.
4. Callback menautkan subject ke akun yang sudah diautentikasi dan mengembalikan JWT untuk user ID yang sama.

Contoh response `GET /auth/google?format=json`:

```json
{
  "data": {
    "authorization_url": "https://accounts.google.com/o/oauth2/v2/auth?client_id=EXAMPLE&response_type=code&state=EXAMPLE"
  }
}
```

URL tersebut ilustrasi singkat; response nyata menyertakan nonce, scope, redirect URI dan PKCE. Opsi `format=json` juga dapat dipakai tanpa JWT untuk memulai login biasa. Jika header Authorization disertakan, token wajib valid dan akun harus aktif; backend tidak diam-diam mengabaikan token yang salah.

Frontend yang melakukan fetch harus memakai origin yang sesuai `CORS_ORIGIN` dan `credentials: "include"`. Gunakan host/situs yang konsisten untuk API dan UI agar cookie SameSite tidak diblokir browser. Pada pengembangan, misalnya gunakan `127.0.0.1` baik untuk UI port 3000 maupun API port 8080. Jangan memulai via Thunder Client lalu memakai browser yang berbeda untuk callback karena cookie sesi OAuth-nya tidak sama.

## Login email/password untuk akun yang dibuat Google

Google tidak memberikan password akun Google kepada aplikasi. Karena itu akun Google baru **belum dapat login password lokal** sampai pemilik menetapkannya. Mencoba registrasi dengan email yang sama tetap menghasilkan `409`, bukan membuat password/akun kedua.

Setelah login Google, panggil:

```http
PUT /api/v1/auth/password
Authorization: Bearer REPLACE_WITH_APPLICATION_JWT
Content-Type: application/json
```

Untuk password lokal pertama:

```json
{
  "new_password": "REPLACE_WITH_A_STRONG_LOCAL_PASSWORD"
}
```

Jika akun sudah memiliki password lokal, perubahan memerlukan password saat ini juga:

```json
{
  "current_password": "REPLACE_WITH_CURRENT_PASSWORD",
  "new_password": "REPLACE_WITH_NEW_PASSWORD"
}
```

Password baru minimal 12 karakter, maksimal 72 byte, bukan hanya whitespace, dan disimpan sebagai bcrypt cost 12. Body maksimal 4096 byte dan tidak menerima field tambahan. Pemilik akun berasal dari Bearer JWT, bukan ID pada body.

Response berhasil `200 OK`:

```json
{
  "data": {
    "message": "Password updated"
  }
}
```

Sesudah itu, `POST /api/v1/auth/login` dengan email dan password lokal akan masuk ke **akun yang sama**; Sign in with Google tetap berfungsi. Password Google asli tidak dipakai untuk login lokal. Tidak ada password default. Penetapan password bukan reset password anonim; JWT akun tetap wajib.

Perubahan password memakai pemeriksaan hash lama pada UPDATE untuk menolak perubahan yang bersaing. Akun nonaktif ditolak. Token aplikasi yang sudah diterbitkan tetap mengikuti perilaku proyek: tidak dicabut otomatis saat password berubah.

## Struktur implementasi

```text
internal/googleauth/
├── provider.go       # OAuth2 code exchange, PKCE dan verifikasi ID token Google
├── service.go        # State browser, validasi identitas, penerbitan JWT aplikasi
├── postgres.go       # State sekali pakai serta pembuatan/penautan users
├── handler.go        # Redirect, cookie, callback, response JSON
├── provider_test.go
├── service_test.go
└── handler_test.go
```

Library OAuth2 dan OpenID Connect dipakai untuk pertukaran kode serta pemeriksaan signature, issuer, audience dan expiry. Backend juga memeriksa nonce, authorized party jika disertakan, access-token hash jika ada, subject dan email terverifikasi. Google client secret hanya dikirim dari backend ke endpoint token Google melalui HTTPS; tidak dimasukkan ke URL login.

State acak disimpan sebagai hash pada tabel `google_oauth_states`, terikat ke nilai acak dari cookie HttpOnly dan valid selama 10 menit. Callback mengonsumsi state secara atomik sehingga tidak dapat dipakai ulang. Cookie memakai SameSite Lax; HTTPS memakai Secure dan prefix `__Host-`. State kedaluwarsa dibersihkan saat alur baru dimulai. Pending login disimpan pada Neon, sehingga tidak bergantung pada satu instance backend.

Callback memiliki context 15 detik; panggilan HTTP Google dibatasi 10 detik. Kunci publik Google di-cache oleh library dan diperbarui ketika diperlukan. Login Google tidak menyimpan access/refresh token Google untuk memanggil API Google lain.

Perubahan skema sudah diterapkan langsung pada `Loan_Banking`. File [005_google_oauth.sql](migrations/005_google_oauth.sql) adalah catatan perubahan, bukan langkah manual yang perlu dijalankan lagi untuk database tersebut.

## Error dan rate limit

| HTTP | Code | Arti |
| --- | --- | --- |
| `400` | `invalid_oauth_state` | State/cookie salah, kedaluwarsa, duplikat, atau sudah dikonsumsi |
| `400` | `google_login_cancelled` | Pengguna membatalkan persetujuan |
| `400` | `invalid_password` | Password lokal baru tidak memenuhi aturan |
| `400` | `invalid_json` | Body endpoint password tidak sesuai |
| `401` | `unauthorized` | Identitas Google, token aplikasi, akun aktif, atau password saat ini tidak valid |
| `409` | `google_link_required` | Login ke akun lama dahulu lalu lakukan penautan eksplisit |
| `409` | `google_account_conflict` | Subject/email tidak dapat ditautkan ke akun yang dimaksud |
| `409` | `user_exists` | Registrasi biasa memakai email/username yang sudah digunakan |
| `413` | `body_too_large` | Body endpoint password melebihi 4096 byte |
| `415` | `unsupported_media_type` | Endpoint password tidak menerima Content-Type selain JSON |
| `429` | `rate_limit_exceeded` | Melebihi batas endpoint |
| `502` | `google_provider_unavailable` | Pertukaran token gagal menghubungi provider atau provider sedang bermasalah |
| `503` | `google_oauth_unavailable` | OAuth belum dikonfigurasi |
| `500` | `internal_error` | Kegagalan internal/database |

Batas per IP: awal Google login 10 request/menit, callback 20 request/menit, perubahan password 5 request/menit. Hitungan berada pada memori proses, seperti limiter autentikasi yang sudah ada. Di balik reverse proxy, resolver IP proyek saat ini memakai IP koneksi langsung, bukan `X-Forwarded-For` bebas dari client.

Log aplikasi tidak menyertakan kode OAuth, JWT, ID token, client secret atau error mentah provider. Jangan menyalin nilai tersebut ke log frontend/proxy maupun repository.

## Verifikasi yang sudah dilakukan

- Tes Go memakai code exchange dalam memori dan JWT Google sintetis bertanda tangan RSA: signature/algoritma, audience, issuer, expiration, nonce, azp, PKCE, error provider, serta sanitasi response.
- Tes state: browser salah, expiry, callback diulang, penolakan consent dan email tidak terverifikasi tidak menghasilkan akun.
- Tes handler: redirect, cookie, callback JSON, callback duplikat, mode JSON dan penautan dengan JWT.
- Tes password: akun Google-only harus menetapkan password lokal dahulu, lalu dapat memakai akun yang sama untuk login password; perubahan berikutnya memerlukan password lama.
- Uji repository langsung pada Neon membuktikan pembuatan/pemakaian ulang akun Google, penolakan registrasi email duplikat, penautan yang mempertahankan ID/role/password, penautan eksplisit, browser binding dan penolakan replay. Semua fixture akun/state di-rollback.

Login browser ke Google asli masih perlu dicoba oleh pemilik akun untuk memverifikasi pengaturan OAuth client, consent screen dan redirect URI di Google Cloud. Pengujian otomatis tidak masuk ke akun Google pengguna.
