import { test, expect } from '@playwright/test'
import type { ModelProvider, ModelProviderInput } from '../src/api'

for (const protocol of ['openai-completions', 'anthropic-messages'] as const) {
  test(`configure ${protocol}, select Pi and OpenCode, submit the chosen model`, async ({ page }, testInfo) => {
    let providers: ModelProvider[] = []
    let task: Record<string, unknown> | undefined
    const browserErrors: string[] = []
    page.on('pageerror', (e) => browserErrors.push(e.message))
    const app = { id: 'app1', name: 'Provider playground', description: '', tags: [], current_sandbox_id: 'sandbox1', created_at: '', updated_at: '' }
    await page.route('**/v1/**', async (route) => {
      const path = new URL(route.request().url()).pathname
      const method = route.request().method()
      const json = (body: unknown, status = 200) => route.fulfill({ status, json: body })
      if (path === '/v1/auth/status') return json({ enabled: false, authenticated: true, password_set: false })
      if (path === '/v1/settings') return json({
        version: 'test', update_available: false, networking: { preview_domain: 'example.com', preview_tls: true },
        auth: { enabled: true }, runtime: { storage_mode: 'directory', base_image: 'sandboxd-base:test' },
        lifecycle: { idle_reap_enabled: true, idle_threshold_seconds: 2100, keepalive_max_seconds: 86400 },
        egress: { mode: 'disabled' }, agents: { providers: ['opencode', 'pi'], default_models: {} },
        presets: [], capabilities: {}, editable: ['agents.default_models'],
      })
      if (path === '/v1/model-providers') {
        if (method === 'POST') {
          const input = route.request().postDataJSON() as ModelProviderInput
          expect(input.api_key).toBe('test-only-upstream-key')
          const { api_key, ...publicFields } = input
          providers = [{ ...publicFields, id: 'connection1', has_api_key: !!api_key }]
          return json(providers[0], 201)
        }
        return json({ providers })
      }
      if (path.endsWith('/test')) return json({ ok: true })
      if (path === '/v1/agents') return json({ providers: ['opencode', 'pi'].map((id) => ({
        id, label: id === 'pi' ? 'Pi' : 'OpenCode', installed_state: 'installed', status: 'needs_login',
        method: '', supports_oauth: false, supports_api_key: false, runnable: true,
      })) })
      if (path === '/v1/apps') return json({ apps: [app] })
      if (path === '/v1/apps/app1') return json(app)
      if (path === '/v1/sandboxes/sandbox1') return json({ id: 'sandbox1', status: 'running', preview: { url: '', status: 'none' }, processes: [] })
      if (path === '/v1/sandboxes/sandbox1/tasks') {
        if (method === 'POST') { task = route.request().postDataJSON(); return json({ id: 'task1' }, 202) }
        return json({ tasks: task ? [{ id: 'task1', status: 'succeeded', prompt: task.prompt, agent_message: 'Done.' }] : [] })
      }
      if (path.endsWith('/tasks/task1/events')) return route.fulfill({
        status: 200, contentType: 'text/event-stream', body: 'event: done\ndata: {"status":"succeeded"}\n\n',
      })
      if (path.endsWith('/tasks/task1')) return json({ id: 'task1', status: 'succeeded' })
      if (path.endsWith('/runtime-inspect')) return json({ suggestions: [], existing_manifest: { present: true } })
      if (path.endsWith('/runtime/manifest')) return json({ present: true })
      if (path.endsWith('/events')) return json({ events: [] })
      if (path.endsWith('/files/content')) return route.fulfill({ status: 404 })
      if (path.endsWith('/files')) return json({ files: [] })
      if (path === '/v1/git-credentials') return json({ credentials: [] })
      if (path === '/v1/api-keys') return json({ keys: [] })
      if (path === '/v1/presets') return json({ presets: [] })
      return json({})
    })
    await page.goto('/')
    await page.getByTestId('nav-settings').click()
    await page.getByRole('button', { name: 'Add connection' }).click()
    await page.getByLabel('Connection name').fill('Community gateway')
    await page.getByLabel('API format').selectOption(protocol)
    await page.getByLabel(/API base URL/).fill('http://gateway:4000/v1')
    await page.getByLabel(/API key \(optional/).fill('test-only-upstream-key')
    await page.getByLabel('Model ID 1').fill('team/local-coder:latest')
    await page.getByRole('button', { name: 'Add model', exact: true }).click()
    await page.getByLabel('Model ID 2').fill('team/second-model')
    await page.screenshot({ path: testInfo.outputPath('connection-form.png'), fullPage: true })
    await page.getByRole('button', { name: 'Save connection' }).click()
    await expect(page.getByText('API key saved')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save connection' })).toHaveCount(0)
    await page.getByRole('button', { name: 'Test model', exact: true }).click()
    await expect(page.getByText('Model responded successfully.')).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath('connections.png'), fullPage: true })
    await page.getByTestId('nav-apps').click()
    await page.getByRole('button', { name: 'Manage', exact: true }).click()
    await page.getByTestId('task-agent').selectOption('pi')
    await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeDisabled()
    // Enter must respect the same connection-required gate as the Send button.
    await page.getByTestId('task-prompt').fill('Should not submit without a connection')
    await page.getByTestId('task-prompt').press('Enter')
    expect(task).toBeUndefined()
    await page.getByTestId('task-provider').selectOption('connection1')
    await page.getByTestId('task-model').selectOption('team/second-model')
    await page.getByTestId('task-prompt').fill('Create a small app')
    await page.getByRole('button', { name: 'Send', exact: true }).click()
    await expect.poll(() => task).toEqual({
      prompt: 'Create a small app', agent: 'pi', provider: 'connection1', model: 'team/second-model', continue: true,
    })
    await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeEnabled()
    await page.getByTestId('task-agent').selectOption('opencode')
    await expect(page.getByTestId('task-provider')).toHaveValue('connection1')
    await expect(page.getByTestId('task-model')).toHaveValue('')
    await page.getByTestId('task-prompt').fill('Continue with OpenCode')
    await page.getByRole('button', { name: 'Send', exact: true }).click()
    await expect.poll(() => task?.agent).toBe('opencode')
    expect(task?.provider).toBe('connection1')
    expect(task?.model).toBeUndefined() // API selects connection's first model
    await page.screenshot({ path: testInfo.outputPath('agent-picker.png'), fullPage: true })
    expect(browserErrors).toEqual([])
  })
}
