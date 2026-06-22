import { useState } from 'react'

export default function App() {
  const [count, setCount] = useState(0)
  return (
    <main className="app">
      <h1>Your app is running 🎉</h1>
      <p>
        Edit <code>src/App.tsx</code> and save — or send the agent a task to
        build something.
      </p>
      <button onClick={() => setCount((c) => c + 1)}>count is {count}</button>
    </main>
  )
}
