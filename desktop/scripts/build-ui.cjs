const { spawnSync } = require('node:child_process')
const path = require('node:path')
const root = path.resolve(__dirname, '../..')
const result = spawnSync(
  process.execPath,
  [
    path.join(root, 'console/node_modules/vite/bin/vite.js'),
    'build',
    '--outDir',
    path.join(root, 'desktop/ui'),
    '--emptyOutDir',
  ],
  {
    cwd: path.join(root, 'console'),
    stdio: 'inherit',
    env: { ...process.env, VITE_DESKTOP: '1', VITE_DEMO: '0', BASE_PATH: '/' },
  },
)
process.exit(result.status ?? 1)
