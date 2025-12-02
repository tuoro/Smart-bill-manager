import express from 'express';
import cors from 'cors';
import path from 'path';
import fs from 'fs';

import paymentRoutes from './routes/payments';
import invoiceRoutes from './routes/invoices';
import emailRoutes from './routes/email';
import dingtalkRoutes from './routes/dingtalk';
import { paymentService } from './services/paymentService';
import { invoiceService } from './services/invoiceService';
import { emailService } from './services/emailService';

const app = express();
const PORT = process.env.PORT || 3001;

// Middleware
app.use(cors());
app.use(express.json());
app.use(express.urlencoded({ extended: true }));

// Ensure uploads directory exists
const uploadsDir = path.join(__dirname, '../uploads');
if (!fs.existsSync(uploadsDir)) {
  fs.mkdirSync(uploadsDir, { recursive: true });
}

// Serve uploaded files
app.use('/uploads', express.static(uploadsDir));

// API Routes
app.use('/api/payments', paymentRoutes);
app.use('/api/invoices', invoiceRoutes);
app.use('/api/email', emailRoutes);
app.use('/api/dingtalk', dingtalkRoutes);

// Health check
app.get('/api/health', (req, res) => {
  res.json({ status: 'ok', timestamp: new Date().toISOString() });
});

// Dashboard summary
app.get('/api/dashboard', (req, res) => {
  try {
    const today = new Date();
    const firstDayOfMonth = new Date(today.getFullYear(), today.getMonth(), 1).toISOString();
    const lastDayOfMonth = new Date(today.getFullYear(), today.getMonth() + 1, 0).toISOString();

    const paymentStats = paymentService.getStats(firstDayOfMonth, lastDayOfMonth);
    const invoiceStats = invoiceService.getStats();
    const emailStatus = emailService.getMonitoringStatus();
    const recentEmails = emailService.getLogs(undefined, 5);

    res.json({
      success: true,
      data: {
        payments: {
          totalThisMonth: paymentStats.totalAmount,
          countThisMonth: paymentStats.totalCount,
          categoryStats: paymentStats.categoryStats,
          dailyStats: paymentStats.dailyStats
        },
        invoices: {
          totalCount: invoiceStats.totalCount,
          totalAmount: invoiceStats.totalAmount,
          bySource: invoiceStats.bySource
        },
        email: {
          monitoringStatus: emailStatus,
          recentLogs: recentEmails
        }
      }
    });
  } catch (error) {
    res.status(500).json({ success: false, message: '获取仪表盘数据失败', error: String(error) });
  }
});

// Serve frontend in production
if (process.env.NODE_ENV === 'production') {
  const frontendPath = path.join(__dirname, '../../frontend/dist');
  app.use(express.static(frontendPath));
  app.get('*', (req, res) => {
    res.sendFile(path.join(frontendPath, 'index.html'));
  });
}

app.listen(PORT, () => {
  console.log(`🚀 Smart Bill Manager API running on port ${PORT}`);
  console.log(`📊 Dashboard: http://localhost:${PORT}`);
  console.log(`📬 Email monitoring ready`);
  console.log(`🤖 DingTalk webhook ready at /api/dingtalk/webhook`);
});

export default app;
