import { v4 as uuidv4 } from 'uuid';
import bcrypt from 'bcryptjs';
import jwt, { SignOptions } from 'jsonwebtoken';
import crypto from 'crypto';
import db from '../models/database';

// Get JWT secret from environment or generate one if not in production
const getJwtSecret = (): string => {
  const envSecret = process.env.JWT_SECRET;
  if (envSecret) {
    return envSecret;
  }
  
  // In production, require JWT_SECRET to be set
  if (process.env.NODE_ENV === 'production') {
    console.warn('⚠️ WARNING: JWT_SECRET not set in production. Using generated secret (will change on restart).');
  }
  
  // Generate a random secret for development/testing
  return crypto.randomBytes(32).toString('hex');
};

const JWT_SECRET = getJwtSecret();
const JWT_EXPIRES_IN = '7d';

export interface User {
  id: string;
  username: string;
  password?: string;
  email?: string;
  role: string;
  is_active: number;
  created_at?: string;
  updated_at?: string;
}

export interface AuthResult {
  success: boolean;
  message: string;
  user?: Omit<User, 'password'>;
  token?: string;
}

const signOptions: SignOptions = {
  expiresIn: JWT_EXPIRES_IN
};

export const authService = {
  // Create a new user
  async register(username: string, password: string, email?: string): Promise<AuthResult> {
    // Check if username already exists
    const existingUser = db.prepare('SELECT id FROM users WHERE username = ?').get(username);
    if (existingUser) {
      return { success: false, message: '用户名已存在' };
    }

    // Hash the password
    const salt = await bcrypt.genSalt(10);
    const hashedPassword = await bcrypt.hash(password, salt);

    // Create user
    const id = uuidv4();
    const stmt = db.prepare(`
      INSERT INTO users (id, username, password, email, role, is_active)
      VALUES (?, ?, ?, ?, 'user', 1)
    `);
    stmt.run(id, username, hashedPassword, email || null);

    const user: Omit<User, 'password'> = {
      id,
      username,
      email,
      role: 'user',
      is_active: 1
    };

    // Generate JWT token
    const token = jwt.sign({ userId: id, username, role: 'user' }, JWT_SECRET, signOptions);

    return {
      success: true,
      message: '注册成功',
      user,
      token
    };
  },

  // User login
  async login(username: string, password: string): Promise<AuthResult> {
    // Find user
    const user = db.prepare('SELECT * FROM users WHERE username = ? AND is_active = 1').get(username) as User | undefined;
    
    if (!user) {
      return { success: false, message: '用户名或密码错误' };
    }

    // Verify password
    const isValid = await bcrypt.compare(password, user.password || '');
    if (!isValid) {
      return { success: false, message: '用户名或密码错误' };
    }

    // Generate JWT token
    const token = jwt.sign({ userId: user.id, username: user.username, role: user.role }, JWT_SECRET, signOptions);

    // Remove password from response
    const { password: _, ...userWithoutPassword } = user;

    return {
      success: true,
      message: '登录成功',
      user: userWithoutPassword,
      token
    };
  },

  // Verify JWT token
  verifyToken(token: string): { valid: boolean; decoded?: { userId: string; username: string; role: string } } {
    try {
      const decoded = jwt.verify(token, JWT_SECRET) as { userId: string; username: string; role: string };
      return { valid: true, decoded };
    } catch {
      return { valid: false };
    }
  },

  // Get user by ID
  getUserById(id: string): Omit<User, 'password'> | undefined {
    const user = db.prepare('SELECT id, username, email, role, is_active, created_at, updated_at FROM users WHERE id = ?').get(id) as Omit<User, 'password'> | undefined;
    return user;
  },

  // Get all users (admin only)
  getAllUsers(): Omit<User, 'password'>[] {
    return db.prepare('SELECT id, username, email, role, is_active, created_at, updated_at FROM users').all() as Omit<User, 'password'>[];
  },

  // Update user password
  async updatePassword(userId: string, oldPassword: string, newPassword: string): Promise<AuthResult> {
    const user = db.prepare('SELECT * FROM users WHERE id = ?').get(userId) as User | undefined;
    
    if (!user) {
      return { success: false, message: '用户不存在' };
    }

    // Verify old password
    const isValid = await bcrypt.compare(oldPassword, user.password || '');
    if (!isValid) {
      return { success: false, message: '原密码错误' };
    }

    // Hash new password
    const salt = await bcrypt.genSalt(10);
    const hashedPassword = await bcrypt.hash(newPassword, salt);

    // Update password
    db.prepare('UPDATE users SET password = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?').run(hashedPassword, userId);

    return { success: true, message: '密码修改成功' };
  },

  // Check if any users exist (for initial setup)
  hasUsers(): boolean {
    const count = db.prepare('SELECT COUNT(*) as count FROM users').get() as { count: number };
    return count.count > 0;
  },

  // Generate a secure random password using rejection sampling to avoid bias
  generateSecurePassword(length: number = 12): string {
    const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789!@#$%';
    const charsLength = chars.length;
    // Calculate the maximum value that doesn't introduce bias
    const maxValid = Math.floor(256 / charsLength) * charsLength;
    
    let password = '';
    while (password.length < length) {
      const randomByte = crypto.randomBytes(1)[0];
      // Rejection sampling: reject values that would cause bias
      if (randomByte < maxValid) {
        password += chars[randomByte % charsLength];
      }
    }
    return password;
  },

  // Create initial admin user (only during setup)
  async createInitialAdmin(username: string, password: string, email?: string): Promise<AuthResult> {
    // Only allow if no users exist
    if (this.hasUsers()) {
      return { success: false, message: '系统已初始化，无法重复设置' };
    }

    // Validate username and password
    if (username.length < 3 || username.length > 50) {
      return { success: false, message: '用户名长度应为3-50个字符' };
    }

    if (password.length < 6) {
      return { success: false, message: '密码长度至少6个字符' };
    }

    const result = await this.register(username, password, email);
    if (result.success && result.user) {
      // Update role to admin
      db.prepare('UPDATE users SET role = ? WHERE username = ?').run('admin', username);
      console.log('=========================================');
      console.log('Admin user created via setup:');
      console.log(`  Username: ${username}`);
      console.log('=========================================');
    }
    return result;
  }
};
