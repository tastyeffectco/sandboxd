import { test, expect, Page } from '@playwright/test'

async function fixture(
  page: Page,
  options: { empty?: boolean; failBoot?: boolean; activeTask?: boolean } = {},
) {
  const state = {
    apps: options.empty
      ? ([] as any[])
      : [
          {
            id: 'app1',
            name: 'Studio planner',
            description: 'A calm place to organize creative work',
            current_sandbox_id: 'sandbox1',
            runtime_preset: 'react-vite',
          },
        ],
    tasks: options.activeTask
      ? ([{ id: 'task-old', status: 'running', prompt: 'Build my project', can_revert: false }] as any[])
      : ([] as any[]),
    config: [] as any[],
    snapshots: [] as any[],
    brain: null as string | null,
    agents: ['opencode', 'pi', 'claude-code'].map((id) => ({
      id,
      label: id === 'claude-code' ? 'Claude Code' : id === 'pi' ? 'Pi' : 'OpenCode',
      installed_state: 'installed',
      status: 'needs_login',
      supports_api_key: id !== 'pi',
      supports_oauth: id !== 'pi',
      runnable: true,
    })),
    calls: [] as { path: string; method: string; body: any }[],
    errors: [] as string[],
    git: [] as any[],
    keys: [] as any[],
    file: 'export default function App() { return <h1>Hello</h1> }',
    sandboxReads: 0,
    created: false,
  }
  const settings = {
    version: 'test',
    networking: { preview_domain: 'example.com', preview_tls: true },
    auth: { enabled: true },
    runtime: { storage_mode: 'directory', base_image: 'sandboxd-base:test' },
    lifecycle: { idle_reap_enabled: true, idle_threshold_seconds: 2100, keepalive_max_seconds: 86400 },
    egress: { mode: 'disabled' },
    agents: { providers: ['opencode', 'pi', 'claude-code'], default_models: {} },
    capabilities: {},
    editable: ['agents.default_models', 'lifecycle.idle_threshold_seconds'],
  }
  page.on('pageerror', (e) => state.errors.push(e.message))
  await page.route('**/preview-fixture', (route) =>
    route.fulfill({
      contentType: 'text/html',
      body: '<html><body style="font-family:system-ui;padding:50px;background:#fafaf7"><h1>Studio planner</h1><p>A little room for your biggest ideas.</p><hr><h2>Today’s focus</h2><p>Design the new workspace</p></body></html>',
    }),
  )
  await page.route('**/v1/**', async (route) => {
    const url = new URL(route.request().url()),
      path = url.pathname,
      method = route.request().method()
    const raw = route.request().postData()
    const body =
      raw && route.request().headers()['content-type']?.includes('application/json') ? JSON.parse(raw) : raw
    if (method !== 'GET') state.calls.push({ path, method, body })
    const json = (body: unknown, status = 200) => route.fulfill({ status, json: body })
    if (path === '/v1/auth/status') return json({ enabled: false, authenticated: true, password_set: false })
    if (path === '/v1/settings') {
      if (method === 'PATCH') Object.assign(settings, body)
      return json(settings)
    }
    if (path === '/v1/model-providers')
      return json({
        providers: [
          {
            id: 'local',
            name: 'Local coder',
            protocol: 'openai-completions',
            base_url: 'http://models:8000/v1',
            has_api_key: false,
            models: [
              { id: 'team/coder', context_window: 32768, max_tokens: 4096 },
              { id: 'team/fast', context_window: 32768, max_tokens: 4096 },
            ],
          },
        ],
      })
    if (path === '/v1/agents') return json({ providers: state.agents })
    if (path.endsWith('/oauth/start')) return json({ authorize_url: 'https://example.com/authorize' })
    if (/\/agents\/.+\/(api-key|oauth\/finish|credential)$/.test(path)) {
      state.agents.find((a) => a.id === path.split('/')[3])!.status = 'connected'
      return json({ status: 'connected' })
    }
    if (path.endsWith('/disconnect')) {
      state.agents.find((a) => a.id === path.split('/')[3])!.status = 'needs_login'
      return json({})
    }
    if (path === '/v1/presets')
      return json({
        presets: [
          { id: 'react-vite', label: 'React / Vite', description: 'Fast interactive websites' },
          { id: 'nextjs', label: 'Next.js', description: 'Full-stack React apps' },
          { id: 'fastapi', label: 'FastAPI', description: 'Python APIs' },
        ],
      })
    if (path === '/v1/apps') {
      if (method === 'POST') {
        state.created = true
        state.apps.push({ ...body, id: 'new-app' })
        return json(state.apps.at(-1), 201)
      }
      return json({ apps: state.apps })
    }
    const app = state.apps.find((a) => path.startsWith(`/v1/apps/${a.id}`))
    if (app && path === `/v1/apps/${app.id}`) {
      if (method === 'PATCH') Object.assign(app, body)
      return json(app)
    }
    if (app && path.endsWith('/sandbox')) {
      if (options.failBoot) {
        options.failBoot = false
        return json({ error: { message: 'Test workspace boot failed' } }, 503)
      }
      app.current_sandbox_id = 'sandbox1'
      return json({ id: 'sandbox1', status: 'creating' }, 201)
    }
    if (path === '/v1/sandboxes/sandbox1') {
      state.sandboxReads++
      return json({
        id: 'sandbox1',
        status: state.created && state.sandboxReads < 2 ? 'creating' : 'running',
        preview: { url: '/preview-fixture', status: 'ready' },
        processes: [{ name: 'web', kind: 'web', running: true, pid: 42, restarts: 0 }],
      })
    }
    if (path === '/v1/sandboxes/sandbox1/tasks') {
      if (method === 'POST') {
        const id = `task-${state.tasks.length + 1}`
        state.tasks.unshift({
          id,
          ...body,
          status: 'succeeded',
          agent_message: 'Created your project.',
          files_changed: ['src/App.tsx'],
          can_revert: true,
        })
        return json({ id }, 202)
      }
      return json({ tasks: state.tasks })
    }
    if (path.endsWith('/cancel')) {
      state.tasks[0].status = 'cancelled'
      return json({ status: 'cancelling' })
    }
    if (/\/tasks\/.+\/events$/.test(path))
      return route.fulfill({
        contentType: 'text/event-stream',
        body: options.activeTask
          ? 'event: message\ndata: {"role":"agent","text":"Still building…"}\n\n'
          : 'event: done\ndata: {"status":"succeeded"}\n\n',
      })
    if (path.endsWith('/files/content')) {
      if (url.searchParams.get('path') === 'src/App.tsx') return route.fulfill({ body: state.file })
      if (url.searchParams.get('path') === 'BRAIN.md' && state.brain !== null)
        return route.fulfill({ body: state.brain })
      return route.fulfill({ status: 404 })
    }
    if (path.endsWith('/files')) {
      if (method === 'PUT') {
        if (url.searchParams.get('path') === 'BRAIN.md') state.brain = body
        else state.file = body
        return json({})
      }
      return json({
        entries: [
          { path: 'src', type: 'dir' },
          { path: 'src/App.tsx', type: 'file', size: 64 },
        ],
      })
    }
    if (path.endsWith('/config')) {
      if (method === 'POST') {
        state.config.push({
          key: body.key,
          sensitive: body.sensitive,
          access_policy: body.access_policy,
          value_set: true,
          ...(body.sensitive ? {} : { value: body.value }),
        })
        return json(state.config.at(-1), 201)
      }
      return json({ config: state.config })
    }
    if (path.includes('/config/')) {
      if (method === 'PATCH') {
        Object.assign(state.config[0], { access_policy: body.access_policy })
        return json(state.config[0])
      }
      state.config = []
      return json({})
    }
    if (path === '/v1/git-credentials') {
      if (method === 'POST') {
        state.git.push({ ...body, token: undefined, token_set: true, id: 'git1' })
        return json(state.git[0], 201)
      }
      return json({ credentials: state.git })
    }
    if (path === '/v1/api-keys') {
      if (method === 'POST') {
        state.keys.push({ id: 'key1', name: body.name, prefix: 'sk-test', scopes: body.scopes })
        return json({ ...state.keys[0], key: 'test-key-shown-once' }, 201)
      }
      return json({ keys: state.keys })
    }
    if (path.startsWith('/v1/api-keys/') && method === 'DELETE') {
      state.keys = []
      return json({})
    }
    if (path.endsWith('/git/status'))
      return json({ available: true, branch: 'main', files: [], ahead: 0, behind: 0 })
    if (path.includes('/processes/') && path.endsWith('/logs')) return json({ lines: ['Ready on port 3000'] })
    if (path.endsWith('/runtime-inspect'))
      return json({ suggestions: [], existing_manifest: { present: true } })
    if (path.endsWith('/runtime/manifest')) return json({ present: true })
    if (path === '/v1/snapshots' && method === 'POST') {
      const snapshot = {
        id: 'snapshot1',
        name: body.name,
        created_at: '2026-10-20T14:00:00Z',
        size_bytes: 1048576,
      }
      state.snapshots.push(snapshot)
      return json({ id: snapshot.id }, 201)
    }
    if (path.endsWith('/snapshots')) return json({ snapshots: state.snapshots })
    if (path.endsWith('/restore')) return json({ id: 'sandbox1', status: 'running' })
    if (path.endsWith('/fork')) {
      const copy = { id: 'copy1', name: body.name, runtime_preset: 'react-vite' }
      state.apps.push(copy)
      return json({ app: copy }, 201)
    }
    if (path.endsWith('/events')) return json({ events: [] })
    return json({})
  })
  return state
}

