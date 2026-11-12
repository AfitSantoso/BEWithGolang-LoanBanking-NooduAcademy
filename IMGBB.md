# Upload dokumen pelanggan menggunakan ImgBB

Backend mengunggah gambar ke ImgBB, lalu menyimpan **ID upload dan URL gambar langsung** pada `Loan_Banking.public.customers`. Frontend menggunakan URL dari response backend untuk menampilkan gambar.

## Konfigurasi dan lokasi kode

API key sudah dikonfigurasi pada `.env` lokal sebagai `IMGBB_API_KEY`. Nilai nyatanya tidak dicantumkan pada dokumentasi atau `.env.example`. Setelah mengubah konfigurasi, restart server:

```bash
go run .
```

Jika key kosong, endpoint lain tetap berfungsi, tetapi upload menghasilkan `503 image_upload_unavailable`.

| Lokasi | Tanggung jawab |
| --- | --- |
| `internal/imagehost/image.go` | Metadata publik hasil upload: ID dan URL |
| `internal/imagehost/imgbb/client.go` | Adapter HTTP ke ImgBB, multipart, timeout, validasi response |
| `internal/customer/documents.go` | Validasi file, pengecekan profil pemilik, nama upload, penyimpanan metadata |
| `internal/customer/postgres.go` | UPDATE pasangan URL/ID berdasarkan `user_id` akun login |
| `internal/customer/http/handler.go` | Endpoint multipart, batas body, rate limit dan status response |
| `internal/config/config.go` | Membaca key backend |
| `migrations/004_customers_imgbb.sql` | Catatan tiga kolom ID tambahan yang sudah diterapkan di Neon |

Service bergantung pada interface `ImageUploader`, sehingga aturan profil tidak terikat langsung pada client HTTP ImgBB. `main.go` memberikan implementasi ImgBB lewat dependency injection.

## Alur pemakaian

1. Register, lalu login dan dapatkan `access_token`.
2. Simpan data diri melalui `PUT /api/v1/customers/me` dengan JSON sesuai [API.md](API.md).
3. Upload masing-masing gambar melalui endpoint dokumen dengan token yang sama.
4. Baca `GET /api/v1/customers/me` untuk memperoleh profil beserta URL/ID dokumen.
5. Frontend memasang URL tersebut pada atribut `src` elemen gambar.

```mermaid
sequenceDiagram
    participant F as Frontend / Thunder Client
    participant B as Backend Go
    participant I as ImgBB API
    participant D as Neon Loan_Banking
    F->>B: PUT dokumen + Bearer token + file
    B->>D: Verifikasi akun aktif dan profil milik user
    B->>B: Validasi ukuran, tipe dan dimensi gambar
    B->>I: POST multipart (key backend + image)
    I-->>B: data.id dan data.url
    B->>D: UPDATE *_image_path dan *_image_id WHERE user_id = pemilik
    D-->>B: Profil terbaru
    B-->>F: 200 JSON profil dengan metadata gambar
```

Key berada pada multipart request **backend → ImgBB**, bukan URL query atau request frontend. Nama berkas dibuat backend dengan pola `customer_<customer_id>_<documentType>_<random>.jpg/png`; nama asli file, NIK, nama ibu, dan nomor rekening tidak digunakan sebagai nama upload.

## Endpoint upload

```http
PUT /api/v1/customers/me/documents/{documentType}
Authorization: Bearer REPLACE_WITH_ACCESS_TOKEN
Content-Type: multipart/form-data; boundary=...
```

| `documentType` | Field file pada request | Kolom URL | Kolom ID ImgBB |
| --- | --- | --- | --- |
| `ktp_image` | `image` | `ktp_image_path` | `ktp_image_id` |
| `selfie_image` | `image` | `selfie_image_path` | `selfie_image_id` |
| `payslip_image` | `image` | `payslip_image_path` | `payslip_image_id` |

Satu request memuat **satu file** pada field `image`. Untuk tiga dokumen, lakukan tiga request. ID profil dan `user_id` tidak dikirim; kepemilikan berasal dari token dan pengecekan akun aktif.

Aturan file:

- JPEG atau PNG, maksimal **5 MiB** per file.
- Dimensi maksimal 10000 piksel pada setiap sisi dan maksimal 25 juta piksel total.
- Tipe dikenali dari isi file serta header gambar, bukan ekstensi/nama dari client.
- SVG, GIF, PDF, WebP, URL dan base64 tidak diterima oleh endpoint aplikasi ini.
- Field multipart tambahan atau file kedua ditolak. Body total dibatasi 5 MiB + 64 KiB untuk overhead multipart.

