/** @type {import('tailwindcss').Config} */
const token = (name) => `rgb(var(--${name}) / <alpha-value>)`

module.exports = {
  content: ['./html/*', './js/popup.js'],
  theme: {
    extend: {
      colors: {
        paper: token('paper'),
        ink: token('ink'),
        ink2: token('ink-2'),
        ink3: token('ink-3'),
        rule: token('rule'),
        airmail: token('red'),
        parAvion: token('blue'),
      },
      fontFamily: {
        sans: ['Archivo', 'system-ui', 'sans-serif'],
      },
    },
  },
  plugins: [],
}
