/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      // "Broadcast" design tokens. The team/result colours are the sRGB
      // equivalents of the design's oklch values, so they also render in older
      // Chromium builds (e.g. the OBS browser source).
      colors: {
        page: 'var(--t-page)',
        panel: {
          DEFAULT: 'var(--t-panel)',
          hover: 'var(--t-panel-hover)',
        },
        modal: 'var(--t-modal)',
        scrim: 'rgba(5, 6, 8, 0.82)',
        line: {
          DEFAULT: 'var(--t-line)',
          2: 'var(--t-line-2)',
        },
        outline: 'var(--t-outline)',
        ink: {
          DEFAULT: 'var(--t-ink)',
          strong: 'var(--t-ink-strong)',
          'on-accent': 'var(--t-ink-on-accent)',
          'on-slab': 'var(--t-on-slab)',
          2: 'var(--t-ink-2)',
          3: 'var(--t-ink-3)',
        },
        muted: {
          DEFAULT: 'var(--t-muted)',
          2: 'var(--t-muted-2)',
        },
        faint: 'var(--t-faint)',
        count: 'var(--t-faint)',
        tile: 'var(--t-panel-hover)',
        'blue-slab': 'var(--t-blue-slab)',
        'orange-slab': 'var(--t-orange-slab)',
        'blue-ink': 'var(--t-blue-ink)',
        'orange-ink': 'var(--t-orange-ink)',
        win: 'var(--t-win)',
        loss: 'var(--t-loss)',
      },
      fontFamily: {
        display: ['var(--t-font-display)', '"Barlow Condensed"', '"Arial Narrow"', 'sans-serif'],
        sans: ['var(--t-font-sans)', 'Inter', 'system-ui', 'sans-serif'],
        mono: ['var(--t-font-mono)', '"JetBrains Mono"', 'monospace'],
      },
      letterSpacing: {
        // Named tracking steps used by the broadcast type scale (em-based).
        t2: '.02em',
        t3: '.03em',
        t4: '.04em',
        t6: '.06em',
        t8: '.08em',
        t10: '.1em',
        t12: '.12em',
        t14: '.14em',
        t16: '.16em',
      },
      lineHeight: {
        tightest: '.9',
      },
      transitionDuration: {
        120: '120ms',
      },
    },
  },
  plugins: [],
}
