import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { AgentChat } from './AgentChat'
import { api } from './api'

vi.mock('./api', () => ({
  api: {
    getAgents: vi.fn().mockResolvedValue([]),
    listModelProviders: vi.fn().mockResolvedValue([]),
    listTasks: vi.fn().mockResolvedValue([]),
    listApps: vi.fn().mockResolvedValue([]),
    putWorkspaceBlob: vi.fn().mockResolvedValue(undefined),
    submitTask: vi.fn().mockResolvedValue({ id: 'task' }),
    taskEventsURL: () => '/events',
  },
}))

afterEach(cleanup)

describe('attaching files', () => {
  it('saves the file in the project and tells the sandboxd agent where it is', async () => {
    globalThis.EventSource = class {
      addEventListener() {}
      close() {}
      onerror = null
    } as unknown as typeof EventSource
    globalThis.URL.createObjectURL = () => 'blob:x'
    globalThis.URL.revokeObjectURL = () => {}
    const noop = () => {}
    render(
      <AgentChat assistant sb={{ id: 'sb', status: 'running' }} onError={noop} toast={noop} refresh={noop} goSettings={noop} />,
    )
    const logo = new File(['png'], 'logo.png', { type: 'image/png' })
    fireEvent.change(screen.getByTestId('chat-file-input'), { target: { files: [logo] } })
    await waitFor(() => expect(api.putWorkspaceBlob).toHaveBeenCalled())
    const path = vi.mocked(api.putWorkspaceBlob).mock.calls[0][1]
    expect(path).toMatch(/^\.attachments\/\d{8}-\d{6}-logo\.png$/)
    expect(screen.getByText('logo.png')).toBeTruthy()
    fireEvent.change(screen.getByTestId('task-prompt'), { target: { value: 'Use it on my site' } })
    await waitFor(() => expect((screen.getByTestId('task-send') as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(screen.getByTestId('task-send'))
    await waitFor(() => expect(api.submitTask).toHaveBeenCalled())
    expect(vi.mocked(api.submitTask).mock.calls[0][1]).toBe(
      `Use it on my site\n\nAttached files (saved in your workspace): ${path}`,
    )
  })
})
