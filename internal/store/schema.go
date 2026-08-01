package store

// SchemaSQL e o DDL para a tabela `requests` (uma por alvo).
// Aplicado via db.Exec na inicializacao do store SQLite.
const SchemaSQL = `
CREATE TABLE IF NOT EXISTS requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  method TEXT NOT NULL,
  url TEXT NOT NULL,
  req_headers TEXT NOT NULL,
  req_body BLOB,
  status INTEGER,
  resp_headers TEXT,
  resp_body BLOB,
  resp_len INTEGER,
  ttfb_ms INTEGER,
  tags TEXT,
  notes TEXT
);
CREATE INDEX IF NOT EXISTS idx_requests_ts ON requests(ts);
CREATE INDEX IF NOT EXISTS idx_requests_method_url ON requests(method, url);
`
