import { Router, Request, Response } from 'express';
import multer from 'multer';
import path from 'path';
import { v4 as uuidv4 } from 'uuid';
import { dingtalkService, DingtalkMessage } from '../services/dingtalkService';
import { invoiceService } from '../services/invoiceService';

const router = Router();

// Configure multer for file upload from DingTalk
const storage = multer.diskStorage({
  destination: (req, file, cb) => {
    cb(null, path.join(__dirname, '../../uploads'));
  },
  filename: (req, file, cb) => {
    const ext = path.extname(file.originalname);
    cb(null, `${uuidv4()}${ext}`);
  }
});

const upload = multer({
  storage,
  limits: {
    fileSize: 20 * 1024 * 1024 // 20MB limit for DingTalk files
  },
  fileFilter: (req, file, cb) => {
    // Accept PDF files
    if (file.mimetype === 'application/pdf' || file.originalname.toLowerCase().endsWith('.pdf')) {
      cb(null, true);
    } else {
      cb(null, false);
    }
  }
});

// Get all DingTalk configurations
router.get('/configs', (req: Request, res: Response) => {
  try {
    const configs = dingtalkService.getAllConfigs();
    res.json({ success: true, data: configs });
  } catch (error) {
    res.status(500).json({ success: false, message: '获取钉钉配置失败', error: String(error) });
  }
});

// Create new DingTalk configuration
router.post('/configs', (req: Request, res: Response) => {
  try {
    const { name, app_key, app_secret, webhook_token, is_active } = req.body;
    
    if (!name) {
      return res.status(400).json({ success: false, message: '配置名称不能为空' });
    }

    const config = dingtalkService.createConfig({
      name,
      app_key,
      app_secret,
      webhook_token,
      is_active: is_active !== undefined ? (is_active ? 1 : 0) : 1
    });

    res.status(201).json({ success: true, data: config, message: '钉钉配置创建成功' });
  } catch (error) {
    res.status(500).json({ success: false, message: '创建钉钉配置失败', error: String(error) });
  }
});

// Update DingTalk configuration
router.put('/configs/:id', (req: Request, res: Response) => {
  try {
    const updated = dingtalkService.updateConfig(req.params.id, req.body);
    if (!updated) {
      return res.status(404).json({ success: false, message: '配置不存在或更新失败' });
    }
    res.json({ success: true, message: '配置更新成功' });
  } catch (error) {
    res.status(500).json({ success: false, message: '更新配置失败', error: String(error) });
  }
});

// Delete DingTalk configuration
router.delete('/configs/:id', (req: Request, res: Response) => {
  try {
    const deleted = dingtalkService.deleteConfig(req.params.id);
    if (!deleted) {
      return res.status(404).json({ success: false, message: '配置不存在' });
    }
    res.json({ success: true, message: '配置删除成功' });
  } catch (error) {
    res.status(500).json({ success: false, message: '删除配置失败', error: String(error) });
  }
});

// Get DingTalk message logs
router.get('/logs', (req: Request, res: Response) => {
  try {
    const { configId, limit } = req.query;
    const logs = dingtalkService.getLogs(
      configId as string | undefined,
      limit ? parseInt(limit as string) : undefined
    );
    res.json({ success: true, data: logs });
  } catch (error) {
    res.status(500).json({ success: false, message: '获取消息日志失败', error: String(error) });
  }
});

