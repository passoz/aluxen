import { expect, test } from '@playwright/test';

const EMPTY_STORAGE = { cookies: [], origins: [] };

let counter = 0;
function uid(): string {
  counter++;
  return `e2e-${counter}-${Date.now()}`;
}

async function login(page: import('@playwright/test').Page) {
  await page.goto('/login');
  await page.getByLabel('Usuário').fill('passoz');
  await page.getByLabel('Senha').fill('Dt32btop@');
  await page.getByRole('button', { name: 'Entrar' }).click();
  await expect(page).toHaveURL(/\/dashboard/);
}

function safeEmail(name: string): string {
  return name.normalize('NFD').replace(/[\u0300-\u036f]/g, '').replace(/\s/g, '').toLowerCase() + '@test.com';
}

async function fillAndWait(page: import('@playwright/test').Page, selector: string, value: string) {
  await page.locator(selector).fill(value);
  await expect(page.locator(selector)).toHaveValue(value);
}

test.describe('Login', () => {
  test.use({ storageState: EMPTY_STORAGE });

  test('login page renders with brand and form fields', async ({ page }) => {
    await page.goto('/login');
    await expect(page.getByText('Aluxen')).toBeVisible();
    await expect(page.getByLabel('Usuário')).toBeVisible();
    await expect(page.getByLabel('Senha')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Entrar' })).toBeVisible();
  });

  test('shows error with invalid credentials', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Usuário').fill('fake');
    await page.getByLabel('Senha').fill('wrong');
    await page.getByRole('button', { name: 'Entrar' }).click();
    await expect(page.getByText('Credenciais inválidas.')).toBeVisible();
  });

  test('shows error with empty fields via HTML5 validation', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Usuário').fill('a');
    await page.getByLabel('Senha').fill('a');
    await page.getByLabel('Usuário').clear();
    await page.getByLabel('Senha').clear();
    await page.getByRole('button', { name: 'Entrar' }).click();
    await expect(page.locator('#username:invalid')).toBeVisible();
  });
});

test.describe('Dashboard UI', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('dashboard loads with correct heading and sidebar', async ({ page }) => {
    await page.goto('/dashboard?tab=tenants');
    await expect(page.locator('h1')).toHaveText('Inquilinos');
    await expect(page.getByRole('link', { name: 'Inquilinos' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Imóveis' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Contratos' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Pagamentos' })).toBeVisible();
  });

  test('navigates between tabs', async ({ page }) => {
    await page.goto('/dashboard?tab=tenants');
    await page.getByRole('link', { name: 'Imóveis' }).click();
    await expect(page).toHaveURL(/tab=properties/);
    await expect(page.locator('h1')).toHaveText('Imóveis');

    await page.getByRole('link', { name: 'Contratos' }).click();
    await expect(page).toHaveURL(/tab=leases/);
    await expect(page.locator('h1')).toHaveText('Contratos');

    await page.getByRole('link', { name: 'Pagamentos' }).click();
    await expect(page).toHaveURL(/tab=receipts/);
    await expect(page.locator('h1')).toHaveText('Pagamentos e Recibos');
  });

  test('toggles sidebar collapse', async ({ page }) => {
    await page.goto('/dashboard?tab=tenants');
    const sidebar = page.locator('nav.sidebar');
    await expect(sidebar).toHaveClass(/open/);
    await page.locator('.menu-btn').click();
    await expect(sidebar).not.toHaveClass(/open/);
    await page.locator('.menu-btn').click();
    await expect(sidebar).toHaveClass(/open/);
  });

  test('toggles theme', async ({ page }) => {
    await page.goto('/dashboard?tab=tenants');
    await page.locator('.theme-toggle').click();
    const theme = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
    expect(theme).toBe('dark');
    await page.locator('.theme-toggle').click();
    const theme2 = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
    expect(theme2).toBe('light');
  });

  test('redirects to dashboard when already authenticated', async ({ page }) => {
    await page.goto('/login');
    await expect(page).toHaveURL(/\/dashboard/);
  });
});

test.describe('Tenants CRUD', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('creates, edits, and deletes a tenant', async ({ page }) => {
    const name = `Maria ${uid()}`;
    const edited = `Maria ${uid()}`;

    await page.goto('/dashboard?tab=tenants');
    await page.getByRole('button', { name: 'Novo inquilino' }).click();
    await fillAndWait(page, '#create-tenant-name', name);
    await fillAndWait(page, '#create-tenant-email', safeEmail(name));
    await page.getByRole('button', { name: 'Adicionar inquilino' }).click();
    await page.getByPlaceholder('Buscar por nome, email ou telefone').fill(name);
    await expect(page.getByText(name).first()).toBeVisible({ timeout: 10000 });

    await page.getByRole('row').filter({ hasText: name }).getByRole('button', { name: 'Editar' }).click();
    await page.locator('#modal-tenant-name').clear();
    await fillAndWait(page, '#modal-tenant-name', edited);
    await page.getByRole('button', { name: 'Salvar alterações' }).click();
    await page.getByPlaceholder('Buscar por nome, email ou telefone').fill(edited);
    await expect(page.getByText(edited).first()).toBeVisible({ timeout: 10000 });

    page.on('dialog', (dialog) => dialog.accept());
    await page.getByRole('row').filter({ hasText: edited }).getByRole('button', { name: 'Excluir' }).click();
    await expect(page.getByRole('cell', { name: edited })).toHaveCount(0);
  });
});

test.describe('Properties CRUD', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('creates, edits, and deletes a property', async ({ page }) => {
    const name = `Casa ${uid()}`;
    const edited = `Casa ${uid()}`;

    await page.goto('/dashboard?tab=properties');
    await page.getByRole('button', { name: 'Novo imóvel' }).click();
    await fillAndWait(page, '#create-property-name', name);
    await fillAndWait(page, '#create-property-address', 'Av. Teste, 100');
    await fillAndWait(page, '#create-property-city', 'Cidade Teste');
    await fillAndWait(page, '#create-property-state', 'TE');
    await page.getByRole('button', { name: 'Adicionar imóvel' }).click();
    await page.getByPlaceholder('Buscar por nome, cidade, estado ou endereço').fill(name);
    await expect(page.getByText(name).first()).toBeVisible({ timeout: 10000 });

    await page.getByRole('row').filter({ hasText: name }).getByRole('button', { name: 'Editar' }).click();
    await page.locator('#modal-property-name').clear();
    await fillAndWait(page, '#modal-property-name', edited);
    await page.getByRole('button', { name: 'Salvar alterações' }).click();
    await page.getByPlaceholder('Buscar por nome, cidade, estado ou endereço').fill(edited);
    await expect(page.getByText(edited).first()).toBeVisible({ timeout: 10000 });

    page.on('dialog', (dialog) => dialog.accept());
    await page.getByRole('row').filter({ hasText: edited }).getByRole('button', { name: 'Excluir' }).click();
    await expect(page.getByText(edited)).toHaveCount(0);
  });
});

