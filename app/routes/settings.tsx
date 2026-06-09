import { Form, useActionData, useLoaderData, useNavigation } from '@remix-run/react';
import { json, redirect } from '@remix-run/node';
import type { ActionFunctionArgs, LoaderFunctionArgs, MetaFunction } from '@remix-run/node';
import { requireUserId } from '../utils/session.server';
import { getSetting, setSetting, queryOne } from '../utils/db.server';
import { changePassword } from '../utils/auth.server';

export const meta: MetaFunction = () => [{ title: 'Aluxen | Configurações' }];

type LoaderData = {
  landlordName: string;
  landlordCpf: string;
  landlordEmail: string;
  username: string;
};

export async function loader({ request }: LoaderFunctionArgs) {
  const userId = await requireUserId(request);
  const user = await queryOne<{ username: string }>('SELECT username FROM users WHERE id = $1', [userId]);
  return json<LoaderData>({
    landlordName: await getSetting('landlord_name') || 'Fernando Passos',
    landlordCpf: await getSetting('landlord_cpf') || '075.898.017-55',
    landlordEmail: await getSetting('landlord_email') || '',
    username: user?.username || '',
  });
}

export async function action({ request }: ActionFunctionArgs) {
  const userId = await requireUserId(request);
  const formData = await request.formData();
  const intent = String(formData.get('intent') || '');

  try {
    if (intent === 'save-landlord') {
      const name = String(formData.get('name') || '').trim();
      const cpf = String(formData.get('cpf') || '').trim();
      const email = String(formData.get('email') || '').trim();
      if (!name) throw new Error('Nome do locador é obrigatório.');
      await setSetting('landlord_name', name);
      await setSetting('landlord_cpf', cpf);
      await setSetting('landlord_email', email);
      return json({ ok: true, message: 'Dados salvos com sucesso.' });
    }

    if (intent === 'change-password') {
      const currentPassword = String(formData.get('current_password') || '');
      const newPassword = String(formData.get('new_password') || '');
      const confirmPassword = String(formData.get('confirm_password') || '');
      if (newPassword !== confirmPassword) throw new Error('Nova senha e confirmação não conferem.');
      await changePassword(userId, currentPassword, newPassword);
      return json({ ok: true, message: 'Senha alterada com sucesso.' });
    }

    return json({ error: 'Ação inválida.' }, { status: 400 });
  } catch (error) {
    return json({ error: error instanceof Error ? error.message : 'Erro ao salvar.' }, { status: 400 });
  }
}

export default function SettingsRoute() {
  const data = useLoaderData<typeof loader>();
  const actionData = useActionData<typeof action>();
  const navigation = useNavigation();
  const busy = navigation.state === 'submitting';

  return (
    <div className="settings-page">
      <div className="card stack-lg" style={{ maxWidth: 560, margin: '2rem auto' }}>
        <h1 style={{ fontSize: '1.5rem' }}>Configurações</h1>

        {actionData?.message ? <div className="success" style={{ padding: '0.75rem 1rem', background: 'var(--color2)', borderRadius: 8 }}>{actionData.message}</div> : null}
        {actionData?.error ? <div className="error" style={{ marginTop: 0 }}>{actionData.error}</div> : null}

        <div className="stack">
          <h2 style={{ fontSize: '1.1rem' }}>Dados do Locador</h2>
          <Form method="post" className="stack">
            <input type="hidden" name="intent" value="save-landlord" />
            <div className="form-group">
              <label htmlFor="landlord-name">Nome</label>
              <input id="landlord-name" name="name" defaultValue={data.landlordName} required />
            </div>
            <div className="form-group">
              <label htmlFor="landlord-cpf">CPF</label>
              <input id="landlord-cpf" name="cpf" defaultValue={data.landlordCpf} />
            </div>
            <div className="form-group">
              <label htmlFor="landlord-email">Email</label>
              <input id="landlord-email" name="email" type="email" defaultValue={data.landlordEmail} />
            </div>
            <button type="submit" className="btn btn-primary" disabled={busy}>Salvar dados</button>
          </Form>
        </div>

        <hr style={{ border: 'none', borderTop: '1px solid var(--glass-border)' }} />

        <div className="stack">
          <h2 style={{ fontSize: '1.1rem' }}>Alterar Senha</h2>
          <Form method="post" className="stack">
            <input type="hidden" name="intent" value="change-password" />
            <div className="form-group">
              <label htmlFor="current-password">Senha atual</label>
              <input id="current-password" name="current_password" type="password" required />
            </div>
            <div className="form-group">
              <label htmlFor="new-password">Nova senha</label>
              <input id="new-password" name="new_password" type="password" required minLength={4} />
            </div>
            <div className="form-group">
              <label htmlFor="confirm-password">Confirmar nova senha</label>
              <input id="confirm-password" name="confirm_password" type="password" required minLength={4} />
            </div>
            <button type="submit" className="btn btn-primary" disabled={busy}>Alterar senha</button>
          </Form>
        </div>

        <div style={{ textAlign: 'center' }}>
          <a href="/dashboard?tab=tenants" className="btn btn-sm btn-secondary">Voltar ao painel</a>
        </div>
      </div>
    </div>
  );
}
