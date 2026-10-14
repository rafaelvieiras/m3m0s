-- +goose Up
-- Domain category names move from Portuguese to English (D48). Old names are
-- still accepted on input (schema.CanonicalDomain).
UPDATE entries SET domain = 'code:' || substr(domain, 8), domain_category = 'code'
 WHERE domain_category = 'codigo' AND domain LIKE 'codigo:%';
UPDATE ingest_queue SET domain = 'code:' || substr(domain, 8)
 WHERE domain LIKE 'codigo:%';
UPDATE entries SET domain = 'writing:' || substr(domain, 9), domain_category = 'writing'
 WHERE domain_category = 'escrita' AND domain LIKE 'escrita:%';
UPDATE ingest_queue SET domain = 'writing:' || substr(domain, 9)
 WHERE domain LIKE 'escrita:%';
UPDATE entries SET domain = 'personal:' || substr(domain, 9), domain_category = 'personal'
 WHERE domain_category = 'pessoal' AND domain LIKE 'pessoal:%';
UPDATE ingest_queue SET domain = 'personal:' || substr(domain, 9)
 WHERE domain LIKE 'pessoal:%';
UPDATE entries SET domain = 'client:' || substr(domain, 9), domain_category = 'client'
 WHERE domain_category = 'cliente' AND domain LIKE 'cliente:%';
UPDATE ingest_queue SET domain = 'client:' || substr(domain, 9)
 WHERE domain LIKE 'cliente:%';

-- +goose Down
UPDATE entries SET domain = 'codigo:' || substr(domain, 6), domain_category = 'codigo'
 WHERE domain_category = 'code' AND domain LIKE 'code:%';
UPDATE ingest_queue SET domain = 'codigo:' || substr(domain, 6)
 WHERE domain LIKE 'code:%';
UPDATE entries SET domain = 'escrita:' || substr(domain, 9), domain_category = 'escrita'
 WHERE domain_category = 'writing' AND domain LIKE 'writing:%';
UPDATE ingest_queue SET domain = 'escrita:' || substr(domain, 9)
 WHERE domain LIKE 'writing:%';
UPDATE entries SET domain = 'pessoal:' || substr(domain, 10), domain_category = 'pessoal'
 WHERE domain_category = 'personal' AND domain LIKE 'personal:%';
UPDATE ingest_queue SET domain = 'pessoal:' || substr(domain, 10)
 WHERE domain LIKE 'personal:%';
UPDATE entries SET domain = 'cliente:' || substr(domain, 8), domain_category = 'cliente'
 WHERE domain_category = 'client' AND domain LIKE 'client:%';
UPDATE ingest_queue SET domain = 'cliente:' || substr(domain, 8)
 WHERE domain LIKE 'client:%';
