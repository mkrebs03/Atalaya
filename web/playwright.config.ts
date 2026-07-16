import process from 'node:process'
import { defineConfig, devices } from '@playwright/test'

const baseURL = process.env.ATALAYA_E2E_BASE_URL ?? 'http://127.0.0.1:4173'
const serverCommand =
  process.env.ATALAYA_E2E_SERVER_COMMAND ??
  '../build/atalaya server --mode development --listen 127.0.0.1:4173 --public-url http://127.0.0.1:4173'

export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  expect: {
    timeout: 5_000,
  },
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
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
  ],
  webServer: {
    command: serverCommand,
    url: baseURL,
    reuseExistingServer: false,
    timeout: 30_000,
  },
})
