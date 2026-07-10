// Minimal Express starter — reliable first boot, not fancy.
const express = require('express')
const app = express()
const port = process.env.PORT || 3000

app.get('/health', (_req, res) => res.json({ status: 'ok' }))
app.get('/', (_req, res) => res.send('Express API running. Edit server.js.'))

app.listen(port, '0.0.0.0', () => console.log(`listening on ${port}`))
