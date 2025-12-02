import { Router, Request, Response } from 'express';
import multer from 'multer';
import path from 'path';
import { v4 as uuidv4 } from 'uuid';
import { invoiceService } from '../services/invoiceService';

const router = Router();

// Configure multer for file upload
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
    fileSize: 10 * 1024 * 1024 // 10MB limit
  },
  fileFilter: (req, file, cb) => {
    if (file.mimetype === 'application/pdf') {
      cb(null, true);
    } else {
      cb(new Error('只支持PDF文件'));
    }
  }
});

// Get all invoices
router.get('/', (req: Request, res: Response) => {
  try {
    const { limit, offset } = req.query;
    const invoices = invoiceService.getAll({
      limit: limit ? parseInt(limit as string) : undefined,
      offset: offset ? parseInt(offset as string) : undefined
    });
    res.json({ success: true, data: invoices });
  } catch (error) {
    res.status(500).json({ success: false, message: '获取发票列表失败', error: String(error) });
  }
});

// Get invoice statistics
router.get('/stats', (req: Request, res: Response) => {
  try {
    const stats = invoiceService.getStats();
    res.json({ success: true, data: stats });
  } catch (error) {
    res.status(500).json({ success: false, message: '获取统计数据失败', error: String(error) });
  }
});

// Get single invoice by ID
router.get('/:id', (req: Request, res: Response) => {
  try {
    const invoice = invoiceService.getById(req.params.id);
    if (!invoice) {
      return res.status(404).json({ success: false, message: '发票不存在' });
    }
    res.json({ success: true, data: invoice });
  } catch (error) {
    res.status(500).json({ success: false, message: '获取发票失败', error: String(error) });
  }
});

// Get invoices by payment ID
router.get('/payment/:paymentId', (req: Request, res: Response) => {
  try {
    const invoices = invoiceService.getByPaymentId(req.params.paymentId);
    res.json({ success: true, data: invoices });
  } catch (error) {
    res.status(500).json({ success: false, message: '获取发票失败', error: String(error) });
  }
});

// Upload new invoice
router.post('/upload', upload.single('file'), async (req: Request, res: Response) => {
  try {
    if (!req.file) {
      return res.status(400).json({ success: false, message: '请上传文件' });
    }

    const invoice = await invoiceService.create({
      payment_id: req.body.payment_id,
      filename: req.file.filename,
      original_name: req.file.originalname,
      file_path: `uploads/${req.file.filename}`,
      file_size: req.file.size,
      source: 'upload'
    });

    res.status(201).json({ success: true, data: invoice, message: '发票上传成功' });
  } catch (error) {
    res.status(500).json({ success: false, message: '上传发票失败', error: String(error) });
  }
});

// Upload multiple invoices
router.post('/upload-multiple', upload.array('files', 10), async (req: Request, res: Response) => {
  try {
    const files = req.files as Express.Multer.File[];
    if (!files || files.length === 0) {
      return res.status(400).json({ success: false, message: '请上传文件' });
    }

    const invoices = [];
    for (const file of files) {
      const invoice = await invoiceService.create({
        payment_id: req.body.payment_id,
        filename: file.filename,
        original_name: file.originalname,
        file_path: `uploads/${file.filename}`,
        file_size: file.size,
        source: 'upload'
      });
      invoices.push(invoice);
    }

    res.status(201).json({ success: true, data: invoices, message: `成功上传 ${invoices.length} 个发票` });
  } catch (error) {
    res.status(500).json({ success: false, message: '上传发票失败', error: String(error) });
  }
});

// Update invoice
router.put('/:id', (req: Request, res: Response) => {
  try {
    const updated = invoiceService.update(req.params.id, req.body);
    if (!updated) {
      return res.status(404).json({ success: false, message: '发票不存在或更新失败' });
    }
    res.json({ success: true, message: '发票更新成功' });
  } catch (error) {
    res.status(500).json({ success: false, message: '更新发票失败', error: String(error) });
  }
});

// Delete invoice
router.delete('/:id', (req: Request, res: Response) => {
  try {
    const deleted = invoiceService.delete(req.params.id);
    if (!deleted) {
      return res.status(404).json({ success: false, message: '发票不存在' });
    }
    res.json({ success: true, message: '发票删除成功' });
  } catch (error) {
    res.status(500).json({ success: false, message: '删除发票失败', error: String(error) });
  }
});

export default router;
