import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './design.css'
import { IS_DEMO, installDemo } from './demo'
import DemoBanner from './DemoBanner'

if (IS_DEMO) installDemo()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    {IS_DEMO && <DemoBanner />}
    <App />
  </React.StrictMode>,
)
