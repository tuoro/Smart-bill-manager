import { Router, Request, Response, NextFunction } from 'express';
import { authService } from '../services/authService';

const router = Router();

// Auth middleware to protect routes
export const authMiddleware = (req: Request, res: Response, next: NextFunction) => {
  const authHeader = req.headers.authorization;
  
  if (!authHeader || !authHeader.startsWith('Bearer ')) {
    return res.status(401).json({ success: false, message: '未授权，请先登录' });
  }

  const token = authHeader.substring(7);
  const result = authService.verifyToken(token);

  if (!result.valid || !result.decoded) {
    return res.status(401).json({ success: false, message: '登录已过期，请重新登录' });
  }

  // Attach user info to request
  (req as Request & { user?: { userId: string; username: string; role: string } }).user = result.decoded;
  next();
};

// Register new user
router.post('/register', async (req: Request, res: Response) => {
  try {
    const { username, password, email } = req.body;

    if (!username || !password) {
      return res.status(400).json({ success: false, message: '用户名和密码不能为空' });
    }

    if (username.length < 3 || username.length > 50) {
      return res.status(400).json({ success: false, message: '用户名长度应为3-50个字符' });
    }

    if (password.length < 6) {
      return res.status(400).json({ success: false, message: '密码长度至少6个字符' });
    }

    const result = await authService.register(username, password, email);

    if (!result.success) {
      return res.status(400).json(result);
    }

    res.status(201).json(result);
  } catch (error) {
    console.error('Register error:', error);
    res.status(500).json({ success: false, message: '注册失败，请稍后重试' });
  }
});

// User login
router.post('/login', async (req: Request, res: Response) => {
  try {
    const { username, password } = req.body;

    if (!username || !password) {
      return res.status(400).json({ success: false, message: '用户名和密码不能为空' });
    }

    const result = await authService.login(username, password);

    if (!result.success) {
      return res.status(401).json(result);
    }

    res.json(result);
  } catch (error) {
    console.error('Login error:', error);
    res.status(500).json({ success: false, message: '登录失败，请稍后重试' });
  }
});

// Get current user info
router.get('/me', authMiddleware, (req: Request, res: Response) => {
  try {
    const userReq = req as Request & { user?: { userId: string; username: string; role: string } };
    const userId = userReq.user?.userId;

    if (!userId) {
      return res.status(401).json({ success: false, message: '未授权' });
    }

    const user = authService.getUserById(userId);
    if (!user) {
      return res.status(404).json({ success: false, message: '用户不存在' });
    }

    res.json({ success: true, data: user });
  } catch (error) {
    console.error('Get user error:', error);
    res.status(500).json({ success: false, message: '获取用户信息失败' });
  }
});

// Verify token
router.get('/verify', authMiddleware, (req: Request, res: Response) => {
  const userReq = req as Request & { user?: { userId: string; username: string; role: string } };
  res.json({
    success: true,
    message: 'Token有效',
    user: userReq.user
  });
});

// Change password
router.post('/change-password', authMiddleware, async (req: Request, res: Response) => {
  try {
    const userReq = req as Request & { user?: { userId: string; username: string; role: string } };
    const userId = userReq.user?.userId;
    const { oldPassword, newPassword } = req.body;

    if (!userId) {
      return res.status(401).json({ success: false, message: '未授权' });
    }

    if (!oldPassword || !newPassword) {
      return res.status(400).json({ success: false, message: '原密码和新密码不能为空' });
    }

    if (newPassword.length < 6) {
      return res.status(400).json({ success: false, message: '新密码长度至少6个字符' });
    }

    const result = await authService.updatePassword(userId, oldPassword, newPassword);

    if (!result.success) {
      return res.status(400).json(result);
    }

    res.json(result);
  } catch (error) {
    console.error('Change password error:', error);
    res.status(500).json({ success: false, message: '修改密码失败，请稍后重试' });
  }
});

// Check if setup is required (no users exist)
router.get('/setup-required', (req: Request, res: Response) => {
  const hasUsers = authService.hasUsers();
  res.json({ success: true, setupRequired: !hasUsers });
});

// Initial setup - create admin user
router.post('/setup', async (req: Request, res: Response) => {
  try {
    const { username, password, email } = req.body;

    if (!username || !password) {
      return res.status(400).json({ success: false, message: '用户名和密码不能为空' });
    }

    const result = await authService.createInitialAdmin(username, password, email);

    if (!result.success) {
      return res.status(400).json(result);
    }

    res.status(201).json(result);
  } catch (error) {
    console.error('Setup error:', error);
    res.status(500).json({ success: false, message: '初始化失败，请稍后重试' });
  }
});

export default router;