test.describe('Full contract lifecycle', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('creates a lease end-to-end', async ({ page }) => {
    const tenant = `João ${uid()}`;
    const property = `Apto ${uid()}`;

    await page.goto('/dashboard?tab=tenants');
    await page.getByRole('button', { name: 'Novo inquilino' }).click();
    await fillAndWait(page, '#create-tenant-name', tenant);
    await fillAndWait(page, '#create-tenant-email', safeEmail(tenant));
    await page.getByRole('button', { name: 'Adicionar inquilino' }).click();
    await page.getByPlaceholder('Buscar por nome, email ou telefone').fill(tenant);
    await expect(page.getByText(tenant).first()).toBeVisible({ timeout: 10000 });

    await page.getByRole('link', { name: 'Imóveis' }).click();
    await page.getByRole('button', { name: 'Novo imóvel' }).click();
    await fillAndWait(page, '#create-property-name', property);
    await fillAndWait(page, '#create-property-address', 'Rua Principal, 123');
    await fillAndWait(page, '#create-property-city', 'São Paulo');
    await fillAndWait(page, '#create-property-state', 'SP');
    await page.getByRole('button', { name: 'Adicionar imóvel' }).click();
    await page.getByPlaceholder('Buscar por nome, cidade, estado ou endereço').fill(property);
    await expect(page.getByText(property).first()).toBeVisible({ timeout: 10000 });

    await page.getByRole('link', { name: 'Contratos' }).click();
    await page.getByRole('button', { name: 'Novo contrato' }).click();

    await page.locator('.custom-select').first().locator('.select-display').click();
    await page.getByRole('button', { name: tenant, exact: true }).click();

    await page.locator('.custom-select').nth(1).locator('.select-display').click();
    await page.getByRole('button', { name: property, exact: true }).click();

    await page.locator('.custom-date-picker').first().locator('.date-picker-display').click();
    await page.locator('.date-picker-days button:not(.blank)').first().click();

    await page.locator('.custom-date-picker').nth(1).locator('.date-picker-display').click();
    await page.locator('.date-picker-header button:last-child').click();
    await page.locator('.date-picker-days button:not(.blank)').last().click();

    await page.locator('#create-lease-rent').fill('2500');
    await page.locator('#create-lease-due-day').fill('5');

    await page.getByRole('button', { name: 'Adicionar contrato' }).click();
    await page.getByPlaceholder('Buscar por inquilino, imóvel ou período').fill(tenant);
    await expect(page.getByText(tenant).first()).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('Ativo').first()).toBeVisible();
  });

  test('terminates, reactivates, and deletes a lease', async ({ page }) => {
    const tenant = `João ${uid()}`;
    const property = `Apto ${uid()}`;

    await page.goto('/dashboard?tab=tenants');
    await page.getByRole('button', { name: 'Novo inquilino' }).click();
    await fillAndWait(page, '#create-tenant-name', tenant);
    await fillAndWait(page, '#create-tenant-email', safeEmail(tenant));
    await page.getByRole('button', { name: 'Adicionar inquilino' }).click();
    await page.getByPlaceholder('Buscar por nome, email ou telefone').fill(tenant);
    await expect(page.getByText(tenant).first()).toBeVisible({ timeout: 10000 });

    await page.getByRole('link', { name: 'Imóveis' }).click();
    await page.getByRole('button', { name: 'Novo imóvel' }).click();
    await fillAndWait(page, '#create-property-name', property);
    await fillAndWait(page, '#create-property-address', 'Rua Principal, 123');
    await fillAndWait(page, '#create-property-city', 'São Paulo');
    await fillAndWait(page, '#create-property-state', 'SP');
    await page.getByRole('button', { name: 'Adicionar imóvel' }).click();
    await page.getByPlaceholder('Buscar por nome, cidade, estado ou endereço').fill(property);
    await expect(page.getByText(property).first()).toBeVisible({ timeout: 10000 });

    await page.getByRole('link', { name: 'Contratos' }).click();
    await page.getByRole('button', { name: 'Novo contrato' }).click();

    await page.locator('.custom-select').first().locator('.select-display').click();
    await page.getByRole('button', { name: tenant, exact: true }).click();

    await page.locator('.custom-select').nth(1).locator('.select-display').click();
    await page.getByRole('button', { name: property, exact: true }).click();

    await page.locator('.custom-date-picker').first().locator('.date-picker-display').click();
    await page.locator('.date-picker-days button:not(.blank)').first().click();

    await page.locator('.custom-date-picker').nth(1).locator('.date-picker-display').click();
    await page.locator('.date-picker-header button:last-child').click();
    await page.locator('.date-picker-days button:not(.blank)').last().click();
    await page.locator('#create-lease-rent').fill('2500');
    await page.locator('#create-lease-due-day').fill('5');
    await page.getByRole('button', { name: 'Adicionar contrato' }).click();
    await page.getByPlaceholder('Buscar por inquilino, imóvel ou período').fill(tenant);
    await expect(page.getByText('Ativo').first()).toBeVisible({ timeout: 10000 });

    await page.evaluate(() => { window.confirm = () => true; });
    await page.getByRole('row').filter({ hasText: tenant }).getByRole('button', { name: 'Encerrar' }).click();
    await expect(page.getByText('Encerrado').first()).toBeVisible({ timeout: 10000 });

    await page.evaluate(() => { window.confirm = () => true; });
    await page.getByRole('row').filter({ hasText: tenant }).getByRole('button', { name: 'Reativar' }).click();
    await expect(page.getByRole('cell', { name: 'Ativo' }).first()).toBeVisible({ timeout: 10000 });

    await page.evaluate(async (t) => {
      window.confirm = () => true;
      const btn = Array.from(document.querySelectorAll('button')).find(b => b.textContent === 'Excluir' && b.closest('tr')?.textContent?.includes(t));
      if (!btn) return;
      const form = btn.closest('form');
      if (!form) return;
      const formData = new FormData(form);
      await fetch('/dashboard', { method: 'POST', body: formData, credentials: 'same-origin' });
    }, tenant);
    await page.waitForTimeout(500);
    await page.goto('/dashboard?tab=leases');
    await page.getByPlaceholder('Buscar por inquilino, imóvel ou período').fill(tenant);
    await expect(page.getByRole('cell', { name: tenant })).toHaveCount(0);
  });
});

