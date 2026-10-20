import { test, expect, Page } from '@playwright/test'

// Opt-in live tests: point CONSOLE_URL at a disposable sandboxd test instance.
// They create workspaces and run an agent; never point this suite at production.
// Set CONSOLE_PASSWORD if the test instance requires an existing console login.
let createdApp: string | undefined

test.beforeEach(async ({ page }) => {
  createdApp = undefined
  await page.goto('/')
  await expect(page.getByTestId('build-prompt').or(page.getByTestId('login-password'))).toBeVisible()
  if (await page.getByTestId('login-password').isVisible()) {
    if (!process.env.CONSOLE_PASSWORD) throw new Error('Set CONSOLE_PASSWORD for this test instance')
    await page.getByTestId('login-password').fill(process.env.CONSOLE_PASSWORD)
    await page.getByTestId('login-submit').click()
  }
  await expect(page.getByTestId('build-prompt')).toBeVisible()
})

test.afterEach(async ({ page }) => {
  // Only remove the app created by this test, never pre-existing projects.
  if (createdApp) expect((await page.request.delete(`/v1/apps/${createdApp}`)).ok()).toBe(true)
})

async function createBlank(page: Page) {
  const name = `e2e-${Date.now()}`
  await page.getByRole('button', { name: 'Project options' }).click()
  await page.getByTestId('app-name').fill(name)
  const created = page.waitForResponse(
    (response) => response.url().endsWith('/v1/apps') && response.request().method() === 'POST',
  )
  await page.getByRole('button', { name: 'Blank project' }).click()
  createdApp = (await (await created).json()).id
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
}

test('prompt-first home loads', async ({ page }) => {
  await expect(page.getByRole('heading', { name: 'What do you want to build?' })).toBeVisible()
  await expect(page.getByTestId('build-app')).toBeVisible()
})

test('create an app and workspace from the UI', async ({ page }) => {
  await createBlank(page)
  await expect(page.getByTestId('preview')).toBeVisible({ timeout: 90_000 })
  await expect(page.getByTestId('task-prompt')).toBeVisible()
})

test('full lifecycle: preview, task and stop/start', async ({ page }) => {
  test.slow()
  await createBlank(page)
  await expect(page.getByTestId('preview')).toBeVisible({ timeout: 90_000 })
  await page.getByTestId('task-prompt').fill('Reply hello without changing any files.')
  await page.getByTestId('task-send').click()
  await expect(page.locator('.task-status')).toBeVisible({ timeout: 120_000 })
  await page.getByRole('button', { name: 'Project actions' }).click()
  await page.getByRole('button', { name: 'Stop workspace', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Start workspace', exact: true })).toBeVisible({
    timeout: 30_000,
  })
  await page.getByRole('button', { name: 'Start workspace', exact: true }).click()
  await expect(page.getByTestId('preview')).toBeVisible({ timeout: 60_000 })
})
