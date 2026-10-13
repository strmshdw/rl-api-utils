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
        page: '#0a0b0d',
        panel: {
          DEFAULT: '#121418',
          hover: '#171a1f',
        },
        modal: '#0f1114',
        scrim: 'rgba(5, 6, 8, 0.82)',
        line: {
          DEFAULT: '#1c1e22',
          2: '#23262c',
        },
        outline: '#2a2d33',
        ink: {
          DEFAULT: '#f2f3f5',
          strong: '#ffffff',
          2: '#c4c7cd',
          3: '#9a9ea7',
        },
        muted: {
          DEFAULT: '#7b7f88',
          2: '#6c7079',
        },
        faint: '#4a4e57',
        count: '#4c5059',
        tile: '#16181c',
        'blue-slab': '#0077c7',
        'orange-slab': '#e16c10',
        'blue-ink': '#30aff8',
        'orange-ink': '#f67f2f',
        win: '#67d283',
        loss: '#ff6a65',
      },
      fontFamily: {
        display: ['"Barlow Condensed"', '"Arial Narrow"', 'sans-serif'],
        sans: ['Barlow', 'system-ui', 'sans-serif'],
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
