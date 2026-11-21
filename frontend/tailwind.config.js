const hiveCarbonPalette = {
  50: '#fafafa',
  100: '#f4f4f5',
  200: '#e9e9ee',
  300: '#d4d4dc',
  400: '#9e9ea9',
  500: '#6f6f78',
  600: '#4a4a54',
  700: '#2c2c35',
  800: '#1d1d24',
  900: '#131316',
  950: '#0c0c0e'
}

/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      screens: {
        '3xl': '1800px'
      },
      colors: {
        // 主色调 - OmniHive 蜜金琥珀色系 (Hive Aura Benchmark)
        primary: {
          50: '#fffbeb',
          100: '#fef3c7',
          200: '#fde68a',
          300: '#fcd34d',
          400: '#fbbf24',
          500: '#f59e0b',
          600: '#d97706',
          700: '#92400e',
          800: '#78350f',
          900: '#451a03',
          950: '#270e01'
        },
        // 辅助色 - 暖炭黑中性灰
        accent: hiveCarbonPalette,
        // 深色模式背景
        dark: hiveCarbonPalette,
        // OmniHive Design System v1 semantic tokens (--oh-*)
        oh: {
          canvas: 'var(--oh-bg-canvas)',
          subtle: 'var(--oh-bg-subtle)',
          surface: 'var(--oh-bg-surface)',
          elevated: 'var(--oh-bg-elevated)',
          inset: 'var(--oh-bg-inset)',
          hover: 'var(--oh-bg-hover)',
          selected: 'var(--oh-bg-selected)',
          primary: 'var(--oh-fg-primary)',
          secondary: 'var(--oh-fg-secondary)',
          muted: 'var(--oh-fg-muted)',
          disabled: 'var(--oh-fg-disabled)',
          accent: {
            DEFAULT: 'var(--oh-accent)',
            hover: 'var(--oh-accent-hover)',
            active: 'var(--oh-accent-active)',
            ink: 'var(--oh-accent-ink)',
            text: 'var(--oh-accent-text)',
            subtle: 'var(--oh-accent-subtle)',
            border: 'var(--oh-accent-border)'
          },
          status: {
            'ok-fg': 'var(--oh-ok-fg)',
            'ok-bg': 'var(--oh-ok-bg)',
            'ok-border': 'var(--oh-ok-border)',
            'warn-fg': 'var(--oh-warn-fg)',
            'warn-bg': 'var(--oh-warn-bg)',
            'warn-border': 'var(--oh-warn-border)',
            'danger-fg': 'var(--oh-danger-fg)',
            'danger-bg': 'var(--oh-danger-bg)',
            'danger-border': 'var(--oh-danger-border)',
            'info-fg': 'var(--oh-info-fg)',
            'info-bg': 'var(--oh-info-bg)',
            'info-border': 'var(--oh-info-border)',
            'neutral-fg': 'var(--oh-neutral-fg)',
            'neutral-bg': 'var(--oh-neutral-bg)',
            'neutral-border': 'var(--oh-neutral-border)',
            'unknown-fg': 'var(--oh-unknown-fg)',
            'unknown-bg': 'var(--oh-unknown-bg)',
            'unknown-border': 'var(--oh-unknown-border)'
          }
        }
      },
      borderColor: {
        'oh-subtle': 'var(--oh-border-subtle)',
        'oh-default': 'var(--oh-border-default)',
        'oh-strong': 'var(--oh-border-strong)',
        'oh-accent': 'var(--oh-accent-border)'
      },
      ringColor: {
        'oh-focus': 'var(--oh-focus-ring)'
      },
      fontFamily: {
        sans: [
          'Inter Variable',
          'Inter',
          'PingFang SC',
          'Hiragino Sans GB',
          'Microsoft YaHei UI',
          'Microsoft YaHei',
          'Noto Sans CJK SC',
          'system-ui',
          '-apple-system',
          'BlinkMacSystemFont',
          'Segoe UI',
          'Roboto',
          'Helvetica Neue',
          'Arial',
          'sans-serif'
        ],
        mono: [
          'JetBrains Mono Variable',
          'JetBrains Mono',
          'ui-monospace',
          'SFMono-Regular',
          'Menlo',
          'Monaco',
          'Consolas',
          'monospace'
        ]
      },
      boxShadow: {
        glass: '0 8px 32px rgba(19, 19, 22, 0.08)',
        'glass-sm': '0 4px 16px rgba(19, 19, 22, 0.05)',
        glow: '0 0 20px rgba(245, 158, 11, 0.25)',
        'glow-lg': '0 0 40px rgba(245, 158, 11, 0.35)',
        card: '0 1px 3px rgba(19, 19, 22, 0.03), 0 1px 2px rgba(19, 19, 22, 0.02)',
        'card-hover': '0 12px 28px -6px rgba(19, 19, 22, 0.09)',
        'inner-glow': 'inset 0 1px 0 rgba(255, 255, 255, 0.1)',
        'oh-xs': 'var(--oh-shadow-xs)',
        'oh-sm': 'var(--oh-shadow-sm)',
        'oh-md': 'var(--oh-shadow-md)',
        'oh-xl': 'var(--oh-shadow-xl)'
      },
      backgroundImage: {
        'gradient-radial': 'radial-gradient(var(--tw-gradient-stops))',
        'gradient-primary': 'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)',
        'gradient-dark': 'linear-gradient(135deg, #1d1d24 0%, #131316 100%)',
        'gradient-glass':
          'linear-gradient(135deg, rgba(255,255,255,0.1) 0%, rgba(255,255,255,0.05) 100%)',
        'mesh-gradient':
          'radial-gradient(at 40% 16%, rgba(245, 158, 11, 0.07) 0px, transparent 50%), radial-gradient(at 82% 4%, rgba(217, 119, 6, 0.05) 0px, transparent 48%), radial-gradient(at 0% 52%, rgba(245, 158, 11, 0.04) 0px, transparent 50%)'
      },
      animation: {
        'fade-in': 'fadeIn 0.3s ease-out',
        'slide-up': 'slideUp 0.3s ease-out',
        'slide-down': 'slideDown 0.3s ease-out',
        'slide-in-right': 'slideInRight 0.3s ease-out',
        'scale-in': 'scaleIn 0.2s ease-out',
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        shimmer: 'shimmer 2s linear infinite',
        glow: 'glow 2s ease-in-out infinite alternate'
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' }
        },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideDown: {
          '0%': { opacity: '0', transform: 'translateY(-10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideInRight: {
          '0%': { opacity: '0', transform: 'translateX(20px)' },
          '100%': { opacity: '1', transform: 'translateX(0)' }
        },
        scaleIn: {
          '0%': { opacity: '0', transform: 'scale(0.95)' },
          '100%': { opacity: '1', transform: 'scale(1)' }
        },
        shimmer: {
          '0%': { backgroundPosition: '-200% 0' },
          '100%': { backgroundPosition: '200% 0' }
        },
        glow: {
          '0%': { boxShadow: '0 0 20px rgba(245, 158, 11, 0.22)' },
          '100%': { boxShadow: '0 0 30px rgba(245, 158, 11, 0.36)' }
        }
      },
      backdropBlur: {
        xs: '2px'
      },
      borderRadius: {
        '4xl': '2rem',
        'oh-xs': 'var(--oh-radius-xs)',
        'oh-sm': 'var(--oh-radius-sm)',
        'oh-md': 'var(--oh-radius-md)',
        'oh-lg': 'var(--oh-radius-lg)',
        'oh-xl': 'var(--oh-radius-xl)',
        'oh-pill': 'var(--oh-radius-pill)'
      }
    }
  },
  plugins: []
}
