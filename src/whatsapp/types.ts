export interface SendResult {
  success: boolean;
  to: string;
  message: string;
  timestamp: string;
  error?: string;
}

export interface WhatsAppStatus {
  isLoggedIn: boolean;
  hasQRCode: boolean;
  qrCodeText?: string;
  chromiumAvailable?: boolean;
  profileDir?: string;
}

export type LoginStatus = 'idle' | 'starting' | 'scan_qr' | 'syncing' | 'connected' | 'error' | 'timeout';

export interface LoginState {
  status: LoginStatus;
  qrCode?: string;
  message?: string;
  error?: string;
}

