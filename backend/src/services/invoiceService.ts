import { v4 as uuidv4 } from 'uuid';
import db from '../models/database';
import fs from 'fs';
import path from 'path';
// eslint-disable-next-line @typescript-eslint/no-require-imports
const pdf = require('pdf-parse');

export interface Invoice {
  id: string;
  payment_id?: string;
  filename: string;
  original_name: string;
  file_path: string;
  file_size?: number;
  invoice_number?: string;
  invoice_date?: string;
  amount?: number;
  seller_name?: string;
  buyer_name?: string;
  extracted_data?: string;
  source?: string;
  created_at?: string;
}

export const invoiceService = {
  async create(invoiceData: {
    payment_id?: string;
    filename: string;
    original_name: string;
    file_path: string;
    file_size?: number;
    source?: string;
  }): Promise<Invoice> {
    const id = uuidv4();
    
    // Try to extract data from PDF
    let extractedData: Record<string, unknown> = {};
    let invoiceNumber: string | undefined;
    let invoiceDate: string | undefined;
    let amount: number | undefined;
    let sellerName: string | undefined;
    let buyerName: string | undefined;

    try {
      const filePath = path.isAbsolute(invoiceData.file_path) 
        ? invoiceData.file_path 
        : path.join(__dirname, '../../', invoiceData.file_path);
      
      if (fs.existsSync(filePath) && invoiceData.filename.toLowerCase().endsWith('.pdf')) {
        const dataBuffer = fs.readFileSync(filePath);
        const pdfData = await pdf(dataBuffer);
        const text = pdfData.text;
        
        // Extract invoice information using regex patterns (Chinese invoice format)
        const invoiceNumberMatch = text.match(/发票号码[：:]\s*(\d+)/);
        const invoiceDateMatch = text.match(/开票日期[：:]\s*(\d{4}年\d{1,2}月\d{1,2}日|\d{4}-\d{2}-\d{2})/);
        const amountMatch = text.match(/合计金额[（(]小写[)）][：:]\s*[¥￥]?([\d.]+)|价税合计[（(]大写[)）].*?[¥￥]([\d.]+)/);
        const sellerMatch = text.match(/销售方[：:]?\s*名称[：:]\s*([^\n]+)|销售方名称[：:]\s*([^\n]+)/);
        const buyerMatch = text.match(/购买方[：:]?\s*名称[：:]\s*([^\n]+)|购买方名称[：:]\s*([^\n]+)/);
        
        invoiceNumber = invoiceNumberMatch?.[1];
        invoiceDate = invoiceDateMatch?.[1];
        amount = amountMatch ? parseFloat(amountMatch[1] || amountMatch[2]) : undefined;
        sellerName = sellerMatch?.[1] || sellerMatch?.[2];
        buyerName = buyerMatch?.[1] || buyerMatch?.[2];
        
        extractedData = {
          text: text.substring(0, 2000), // Store first 2000 chars
          pageCount: pdfData.numpages,
          info: pdfData.info
        };
      }
    } catch (error) {
      console.error('Error parsing PDF:', error);
    }

    const stmt = db.prepare(`
      INSERT INTO invoices (id, payment_id, filename, original_name, file_path, file_size, 
        invoice_number, invoice_date, amount, seller_name, buyer_name, extracted_data, source)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `);

    stmt.run(
      id,
      invoiceData.payment_id || null,
      invoiceData.filename,
      invoiceData.original_name,
      invoiceData.file_path,
      invoiceData.file_size || null,
      invoiceNumber || null,
      invoiceDate || null,
      amount || null,
      sellerName || null,
      buyerName || null,
      JSON.stringify(extractedData),
      invoiceData.source || 'upload'
    );

    return {
      id,
      ...invoiceData,
      invoice_number: invoiceNumber,
      invoice_date: invoiceDate,
      amount,
      seller_name: sellerName,
      buyer_name: buyerName,
      extracted_data: JSON.stringify(extractedData)
    };
  },

  getAll(options?: { limit?: number; offset?: number }): Invoice[] {
    let query = 'SELECT * FROM invoices ORDER BY created_at DESC';
    const params: number[] = [];

    if (options?.limit) {
      query += ' LIMIT ?';
      params.push(options.limit);
      if (options?.offset) {
        query += ' OFFSET ?';
        params.push(options.offset);
      }
    }

    return db.prepare(query).all(...params) as Invoice[];
  },

  getById(id: string): Invoice | undefined {
    return db.prepare('SELECT * FROM invoices WHERE id = ?').get(id) as Invoice | undefined;
  },

  getByPaymentId(paymentId: string): Invoice[] {
    return db.prepare('SELECT * FROM invoices WHERE payment_id = ?').all(paymentId) as Invoice[];
  },

  update(id: string, data: Partial<Invoice>): boolean {
    const existing = this.getById(id);
    if (!existing) return false;

    const fields = ['payment_id', 'invoice_number', 'invoice_date', 'amount', 'seller_name', 'buyer_name'];
    const updates: string[] = [];
    const params: (string | number | null)[] = [];

    for (const field of fields) {
      if (field in data) {
        updates.push(`${field} = ?`);
        params.push((data as Record<string, unknown>)[field] as string | number | null);
      }
    }

    if (updates.length === 0) return false;

    params.push(id);
    const stmt = db.prepare(`UPDATE invoices SET ${updates.join(', ')} WHERE id = ?`);
    const result = stmt.run(...params);
    return result.changes > 0;
  },

  delete(id: string): boolean {
    const invoice = this.getById(id);
    if (!invoice) return false;

    // Delete the file
    try {
      const filePath = path.isAbsolute(invoice.file_path) 
        ? invoice.file_path 
        : path.join(__dirname, '../../', invoice.file_path);
      if (fs.existsSync(filePath)) {
        fs.unlinkSync(filePath);
      }
    } catch (error) {
      console.error('Error deleting file:', error);
    }

    const result = db.prepare('DELETE FROM invoices WHERE id = ?').run(id);
    return result.changes > 0;
  },

  getStats() {
    const invoices = db.prepare('SELECT * FROM invoices').all() as Invoice[];
    const totalAmount = invoices.reduce((sum, inv) => sum + (inv.amount || 0), 0);
    const bySource: Record<string, number> = {};
    const byMonth: Record<string, number> = {};

    for (const invoice of invoices) {
      const source = invoice.source || 'unknown';
      bySource[source] = (bySource[source] || 0) + 1;

      if (invoice.invoice_date) {
        const month = invoice.invoice_date.substring(0, 7);
        byMonth[month] = (byMonth[month] || 0) + (invoice.amount || 0);
      }
    }

    return {
      totalCount: invoices.length,
      totalAmount,
      bySource,
      byMonth
    };
  }
};
