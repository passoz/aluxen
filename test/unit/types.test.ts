import { describe, it, expect } from 'vitest';
import { Tenant, Property, Lease, Receipt, User, AuthResponse } from '../types';

describe('Types', () => {
  describe('Tenant', () => {
    it('should have correct properties', () => {
      const tenant: Tenant = {
        id: '123e4567-e89b-12d3-a456-426614174000',
        name: 'John Doe',
        email: 'john@example.com',
        phone: '1234567890',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
      };

      expect(tenant.id).toBeDefined();
      expect(tenant.name).toBe('John Doe');
      expect(tenant.email).toBe('john@example.com');
      expect(tenant.phone).toBe('1234567890');
    });

    it('should allow optional phone', () => {
      const tenant: Tenant = {
        id: '123e4567-e89b-12d3-a456-426614174000',
        name: 'Jane Doe',
        email: 'jane@example.com',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
      };

      expect(tenant.phone).toBeUndefined();
    });
  });

  describe('Property', () => {
    it('should have correct properties', () => {
      const property: Property = {
        id: '123e4567-e89b-12d3-a456-426614174001',
        name: 'Downtown Apartment',
        address: '123 Main St',
        city: 'São Paulo',
        state: 'SP',
        zip: '12345-678',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
      };

      expect(property.id).toBeDefined();
      expect(property.name).toBe('Downtown Apartment');
      expect(property.address).toBe('123 Main St');
      expect(property.city).toBe('São Paulo');
      expect(property.state).toBe('SP');
    });
  });

  describe('Lease', () => {
    it('should have correct properties with join data', () => {
      const lease: Lease = {
        id: '123e4567-e89b-12d3-a456-426614174002',
        tenant_id: '123e4567-e89b-12d3-a456-426614174000',
        property_id: '123e4567-e89b-12d3-a456-426614174001',
        start_date: '2024-01-01',
        end_date: '2024-12-31',
        rent_amount: 1500.00,
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
        tenant_name: 'John Doe',
        property_name: 'Downtown Apartment',
      };

      expect(lease.tenant_name).toBe('John Doe');
      expect(lease.property_name).toBe('Downtown Apartment');
      expect(lease.rent_amount).toBe(1500.00);
    });
  });

  describe('Receipt', () => {
    it('should have correct properties', () => {
      const receipt: Receipt = {
        id: '123e4567-e89b-12d3-a456-426614174003',
        lease_id: '123e4567-e89b-12d3-a456-426614174002',
        due_date: '2024-01-10',
        paid_at: '2024-01-08',
        amount: 1500.00,
        pdf_path: '/receipts/123.pdf',
        created_at: '2024-01-01T00:00:00Z',
        tenant_name: 'John Doe',
        property_name: 'Downtown Apartment',
      };

      expect(receipt.paid_at).toBeDefined();
      expect(receipt.pdf_path).toBeDefined();
    });

    it('should allow null paid_at for pending receipts', () => {
      const receipt: Receipt = {
        id: '123e4567-e89b-12d3-a456-426614174003',
        lease_id: '123e4567-e89b-12d3-a456-426614174002',
        due_date: '2024-01-10',
        amount: 1500.00,
        created_at: '2024-01-01T00:00:00Z',
      };

      expect(receipt.paid_at).toBeUndefined();
    });
  });
});