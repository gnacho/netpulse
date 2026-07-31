/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: ["class"],
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        // NetPulse design tokens (design.md §3)
        canvas: "rgb(var(--canvas) / <alpha-value>)",
        surface: "rgb(var(--surface) / <alpha-value>)",
        elevated: "rgb(var(--elevated) / <alpha-value>)",
        hover: "rgb(var(--hover) / <alpha-value>)",
        border: "rgb(var(--border) / <alpha-value>)",
        "border-strong": "rgb(var(--border-strong) / <alpha-value>)",
        "text-primary": "rgb(var(--text-primary) / <alpha-value>)",
        "text-secondary": "rgb(var(--text-secondary) / <alpha-value>)",
        "text-muted": "rgb(var(--text-muted) / <alpha-value>)",
        accent: {
          DEFAULT: "rgb(var(--accent) / <alpha-value>)",
          soft: "rgb(var(--accent) / 0.12)",
        },
        tunnel: "rgb(var(--tunnel) / <alpha-value>)",
        ok: "rgb(var(--ok) / <alpha-value>)",
        warn: "rgb(var(--warn) / <alpha-value>)",
        danger: "rgb(var(--danger) / <alpha-value>)",
        info: "rgb(var(--info) / <alpha-value>)",
        // shadcn/ui aliases mapped to design tokens
        background: "rgb(var(--canvas) / <alpha-value>)",
        foreground: "rgb(var(--text-primary) / <alpha-value>)",
        input: "rgb(var(--border-strong) / <alpha-value>)",
        ring: "rgb(var(--accent) / <alpha-value>)",
        primary: {
          DEFAULT: "rgb(var(--accent) / <alpha-value>)",
          foreground: "rgb(var(--canvas) / <alpha-value>)",
        },
        secondary: {
          DEFAULT: "rgb(var(--elevated) / <alpha-value>)",
          foreground: "rgb(var(--text-primary) / <alpha-value>)",
        },
        destructive: {
          DEFAULT: "rgb(var(--danger) / <alpha-value>)",
          foreground: "rgb(var(--canvas) / <alpha-value>)",
        },
        muted: {
          DEFAULT: "rgb(var(--elevated) / <alpha-value>)",
          foreground: "rgb(var(--text-muted) / <alpha-value>)",
        },
        popover: {
          DEFAULT: "rgb(var(--elevated) / <alpha-value>)",
          foreground: "rgb(var(--text-primary) / <alpha-value>)",
        },
        card: {
          DEFAULT: "rgb(var(--surface) / <alpha-value>)",
          foreground: "rgb(var(--text-primary) / <alpha-value>)",
        },
        sidebar: {
          DEFAULT: "rgb(var(--surface) / <alpha-value>)",
          foreground: "rgb(var(--text-primary) / <alpha-value>)",
          primary: "rgb(var(--accent) / <alpha-value>)",
          "primary-foreground": "rgb(var(--canvas) / <alpha-value>)",
          accent: "rgb(var(--accent) / 0.12)",
          "accent-foreground": "rgb(var(--accent) / <alpha-value>)",
          border: "rgb(var(--border) / <alpha-value>)",
          ring: "rgb(var(--accent) / <alpha-value>)",
        },
      },
      fontFamily: {
        display: ['"Space Grotesk"', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        sans: ['Inter', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        mono: ['"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'monospace'],
      },
      fontSize: {
        display: ['2.75rem', { lineHeight: '1.1', fontWeight: '700' }],
        'display-sm': ['2.25rem', { lineHeight: '1.1', fontWeight: '700' }],
        h1: ['1.5rem', { lineHeight: '1.2', fontWeight: '600' }],
        h2: ['1.125rem', { lineHeight: '1.3', fontWeight: '600' }],
        stat: ['1.75rem', { lineHeight: '1.1', fontWeight: '600' }],
        label: ['0.75rem', { lineHeight: '1.4', fontWeight: '500', letterSpacing: '0.06em' }],
        caption: ['0.6875rem', { lineHeight: '1.4', fontWeight: '500' }],
        'mono-sm': ['0.75rem', { lineHeight: '1.4', fontWeight: '500' }],
      },
      borderRadius: {
        xl: "calc(var(--radius) + 2px)",
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
        sm: "calc(var(--radius) - 4px)",
        xs: "calc(var(--radius) - 6px)",
      },
      boxShadow: {
        xs: "0 1px 2px 0 rgb(0 0 0 / 0.05)",
        'glow-ok': "0 0 40px -12px rgb(var(--ok) / 0.35)",
        'glow-warn': "0 0 40px -12px rgb(var(--warn) / 0.35)",
        'glow-danger': "0 0 40px -12px rgb(var(--danger) / 0.35)",
        'glow-accent': "0 0 40px -12px rgb(var(--accent) / 0.35)",
        'glow-tunnel': "0 0 40px -12px rgb(var(--tunnel) / 0.35)",
      },
      keyframes: {
        "accordion-down": {
          from: { height: "0" },
          to: { height: "var(--radix-accordion-content-height)" },
        },
        "accordion-up": {
          from: { height: "var(--radix-accordion-content-height)" },
          to: { height: "0" },
        },
        "caret-blink": {
          "0%,70%,100%": { opacity: "1" },
          "20%,50%": { opacity: "0" },
        },
        "ping-soft": {
          "0%": { transform: "scale(1)", opacity: "0.7" },
          "80%, 100%": { transform: "scale(2.2)", opacity: "0" },
        },
      },
      animation: {
        "accordion-down": "accordion-down 0.2s ease-out",
        "accordion-up": "accordion-up 0.2s ease-out",
        "caret-blink": "caret-blink 1.25s ease-out infinite",
        "ping-soft": "ping-soft 1.6s cubic-bezier(0, 0, 0.2, 1) infinite",
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
}