test.describe('Receipts flow', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  async function setup(page: import('@playwright/test').Page) {
    const tenant = `Pedro ${uid()}`;
    const property = `Kitnet ${uid()}`;

    await page.goto('/dashboard?tab=tenants');
    await page.getByRole('button', { name: 'Novo inquilino' }).click();
    await fillAndWait(page, '#create-tenant-name', tenant);
    await fillAndWait(page, '#create-tenant-email', safeEmail(tenant));
    await page.getByRole('button', { name: 'Adicionar inquilino' }).click();
    await page.getByPlaceholder('Buscar por nome, email ou telefone').fill(tenant);
    await expect(page.getByText(tenant).first()).toBeVisible({ timeout: 10000 });

    await page.getByRole('link', { name: 'Imóveis' }).click();
    await page.getByRole('button', { name: 'Novo imóvel' }).click();
    await fillAndWait(page, '#create-property-name', property);
    await fillAndWait(page, '#create-property-address', 'Rua das Flores, 50');
    await fillAndWait(page, '#create-property-city', 'Belo Horizonte');
    await fillAndWait(page, '#create-property-state', 'MG');
    await page.getByRole('button', { name: 'Adicionar imóvel' }).click();
    await page.getByPlaceholder('Buscar por nome, cidade, estado ou endereço').fill(property);
    await expect(page.getByText(property).first()).toBeVisible({ timeout: 10000 });

    await page.getByRole('link', { name: 'Contratos' }).click();
    await page.getByRole('button', { name: 'Novo contrato' }).click();
    await page.locator('.custom-select').first().locator('.select-display').click();
    await page.getByRole('button', { name: tenant, exact: true }).click();

    await page.locator('.custom-select').nth(1).locator('.select-display').click();
    await page.getByRole('button', { name: property, exact: true }).click();
    await page.locator('.custom-date-picker').first().locator('.date-picker-display').click();
    await page.locator('.date-picker-days button:not(.blank)').first().click();

    await page.locator('.custom-date-picker').nth(1).locator('.date-picker-display').click();
    await page.locator('.date-picker-header button:last-child').click();
    await page.locator('.date-picker-days button:not(.blank)').last().click();
    await page.locator('#create-lease-rent').fill('1200');
    await page.locator('#create-lease-due-day').fill('5');
    await page.getByRole('button', { name: 'Adicionar contrato' }).click();
    await page.getByPlaceholder('Buscar por inquilino, imóvel ou período').fill(tenant);
    await expect(page.getByText('Ativo').first()).toBeVisible({ timeout: 10000 });

    return { tenant, property };
  }

  test('generates and pays receipts', async ({ page }) => {
    const { tenant } = await setup(page);

    await page.getByRole('link', { name: 'Pagamentos' }).click();
    await page.getByRole('button', { name: 'Gerar pagamentos' }).click();
    await expect(page.getByText(tenant).first()).toBeVisible({ timeout: 10000 });

    // Filter by tenant before picking a row to avoid selecting a different tenant's receipt
    await page.getByPlaceholder('Buscar por contrato, imóvel, vencimento ou valor').fill(tenant);
    const receiptRow = page.locator('table.receipts-table tbody tr').first();
    await expect(receiptRow).toBeVisible();

    const checkbox = receiptRow.locator('input[type="checkbox"]');
    await checkbox.check();
    await page.getByRole('button', { name: /Pagar/ }).waitFor({ state: 'visible', timeout: 5000 });

    await page.evaluate(() => { window.confirm = () => true; });
    await page.getByRole('button', { name: /Pagar/ }).click();
    await expect(page.getByText('Pago').first()).toBeVisible({ timeout: 10000 });
  });

  test('deletes a receipt', async ({ page }) => {
    const { tenant } = await setup(page);

    await page.getByRole('link', { name: 'Pagamentos' }).click();
    await page.getByRole('button', { name: 'Gerar pagamentos' }).click();
    await expect(page.getByText(tenant).first()).toBeVisible({ timeout: 10000 });

    await page.evaluate(() => { window.confirm = () => true; });
    await page.getByRole('button', { name: 'Excluir' }).first().click();
    await expect(page.getByText(tenant)).toHaveCount(0);
  });
});

test.describe('Settings', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('settings page loads with landlord data', async ({ page }) => {
    await page.goto('/settings');
    await expect(page.getByText('Configurações')).toBeVisible();
    await expect(page.getByText('Dados do Locador')).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Alterar Senha' })).toBeVisible();
  });

  test('updates landlord name', async ({ page }) => {
    await page.goto('/settings');
    const nameInput = page.locator('#landlord-name');
    await nameInput.clear();
    await nameInput.fill('Testador E2E');
    await page.getByRole('button', { name: 'Salvar dados' }).click();
    await expect(page.getByText('Dados salvos com sucesso.')).toBeVisible();
  });

  test('back to dashboard link works', async ({ page }) => {
    await page.goto('/settings');
    await page.getByRole('link', { name: 'Voltar ao painel' }).click();
    await expect(page).toHaveURL(/\/dashboard/);
  });
});

test.describe('Logout', () => {
  test.use({ storageState: EMPTY_STORAGE });

  test('logs out and cannot access dashboard', async ({ page }) => {
    await login(page);

    await page.getByRole('button', { name: 'Sair' }).click();
    await expect(page).toHaveURL(/\/login/);

    await page.goto('/dashboard?tab=tenants');
    await expect(page).toHaveURL(/\/login/);
  });
});
