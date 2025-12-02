import { v4 as uuidv4 } from 'uuid';
import crypto from 'crypto';
import https from 'https';
import http from 'http';
import { URL } from 'url';
import db from '../models/database';
import { invoiceService } from './invoiceService';
import fs from 'fs';
import path from 'path';

export interface DingtalkConfig {
  id: string;
  name: string;
  app_key?: string;
  app_secret?: string;
  webhook_token?: string;
  is_active: number;
  created_at?: string;
}

export interface DingtalkLog {
  id: string;
  config_id: string;
  message_type: string;
  sender_nick?: string;
  sender_id?: string;
  content?: string;
  has_attachment: number;
  attachment_count: number;
  status: string;
  created_at?: string;
}

export interface DingtalkMessage {
  msgtype: string;
  text?: {
    content: string;
  };
  msgId?: string;
  createAt?: number;
  conversationType?: string;
  conversationId?: string;
  senderId?: string;
  senderNick?: string;
  senderCorpId?: string;
  sessionWebhook?: string;
  sessionWebhookExpiredTime?: number;
  isAdmin?: boolean;
  chatbotUserId?: string;
  isInAtList?: boolean;
  senderStaffId?: string;
  chatbotCorpId?: string;
  atUsers?: Array<{
    dingtalkId: string;
    staffId?: string;
  }>;
  content?: {
    downloadCode?: string;
    fileName?: string;
  };
}

// Store configurations in memory for quick access
const configCache: Map<string, DingtalkConfig> = new Map();

