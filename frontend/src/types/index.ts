export interface Payment {
  id: string;
  is_draft?: boolean;
  trip_id?: string;
  trip_assignment_source?: 'auto' | 'manual' | 'blocked' | string;
  trip_assignment_state?: 'assigned' | 'no_match' | 'overlap' | 'blocked' | string;
  bad_debt?: boolean;
  amount: number;
  merchant?: string;
  category?: string;
  payment_method?: string;
  description?: string;
  transaction_time: string;
  transaction_time_ts?: number;
  invoiceCount?: number;
  screenshot_path?: string;
  file_sha256?: string;
  extracted_data?: string;
  dedup_status?: string;
  dedup_ref_id?: string;
  created_at?: string;
}

export interface Trip {
  id: string;
  name: string;
  start_time: string;
  end_time: string;
  start_time_ts?: number;
  end_time_ts?: number;
  timezone?: string;
  reimburse_status?: 'unreimbursed' | 'reimbursed' | string;
  bad_debt_locked?: boolean;
  note?: string;
  created_at?: string;
  updated_at?: string;
}

export interface TripSummary {
  trip_id: string;
  payment_count: number;
  total_amount: number;
  linked_invoices: number;
  unlinked_payments: number;
}

export interface TripCascadePreview {
  trip_id: string;
  payments: number;
  invoices: number;
  unlinked_only: number;
}

export interface TripPaymentInvoice {
  id: string;
  invoice_number?: string;
  invoice_date?: string;
  amount?: number;
  seller_name?: string;
  bad_debt?: boolean;
}

export interface TripPaymentWithInvoices extends Payment {
  invoices: TripPaymentInvoice[];
}

export interface AssignmentChangeSummary {
  range_start_ts: number;
  range_end_ts: number;
  auto_assigned: number;
  auto_unassigned: number;
  manual_blocked_overlaps: number;
}

export interface PendingCandidateTrip {
  id: string;
  name: string;
  start_time: string;
  end_time: string;
  timezone: string;
}

export interface PendingPayment {
  payment: Payment;
  candidates: PendingCandidateTrip[];
}

export interface Invoice {
  id: string;
  is_draft?: boolean;
  payment_id?: string;
  filename: string;
  original_name: string;
  file_path: string;
  file_size?: number;
  file_sha256?: string;
  invoice_number?: string;
  invoice_date?: string;
  amount?: number;
   tax_amount?: number;
  bad_debt?: boolean;
  seller_name?: string;
  buyer_name?: string;
  extracted_data?: string;
  parse_status?: string;
  parse_error?: string;
  raw_text?: string;
  attachments?: InvoiceAttachment[];
  source?: string;
  dedup_status?: string;
  dedup_ref_id?: string;
  created_at?: string;
}

export interface InvoiceAttachment {
  id: string;
  invoice_id: string;
  kind: string;
  filename: string;
  original_name: string;
  file_path: string;
  file_size?: number;
  file_sha256?: string;
  source?: string;
  created_at?: string;
}

export interface DedupCandidate {
  id: string;
  is_draft: boolean;
  amount?: number;
  transaction_time?: string;
  merchant?: string;
  invoice_number?: string;
  invoice_date?: string;
  seller_name?: string;
  created_at?: string;
}

export interface DedupHint {
  kind: 'hash_duplicate' | 'suspected_duplicate' | string;
  reason?: string;
  entity?: 'payment' | 'invoice' | string;
  existing_id?: string;
  existing_is_draft?: boolean;
  candidates?: DedupCandidate[];
}

export interface EmailConfig {
  id: string;
  email: string;
  imap_host: string;
  imap_port: number;
  password: string;
  is_active: number;
  last_check?: string;
  created_at?: string;
}

export interface EmailLog {
  id: string;
  email_config_id: string;
  mailbox?: string;
  message_uid?: number;
  subject?: string;
  from_address?: string;
  received_date?: string;
  has_attachment: number;
  attachment_count: number;
  invoice_xml_url?: string;
  invoice_pdf_url?: string;
  parsed_invoice_id?: string;
  parse_error?: string;
  status: string;
  created_at?: string;
}

export interface DashboardData {
  payments: {
    totalThisMonth: number;
    countThisMonth: number;
    dailyStats: Record<string, number>;
  };
  recentPayments: (Payment & { invoiceCount: number })[];
  invoices: {
    totalCount: number;
    totalAmount: number;
    bySource: Record<string, number>;
  };
  email: {
    monitoringStatus: { configId: string; status: string }[];
    recentLogs: EmailLog[];
  };
}

export interface User {
  id: string;
  username: string;
  email?: string;
  role: string;
  is_active: number;
  created_at?: string;
  updated_at?: string;
}

export interface AuthResult {
  success: boolean;
  message: string;
  user?: User;
  token?: string;
}

export interface ApiResponse<T> {
  success: boolean;
  data?: T;
  message?: string;
  error?: string;
}
