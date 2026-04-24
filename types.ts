export interface Tenant {
  id: string;
  name: string;
  email: string;
  phone?: string;
  created_at: string;
  updated_at: string;
}

export interface Property {
  id: string;
  name: string;
  address: string;
  city: string;
  state: string;
  zip: string;
  created_at: string;
  updated_at: string;
}

export interface Lease {
  id: string;
  tenant_id: string;
  property_id: string;
  start_date: string;
  end_date: string;
  rent_amount: number;
  created_at: string;
  updated_at: string;
  tenant_name?: string;
  property_name?: string;
}

export interface Receipt {
  id: string;
  lease_id: string;
  due_date: string;
  paid_at?: string;
  amount: number;
  pdf_path?: string;
  created_at: string;
  tenant_name?: string;
  property_name?: string;
}

export interface User {
  id: string;
  username: string;
  created_at: string;
}

export interface AuthResponse {
  token: string;
}