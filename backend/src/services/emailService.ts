import { v4 as uuidv4 } from 'uuid';
import Imap from 'node-imap';
import { simpleParser, ParsedMail } from 'mailparser';
import db from '../models/database';
import { invoiceService } from './invoiceService';
import fs from 'fs';
import path from 'path';

// Type assertion helper
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const parseEmail = (stream: any): Promise<ParsedMail> => {
  return new Promise((resolve, reject) => {
    simpleParser(stream, (err, parsed) => {
      if (err) reject(err);
      else resolve(parsed);
    });
  });
};

export interface EmailConfig {
  id: string;
  email: string;
  imap_host: string;
  imap_port: number;
  password: string;
  is_active: number;
  last_check?: string;
  created_at?: string;
}

export interface EmailLog {
  id: string;
  email_config_id: string;
  subject?: string;
  from_address?: string;
  received_date?: string;
  has_attachment: number;
  attachment_count: number;
  status: string;
  created_at?: string;
}

// Store running IMAP connections
const activeConnections: Map<string, Imap> = new Map();

export const emailService = {
  // Create email configuration
  createConfig(config: Omit<EmailConfig, 'id' | 'created_at'>): EmailConfig {
    const id = uuidv4();
    const stmt = db.prepare(`
      INSERT INTO email_configs (id, email, imap_host, imap_port, password, is_active)
      VALUES (?, ?, ?, ?, ?, ?)
    `);
    stmt.run(id, config.email, config.imap_host, config.imap_port, config.password, config.is_active);
    return { id, ...config };
  },

  // Get all email configurations
  getAllConfigs(): EmailConfig[] {
    const configs = db.prepare('SELECT * FROM email_configs').all() as EmailConfig[];
    // Mask passwords for security
    return configs.map(c => ({ ...c, password: '********' }));
  },

  // Get config by ID (internal use - includes password)
  getConfigById(id: string): EmailConfig | undefined {
    return db.prepare('SELECT * FROM email_configs WHERE id = ?').get(id) as EmailConfig | undefined;
  },

  // Update config
  updateConfig(id: string, data: Partial<EmailConfig>): boolean {
    const fields = ['email', 'imap_host', 'imap_port', 'password', 'is_active'];
    const updates: string[] = [];
    const params: (string | number | null)[] = [];

    for (const field of fields) {
      if (field in data && data[field as keyof EmailConfig] !== '********') {
        updates.push(`${field} = ?`);
        params.push(data[field as keyof EmailConfig] as string | number);
      }
    }

    if (updates.length === 0) return false;

    params.push(id);
    const stmt = db.prepare(`UPDATE email_configs SET ${updates.join(', ')} WHERE id = ?`);
    const result = stmt.run(...params);
    return result.changes > 0;
  },

  // Delete config
  deleteConfig(id: string): boolean {
    this.stopMonitoring(id);
    const result = db.prepare('DELETE FROM email_configs WHERE id = ?').run(id);
    return result.changes > 0;
  },

  // Get email logs
  getLogs(configId?: string, limit: number = 50): EmailLog[] {
    if (configId) {
      return db.prepare('SELECT * FROM email_logs WHERE email_config_id = ? ORDER BY created_at DESC LIMIT ?')
        .all(configId, limit) as EmailLog[];
    }
    return db.prepare('SELECT * FROM email_logs ORDER BY created_at DESC LIMIT ?')
      .all(limit) as EmailLog[];
  },

  // Test IMAP connection
  async testConnection(config: { email: string; imap_host: string; imap_port: number; password: string }): Promise<{ success: boolean; message: string }> {
    return new Promise((resolve) => {
      const imap = new Imap({
        user: config.email,
        password: config.password,
        host: config.imap_host,
        port: config.imap_port,
        tls: true,
        tlsOptions: { rejectUnauthorized: false },
        connTimeout: 10000,
        authTimeout: 10000
      });

      imap.once('ready', () => {
        imap.end();
        resolve({ success: true, message: '连接成功！' });
      });

      imap.once('error', (err: Error) => {
        resolve({ success: false, message: `连接失败: ${err.message}` });
      });

      imap.connect();
    });
  },

  // Start monitoring emails for a specific config
  startMonitoring(configId: string): boolean {
    const config = this.getConfigById(configId);
    if (!config || !config.is_active) return false;

    // Stop existing connection if any
    this.stopMonitoring(configId);

    const imap = new Imap({
      user: config.email,
      password: config.password,
      host: config.imap_host,
      port: config.imap_port,
      tls: true,
      tlsOptions: { rejectUnauthorized: false }
    });

    imap.once('ready', () => {
      console.log(`[Email Monitor] Connected to ${config.email}`);
      this.openInbox(imap, configId);
    });

    imap.once('error', (err: Error) => {
      console.error(`[Email Monitor] Error for ${config.email}:`, err);
      activeConnections.delete(configId);
    });

    imap.once('end', () => {
      console.log(`[Email Monitor] Connection ended for ${config.email}`);
      activeConnections.delete(configId);
    });

    imap.connect();
    activeConnections.set(configId, imap);
    return true;
  },

  // Stop monitoring
  stopMonitoring(configId: string): boolean {
    const connection = activeConnections.get(configId);
    if (connection) {
      try {
        connection.end();
      } catch (e) {
        console.error('Error ending connection:', e);
      }
      activeConnections.delete(configId);
      return true;
    }
    return false;
  },

  // Get monitoring status
  getMonitoringStatus(): { configId: string; status: string }[] {
    const configs = db.prepare('SELECT id FROM email_configs').all() as { id: string }[];
    return configs.map(c => ({
      configId: c.id,
      status: activeConnections.has(c.id) ? 'running' : 'stopped'
    }));
  },

  // Open inbox and start watching
  openInbox(imap: Imap, configId: string): void {
    imap.openBox('INBOX', false, (err, box) => {
      if (err) {
        console.error('Error opening inbox:', err);
        return;
      }

      console.log(`[Email Monitor] Inbox opened. ${box.messages.total} total messages`);

      // Check for new unread messages
      this.fetchUnreadEmails(imap, configId);

      // Listen for new mail
      imap.on('mail', () => {
        console.log('[Email Monitor] New mail received!');
        this.fetchUnreadEmails(imap, configId);
      });
    });
  },

  // Fetch unread emails
  fetchUnreadEmails(imap: Imap, configId: string): void {
    imap.search(['UNSEEN'], (err, results) => {
      if (err) {
        console.error('Error searching emails:', err);
        return;
      }

      if (!results || results.length === 0) {
        console.log('[Email Monitor] No new unread emails');
        return;
      }

      console.log(`[Email Monitor] Found ${results.length} unread emails`);

      const f = imap.fetch(results, { bodies: '', markSeen: true });

      f.on('message', (msg, seqno) => {
        console.log(`[Email Monitor] Processing message #${seqno}`);
        
        msg.on('body', (stream) => {
          parseEmail(stream).then(parsed => {
            this.processEmail(parsed, configId);
          }).catch(err => {
            console.error('Error parsing email:', err);
          });
        });
      });

      f.once('error', (err) => {
        console.error('Fetch error:', err);
      });

      f.once('end', () => {
        // Update last check time
        db.prepare('UPDATE email_configs SET last_check = ? WHERE id = ?')
          .run(new Date().toISOString(), configId);
      });
    });
  },

  // Process parsed email
  async processEmail(parsed: ParsedMail, configId: string): Promise<void> {
    const logId = uuidv4();
    const attachmentCount = parsed.attachments?.length || 0;
    const hasAttachment = attachmentCount > 0 ? 1 : 0;

    // Log the email
    db.prepare(`
      INSERT INTO email_logs (id, email_config_id, subject, from_address, received_date, has_attachment, attachment_count, status)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `).run(
      logId,
      configId,
      parsed.subject || '(无主题)',
      parsed.from?.text || '',
      parsed.date?.toISOString() || new Date().toISOString(),
      hasAttachment,
      attachmentCount,
      'processed'
    );

    console.log(`[Email Monitor] Email logged: ${parsed.subject}`);

    // Process PDF attachments
    if (parsed.attachments) {
      for (const attachment of parsed.attachments) {
        if (attachment.contentType === 'application/pdf' || 
            attachment.filename?.toLowerCase().endsWith('.pdf')) {
          await this.saveAttachment(attachment, configId);
        }
      }
    }
  },

  // Save PDF attachment
  async saveAttachment(attachment: { filename?: string; content: Buffer }, configId: string): Promise<void> {
    const filename = `${Date.now()}_${attachment.filename || 'invoice.pdf'}`;
    const uploadDir = path.join(__dirname, '../../uploads');
    
    if (!fs.existsSync(uploadDir)) {
      fs.mkdirSync(uploadDir, { recursive: true });
    }

    const filePath = path.join(uploadDir, filename);
    fs.writeFileSync(filePath, attachment.content);

    console.log(`[Email Monitor] Saved attachment: ${filename}`);

    // Create invoice record
    await invoiceService.create({
      filename,
      original_name: attachment.filename || 'invoice.pdf',
      file_path: `uploads/${filename}`,
      file_size: attachment.content.length,
      source: 'email'
    });
  },

  // Manual check for new emails
  async manualCheck(configId: string): Promise<{ success: boolean; message: string; newEmails: number }> {
    const config = this.getConfigById(configId);
    if (!config) {
      return { success: false, message: '配置不存在', newEmails: 0 };
    }

    return new Promise((resolve) => {
      const imap = new Imap({
        user: config.email,
        password: config.password,
        host: config.imap_host,
        port: config.imap_port,
        tls: true,
        tlsOptions: { rejectUnauthorized: false }
      });

      let newEmailCount = 0;

      imap.once('ready', () => {
        imap.openBox('INBOX', false, (err) => {
          if (err) {
            imap.end();
            resolve({ success: false, message: `打开收件箱失败: ${err.message}`, newEmails: 0 });
            return;
          }

          imap.search(['UNSEEN'], (err, results) => {
            if (err) {
              imap.end();
              resolve({ success: false, message: `搜索邮件失败: ${err.message}`, newEmails: 0 });
              return;
            }

            if (!results || results.length === 0) {
              imap.end();
              resolve({ success: true, message: '没有新邮件', newEmails: 0 });
              return;
            }

            newEmailCount = results.length;
            const f = imap.fetch(results, { bodies: '', markSeen: true });

            f.on('message', (msg) => {
              msg.on('body', (stream) => {
                parseEmail(stream).then(parsed => {
                  this.processEmail(parsed, configId);
                }).catch(() => {
                  // Ignore parse errors
                });
              });
            });

            f.once('end', () => {
              db.prepare('UPDATE email_configs SET last_check = ? WHERE id = ?')
                .run(new Date().toISOString(), configId);
              imap.end();
              resolve({ success: true, message: `成功处理 ${newEmailCount} 封邮件`, newEmails: newEmailCount });
            });

            f.once('error', (fetchErr) => {
              imap.end();
              resolve({ success: false, message: `获取邮件失败: ${fetchErr.message}`, newEmails: 0 });
            });
          });
        });
      });

      imap.once('error', (err) => {
        resolve({ success: false, message: `连接失败: ${err.message}`, newEmails: 0 });
      });

      imap.connect();
    });
  }
};
