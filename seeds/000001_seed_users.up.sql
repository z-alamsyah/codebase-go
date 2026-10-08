-- Dummy users for local development. Every password is: Password123!
-- pgcrypto's crypt() + gen_salt('bf') produces bcrypt hashes compatible with
-- golang.org/x/crypto/bcrypt.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

INSERT INTO users (id, name, email, phone, password_hash) VALUES
    ('01928f6a-0000-7000-8000-000000000001', 'Andi Pratama',    'andi@example.com',    '+6281200000001', crypt('Password123!', gen_salt('bf', 10))),
    ('01928f6a-0000-7000-8000-000000000002', 'Budi Santoso',    'budi@example.com',    '+6281200000002', crypt('Password123!', gen_salt('bf', 10))),
    ('01928f6a-0000-7000-8000-000000000003', 'Citra Lestari',   'citra@example.com',   '+6281200000003', crypt('Password123!', gen_salt('bf', 10))),
    ('01928f6a-0000-7000-8000-000000000004', 'Dewi Anggraini',  'dewi@example.com',    '+6281200000004', crypt('Password123!', gen_salt('bf', 10))),
    ('01928f6a-0000-7000-8000-000000000005', 'Eko Saputra',     'eko@example.com',     '+6281200000005', crypt('Password123!', gen_salt('bf', 10))),
    ('01928f6a-0000-7000-8000-000000000006', 'Fajar Nugroho',   'fajar@example.com',   '+6281200000006', crypt('Password123!', gen_salt('bf', 10))),
    ('01928f6a-0000-7000-8000-000000000007', 'Gita Permata',    'gita@example.com',    '+6281200000007', crypt('Password123!', gen_salt('bf', 10))),
    ('01928f6a-0000-7000-8000-000000000008', 'Hadi Wijaya',     'hadi@example.com',    '+6281200000008', crypt('Password123!', gen_salt('bf', 10))),
    ('01928f6a-0000-7000-8000-000000000009', 'Indah Puspita',   'indah@example.com',   '+6281200000009', crypt('Password123!', gen_salt('bf', 10))),
    ('01928f6a-0000-7000-8000-000000000010', 'Joko Susilo',     'joko@example.com',    '+6281200000010', crypt('Password123!', gen_salt('bf', 10)))
ON CONFLICT DO NOTHING;
