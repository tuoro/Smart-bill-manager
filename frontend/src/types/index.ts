export interface Payment {
  id: string;
  trip_id?: string;
  amount: number;
  merchant?: string;
  category?: string;
  payment_method?: string;
  description?: string;
  transaction_time: string;
  screenshot_path?: string;
  extracted_data?: string;
  created_at?: string;
}

export interface Trip {
  id: string;
  name: string;
  start_time: string;
  end_time: string;
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

export interface TripAssignPreview {
  trip_id: string;
  matched_payments: number;
  will_assign: number;
  already_in_this_trip: number;
  assigned_other_trip: number;
  skipped_other_trip_ids?: string[];
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
}

export interface TripPaymentWithInvoices extends Payment {
  invoices: TripPaymentInvoice[];
}

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
  parse_status?: string;
  parse_error?: string;
  raw_text?: string;
  source?: string;
  created_at?: string;
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
  subject?: string;
  from_address?: string;
  received_date?: string;
  has_attachment: number;
  attachment_count: number;
  status: string;
  created_at?: string;
}

export interface DashboardData {
  payments: {
    totalThisMonth: number;
    countThisMonth: number;
    dailyStats: Record<string, number>;
  };
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
