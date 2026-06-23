// Minimal Next.js App Router layout (plain JS — no tsconfig bootstrap needed).
export const metadata = { title: 'Next app' }

export default function RootLayout({ children }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  )
}
