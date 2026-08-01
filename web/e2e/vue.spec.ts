import process from 'node:process'
import { expect, test } from '@playwright/test'

const password = process.env.ATALAYA_E2E_PASSWORD ?? 'correct horse battery staple 2026!'

async function login(page: import('@playwright/test').Page, value = password) {
  await page.goto('/login')
  await submitLoginForm(page, value)
}

async function submitLoginForm(page: import('@playwright/test').Page, value = password) {
  await page.getByLabel('Usuario').fill('admin')
  await page.getByLabel('Contraseña', { exact: true }).fill(value)
  await page.getByRole('button', { name: 'Ingresar' }).click()
}

async function submitPasswordChange(page: import('@playwright/test').Page) {
  await page.getByLabel('Contraseña actual').fill(password)
  await page.getByLabel('Nueva contraseña', { exact: true }).fill('unused horse battery staple!')
  await page.getByLabel('Confirmar nueva contraseña').fill('unused horse battery staple!')
  await page.getByRole('button', { name: 'Cambiar contraseña' }).click()
}

async function mockAuthenticatedLogin(page: import('@playwright/test').Page) {
  await page.route('**/api/v1/hosts?*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ items: [], next_cursor: null }),
    })
  })
  await page.route('**/api/v1/incidents?*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ items: [], next_cursor: null }),
    })
  })
  await page.route('**/api/v1/alert-rules?*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ items: [], next_cursor: null }),
    })
  })
  await page.route('**/api/v1/auth/login', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        administrator: { id: 'e2e-administrator', username: 'admin' },
        session: {
          idle_expires_at: '2099-01-01T00:00:00Z',
          absolute_expires_at: '2099-01-01T00:00:00Z',
        },
      }),
    })
  })
}

test('protects direct routes and validates credentials', async ({ page }) => {
  await page.goto('/account')
  // The authenticated SPA fallback rejects the document request before Vue runs.
  await expect(page).toHaveURL(/\/login$/)
  await login(page, 'wrong password value')
  await expect(page.getByRole('alert')).toContainText('incorrectos')
  await submitLoginForm(page)
  await expect(page).toHaveURL('/')
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('La torre recibe señal.')
  await expect(page.getByRole('heading', { name: 'Oracle' })).toBeVisible()
  await expect(page.getByText('12,5 %').first()).toBeVisible()
  await page.reload()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('La torre recibe señal.')
})

test('changes password, revokes another session, and logs out', async ({ browser, page }) => {
  const secondContext = await browser.newContext()
  const secondPage = await secondContext.newPage()
  await login(page)
  await login(secondPage)

  await page.getByRole('link', { name: 'Cuenta' }).click()
  await page.getByLabel('Contraseña actual').fill('wrong current password')
  await page.getByLabel('Nueva contraseña', { exact: true }).fill('unused horse battery staple!')
  await page.getByLabel('Confirmar nueva contraseña').fill('unused horse battery staple!')
  await page.getByRole('button', { name: 'Cambiar contraseña' }).click()
  await expect(page.getByRole('alert')).toContainText('actual no es correcta')

  const nextPassword = 'updated horse battery staple 2026!'
  await page.getByLabel('Contraseña actual').fill(password)
  await page.getByLabel('Nueva contraseña', { exact: true }).fill(nextPassword)
  await page.getByLabel('Confirmar nueva contraseña').fill(nextPassword)
  await page.getByRole('button', { name: 'Cambiar contraseña' }).click()
  await expect(page.getByRole('status')).toContainText('actualizada')

  await secondPage.reload()
  await expect(secondPage).toHaveURL(/\/login/)

  await page.getByLabel('Contraseña actual').fill(nextPassword)
  await page.getByLabel('Nueva contraseña', { exact: true }).fill(password)
  await page.getByLabel('Confirmar nueva contraseña').fill(password)
  await page.getByRole('button', { name: 'Cambiar contraseña' }).click()
  await expect(page.getByRole('status')).toContainText('actualizada')
  await page.getByRole('button', { name: 'Cerrar sesión' }).click()
  await expect(page).toHaveURL(/\/login/)
  await secondContext.close()
})

