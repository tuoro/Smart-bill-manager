import express from 'express';
import cors from 'cors';
import path from 'path';
import fs from 'fs';

// Catch and log any initialization errors
process.on('uncaughtException', (error) => {
  console.error('Uncaught Exception:', error);
  process.exit(1);
});

process.on('unhandledRejection', (reason, promise) => {
  console.error('Unhandled Rejection at:', promise, 'reason:', reason);
  process.exit(1);
});

console.log('Starting Smart Bill Manager...');
console.log('Environment:', process.env.NODE_ENV);
console.log('Working directory:', process.cwd());
console.log('__dirname:', __dirname);

// Import routes and services with error handling
let authRoutes: express.Router;
let authMiddleware: typeof import('./routes/auth').authMiddleware;
let paymentRoutes: express.Router;
let invoiceRoutes: express.Router;
let emailRoutes: express.Router;
let dingtalkRoutes: express.Router;
let paymentService: typeof import('./services/paymentService').paymentService;
let invoiceService: typeof import('./services/invoiceService').invoiceService;
let emailService: typeof import('./services/emailService').emailService;
let authService: typeof import('./services/authService').authService;

try {
  console.log('Loading routes and services...');
  const authModule = require('./routes/auth');
  authRoutes = authModule.default;
  authMiddleware = authModule.authMiddleware;
  paymentRoutes = require('./routes/payments').default;
  invoiceRoutes = require('./routes/invoices').default;
  emailRoutes = require('./routes/email').default;
  dingtalkRoutes = require('./routes/dingtalk').default;
  paymentService = require('./services/paymentService').paymentService;
  invoiceService = require('./services/invoiceService').invoiceService;
  emailService = require('./services/emailService').emailService;
  authService = require('./services/authService').authService;
  console.log('Routes and services loaded successfully');
} catch (error) {
  console.error('Failed to load routes or services:', error);
  process.exit(1);
}

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

// Auth routes (public)
app.use('/api/auth', authRoutes);

// Health check (public)
app.get('/api/health', (req, res) => {
  res.json({ status: 'ok', timestamp: new Date().toISOString() });
});

// Protected API Routes
app.use('/api/payments', authMiddleware, paymentRoutes);
app.use('/api/invoices', authMiddleware, invoiceRoutes);
app.use('/api/email', authMiddleware, emailRoutes);
app.use('/api/dingtalk', authMiddleware, dingtalkRoutes);

// Dashboard summary (protected)
app.get('/api/dashboard', authMiddleware, (req, res) => {
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

// Initialize application
const startApp = async () => {
  // Ensure default admin exists
  await authService.ensureAdminExists();

  app.listen(PORT, () => {
    console.log(`🚀 Smart Bill Manager API running on port ${PORT}`);
    console.log(`📊 Dashboard: http://localhost:${PORT}`);
    console.log(`📬 Email monitoring ready`);
    console.log(`🤖 DingTalk webhook ready at /api/dingtalk/webhook`);
    console.log(`🔐 Auth system enabled`);
  });
};

startApp().catch((error) => {
  console.error('Failed to start application:', error);
  process.exit(1);
});

export default app;
