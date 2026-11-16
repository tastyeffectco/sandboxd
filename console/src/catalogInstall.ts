import { api } from './api'
import { CatalogApp, catalogManifest, catalogTag } from './catalog'

// Installs a catalog entry without asking an AI: a new project (from the
// upstream repo for source apps, empty otherwise), the entry's small files and
// its sandbox.yaml, then a restart so the workspace starts the app. Works the
// same when the free AI model is used up. Returns the app id.
export async function installCatalogApp(
  entry: CatalogApp,
  opts: { wait?: (ms: number) => Promise<void>; readyWithin?: number } = {},
): Promise<string> {
  const wait = opts.wait || ((ms: number) => new Promise<void>((r) => setTimeout(r, ms)))
  const app = await api.createApp({
    name: entry.name,
    description: entry.pitch,
    tags: [catalogTag(entry.id)],
    ...(entry.git ? { git: entry.git } : {}),
  })
  const sandbox = await api.createAppSandbox(app.id, entry.git ? {} : { template: 'blank' })
  // A new workspace (and a source app's copy of its repository) takes a moment.
  const deadline = Date.now() + (opts.readyWithin ?? 180_000)
  for (let sb = sandbox; sb.status !== 'running'; sb = await api.getSandbox(sandbox.id)) {
    if (Date.now() > deadline) throw new Error(`${entry.name}'s workspace did not start. Open it and try again.`)
    await wait(2000)
  }
  for (const [path, content] of Object.entries(entry.files || {})) {
    await api.putWorkspaceFile(sandbox.id, path, content)
  }
  await api.putWorkspaceFile(sandbox.id, 'sandbox.yaml', catalogManifest(entry))
  await api.stopSandbox(sandbox.id).catch(() => undefined)
  await api.startSandbox(sandbox.id)
  return app.id
}
