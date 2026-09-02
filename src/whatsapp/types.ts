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
}
