/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    "../internal/templates/**/*.html",
  ],
  theme: {
    extend: {
      colors: {
        // Brand colors use CSS variables that are set dynamically by the Go templates
        // based on the current season (see internal/seasons/seasons.go for color definitions)
        brand: {
          50: 'var(--color-brand-50)',
          100: 'var(--color-brand-100)',
          200: 'var(--color-brand-200)',
          300: 'var(--color-brand-300)',
          400: 'var(--color-brand-400)',
          500: 'var(--color-brand-500)',
          600: 'var(--color-brand-600)',
          700: 'var(--color-brand-700)',
          800: 'var(--color-brand-800)',
          900: 'var(--color-brand-900)',
        },
      },
    },
  },
  plugins: [],
}
