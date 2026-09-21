/** Ambiente tipado das rotas Hono — variáveis de request compartilhadas entre middlewares e handlers. */
export type AppEnv = {
  Variables: {
    /** Identificador curto do request, usado para correlacionar logs e auditoria. */
    requestId: string;
    /** Memo por requisição: administrador ainda com a senha padrão (troca obrigatória). */
    mustChangePassword?: boolean;
  };
};
