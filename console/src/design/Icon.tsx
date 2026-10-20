import type { CSSProperties } from 'react'

const paths = {
  spark: 'm12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5Z',
  grid: 'M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z',
  code: 'm8 5-7 7 7 7 m8-14 7 7-7 7 M14 3l-4 18',
  settings: 'M4 6h16 M4 12h16 M4 18h16 M8 3v6 M16 9v6 M10 15v6',
  arrow: 'M5 12h14 m-6-6 6 6-6 6',
  plus: 'M12 5v14 M5 12h14',
  search: 'M21 21l-5-5 M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0',
  git: 'M6 3v12a4 4 0 0 0 4 4h8 M6 8h8a4 4 0 0 0 4-4 M3 3h6 M15 3h6 M15 16v6',
  book: 'M12 5C8 2 4 3 2 4v16c4-2 7-1 10 1 3-2 6-3 10-1V4c-2-1-6-2-10 1v16',
  box: 'm12 3 9 5v9l-9 5-9-5V8z M3 8l9 5 9-5 M12 13v9',
  close: 'm6 6 12 12 M6 18 18 6',
  chevron: 'm7 10 5 5 5-5',
  monitor: 'M3 3h18v14H3z M8 21h8 M12 17v4',
  phone: 'M7 2h10v20H7z M11 18h2',
  terminal: 'm4 6 6 6-6 6 M13 18h7',
  lock: 'M5 10h14v11H5z M8 10V6a4 4 0 0 1 8 0v4',
  history: 'M3 11a9 9 0 1 1 3 8 M3 4v7h7 M12 7v5l3 2',
  external: 'M14 3h7v7 M21 3 10 14 M10 3H3v18h18v-7',
  menu: 'M3 6h18 M3 12h18 M3 18h18',
  check: 'm5 12 4 4L19 6',
  logout: 'M9 3H3v18h6 M10 12h11 m-5-5 5 5-5 5',
  refresh: 'M20 7a9 9 0 1 0 1 8 M20 2v6h-6',
}
export type IconName = keyof typeof paths
export function Icon({ name, size = 18, style }: { name: IconName; size?: number; style?: CSSProperties }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      style={{ flexShrink: 0, ...style }}
    >
      <path d={paths[name]} />
    </svg>
  )
}
