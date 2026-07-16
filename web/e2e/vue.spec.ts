import { test, expect } from '@playwright/test'

test('serves the Phase 0 bootstrap', async ({ page }) => {
  await page.goto('/')

  await expect(page.getByRole('heading', { level: 1 })).toContainText('La torre de vigilancia')
  await expect(page.getByText('Bootstrap operativo')).toBeVisible()
  await expect(page.locator('.foundation-card')).toHaveCount(3)
  await expect(page.getByRole('list')).toBeVisible()

  const skipLink = page.getByRole('link', { name: 'Saltar al contenido' })
  await skipLink.focus()
  await skipLink.press('Enter')
  await expect(page.locator('#main-content')).toBeFocused()
})

test('keeps client-side routes available on a direct visit', async ({ page }) => {
  await page.goto('/ruta-desconocida')

  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Esta ruta no está en el mapa.')
  await expect(page.getByRole('link', { name: /volver/i })).toHaveAttribute('href', '/')
})

test('exposes only process health outside the application shell', async ({ request }) => {
  const health = await request.get('/healthz')

  expect(health.status()).toBe(200)
  expect(health.headers()['content-type']).toBe('application/json')
  expect(health.headers()['cache-control']).toBe('no-store')
  expect(await health.text()).toBe('{"status":"ok"}')

  const missingAPI = await request.get('/api/v1/not-implemented')
  expect(missingAPI.status()).toBe(404)
  expect(missingAPI.headers()['content-type']).not.toContain('text/html')
  expect(await missingAPI.text()).not.toContain('<!doctype html>')

  const missingAsset = await request.get('/favicon.ico')
  expect(missingAsset.status()).toBe(404)
  expect(missingAsset.headers()['content-type']).toContain('text/plain')
  expect(missingAsset.headers()['content-type']).not.toContain('text/html')
  expect(await missingAsset.text()).not.toContain('<!doctype html>')
})
