import { expect, test } from '@playwright/test';

test('login page renders with seeded credentials hint', async ({ page }) => {
  await page.goto('/login');

  await expect(page.getByText('Aluxen')).toBeVisible();
  await expect(page.getByLabel('Usuário')).toBeVisible();
  await expect(page.getByLabel('Senha')).toBeVisible();
  await expect(page.getByText('admin123')).toBeVisible();
});

test('seeded user can log in and open sidebar', async ({ page }) => {
  await page.goto('/login');
  await page.getByLabel('Usuário').fill('admin');
  await page.getByLabel('Senha').fill('admin123');
  await page.getByRole('button', { name: 'Entrar' }).click();

  await expect(page).toHaveURL(/\/dashboard\?tab=tenants/);
  await expect(page.locator('h1')).toHaveText('Inquilinos');

  const sidebar = page.locator('nav.sidebar');
  await page.locator('.menu-btn').click();
  await expect(sidebar).toHaveClass(/open/);
});
