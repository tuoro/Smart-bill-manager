import { v4 as uuidv4 } from 'uuid';
import bcrypt from 'bcryptjs';
import jwt, { SignOptions } from 'jsonwebtoken';
import db from '../models/database';

// Get JWT secret from environment or use a default for development
const JWT_SECRET = process.env.JWT_SECRET || 'smart-bill-manager-secret-key-change-in-production';
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

  // Create default admin user if no users exist
  async ensureAdminExists(): Promise<void> {
    if (!this.hasUsers()) {
      console.log('No users found, creating default admin user...');
      const result = await this.register('admin', 'admin123', 'admin@localhost');
      if (result.success) {
        // Update role to admin
        db.prepare('UPDATE users SET role = ? WHERE username = ?').run('admin', 'admin');
        console.log('Default admin user created: username=admin, password=admin123');
        console.log('⚠️ Please change the default password after first login!');
      }
    }
  }
};
