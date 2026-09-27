/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        rl: {
          blue: {
            DEFAULT: '#00a2ff',
            dark: '#0070cc',
            glow: 'rgba(0, 162, 255, 0.4)',
          },
          orange: {
            DEFAULT: '#ff7b00',
            dark: '#cc5500',
            glow: 'rgba(255, 123, 0, 0.4)',
          },
        },
      },
      boxShadow: {
        'glow-blue': '0 0 15px rgba(0, 162, 255, 0.5)',
        'glow-orange': '0 0 15px rgba(255, 123, 0, 0.5)',
      },
    },
  },
  plugins: [],
}
