export type Tenant = {
  id: string;
  name: string;
  email: string;
  phone: string | null;
  cpf: string | null;
};

export type Property = {
  id: string;
  name: string;
  address: string;
  city: string;
  state: string;
  zip: string | null;
};

export type Lease = {
  id: string;
  tenant_id: string;
  property_id: string;
  start_date: string;
  end_date: string;
  due_day: string;
  rent_amount: string;
  tenant_name: string;
  property_name: string;
  status: string;
};

export type Receipt = {
  id: string;
  lease_id?: string | null;
  tenant_id?: string | null;
  property_id?: string | null;
  category?: 'rent' | 'extra';
  description?: string | null;
  due_date: string;
  paid_at: string | null;
  amount: string;
  paid_amount?: string | null;
  pdf_path: string | null;
  generated_key: string | null;
  tenant_name: string;
  property_name: string;
};

export type MutationResponse = { ok?: boolean; error?: string };
export type SortDirection = 'asc' | 'desc';
export type SortState = { key: string; direction: SortDirection };
export type PopoverPlacement = { dropup: boolean; maxHeight: number };
export type QueuePayload = Record<string, string>;

export type OfflineMutation = {
  id: string;
  payload: QueuePayload;
  successMessage: string;
};

export type DashboardCache = {
  user: string;
  tenants: Tenant[];
  properties: Property[];
  leases: Lease[];
  receipts: Receipt[];
  savedAt: string;
};

export type PendingIds = {
  tenants: Set<string>;
  properties: Set<string>;
  leases: Set<string>;
  receipts: Set<string>;
};

export const tabs = ['dashboard', 'tenants', 'properties', 'leases', 'receipts'] as const;
export type Tab = (typeof tabs)[number];

export interface MetricsPayload {
  totals: {
    tenants: number;
    properties: number;
    activeLeases: number;
    terminatedLeases: number;
    receipts: number;
    paidReceipts: number;
    partialReceipts: number;
    dueReceipts: number;
    overdueReceipts: number;
    totalAmount: number;
    paidAmount: number;
    openAmount: number;
    overdueAmount: number;
    expectedMonthly: number;
  };
  monthly: Array<{ key: string; label: string; received: number; expected: number; open: number }>;
  status: Array<{ status: string; label: string; count: number }>;
  categories: Array<{ category: string; label: string; count: number; total: number }>;
  properties: Array<{ id: string; name: string; activeLeases: number }>;
  topTenants: Array<{ id: string; name: string; paidTotal: number; receipts: number }>;
}
