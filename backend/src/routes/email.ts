import { Router, Request, Response } from 'express';
import { emailService } from '../services/emailService';

const router = Router();

// Get all email configurations
router.get('/configs', (req: Request, res: Response) => {
  try {
    const configs = emailService.getAllConfigs();
    res.json({ success: true, data: configs });
  } catch (error) {
    res.status(500).json({ success: false, message: '获取邮箱配置失败', error: String(error) });
  }
});

// Create new email configuration
router.post('/configs', async (req: Request, res: Response) => {
  try {
    const { email, imap_host, imap_port, password, is_active } = req.body;
    
    if (!email || !imap_host || !password) {
      return res.status(400).json({ success: false, message: '邮箱地址、IMAP服务器和密码是必填项' });
    }

    // Test connection first
    const testResult = await emailService.testConnection({
      email,
      imap_host,
      imap_port: imap_port || 993,
      password
    });

    if (!testResult.success) {
      return res.status(400).json({ success: false, message: testResult.message });
    }

    const config = emailService.createConfig({
      email,
      imap_host,
      imap_port: imap_port || 993,
      password,
      is_active: is_active !== undefined ? is_active : 1
    });

    res.status(201).json({ 
      success: true, 
      data: { ...config, password: '********' }, 
      message: '邮箱配置创建成功' 
    });
  } catch (error) {
    res.status(500).json({ success: false, message: '创建邮箱配置失败', error: String(error) });
  }
});

// Update email configuration
router.put('/configs/:id', (req: Request, res: Response) => {
  try {
    const updated = emailService.updateConfig(req.params.id, req.body);
    if (!updated) {
      return res.status(404).json({ success: false, message: '邮箱配置不存在或更新失败' });
    }
    res.json({ success: true, message: '邮箱配置更新成功' });
  } catch (error) {
    res.status(500).json({ success: false, message: '更新邮箱配置失败', error: String(error) });
  }
});

// Delete email configuration
router.delete('/configs/:id', (req: Request, res: Response) => {
  try {
    const deleted = emailService.deleteConfig(req.params.id);
    if (!deleted) {
      return res.status(404).json({ success: false, message: '邮箱配置不存在' });
    }
    res.json({ success: true, message: '邮箱配置删除成功' });
  } catch (error) {
    res.status(500).json({ success: false, message: '删除邮箱配置失败', error: String(error) });
  }
});

// Test email connection
router.post('/test', async (req: Request, res: Response) => {
  try {
    const { email, imap_host, imap_port, password } = req.body;
    
    if (!email || !imap_host || !password) {
      return res.status(400).json({ success: false, message: '请提供完整的连接信息' });
    }

    const result = await emailService.testConnection({
      email,
      imap_host,
      imap_port: imap_port || 993,
      password
    });

    res.json({ success: result.success, message: result.message });
  } catch (error) {
    res.status(500).json({ success: false, message: '测试连接失败', error: String(error) });
  }
});

// Get email logs
router.get('/logs', (req: Request, res: Response) => {
  try {
    const { configId, limit } = req.query;
    const logs = emailService.getLogs(
      configId as string, 
      limit ? parseInt(limit as string) : 50
    );
    res.json({ success: true, data: logs });
  } catch (error) {
    res.status(500).json({ success: false, message: '获取邮件日志失败', error: String(error) });
  }
});

// Start monitoring for a specific config
router.post('/monitor/start/:id', (req: Request, res: Response) => {
  try {
    const started = emailService.startMonitoring(req.params.id);
    if (!started) {
      return res.status(400).json({ success: false, message: '无法启动监控，请检查配置' });
    }
    res.json({ success: true, message: '邮箱监控已启动' });
  } catch (error) {
    res.status(500).json({ success: false, message: '启动监控失败', error: String(error) });
  }
});

// Stop monitoring for a specific config
router.post('/monitor/stop/:id', (req: Request, res: Response) => {
  try {
    const stopped = emailService.stopMonitoring(req.params.id);
    res.json({ success: true, message: stopped ? '邮箱监控已停止' : '监控未在运行' });
  } catch (error) {
    res.status(500).json({ success: false, message: '停止监控失败', error: String(error) });
  }
});

// Get monitoring status
router.get('/monitor/status', (req: Request, res: Response) => {
  try {
    const status = emailService.getMonitoringStatus();
    res.json({ success: true, data: status });
  } catch (error) {
    res.status(500).json({ success: false, message: '获取监控状态失败', error: String(error) });
  }
});

// Manual check for new emails
router.post('/check/:id', async (req: Request, res: Response) => {
  try {
    const result = await emailService.manualCheck(req.params.id);
    res.json({ 
      success: result.success, 
      message: result.message,
      data: { newEmails: result.newEmails }
    });
  } catch (error) {
    res.status(500).json({ success: false, message: '检查邮件失败', error: String(error) });
  }
});

export default router;
