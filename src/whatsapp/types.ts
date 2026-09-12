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

/** Etapas observáveis pelo usuário durante o envio automático (sem detalhes técnicos). */
export type SendStage = 'preparing' | 'connecting' | 'opening_chat' | 'sending' | 'confirming' | 'done';

export type SendJobStatus = 'running' | 'done' | 'error';

export interface SendJob {
  id: string;
  userId: string;
  status: SendJobStatus;
  stage: SendStage;
  result?: SendResult;
  error?: string;
  createdAt: number;
  updatedAt: number;
}

export type LoginStatus = 'idle' | 'starting' | 'scan_qr' | 'syncing' | 'connected' | 'error' | 'timeout';

export interface LoginState {
  status: LoginStatus;
  qrCode?: string;
  message?: string;
  error?: string;
}