test('prompt-first creation waits for workspace and submits the selected stack, agent and model exactly once', async ({
  page,
}, info) => {
  const s = await fixture(page, { empty: true })
  await page.goto('/')
  await page.getByTestId('build-prompt').fill('Build a studio planning app with a calendar and a task board')
  await page.getByTestId('stack-toggle').click()
  await page.getByTestId('preset-nextjs').click()
  await page.getByRole('button', { name: 'OpenCode', exact: true }).click()
  await page.getByTestId('task-agent').selectOption('pi')
  await page.getByTestId('task-provider').selectOption('local')
  await page.getByTestId('task-model').selectOption('team/fast')
  await page.screenshot({ path: info.outputPath('builder-options.png') })
  await page.getByTestId('build-app').click()
  await expect(page.locator('.agent-message').filter({ hasText: 'Created your project.' })).toBeVisible()
  expect(s.calls.find((c) => c.path === '/v1/apps')?.body.runtime_preset).toBe('nextjs')
  const tasks = s.calls.filter((c) => c.path.endsWith('/tasks'))
  expect(tasks).toHaveLength(1)
  expect(tasks[0].body).toEqual({
    prompt: 'Build a studio planning app with a calendar and a task board',
    agent: 'pi',
    provider: 'local',
    model: 'team/fast',
    continue: false,
  })
  await page.getByTestId('tab-files').click()
  await expect(page.locator('.agent-message').filter({ hasText: 'Created your project.' })).toBeVisible()
  await page.getByTestId('tab-overview').click()
  await page.screenshot({ path: info.outputPath('workspace.png') })
  expect(s.errors).toEqual([])
})

