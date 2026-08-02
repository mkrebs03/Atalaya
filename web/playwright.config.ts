import process from 'node:process'
import { defineConfig, devices } from '@playwright/test'

const baseURL = process.env.ATALAYA_E2E_BASE_URL ?? 'http://127.0.0.1:4173'
const serverCommand =
  process.env.ATALAYA_E2E_SERVER_COMMAND ??
  '../build/atalaya server --config ../build/e2e/server.yaml'

export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  expect: {
    timeout: 5_000,
    toHaveScreenshot: {
      animations: 'disabled',
      caret: 'hide',
      maxDiffPixelRatio: 0.005,
    },
  },
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  // All projects exercise the same single-administrator database and restore its password.
  workers: 1,
  reporter: process.env.CI ? 'line' : 'html',
  use: {
    baseURL,
    headless: true,
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'mobile-chromium',
      use: { ...devices['Pixel 7'] },
    },
  ],
  webServer: {
    command: serverCommand,
    url: baseURL,
    reuseExistingServer: process.env.ATALAYA_E2E_REUSE_SERVER === 'true',
    timeout: 30_000,
  },
})