export const dingtalkService = {
  // Initialize tables if not exists
  initTables(): void {
    db.exec(`
      CREATE TABLE IF NOT EXISTS dingtalk_configs (
        id TEXT PRIMARY KEY,
        name TEXT NOT NULL,
        app_key TEXT,
        app_secret TEXT,
        webhook_token TEXT,
        is_active INTEGER DEFAULT 1,
        created_at TEXT DEFAULT CURRENT_TIMESTAMP
      );

      CREATE TABLE IF NOT EXISTS dingtalk_logs (
        id TEXT PRIMARY KEY,
        config_id TEXT NOT NULL,
        message_type TEXT,
        sender_nick TEXT,
        sender_id TEXT,
        content TEXT,
        has_attachment INTEGER DEFAULT 0,
        attachment_count INTEGER DEFAULT 0,
        status TEXT DEFAULT 'processed',
        created_at TEXT DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY (config_id) REFERENCES dingtalk_configs(id)
      );

      CREATE INDEX IF NOT EXISTS idx_dingtalk_logs_date ON dingtalk_logs(created_at);
    `);
  },

  // Create DingTalk configuration
  createConfig(config: Omit<DingtalkConfig, 'id' | 'created_at'>): DingtalkConfig {
    const id = uuidv4();
    const stmt = db.prepare(`
      INSERT INTO dingtalk_configs (id, name, app_key, app_secret, webhook_token, is_active)
      VALUES (?, ?, ?, ?, ?, ?)
    `);
    stmt.run(id, config.name, config.app_key || null, config.app_secret || null, config.webhook_token || null, config.is_active);
    const newConfig = { id, ...config };
    configCache.set(id, newConfig);
    return newConfig;
  },

  // Get all configurations
  getAllConfigs(): DingtalkConfig[] {
    const configs = db.prepare('SELECT * FROM dingtalk_configs').all() as DingtalkConfig[];
    // Mask secrets for security
    return configs.map(c => ({
      ...c,
      app_secret: c.app_secret ? '********' : undefined,
      webhook_token: c.webhook_token ? '********' : undefined
    }));
  },

  // Get config by ID (internal use - includes secrets)
  getConfigById(id: string): DingtalkConfig | undefined {
    // Check cache first
    if (configCache.has(id)) {
      return configCache.get(id);
    }
    const config = db.prepare('SELECT * FROM dingtalk_configs WHERE id = ?').get(id) as DingtalkConfig | undefined;
    if (config) {
      configCache.set(id, config);
    }
    return config;
  },

  // Get first active config
  getActiveConfig(): DingtalkConfig | undefined {
    return db.prepare('SELECT * FROM dingtalk_configs WHERE is_active = 1 LIMIT 1').get() as DingtalkConfig | undefined;
  },

  // Update config
  updateConfig(id: string, data: Partial<DingtalkConfig>): boolean {
    const fields = ['name', 'app_key', 'app_secret', 'webhook_token', 'is_active'];
    const updates: string[] = [];
    const params: (string | number | null)[] = [];

    for (const field of fields) {
      if (field in data && data[field as keyof DingtalkConfig] !== '********') {
        updates.push(`${field} = ?`);
        params.push(data[field as keyof DingtalkConfig] as string | number | null);
      }
    }

    if (updates.length === 0) return false;

    params.push(id);
    const stmt = db.prepare(`UPDATE dingtalk_configs SET ${updates.join(', ')} WHERE id = ?`);
    const result = stmt.run(...params);
    
    // Invalidate cache
    configCache.delete(id);
    
    return result.changes > 0;
  },

  // Delete config
  deleteConfig(id: string): boolean {
    configCache.delete(id);
    const result = db.prepare('DELETE FROM dingtalk_configs WHERE id = ?').run(id);
    return result.changes > 0;
  },

  // Get logs
  getLogs(configId?: string, limit: number = 50): DingtalkLog[] {
    if (configId) {
      return db.prepare('SELECT * FROM dingtalk_logs WHERE config_id = ? ORDER BY created_at DESC LIMIT ?')
        .all(configId, limit) as DingtalkLog[];
    }
    return db.prepare('SELECT * FROM dingtalk_logs ORDER BY created_at DESC LIMIT ?')
      .all(limit) as DingtalkLog[];
  },

  // Verify DingTalk webhook signature
  verifySignature(timestamp: string, sign: string, secret: string): boolean {
    if (!timestamp || !sign || !secret) {
      return false;
    }

    const stringToSign = `${timestamp}\n${secret}`;
    const hmac = crypto.createHmac('sha256', secret);
    hmac.update(stringToSign);
    const calculatedSign = hmac.digest('base64');
    
    return calculatedSign === sign;
  },

  // Process incoming webhook message
  async processWebhookMessage(message: DingtalkMessage, configId: string): Promise<{ success: boolean; message: string; response?: Record<string, unknown> }> {
    const logId = uuidv4();
    let attachmentCount = 0;
    let hasAttachment = 0;

    // Determine message content and type
    const messageType = message.msgtype || 'unknown';
    let content = '';
    
    if (message.text?.content) {
      content = message.text.content;
    }

    // Check for file type messages (richText or file)
    if (message.msgtype === 'file' && message.content?.downloadCode) {
      hasAttachment = 1;
      attachmentCount = 1;
      
      // Download and process the file
      try {
        await this.downloadAndProcessFile(message.content.downloadCode, message.content.fileName || 'invoice.pdf', configId);
        content = `文件: ${message.content.fileName || '未知文件名'}`;
      } catch (error) {
        console.error('[DingTalk] Error downloading file:', error);
        content = `文件下载失败: ${message.content.fileName || '未知文件名'}`;
      }
    }

    // Log the message
    db.prepare(`
      INSERT INTO dingtalk_logs (id, config_id, message_type, sender_nick, sender_id, content, has_attachment, attachment_count, status)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `).run(
      logId,
      configId,
      messageType,
      message.senderNick || '',
      message.senderId || '',
      content,
      hasAttachment,
      attachmentCount,
      'processed'
    );

    console.log(`[DingTalk] Message logged: ${messageType} from ${message.senderNick || 'unknown'}`);

    // Prepare response message
    const responseMessage = hasAttachment 
      ? `收到发票文件，正在处理中...` 
      : `收到消息: ${content.substring(0, 50)}${content.length > 50 ? '...' : ''}`;

    return {
      success: true,
      message: '消息处理成功',
      response: {
        msgtype: 'text',
        text: {
          content: responseMessage
        }
      }
    };
  },

  // Download file from DingTalk using download code
  async downloadAndProcessFile(downloadCode: string, fileName: string, configId: string): Promise<void> {
    const config = this.getConfigById(configId);
    if (!config || !config.app_key || !config.app_secret) {
      throw new Error('DingTalk configuration missing app_key or app_secret');
    }

    // Get access token
    const accessToken = await this.getAccessToken(config.app_key, config.app_secret);
    
    // Download the file using the download code
    const fileBuffer = await this.downloadFileWithToken(downloadCode, accessToken);
    
    // Save the file
    await this.saveFile(fileBuffer, fileName, configId);
  },

  // Get DingTalk access token
  async getAccessToken(appKey: string, appSecret: string): Promise<string> {
    return new Promise((resolve, reject) => {
      const url = `https://oapi.dingtalk.com/gettoken?appkey=${encodeURIComponent(appKey)}&appsecret=${encodeURIComponent(appSecret)}`;
      
      https.get(url, (res) => {
        let data = '';
        res.on('data', (chunk) => {
          data += chunk;
        });
        res.on('end', () => {
          try {
            const result = JSON.parse(data);
            if (result.errcode === 0 && result.access_token) {
              resolve(result.access_token);
            } else {
              reject(new Error(result.errmsg || 'Failed to get access token'));
            }
          } catch (e) {
            reject(e);
          }
        });
      }).on('error', reject);
    });
  },

  // Download file using access token
  async downloadFileWithToken(downloadCode: string, accessToken: string): Promise<Buffer> {
    return new Promise((resolve, reject) => {
      const postData = JSON.stringify({
        downloadCode: downloadCode,
        robotCode: '' // Will be filled if needed
      });

      const url = new URL(`https://oapi.dingtalk.com/robot/message/file/download?access_token=${accessToken}`);
      
      const options = {
        hostname: url.hostname,
        port: 443,
        path: url.pathname + url.search,
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Content-Length': Buffer.byteLength(postData)
        }
      };

      const req = https.request(options, (res) => {
        const chunks: Buffer[] = [];
        res.on('data', (chunk: Buffer) => {
          chunks.push(chunk);
        });
        res.on('end', () => {
          const buffer = Buffer.concat(chunks);
          // Check if response is JSON error
          try {
            const jsonResponse = JSON.parse(buffer.toString());
            if (jsonResponse.errcode && jsonResponse.errcode !== 0) {
              reject(new Error(jsonResponse.errmsg || 'Download failed'));
              return;
            }
          } catch {
            // Not JSON, likely binary file data
          }
          resolve(buffer);
        });
      });

      req.on('error', reject);
      req.write(postData);
      req.end();
    });
  },

  // Save file and create invoice
  async saveFile(fileBuffer: Buffer, fileName: string, configId: string): Promise<void> {
    const safeFileName = `${Date.now()}_${fileName.replace(/[^a-zA-Z0-9._-]/g, '_')}`;
    const uploadDir = path.join(__dirname, '../../uploads');
    
    if (!fs.existsSync(uploadDir)) {
      fs.mkdirSync(uploadDir, { recursive: true });
    }

    const filePath = path.join(uploadDir, safeFileName);
    fs.writeFileSync(filePath, fileBuffer);

    console.log(`[DingTalk] Saved file: ${safeFileName}`);

    // Only create invoice record if it's a PDF
    if (fileName.toLowerCase().endsWith('.pdf')) {
      await invoiceService.create({
        filename: safeFileName,
        original_name: fileName,
        file_path: `uploads/${safeFileName}`,
        file_size: fileBuffer.length,
        source: 'dingtalk'
      });
    }
  },

  // Handle file upload via URL (for direct file URLs in messages)
  async downloadFromUrl(fileUrl: string, fileName: string, configId: string): Promise<void> {
    return new Promise((resolve, reject) => {
      const url = new URL(fileUrl);
      const protocol = url.protocol === 'https:' ? https : http;

      protocol.get(fileUrl, (res) => {
        if (res.statusCode === 302 || res.statusCode === 301) {
          // Follow redirect
          const redirectUrl = res.headers.location;
          if (redirectUrl) {
            this.downloadFromUrl(redirectUrl, fileName, configId).then(resolve).catch(reject);
            return;
          }
        }

        const chunks: Buffer[] = [];
        res.on('data', (chunk: Buffer) => {
          chunks.push(chunk);
        });
        res.on('end', async () => {
          const buffer = Buffer.concat(chunks);
          try {
            await this.saveFile(buffer, fileName, configId);
            resolve();
          } catch (err) {
            reject(err);
          }
        });
      }).on('error', reject);
    });
  },

  // Send response back to DingTalk via session webhook
  async sendResponse(sessionWebhook: string, response: Record<string, unknown>): Promise<void> {
    return new Promise((resolve, reject) => {
      const postData = JSON.stringify(response);
      const url = new URL(sessionWebhook);

      const options = {
        hostname: url.hostname,
        port: 443,
        path: url.pathname + url.search,
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Content-Length': Buffer.byteLength(postData)
        }
      };

      const req = https.request(options, (res) => {
        let data = '';
        res.on('data', (chunk) => {
          data += chunk;
        });
        res.on('end', () => {
          console.log('[DingTalk] Response sent:', data);
          resolve();
        });
      });

      req.on('error', (err) => {
        console.error('[DingTalk] Failed to send response:', err);
        reject(err);
      });

      req.write(postData);
      req.end();
    });
  }
};

// Initialize tables on module load
dingtalkService.initTables();