test('authenticated shell remains usable at narrow width', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 })
  await login(page)
  await expect(page.getByRole('navigation', { name: 'Principal' })).toBeVisible()
  await expect
    .poll(() => page.evaluate('document.documentElement.scrollWidth <= window.innerWidth'))
    .toBe(true)
  await page.getByRole('link', { name: /Abrir Oracle\. Estado:/ }).click()
  await expect(page).toHaveURL(/\/hosts\//)
  await expect(page.getByRole('heading', { level: 1, name: 'Oracle' })).toBeVisible()
  await expect(page.getByText('12,5 %').first()).toBeVisible()
  await expect(page.getByText('Direcciones · 100.64.0.10')).toBeVisible()
  await expect(page.getByRole('row', { name: /\/.*ext4.*Presente.*25 %.*96 GiB/ })).toBeVisible()
  await expect
    .poll(() => page.evaluate('document.documentElement.scrollWidth <= window.innerWidth'))
    .toBe(true)
  await page.getByRole('link', { name: 'Cuenta' }).click()
  await expect(page.getByRole('heading', { name: 'Cambiar contraseña' })).toBeVisible()
  await expect.poll(() => page.evaluate('window.scrollY')).toBe(0)
  await expect.poll(() => page.evaluate('document.activeElement?.id')).toBe('main-content')
  await expect
    .poll(() => page.evaluate('document.documentElement.scrollWidth <= window.innerWidth'))
    .toBe(true)
})

test('handles an API 401 during an active SPA operation', async ({ page }) => {
  await mockAuthenticatedLogin(page)
  await page.route('**/api/v1/auth/csrf', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: '{"csrf_token":"first"}',
    })
  })
  await login(page)
  await page.getByRole('link', { name: 'Cuenta' }).click()
  await page.route('**/api/v1/auth/change-password', async (route) => {
    await route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify({ error: { code: 'authentication_required', message: 'required' } }),
    })
  })

  await submitPasswordChange(page)

  await expect(page).toHaveURL(/\/login$/)
  await expect(page.getByRole('button', { name: 'Ingresar' })).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'Principal' })).toHaveCount(0)
})

test('redirects when host polling discovers an expired session', async ({ page }) => {
  await login(page)
  await page.route('**/api/v1/hosts?*', async (route) => {
    await route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify({ error: { code: 'authentication_required' } }),
    })
  })
  await page.reload()
  await expect(page).toHaveURL(/\/login$/)
  await expect(page.getByRole('button', { name: 'Ingresar' })).toBeVisible()
})

test('handles csrf_failed in SPA and fetches a fresh CSRF token', async ({ page }) => {
  let csrfRequests = 0
  let passwordRequests = 0
  await page.route('**/api/v1/auth/csrf', async (route) => {
    csrfRequests += 1
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ csrf_token: `token-${csrfRequests}` }),
    })
  })
  await page.route('**/api/v1/auth/change-password', async (route) => {
    passwordRequests += 1
    await route.fulfill({
      status: 403,
      contentType: 'application/json',
      body: JSON.stringify({
        error:
          passwordRequests === 1
            ? { code: 'csrf_failed', message: 'invalid csrf' }
            : { code: 'current_password_invalid', message: 'invalid password' },
      }),
    })
  })

  await mockAuthenticatedLogin(page)
  await login(page)
  await page.getByRole('link', { name: 'Cuenta' }).click()
  await submitPasswordChange(page)
  await expect(page).toHaveURL(/\/login$/)
  await expect(page.getByRole('button', { name: 'Ingresar' })).toBeVisible()

  await submitLoginForm(page)
  await page.getByRole('link', { name: 'Cuenta' }).click()
  await submitPasswordChange(page)
  await expect(page.getByRole('alert')).toContainText('actual no es correcta')
  expect(csrfRequests).toBe(2)
})

test('exposes only process health publicly', async ({ request }) => {
  const health = await request.get('/healthz')
  expect(health.status()).toBe(200)
  expect(health.headers()['cache-control']).toBe('no-store')
  expect(await health.text()).toBe('{"status":"ok"}')
})