test('failed workspace creation keeps the original app and prompt for retry', async ({ page }) => {
  const s = await fixture(page, { empty: true, failBoot: true })
  await page.goto('/')
  await page.getByTestId('build-prompt').fill('Build a portfolio')
  await page.getByTestId('build-app').click()
  await expect(page.getByRole('button', { name: 'Create workspace' })).toBeVisible()
  await expect(page.getByTestId('task-prompt')).toHaveValue('Build a portfolio')
  await page.getByRole('button', { name: 'Create workspace' }).click()
  await expect(page.locator('.agent-message').filter({ hasText: 'Created your project.' })).toBeVisible()
  expect(s.calls.filter((c) => c.path === '/v1/apps')).toHaveLength(1)
  expect(s.calls.filter((c) => c.path.endsWith('/tasks'))).toHaveLength(1)
})

test('code edits survive switching panels and secrets are write-only with explicit rotation', async ({
  page,
}, info) => {
  const s = await fixture(page)
  await page.goto('/#/app/app1/files')
  await page.getByRole('button', { name: 'Open folder src' }).click()
  await page.getByRole('button', { name: 'Open file src/App.tsx' }).click()
  await page.locator('.cm-content').fill('export default function App() { return <h1>Updated</h1> }')
  await page.getByTestId('tab-overview').click()
  await page.getByTestId('tab-files').click()
  await expect(page.locator('.cm-content')).toContainText('Updated')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect.poll(() => s.file).toContain('Updated')
  await page.locator('.workspace-more summary').click()
  await page.getByTestId('tab-config').click()
  await page.getByRole('button', { name: 'Add value', exact: true }).click()
  await page.getByLabel('Name', { exact: true }).fill('SERVICE_KEY')
  await page.getByLabel('Value', { exact: true }).fill('test-secret-not-for-dom')
  await page.getByRole('button', { name: 'Save value' }).click()
  await expect(page.getByText('SERVICE_KEY', { exact: true })).toBeVisible()
  await expect(page.getByText('test-secret-not-for-dom')).toHaveCount(0)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await expect(page.getByLabel('Value', { exact: true })).toHaveCount(0)
  await page.getByLabel('Access policy').selectOption('runtime_access')
  await page.getByRole('button', { name: 'Save value' }).click()
  expect(s.calls.find((c) => c.method === 'PATCH' && c.path.includes('/config/'))?.body).toEqual({
    access_policy: 'runtime_access',
  })
  await page.screenshot({ path: info.outputPath('secrets.png') })
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page.getByLabel('Replace the saved value').check()
  await page.getByLabel('Value', { exact: true }).fill('rotated-test-secret')
  await page.getByRole('button', { name: 'Save value' }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(s.calls.filter((c) => c.method === 'PATCH' && c.path.includes('/config/')).at(-1)?.body.value).toBe(
    'rotated-test-secret',
  )
  await expect(page.getByText('rotated-test-secret')).toHaveCount(0)

  expect(s.errors).toEqual([])
})

test('agent credentials, guided OAuth, Git credentials and settings use accessible dialogs', async ({
  page,
}, info) => {
  const s = await fixture(page)
  await page.goto('/#/settings/agents')
  await page.getByTestId('agent-claude-code').getByRole('button', { name: 'Use API key' }).click()
  await page.getByLabel('Claude Code API key').fill('test-claude-key')
  await page.getByRole('button', { name: 'Connect account', exact: true }).click()
  await expect(page.getByTestId('agent-claude-code').getByText('Connected', { exact: true })).toBeVisible()
  expect(s.calls.find((c) => c.path.endsWith('/claude-code/api-key'))?.body).toEqual({
    api_key: 'test-claude-key',
  })
  await page.getByTestId('agent-claude-code').getByRole('button', { name: 'Disconnect' }).click()
  await page.getByRole('button', { name: 'Disconnect account' }).click()
  await page.getByTestId('agent-claude-code').getByRole('button', { name: 'Connect subscription' }).click()
  await page.getByRole('button', { name: 'Start authorization' }).click()
  await expect(page.getByRole('link', { name: /Open authorization/ })).toHaveAttribute(
    'href',
    'https://example.com/authorize',
  )
  await page.getByLabel('2. Paste the authorization code').fill('test-oauth-code')
  await page.getByRole('button', { name: 'Connect account', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.screenshot({ path: info.outputPath('agents.png') })
  await page.getByRole('button', { name: 'Git credentials Connect private repositories' }).click()
  await page.getByRole('button', { name: 'Add Git credential' }).click()
  await page.getByLabel('Connection name').fill('Team GitHub')
  await page.getByLabel('Access token').fill('test-git-token')
  await page.getByRole('button', { name: 'Save credential' }).click()
  await expect(page.getByText('Team GitHub', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Instance Networking, runtime and system context' }).click()
  await expect(page.getByText('sandboxd-base:test', { exact: true })).toBeVisible()
  expect(s.errors).toEqual([])
})

test('an existing task reconnects and can be stopped after navigation', async ({ page }) => {
  const s = await fixture(page, { activeTask: true })
  await page.goto('/#/app/app1')
  await expect(page.locator('.agent-message').filter({ hasText: 'Still building…' })).toBeVisible()
  await page.getByTestId('tab-files').click()
  await page.getByRole('button', { name: 'Stop', exact: true }).click()
  await expect(page.getByText('cancelled', { exact: true })).toBeVisible()
  expect(s.calls.filter((c) => c.path.endsWith('/cancel'))).toHaveLength(1)
})

test('mobile navigation, chat and configuration fit the viewport', async ({ page }, info) => {
  const s = await fixture(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/')
  await expect(page.getByTestId('build-prompt')).toBeVisible()
  await page.screenshot({ path: info.outputPath('home-mobile.png') })
  await page.getByTestId('app-card').click()
  await page.getByRole('button', { name: 'Chat', exact: true }).click()
  await expect(page.getByTestId('task-prompt')).toBeInViewport()
  await page.screenshot({ path: info.outputPath('chat-mobile.png') })
  await page.getByRole('button', { name: 'Workspace', exact: true }).click()
  await expect(page.getByTestId('preview')).toBeVisible()
  await page.getByRole('button', { name: 'Open navigation' }).click()
  await page.getByTestId('nav-settings').click()
  await page.getByRole('button', { name: /^Coding agents/ }).click()
  await page.screenshot({ path: info.outputPath('settings-mobile.png') })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  expect(s.errors).toEqual([])
})

test('home and conversation drafts survive a trip to Settings', async ({ page }) => {
  const s = await fixture(page)
  await page.goto('/')
  await page.getByTestId('build-prompt').fill('Make a beautiful recipe collection')
  await page.getByTestId('nav-settings').click()
  await page.getByTestId('nav-apps').click()
  await expect(page.getByTestId('build-prompt')).toHaveValue('Make a beautiful recipe collection')
  await page.getByTestId('app-card').filter({ hasText: 'Studio planner' }).click()
  await page.getByTestId('task-prompt').fill('Add a shopping list')
  await page.getByTestId('nav-settings').click()
  await page.getByRole('button', { name: 'Studio planner', exact: true }).click()
  await expect(page.getByTestId('task-prompt')).toHaveValue('Add a shopping list')
  expect(s.calls.filter((c) => c.path.endsWith('/tasks'))).toHaveLength(0)
})

test('unsaved code can cancel navigation and survives until saved', async ({ page }) => {
  await fixture(page)
  await page.goto('/#/app/app1/files')
  await page.getByRole('button', { name: 'Open folder src' }).click()
  await page.getByRole('button', { name: 'Open file src/App.tsx' }).click()
  await page.locator('.cm-content').fill('const retained = true')
  page.once('dialog', (dialog) => dialog.dismiss())
  await page.getByTestId('nav-settings').click()
  await expect(page.locator('.cm-content')).toContainText('const retained = true')
  await expect(page).toHaveURL(/app\/app1\/files/)
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.getByTestId('nav-settings').click()
  await expect(page).toHaveURL(/settings/)
})

test('versions capture, restore and duplicate through explicit dialogs', async ({ page }, info) => {
  const s = await fixture(page)
  await page.goto('/#/app/app1/snapshots')
  await page.getByRole('button', { name: 'Save version', exact: true }).click()
  await page.getByLabel('Version name').fill('Before the redesign')
  await page.getByRole('dialog').getByRole('button', { name: 'Save version', exact: true }).click()
  await expect(page.getByText('Before the redesign', { exact: true })).toBeVisible()
  expect(s.calls.find((c) => c.path === '/v1/snapshots')?.body).toEqual({
    source_sandbox_id: 'sandbox1',
    name: 'Before the redesign',
  })
  await page.screenshot({ path: info.outputPath('versions.png') })
  await page.getByRole('button', { name: 'Restore', exact: true }).click()
  expect(s.calls.filter((c) => c.path.endsWith('/restore'))).toHaveLength(0)
  await page.getByRole('button', { name: 'Restore version', exact: true }).click()
  await expect.poll(() => s.calls.filter((c) => c.path.endsWith('/restore')).length).toBe(1)
  await page.getByRole('button', { name: 'Duplicate', exact: true }).click()
  await page.getByLabel('Project name', { exact: true }).fill('A fresh start')
  await page.getByRole('button', { name: 'Create copy', exact: true }).click()
  await expect(page).toHaveURL(/app\/copy1/)
  expect(s.calls.find((c) => c.path.endsWith('/fork'))?.body).toEqual({
    snapshot_id: 'snapshot1',
    name: 'A fresh start',
  })
  expect(s.errors).toEqual([])
})

test('Git import preserves repository and branch without submitting an empty prompt', async ({ page }) => {
  const s = await fixture(page, { empty: true })
  await page.goto('/')
  await page.getByRole('button', { name: 'Import from Git' }).click()
  await page.getByTestId('git-repo-url').fill('https://github.com/example/studio.git')
  await page.getByTestId('git-branch').fill('design')
  await page.getByTestId('build-app').click()
  await expect(page.getByTestId('task-prompt')).toBeVisible()
  const app = s.calls.find((c) => c.path === '/v1/apps')?.body
  expect(app.git).toEqual({ repo_url: 'https://github.com/example/studio.git', branch: 'design' })
  expect(app.runtime_preset).toBeUndefined()
  expect(s.calls.filter((c) => c.path.endsWith('/tasks'))).toHaveLength(0)
})

test('API keys show once, preserve scopes and require confirmation to revoke', async ({ page }) => {
  const s = await fixture(page)
  await page.goto('/#/settings/access')
  await page.getByTestId('api-key-name').fill('Updater')
  await page.getByTestId('api-key-access').selectOption('upgrade')
  await page.getByTestId('api-key-create').click()
  await expect(page.getByTestId('api-key-new-value')).toHaveValue('test-key-shown-once')
  expect(s.calls.find((c) => c.path === '/v1/api-keys')?.body.scopes).toEqual(['upgrade'])
  await page.getByRole('button', { name: 'Done', exact: true }).click()
  await expect(page.getByTestId('api-key-new-value')).toHaveCount(0)
  await page.getByTestId('api-key-revoke').click()
  expect(s.calls.filter((c) => c.method === 'DELETE')).toHaveLength(0)
  await page.getByRole('dialog').getByRole('button', { name: 'Confirm', exact: true }).click()
  await expect(page.getByTestId('api-key-key1')).toHaveCount(0)
  expect(s.errors).toEqual([])
})

test('project memory edits survive status refreshes and panel changes', async ({ page }) => {
  const s = await fixture(page)
  await page.goto('/#/app/app1/brain')
  await page.getByTestId('brain-create').click()
  await page.getByTestId('brain-edit-toggle').click()
  await page.getByTestId('brain-editor').fill('# Keep this decision\nUse a simple calendar.')
  await page.getByTestId('tab-overview').click()
  await page.waitForTimeout(5500)
  await page.locator('.workspace-more summary').click()
  await page.getByTestId('tab-brain').click()
  await expect(page.getByTestId('brain-editor')).toHaveValue('# Keep this decision\nUse a simple calendar.')
  await page.getByTestId('brain-save').click()
  await expect.poll(() => s.brain).toContain('Keep this decision')
  await page.getByTestId('brain-new-note').click()
  await page.getByLabel('Note name').fill('Architecture')
  await page.getByRole('button', { name: 'Create note', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.getByTestId('brain-doc-hub').click()
  await page.getByTestId('brain-edit-toggle').click()
  await page.getByTestId('brain-editor').fill('This edit should be discarded')
  page.once('dialog', (dialog) => dialog.accept())
  await page.getByTestId('brain-doc-architecture').click()
  await page.getByTestId('brain-doc-hub').click()
  await page.getByTestId('brain-edit-toggle').click()
  await expect(page.getByTestId('brain-editor')).toHaveValue('# Keep this decision\nUse a simple calendar.')

  expect(s.errors).toEqual([])
})

test('sign-in is responsive and submits the console password through the API', async ({ page }, info) => {
  await fixture(page)
  let loggedIn = false
  await page.route('**/v1/auth/status', (route) =>
    route.fulfill({ json: { enabled: true, authenticated: loggedIn, password_set: true } }),
  )
  await page.route('**/v1/auth/login', (route) => {
    expect(route.request().postDataJSON()).toEqual({ password: 'test-console-password' })
    loggedIn = true
    return route.fulfill({ json: {} })
  })
  await page.goto('/')
  await page.screenshot({ path: info.outputPath('sign-in-desktop.png') })
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByTestId('login-submit')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath('sign-in-mobile.png') })
  await page.getByTestId('login-password').fill('test-console-password')
  await page.getByTestId('login-submit').click()
  await expect(page.getByTestId('build-prompt')).toBeVisible()
})

test('workspace tools, settings and the app library remain usable on mobile', async ({ page }) => {
  const s = await fixture(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/#/app/app1/runtime')
  await page.getByTestId('process-logs-web').click()
  await expect(page.getByTestId('process-log-output')).toContainText('Ready on port 3000')
  await page.getByRole('button', { name: 'Refresh', exact: true }).click()
  await expect(page.getByTestId('process-log-output')).toContainText('Ready on port 3000')
  for (const tab of ['git', 'config', 'snapshots', 'activity']) {
    await page.goto('/#/app/app1/' + tab)
    await expect(page.getByTestId('task-prompt')).toBeAttached()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  }
  for (const section of ['models', 'git', 'access', 'lifecycle', 'instance']) {
    await page.goto('/#/settings/' + section)
    await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  }
  await page.goto('/#/store')
  await page.getByLabel('Search app library').fill('ghost')
  await expect(page.getByText('Ghost', { exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  expect(s.errors).toEqual([])
})

test('terminal opens explicitly and keeps its connection when switching workspace panels', async ({
  page,
}) => {
  const s = await fixture(page)
  let connections = 0
  let closed = false
  await page.routeWebSocket('**/v1/sandboxes/sandbox1/terminal', (socket) => {
    connections++
    socket.onClose(() => {
      closed = true
    })
    socket.send(Buffer.from('ready in workspace\r\n'))
  })
  await page.goto('/#/app/app1/terminal')
  await expect(page.getByRole('button', { name: 'Open terminal', exact: true })).toBeVisible()
  expect(connections).toBe(0)
  await page.getByRole('button', { name: 'Open terminal', exact: true }).click()
  await expect.poll(() => connections).toBe(1)
  await expect(page.locator('.xterm-screen')).toBeVisible()
  await page.getByTestId('tab-overview').click()
  await page.getByTestId('tab-terminal').click()
  await expect(page.locator('.xterm-screen')).toBeVisible()
  expect(connections).toBe(1)
  expect(closed).toBe(false)
  expect(s.errors).toEqual([])
})
