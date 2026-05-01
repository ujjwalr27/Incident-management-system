/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        p0: '#ef4444',
        p1: '#f97316',
        p2: '#eab308',
        p3: '#22c55e',
      }
    }
  },
  plugins: []
}
