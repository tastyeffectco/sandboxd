import { api } from './api'
import { CatalogApp, catalogTag } from './catalog'

// Installs a catalog entry: a new project (from the upstream repo for source
// apps, empty otherwise), plus any small files the entry brings. The caller
// then starts the install task with installPrompt(entry). Returns the app id.
export async function installCatalogApp(entry: CatalogApp): Promise<string> {
  const app = await api.createApp({
    name: entry.name,
    description: entry.pitch,
    tags: [catalogTag(entry.id)],
    ...(entry.git ? { git: entry.git } : {}),
  })
  const sandbox = await api.createAppSandbox(app.id, entry.git ? {} : { template: 'blank' })
  for (const [path, content] of Object.entries(entry.files || {})) {
    await api.putWorkspaceFile(sandbox.id, path, content)
  }
  return app.id
}
