import { DriveStep } from 'driver.js'

/**
 * 管理员轻量级核心引导流程（4–5 步非阻塞）
 * @param t 国际化函数
 * @param isSimpleMode 是否为简易模式（简易模式下会过滤分组相关步骤）
 * @param isZh 是否为中文界面
 */
export const getAdminSteps = (
  t: (key: string) => string,
  isSimpleMode = false,
  isZh = true
): DriveStep[] => {
  const allSteps: DriveStep[] = [
    {
      popover: {
        title: t('onboarding.admin.welcome.title'),
        description: isZh
          ? '欢迎使用控制台。接下来用不到 30 秒了解上游接入、分组路由与快捷导航，您也可以随时点击「跳过引导」。'
          : 'Welcome to the console. Take a quick 30-second tour of upstream connections, groups, and quick navigation—or skip anytime.',
        align: 'center',
        nextBtnText: t('onboarding.admin.welcome.nextBtn'),
        showButtons: ['next', 'close']
      }
    },
    {
      element: '#sidebar-channel-manage',
      popover: {
        title: t('onboarding.admin.accountManage.title'),
        description: t('onboarding.admin.accountManage.description'),
        side: 'right',
        align: 'center',
        showButtons: ['next', 'previous', 'close']
      }
    },
    {
      element: '#sidebar-group-manage',
      popover: {
        title: t('onboarding.admin.groupManage.title'),
        description: t('onboarding.admin.groupManage.description'),
        side: 'right',
        align: 'center',
        showButtons: ['next', 'previous', 'close']
      }
    },
    {
      element: '[data-testid="console-cmdk-trigger"]',
      popover: {
        title: isZh ? '快捷命令面板 (⌘K)' : 'Quick Command Palette (⌘K)',
        description: isZh
          ? '按下 ⌘K 或点击此处，可瞬间搜索模型、切换工作空间并跳转任意管理页面。'
          : 'Press ⌘K or click here to jump to any page, switch workspaces, or search live models.',
        side: 'bottom',
        align: 'end',
        showButtons: ['next', 'previous', 'close']
      }
    },
    {
      element: '[data-testid="console-tier-switch"]',
      popover: {
        title: isZh ? '切换开发者台创建密钥' : 'Switch to Developer Console',
        description: isZh
          ? '配置好上游与分组后，点击此处切至开发者台创建 API Key 并开始调用测试。'
          : 'Once upstreams and groups are ready, switch to the Developer Console to create API keys and test requests.',
        side: 'bottom',
        align: 'end',
        showButtons: ['next', 'previous', 'close']
      }
    }
  ]

  if (isSimpleMode) {
    return allSteps.filter((step) => {
      const element = step.element as string | undefined
      return !element || !element.includes('sidebar-group-manage')
    })
  }

  return allSteps
}

/**
 * 普通用户轻量级引导流程（4 步）
 */
export const getUserSteps = (t: (key: string) => string, isZh = true): DriveStep[] => [
  {
    popover: {
      title: t('onboarding.user.welcome.title'),
      description: t('onboarding.user.welcome.description'),
      align: 'center',
      nextBtnText: t('onboarding.user.welcome.nextBtn'),
      showButtons: ['next', 'close']
    }
  },
  {
    element: '[data-tour="sidebar-my-keys"]',
    popover: {
      title: t('onboarding.user.keyManage.title'),
      description: t('onboarding.user.keyManage.description'),
      side: 'right',
      align: 'center',
      showButtons: ['next', 'previous', 'close']
    }
  },
  {
    element: '[data-testid="console-cmdk-trigger"]',
    popover: {
      title: isZh ? '快捷命令面板 (⌘K)' : 'Quick Command Palette (⌘K)',
      description: isZh
        ? '随时按 ⌘K 搜索可用模型、查看费用账单或快速跳转功能页面。'
        : 'Press ⌘K anytime to search models, inspect billing, or jump between views.',
      side: 'bottom',
      align: 'end',
      showButtons: ['next', 'previous', 'close']
    }
  },
  {
    element: '[data-testid="user-menu-trigger"]',
    popover: {
      title: isZh ? '个人设置与帮助' : 'Profile & Help',
      description: isZh
        ? '在右上角菜单中可管理账户安全、切换主题与语言，或随时重放引导。'
        : 'Manage your account security, theme preferences, and replay this tour anytime from the user menu.',
      side: 'bottom',
      align: 'end',
      showButtons: ['next', 'previous', 'close']
    }
  }
]
