import { test, expect } from '@playwright/test'

// These drive the real console + sandboxd stack
// (docker compose --profile console up). The lifecycle test creates a
// sandbox, which builds + installs on first boot, so it is slow.

test('app list loads', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Apps' })).toBeVisible()
  await expect(page.getByTestId('create-app')).toBeVisible()
})

test('create app from the UI', async ({ page }) => {
  await page.goto('/')
  const name = `e2e-${Date.now()}`
  await page.getByTestId('app-name').fill(name)
  await page.getByTestId('create-app').click()
  // Navigates to the new app's detail.
  await expect(page.getByRole('heading', { name })).toBeVisible()
  await expect(page.getByTestId('create-sandbox')).toBeVisible()
})

test('full lifecycle: sandbox → preview → task → stop/start', async ({ page }) => {
  test.slow()
  await page.goto('/')
  const name = `e2e-life-${Date.now()}`
  await page.getByTestId('app-name').fill(name)
  await page.getByTestId('create-app').click()
  await expect(page.getByRole('heading', { name })).toBeVisible()

  // Create the app's sandbox.
  await page.getByTestId('create-sandbox').click()

  // Preview iframe appears once the dev server is up (install + vite).
  await expect(page.getByTestId('preview')).toBeVisible({ timeout: 90_000 })
  await expect(page.getByTestId('status')).toContainText('running')

  // Submit a task and see a status badge appear (running → terminal).
  await page.getByTestId('task-prompt').fill('say hello')
  await page.getByTestId('run-task').click()
  await expect(page.getByTestId('task-status')).toBeVisible({ timeout: 30_000 })

  // Stop → state flips to stopped; Start brings it back.
  await page.getByTestId('stop').click()
  await expect(page.getByTestId('status')).toContainText('stopped', { timeout: 30_000 })
  await page.getByTestId('start').click()
  await expect(page.getByTestId('status')).toContainText('running', { timeout: 60_000 })
})
