import process from 'node:process'
import { expect, test } from '@playwright/test'

const password = process.env.ATALAYA_E2E_PASSWORD ?? 'correct horse battery staple 2026!'

async function login(page: import('@playwright/test').Page) {
  await page.getByLabel('Usuario').fill('admin')
  await page.getByLabel('Contraseña', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Ingresar' }).click()
  await expect(page).toHaveURL('/')
}

async function openAdministration(page: import('@playwright/test').Page, item: string) {
  await page.getByRole('button', { name: /Administración/ }).click()
  await page.getByRole('link', { name: item, exact: true }).click()
}

async function captureViews(
  page: import('@playwright/test').Page,
  viewport: { width: number; height: number },
  suffix: string,
) {
  await page.setViewportSize(viewport)
  await page.emulateMedia({ reducedMotion: 'reduce' })

  await page.goto('/login')
  await expect(page).toHaveScreenshot(`login-${suffix}.png`, { fullPage: true })

  await login(page)
  await expect(page).toHaveScreenshot(`home-${suffix}.png`, {
    fullPage: true,
    mask: [page.locator('.host-card__seen')],
    maskColor: '#ece9df',
  })
  await page.getByRole('link', { name: /Abrir local-agent\. Estado:/ }).click()
  await expect(page).toHaveScreenshot(`host-detail-${suffix}.png`, {
    fullPage: true,
    mask: [page.locator('.host-detail__received')],
    maskColor: '#ece9df',
  })

  await openAdministration(page, 'Cuenta')
  await expect(page).toHaveScreenshot(`account-${suffix}.png`, { fullPage: true })
}

test('preserves the Atalaya visual system', async ({ page }, testInfo) => {
  expect(['chromium', 'mobile-chromium']).toContain(testInfo.project.name)
  const mobile = testInfo.project.name === 'mobile-chromium'
  await captureViews(
    page,
    mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 },
    mobile ? 'mobile' : 'desktop',
  )
})