Format/ukuran aplikasi sengaja lebih terbatas daripada API provider. Dokumentasi resmi ImgBB menerima file biner melalui POST; parameter `expiration` opsional. Upload produksi pada implementasi ini **tidak mengirim expiration**, sehingga tidak sengaja mengikuti contoh `expiration=600` yang akan membuat gambar terhapus setelah sepuluh menit. Tidak ada jaminan URL akan tersedia selamanya jika gambar dihapus atau kebijakan provider berubah. [Dokumentasi API ImgBB](https://api.imgbb.com/).

Endpoint dibatasi **10 request per menit per akun**, terpisah dari batas login/register. Batas ini berada pada memori proses. Header `Retry-After` diberikan pada `429`. Request upload memiliki context 60 detik, panggilan ImgBB dibatasi 20 detik; timeout baca/tulis HTTP server disesuaikan menjadi 30/90 detik. Tidak ada retry otomatis yang bisa menggandakan upload tanpa diketahui client.

## Mencoba di Thunder Client

1. Pilih method **PUT** dan URL `http://127.0.0.1:8080/api/v1/customers/me/documents/ktp_image`.
2. Pada Auth pilih Bearer, lalu isi access token hasil login.
3. Pada Body pilih **Form / multipart**, tambahkan field `image` bertipe **File**, dan pilih berkas JPEG/PNG.
4. Biarkan aplikasi membentuk header multipart beserta boundary. Hapus header `Content-Type: application/json` yang mungkin masih tersimpan dari request sebelumnya.
5. Kirim. Ganti bagian terakhir URL menjadi `selfie_image` atau `payslip_image` untuk dokumen lainnya.

Contoh cURL:

```bash
curl -X PUT 'http://127.0.0.1:8080/api/v1/customers/me/documents/ktp_image' \
  -H 'Authorization: Bearer REPLACE_WITH_ACCESS_TOKEN' \
  -F 'image=@/path/to/ktp.jpg'
```

Jangan mengirim API key ImgBB dari Thunder Client ke endpoint backend. Key sudah dibaca backend melalui `.env`.

## Response dan isi tabel

Upload berhasil menghasilkan **`200 OK`**, dengan profil lengkap pada `data`, sama seperti GET profil. Berikut **cuplikan field dokumen saja** setelah tiga upload; ID dan URL merupakan ilustrasi, bukan gambar yang benar-benar tersedia:

```json
{
  "data": {
    "id": 7,
    "user_id": 4,
    "ktp_image_path": "https://i.ibb.co/example/customer-7-ktp.jpg",
    "ktp_image_id": "exampleKTP",
    "selfie_image_path": "https://i.ibb.co/example/customer-7-selfie.jpg",
    "selfie_image_id": "exampleSelfie",
    "payslip_image_path": "https://i.ibb.co/example/customer-7-payslip.jpg",
    "payslip_image_id": "examplePayslip"
  }
}
```

Backend memakai **`data.url` dari ImgBB**, yaitu URL gambar langsung pada `i.ibb.co`. `url_viewer` mengarah ke halaman HTML dan bukan nilai untuk `src` gambar. Nilai provider `delete_url` dan key tidak menjadi bagian response API.

Sebelum upload, pasangan field bernilai `null`. Profil lama yang sudah memiliki path tetap dipertahankan dan mungkin belum memiliki ID provider. PUT JSON data diri tidak dapat mengubah URL/ID tersebut, dan tidak menghapus gambar saat pengguna memperbarui alamat atau pendapatan.

Anda dapat memeriksa metadata pada database yang sama melalui query baca-saja:

```sql
SELECT id, user_id,
       ktp_image_path, ktp_image_id,
       selfie_image_path, selfie_image_id,
       payslip_image_path, payslip_image_id
FROM public.customers
WHERE user_id = 4;
```

Angka `4` hanyalah contoh; gunakan ID akun yang benar. Tampilan DBCode/dashboard harus memilih database **`Loan_Banking`** dan branch yang sesuai dengan `.env`.

## Contoh pemanggilan dari frontend

Fungsi upload berikut memakai file dari input HTML. Tidak ada key ImgBB di frontend dan tidak perlu mengatur `Content-Type` sendiri, karena browser membentuk boundary multipart.

```javascript
const baseURL = "http://127.0.0.1:8080";

async function uploadCustomerDocument(accessToken, documentType, file) {
  const form = new FormData();
  form.append("image", file);

  const response = await fetch(
    `${baseURL}/api/v1/customers/me/documents/${documentType}`,
    {
      method: "PUT",
      headers: { Authorization: `Bearer ${accessToken}` },
      body: form,
    }
  );

  const result = await response.json();
  if (!response.ok) {
    throw new Error(result.error?.message || "Upload gagal");
  }
  return result.data;
}

// Contoh penggunaan dalam event handler dengan token hasil login:
// const file = document.querySelector('#ktp-file').files[0];
// const profile = await uploadCustomerDocument(token, 'ktp_image', file);
// document.querySelector('#ktp-preview').src = profile.ktp_image_path;
```

Mengambil ulang profil dan menampilkan gambar yang sudah tersimpan:

```javascript
async function showCustomerDocuments(accessToken) {
  const response = await fetch(`${baseURL}/api/v1/customers/me`, {
    headers: { Authorization: `Bearer ${accessToken}` },
    cache: "no-store",
  });
  const result = await response.json();
  if (!response.ok) {
    throw new Error(result.error?.message || "Gagal membaca profil");
  }

  const container = document.querySelector("#documents");
  container.replaceChildren();
  for (const kind of ["ktp_image", "selfie_image", "payslip_image"]) {
    const url = result.data[`${kind}_path`];
    if (!url) continue;

    const image = document.createElement("img");
    image.src = url;
    image.alt = kind;
    image.referrerPolicy = "no-referrer";
    image.dataset.imageId = result.data[`${kind}_id`] || "";
    container.appendChild(image);
  }
}
```

Halaman HTML perlu menyediakan container `<div id="documents"></div>`. Jika frontend memakai Content Security Policy, izinkan `https://i.ibb.co` pada `img-src`. Sesuaikan `CORS_ORIGIN` backend dengan origin frontend untuk request API. Response non-JSON dari proxy/server dapat memerlukan penanganan tambahan pada aplikasi frontend.

## Status error upload

| HTTP | Code | Arti |
| --- | --- | --- |
| `400` | `invalid_document_type` | Jenis bukan KTP/selfie/payslip yang didukung |
| `400` | `invalid_multipart` | Tidak ada satu file `image`, field tambahan, atau body multipart rusak |
| `400` | `invalid_image` | Tipe/header/dimensi tidak memenuhi aturan |
| `401` | `unauthorized` | Token atau akun tidak valid/aktif |
| `404` | `customer_not_found` | Lengkapi profil JSON dahulu |
| `413` | `image_too_large` | Melebihi batas file/body upload |
| `415` | `unsupported_media_type` | Request bukan multipart/form-data |
| `429` | `rate_limit_exceeded` | Batas 10 request upload per menit per akun terlampaui |
| `502` | `image_upload_failed` | ImgBB menolak upload, timeout, atau response provider tidak valid |
| `503` | `image_upload_unavailable` | API key belum dikonfigurasi pada backend |
| `500` | `internal_error` | Penyimpanan database atau operasi internal gagal |

Error tidak menyertakan key, isi dokumen, atau response mentah provider.

## Penggantian gambar, kegagalan, dan akses URL

Setiap upload berhasil membuat gambar baru di ImgBB dan mengganti pasangan URL/ID jenis tersebut pada profil yang sama. Dokumen lain, pemilik akun, data diri dan total utang tetap dipertahankan. Pembaruan SQL memakai `WHERE user_id = akun_login` dengan nama kolom dari whitelist tetap.

ImgBB dan PostgreSQL tidak berada dalam satu transaksi. Jika upload provider gagal, metadata database tidak diperbarui. Jika upload berhasil tetapi UPDATE database gagal, gambar mungkin sudah ada di ImgBB walaupun endpoint mengembalikan error. Gambar lama saat penggantian dan gambar tersebut **belum dihapus otomatis**. Implementasi ini tidak menyimpan `delete_url` atau menyediakan endpoint penghapusan gambar; pengelolaan aset lama dilakukan dari akun/dashboard ImgBB.

Endpoint profil/upload dilindungi JWT, tetapi **URL gambar langsung dapat diakses siapa pun yang memilikinya**. JWT tidak membuat gambar ImgBB menjadi privat. Jangan menampilkan URL pada halaman publik; gunakan data uji untuk pengembangan dan pertimbangkan sifat akses ini sebelum mengunggah dokumen pribadi.

## Hasil verifikasi

- Pengujian adapter menggunakan server HTTP lokal: multipart, key pada form, penolakan redirect, timeout/cancel, batas response, validasi URL, error aman, dan expiration opsional.
- Pengujian service: file dan ukuran, profil pemilik sebelum upload, nama acak dengan ID pelanggan, kegagalan provider tanpa menyimpan metadata.
- Pengujian HTTP: form, field tak dikenal, ukuran, autentikasi, batas waktu upload yang terpisah dari autentikasi, status error, dan rate limit per akun.
- Verifikasi langsung dengan key lokal berhasil mengunggah **satu PNG buatan tanpa data pribadi**, menggunakan expiration 60 detik khusus pengujian.
- Ketiga pasangan URL/ID dan isolasi pemilik diuji pada `Loan_Banking` dalam transaksi rollback; tidak ada akun/profil percobaan yang ditinggalkan.

Perintah pemeriksaan lokal:

```bash
go test -race ./...
go vet ./...
go build -o bin/server .
```
