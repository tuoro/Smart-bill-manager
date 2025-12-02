import Database, { Database as DatabaseType } from 'better-sqlite3';
import path from 'path';

const dbPath = path.join(__dirname, '../../data/bills.db');

// Ensure the data directory exists
import fs from 'fs';
const dataDir = path.dirname(dbPath);
if (!fs.existsSync(dataDir)) {
  fs.mkdirSync(dataDir, { recursive: true });
}

const db: DatabaseType = new Database(dbPath);

// Initialize tables
db.exec(`
  CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT UNIQUE NOT NULL,
    password TEXT NOT NULL,
    email TEXT,
    role TEXT DEFAULT 'user',
    is_active INTEGER DEFAULT 1,
    created_at TEXT DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT DEFAULT CURRENT_TIMESTAMP
  );

  CREATE TABLE IF NOT EXISTS payments (
    id TEXT PRIMARY KEY,
    amount REAL NOT NULL,
    merchant TEXT,
    category TEXT,
    payment_method TEXT,
    description TEXT,
    transaction_time TEXT NOT NULL,
    created_at TEXT DEFAULT CURRENT_TIMESTAMP
  );

  CREATE TABLE IF NOT EXISTS invoices (
    id TEXT PRIMARY KEY,
    payment_id TEXT,
    filename TEXT NOT NULL,
    original_name TEXT NOT NULL,
    file_path TEXT NOT NULL,
    file_size INTEGER,
    invoice_number TEXT,
    invoice_date TEXT,
    amount REAL,
    seller_name TEXT,
    buyer_name TEXT,
    extracted_data TEXT,
    source TEXT DEFAULT 'upload',
    created_at TEXT DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (payment_id) REFERENCES payments(id)
  );

  CREATE TABLE IF NOT EXISTS email_configs (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL,
    imap_host TEXT NOT NULL,
    imap_port INTEGER DEFAULT 993,
    password TEXT NOT NULL,
    is_active INTEGER DEFAULT 1,
    last_check TEXT,
    created_at TEXT DEFAULT CURRENT_TIMESTAMP
  );

  CREATE TABLE IF NOT EXISTS email_logs (
    id TEXT PRIMARY KEY,
    email_config_id TEXT NOT NULL,
    subject TEXT,
    from_address TEXT,
    received_date TEXT,
    has_attachment INTEGER DEFAULT 0,
    attachment_count INTEGER DEFAULT 0,
    status TEXT DEFAULT 'processed',
    created_at TEXT DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (email_config_id) REFERENCES email_configs(id)
  );

  CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
  CREATE INDEX IF NOT EXISTS idx_payments_time ON payments(transaction_time);
  CREATE INDEX IF NOT EXISTS idx_invoices_date ON invoices(invoice_date);
  CREATE INDEX IF NOT EXISTS idx_email_logs_date ON email_logs(received_date);
`);

export default db;
