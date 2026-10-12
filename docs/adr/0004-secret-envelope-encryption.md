# ADR-0004: Secret — Envelope Encryption + SecretStore

- **Status:** Accepted
- **Tanggal:** 2026-10-12

## Konteks
SEMAR menyimpan kredensial ke seluruh cloud. Kebocoran database tidak boleh langsung membocorkan kredensial. Vault belum tersedia dan menambah beban operasional.

## Keputusan
- Antarmuka `SecretStore` dengan implementasi awal **envelope encryption** di PostgreSQL: DEK per secret (AES-256-GCM, AAD = `table|row_id|purpose`), DEK dibungkus oleh master key (KEK).
- Setiap baris menyimpan `kek_version` sejak Phase 1.
- KEK disimpan **di luar database** (KMS/Vault, atau minimal host/namespace berbeda dari PostgreSQL di produksi).

## Konsekuensi
- Rotasi KEK = rewrap DEK secara online, tanpa mengenkripsi ulang data.
- Implementasi Vault dapat ditambahkan di balik `SecretStore` tanpa mengubah pemanggil.
- Backup harus mencakup KEK secara terpisah dari database; runbook rotasi wajib.

## Alternatif
- **Vault sejak awal:** paling kuat, tetapi menambah komponen Tier-0 sebelum MVP.
- **Enkripsi kolom dengan satu kunci statis:** sederhana, tetapi rotasi berarti enkripsi ulang seluruh data.
