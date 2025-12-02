import { v4 as uuidv4 } from 'uuid';
import db from '../models/database';

export interface Payment {
  id: string;
  amount: number;
  merchant?: string;
  category?: string;
  payment_method?: string;
  description?: string;
  transaction_time: string;
  created_at?: string;
}

export const paymentService = {
  create(payment: Omit<Payment, 'id' | 'created_at'>): Payment {
    const id = uuidv4();
    const stmt = db.prepare(`
      INSERT INTO payments (id, amount, merchant, category, payment_method, description, transaction_time)
      VALUES (?, ?, ?, ?, ?, ?, ?)
    `);
    stmt.run(
      id,
      payment.amount,
      payment.merchant || null,
      payment.category || null,
      payment.payment_method || null,
      payment.description || null,
      payment.transaction_time
    );
    return { id, ...payment };
  },

  getAll(options?: { 
    limit?: number; 
    offset?: number; 
    startDate?: string; 
    endDate?: string;
    category?: string;
  }): Payment[] {
    let query = 'SELECT * FROM payments WHERE 1=1';
    const params: (string | number)[] = [];

    if (options?.startDate) {
      query += ' AND transaction_time >= ?';
      params.push(options.startDate);
    }
    if (options?.endDate) {
      query += ' AND transaction_time <= ?';
      params.push(options.endDate);
    }
    if (options?.category) {
      query += ' AND category = ?';
      params.push(options.category);
    }

    query += ' ORDER BY transaction_time DESC';

    if (options?.limit) {
      query += ' LIMIT ?';
      params.push(options.limit);
      if (options?.offset) {
        query += ' OFFSET ?';
        params.push(options.offset);
      }
    }

    return db.prepare(query).all(...params) as Payment[];
  },

  getById(id: string): Payment | undefined {
    return db.prepare('SELECT * FROM payments WHERE id = ?').get(id) as Payment | undefined;
  },

  update(id: string, payment: Partial<Payment>): boolean {
    const existing = this.getById(id);
    if (!existing) return false;

    const fields = ['amount', 'merchant', 'category', 'payment_method', 'description', 'transaction_time'];
    const updates: string[] = [];
    const params: (string | number | null)[] = [];

    for (const field of fields) {
      if (field in payment) {
        updates.push(`${field} = ?`);
        params.push((payment as Record<string, unknown>)[field] as string | number | null);
      }
    }

    if (updates.length === 0) return false;

    params.push(id);
    const stmt = db.prepare(`UPDATE payments SET ${updates.join(', ')} WHERE id = ?`);
    const result = stmt.run(...params);
    return result.changes > 0;
  },

  delete(id: string): boolean {
    const result = db.prepare('DELETE FROM payments WHERE id = ?').run(id);
    return result.changes > 0;
  },

  getStats(startDate?: string, endDate?: string) {
    let query = 'SELECT * FROM payments WHERE 1=1';
    const params: string[] = [];

    if (startDate) {
      query += ' AND transaction_time >= ?';
      params.push(startDate);
    }
    if (endDate) {
      query += ' AND transaction_time <= ?';
      params.push(endDate);
    }

    const payments = db.prepare(query).all(...params) as Payment[];

    const totalAmount = payments.reduce((sum, p) => sum + p.amount, 0);
    const categoryStats: Record<string, number> = {};
    const merchantStats: Record<string, number> = {};
    const dailyStats: Record<string, number> = {};

    for (const payment of payments) {
      const category = payment.category || '未分类';
      categoryStats[category] = (categoryStats[category] || 0) + payment.amount;

      const merchant = payment.merchant || '未知商家';
      merchantStats[merchant] = (merchantStats[merchant] || 0) + payment.amount;

      const date = payment.transaction_time.split('T')[0];
      dailyStats[date] = (dailyStats[date] || 0) + payment.amount;
    }

    return {
      totalAmount,
      totalCount: payments.length,
      categoryStats,
      merchantStats,
      dailyStats
    };
  }
};
