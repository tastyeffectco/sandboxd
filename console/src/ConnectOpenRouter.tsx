import { useState } from 'react'
import { api, ModelProvider } from './api'
import { Btn } from './design/kit'

// The strong model agents were tested with, and OpenRouter's free router
// (it picks a free model that can use tools). New accounts have no credit,
// so they start on the free one.
export const OPENROUTER_MODELS = {
  strong: { id: 'deepseek/deepseek-v4-pro', context_window: 131072, max_tokens: 8192 },
  free: { id: 'openrouter/free', context_window: 131072, max_tokens: 8192 },
}

export function openRouterConnection(key: string, free: boolean) {
  const { strong, free: freeModel } = OPENROUTER_MODELS
  return {
    name: 'OpenRouter',
    protocol: 'openai-completions' as const,
    base_url: 'https://openrouter.ai/api/v1',
    api_key: key,
    models: free ? [freeModel, strong] : [strong, freeModel],
  }
}

export const canConnectOpenRouter = () => !!window.sandboxdDesktop?.openRouterSignIn

// One click: sign in on openrouter.ai in the browser, come back connected.
export function ConnectOpenRouter({
  onConnected,
  onError,
  label = 'Connect OpenRouter',
}: {
  onConnected: (provider: ModelProvider, free: boolean) => void
  onError: (message: string) => void
  label?: string
}) {
  const [waiting, setWaiting] = useState(false)
  const connect = async () => {
    const signIn = window.sandboxdDesktop?.openRouterSignIn
    if (!signIn) return
    setWaiting(true)
    try {
      const { key, free } = await signIn()
      onConnected(await api.saveModelProvider(undefined, openRouterConnection(key, free)), free)
    } catch (e) {
      onError((e as Error).message.replace(/^Error invoking remote method '[^']+': (Error: )?/, ''))
    } finally {
      setWaiting(false)
    }
  }
  return (
    <Btn variant="primary" disabled={waiting} onClick={() => void connect()} data-testid="connect-openrouter">
      {waiting ? 'Finish signing in in your browser…' : label}
    </Btn>
  )
}
