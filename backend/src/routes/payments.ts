import { Router, Request, Response } from 'express';
import { paymentService } from '../services/paymentService';

const router = Router();

// Get all payments with optional filters
router.get('/', (req: Request, res: Response) => {
  try {
    const { limit, offset, startDate, endDate, category } = req.query;
    const payments = paymentService.getAll({
      limit: limit ? parseInt(limit as string) : undefined,
      offset: offset ? parseInt(offset as string) : undefined,
      startDate: startDate as string,
      endDate: endDate as string,
      category: category as string
    });
    res.json({ success: true, data: payments });
  } catch (error) {
    res.status(500).json({ success: false, message: '获取支付记录失败', error: String(error) });
  }
});

// Get payment statistics
router.get('/stats', (req: Request, res: Response) => {
  try {
    const { startDate, endDate } = req.query;
    const stats = paymentService.getStats(startDate as string, endDate as string);
    res.json({ success: true, data: stats });
  } catch (error) {
    res.status(500).json({ success: false, message: '获取统计数据失败', error: String(error) });
  }
});

// Get single payment by ID
router.get('/:id', (req: Request, res: Response) => {
  try {
    const payment = paymentService.getById(req.params.id);
    if (!payment) {
      return res.status(404).json({ success: false, message: '支付记录不存在' });
    }
    res.json({ success: true, data: payment });
  } catch (error) {
    res.status(500).json({ success: false, message: '获取支付记录失败', error: String(error) });
  }
});

// Create new payment
router.post('/', (req: Request, res: Response) => {
  try {
    const { amount, merchant, category, payment_method, description, transaction_time } = req.body;
    
    if (!amount || !transaction_time) {
      return res.status(400).json({ success: false, message: '金额和交易时间是必填项' });
    }

    const payment = paymentService.create({
      amount: parseFloat(amount),
      merchant,
      category,
      payment_method,
      description,
      transaction_time
    });

    res.status(201).json({ success: true, data: payment, message: '支付记录创建成功' });
  } catch (error) {
    res.status(500).json({ success: false, message: '创建支付记录失败', error: String(error) });
  }
});

// Update payment
router.put('/:id', (req: Request, res: Response) => {
  try {
    const updated = paymentService.update(req.params.id, req.body);
    if (!updated) {
      return res.status(404).json({ success: false, message: '支付记录不存在或更新失败' });
    }
    res.json({ success: true, message: '支付记录更新成功' });
  } catch (error) {
    res.status(500).json({ success: false, message: '更新支付记录失败', error: String(error) });
  }
});

// Delete payment
router.delete('/:id', (req: Request, res: Response) => {
  try {
    const deleted = paymentService.delete(req.params.id);
    if (!deleted) {
      return res.status(404).json({ success: false, message: '支付记录不存在' });
    }
    res.json({ success: true, message: '支付记录删除成功' });
  } catch (error) {
    res.status(500).json({ success: false, message: '删除支付记录失败', error: String(error) });
  }
});

export default router;