// Webhook endpoint for receiving DingTalk robot messages
router.post('/webhook', async (req: Request, res: Response) => {
  try {
    const timestamp = req.headers['timestamp'] as string;
    const sign = req.headers['sign'] as string;
    
    // Get active configuration
    const config = dingtalkService.getActiveConfig();
    if (!config) {
      console.log('[DingTalk Webhook] No active configuration found');
      return res.status(200).json({ msgtype: 'text', text: { content: '服务未配置' } });
    }

    // Verify signature if webhook_token is configured
    if (config.webhook_token) {
      const isValid = dingtalkService.verifySignature(timestamp, sign, config.webhook_token);
      if (!isValid) {
        console.log('[DingTalk Webhook] Invalid signature');
        return res.status(200).json({ msgtype: 'text', text: { content: '签名验证失败' } });
      }
    }

    const message = req.body as DingtalkMessage;
    console.log('[DingTalk Webhook] Received message:', JSON.stringify(message, null, 2));

    // Process the message
    const result = await dingtalkService.processWebhookMessage(message, config.id);

    // If there's a session webhook, send response back
    if (message.sessionWebhook && result.response) {
      try {
        await dingtalkService.sendResponse(message.sessionWebhook, result.response);
      } catch (err) {
        console.error('[DingTalk Webhook] Failed to send response:', err);
      }
    }

    // Return response for direct reply
    if (result.response) {
      res.json(result.response);
    } else {
      res.json({ msgtype: 'text', text: { content: result.message } });
    }
  } catch (error) {
    console.error('[DingTalk Webhook] Error:', error);
    res.status(200).json({ msgtype: 'text', text: { content: '处理消息时发生错误' } });
  }
});

// Webhook endpoint with config ID for multiple robot support
router.post('/webhook/:configId', async (req: Request, res: Response) => {
  try {
    const { configId } = req.params;
    const timestamp = req.headers['timestamp'] as string;
    const sign = req.headers['sign'] as string;

    const config = dingtalkService.getConfigById(configId);
    if (!config || !config.is_active) {
      console.log(`[DingTalk Webhook] Config ${configId} not found or inactive`);
      return res.status(200).json({ msgtype: 'text', text: { content: '配置不存在或已禁用' } });
    }

    // Verify signature if webhook_token is configured
    if (config.webhook_token) {
      const isValid = dingtalkService.verifySignature(timestamp, sign, config.webhook_token);
      if (!isValid) {
        console.log('[DingTalk Webhook] Invalid signature');
        return res.status(200).json({ msgtype: 'text', text: { content: '签名验证失败' } });
      }
    }

    const message = req.body as DingtalkMessage;
    console.log(`[DingTalk Webhook ${configId}] Received message:`, JSON.stringify(message, null, 2));

    // Process the message
    const result = await dingtalkService.processWebhookMessage(message, configId);

    // If there's a session webhook, send response back
    if (message.sessionWebhook && result.response) {
      try {
        await dingtalkService.sendResponse(message.sessionWebhook, result.response);
      } catch (err) {
        console.error('[DingTalk Webhook] Failed to send response:', err);
      }
    }

    // Return response for direct reply
    if (result.response) {
      res.json(result.response);
    } else {
      res.json({ msgtype: 'text', text: { content: result.message } });
    }
  } catch (error) {
    console.error('[DingTalk Webhook] Error:', error);
    res.status(200).json({ msgtype: 'text', text: { content: '处理消息时发生错误' } });
  }
});

// Manual file upload endpoint for DingTalk (when users manually forward files)
router.post('/upload', upload.single('file'), async (req: Request, res: Response) => {
  try {
    if (!req.file) {
      return res.status(400).json({ success: false, message: '请上传PDF文件' });
    }

    const invoice = await invoiceService.create({
      payment_id: req.body.payment_id,
      filename: req.file.filename,
      original_name: req.file.originalname,
      file_path: `uploads/${req.file.filename}`,
      file_size: req.file.size,
      source: 'dingtalk'
    });

    res.status(201).json({ success: true, data: invoice, message: '发票上传成功' });
  } catch (error) {
    res.status(500).json({ success: false, message: '上传发票失败', error: String(error) });
  }
});

// Manual URL download endpoint (for testing or manual file imports)
router.post('/download-url', async (req: Request, res: Response) => {
  try {
    const { url, fileName } = req.body;
    
    if (!url) {
      return res.status(400).json({ success: false, message: '请提供文件URL' });
    }

    const config = dingtalkService.getActiveConfig();
    const configId = config?.id || 'manual';

    await dingtalkService.downloadFromUrl(url, fileName || 'invoice.pdf', configId);
    
    res.json({ success: true, message: '文件下载并处理成功' });
  } catch (error) {
    res.status(500).json({ success: false, message: '下载文件失败', error: String(error) });
  }
});

export default router;
