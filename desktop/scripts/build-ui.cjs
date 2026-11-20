const { spawnSync } = require('node:child_process')
const path = require('node:path')
const root = path.resolve(__dirname, '../..')
// ui: the desktop window. ui-web: the same console for "Open in Browser".
const build = (outDir, desktop) =>
  spawnSync(
    process.execPath,
    [path.join(root, 'console/node_modules/vite/bin/vite.js'), 'build', '--outDir', path.join(root, outDir), '--emptyOutDir'],
    {
      cwd: path.join(root, 'console'),
      stdio: 'inherit',
      env: { ...process.env, VITE_DESKTOP: desktop ? '1' : '0', VITE_DEMO: '0', BASE_PATH: '/' },
    },
  ).status ?? 1
process.exit(build('desktop/ui', true) || build('desktop/ui-web', false))
