export default {
  "title": "配置与使用手册",
  "subtitle": "账号准备、密钥字段、模型目录、客户端配置与额度排查。按目录查阅配置项、操作步骤和错误处理。",
  "eyebrow": "使用手册 · 配置与入门",
  "contents": "本页目录",
  "copy": "复制",
  "copied": "已复制到剪贴板",
  "copyFailed": "复制失败，请选中代码后手动复制。",
  "next": "前往相关页面",
  "placeholder": "示例中的 API 地址取自本站配置（api_base_url），密钥请换成你自己的，model 请填写目录中返回的 id。以下 curl 命令使用 Bash / zsh 语法；Windows 可在 WSL 或 Git Bash 中运行。",
  "apiBaseMissing": "本站尚未配置公开 API 地址（api_base_url），本页无法生成接入示例。请联系管理员配置后再查阅。",
  "quick": {
    "title": "快速开始",
    "steps": {
      "account": {
        "title": "注册或使用已有账号登录",
        "body": "注册入口以站点开放情况为准；未开放时联系管理员获取账号。登录后打开「API 密钥」查看 API 地址；控制台地址可能与 API 地址不同，不要填写 /keys、/login 或管理页面路径。"
      },
      "funding": {
        "title": "准备余额或有效订阅",
        "body": "刚注册且没有余额或有效订阅时，先查看充值/订阅入口；已有兑换码也可兑换对应权益。入口与可选内容以本站实际开放为准。"
      },
      "key": {
        "title": "创建并复制 API Key",
        "body": "填写便于辨认的名称，选择有权限的分组，保存并复制完整密钥。默认生成 sk- 前缀密钥，不要自行添加或裁剪前缀；没有可选分组时联系管理员开通权限。"
      },
      "catalog": {
        "title": "查询当前密钥的模型目录",
        "body": "先运行下方环境变量与模型查询命令，读取返回列表中的 id，再原样填入请求的 model 字段。"
      },
      "request": {
        "title": "发送请求并检查结果",
        "body": "运行下方聊天请求；客户端配置见对应章节。请求失败时按常见问题表排查，重试前检查用量或已有结果，避免重复消费。"
      }
    },
    "tables": {},
    "notes": {}
  },
  "keys": {
    "title": "创建与管理 API Key",
    "steps": {},
    "tables": {
      "fields": {
        "title": "密钥字段",
        "columns": {
          "field": "配置项",
          "meaning": "含义与操作",
          "example": "示例"
        },
        "rows": {
          "name": {
            "field": "名称",
            "meaning": "用于识别设备或应用；建议分开创建，便于查看用量和单独停用。",
            "example": "个人笔记本"
          },
          "group": {
            "field": "分组",
            "meaning": "决定模型、计费与订阅范围；编辑密钥或切换分组后重新获取模型列表。",
            "example": "选择你有权限且与订阅对应的分组"
          },
          "expiry": {
            "field": "有效期",
            "meaning": "按需要设置有效期；过期后需调整有效期或替换密钥。",
            "example": "按使用期限选择到期时间"
          },
          "quota": {
            "field": "额度",
            "meaning": "该密钥消费上限，不是充值余额；不覆盖用户余额、分组或订阅限制。",
            "example": "10 美元上限不会增加 10 美元余额"
          },
          "ip": {
            "field": "IP 白名单 / 黑名单",
            "meaning": "按需限制请求来源；检查客户端实际出口 IP 是否符合规则。",
            "example": "白名单：你的客户端出口 IP"
          },
          "status": {
            "field": "状态",
            "meaning": "active：启用；inactive：停用；expired：过期；quota_exhausted：额度用尽。",
            "example": "active（仍需满足余额与订阅限制）"
          }
        }
      }
    },
    "notes": {
      "actions": "操作顺序：「API 密钥」→ 创建 → 填写字段 → 保存 → 复制或「使用」。操作菜单支持停用、重新启用、编辑与删除；重新启用不会补余额或续订。",
      "security": "删除后旧密钥不能继续调用，先在客户端替换新密钥再删除。不要将真实密钥放入截图、公开仓库或聊天记录。"
    }
  },
  "models": {
    "title": "选择可用模型",
    "steps": {},
    "tables": {
      "layers": {
        "title": "模型目录的三层结构",
        "columns": {
          "layer": "层",
          "content": "内容",
          "purpose": "作用",
          "view": "如何查看"
        },
        "rows": {
          "account": {
            "layer": "上游账号",
            "content": "管理员配置的上游账号与模型映射",
            "purpose": "可用性取决于账号、模型映射及上游支持。",
            "view": "列表为空时联系管理员检查账号配置与路由。"
          },
          "group": {
            "layer": "密钥分组",
            "content": "分组白名单与成员账号",
            "purpose": "白名单缩小已有目录；composite 组合分组汇集符合规则的成员账号模型。",
            "view": "查看密钥所属分组；权限由管理员核实，换密钥或分组后刷新目录。"
          },
          "protocol": {
            "layer": "客户端协议",
            "content": "OpenAI 兼容目录 / Codex 目录",
            "purpose": "选择协议对应的模型 ID；聊天用 chat/completions，Codex 用 responses。",
            "view": "用调用时同一把密钥查询下表入口；刷新客户端 / Agent 模型选择器。"
          }
        }
      },
      "endpoints": {
        "title": "目录查询入口",
        "columns": {
          "endpoint": "端点",
          "purpose": "用途",
          "notes": "调用与注意"
        },
        "rows": {
          "openai": {
            "endpoint": "GET /v1/models",
            "purpose": "普通 OpenAI 兼容工具",
            "notes": "/models 是根路径别名；带 client_version 查询参数时走 Codex 目录。"
          },
          "codex": {
            "endpoint": "GET /backend-api/codex/models",
            "purpose": "Codex 兼容客户端",
            "notes": "使用同一密钥鉴权，从返回目录选择 Codex 模型 ID。"
          }
        }
      }
    },
    "notes": {
      "availability": "目录列出当前分组可见的模型，不保证此刻有额度、可调度账号或支持所有端点；换密钥或分组后请刷新列表。"
    }
  },
  "clients": {
    "title": "配置客户端与流式响应",
    "steps": {},
    "tables": {
      "matrix": {
        "title": "客户端配置矩阵",
        "columns": {
          "client": "客户端",
          "base": "Base URL",
          "key": "API Key",
          "model": "Model",
          "notes": "说明"
        },
        "rows": {
          "curl": {
            "client": "curl",
            "base": "$SUB2API_BASE_URL",
            "key": "Authorization: Bearer $SUB2API_API_KEY",
            "model": "model: MODEL_ID_FROM_LIST",
            "notes": "聊天调用 chat/completions；流式示例见下方，首次请求见快速开始。"
          },
          "python": {
            "client": "Python · OpenAI SDK",
            "base": "base_url = SUB2API_BASE_URL",
            "key": "api_key = SUB2API_API_KEY",
            "model": "model = MODEL_ID_FROM_LIST",
            "notes": "先安装 openai 包；SDK 自动追加接口路径。"
          },
          "cursor": {
            "client": "Cursor",
            "base": "自定义 OpenAI API 地址",
            "key": "本站创建的完整密钥",
            "model": "MODEL_ID_FROM_LIST",
            "notes": "配置自定义地址、密钥与模型；只有使用该自定义接口的功能经过本网关。"
          },
          "codex": {
            "client": "Codex",
            "base": "model_providers.sub2api.base_url",
            "key": "env_key = \"SUB2API_API_KEY\"",
            "model": "model = \"MODEL_ID_FROM_CODEX_LIST\"",
            "notes": "在 ~/.codex/config.toml 配置 provider，密钥放环境变量；示例使用 HTTP Responses。"
          },
          "cline": {
            "client": "Cline（VS Code）",
            "base": "API Provider 选 OpenAI Compatible，Base URL 填本站 /v1",
            "key": "填入本站创建的完整密钥",
            "model": "MODEL_ID_FROM_LIST",
            "notes": "在 Cline 设置面板（⚙️）配置；Base URL 填到 /v1 为止，不要填 /chat/completions。需工具调用的模型必须支持 OpenAI 工具协议。"
          },
          "roo": {
            "client": "Roo Code（VS Code）",
            "base": "API Provider 选 OpenAI Compatible，Base URL 填本站 /v1",
            "key": "填入本站创建的完整密钥",
            "model": "MODEL_ID_FROM_LIST",
            "notes": "设置面板中同时选择 API Provider/Base URL/API Key/Model；Roo Code 只使用原生工具调用，模型不支持工具即不可用。"
          },
          "continue": {
            "client": "Continue（VS Code / JetBrains）",
            "base": "config.yaml 中 apiBase 填本站 /v1",
            "key": "apiKey: sk-your-api-key",
            "model": "model: MODEL_ID_FROM_LIST",
            "notes": "provider 用 openai + apiBase 指向本站；需要控制上下文模型行为时再单独配 context 模型。"
          },
          "jetbrains": {
            "client": "JetBrains AI Assistant",
            "base": "Provider 选 OpenAI-compatible endpoint，URL 填本站 /v1",
            "key": "填入本站创建的完整密钥",
            "model": "选择识别到的模型 ID",
            "notes": "Settings | Tools | AI Assistant | Providers & API keys 配置；聊天/代码辅助与 AI Completion 是两处独立设置。"
          },
          "windsurf": {
            "client": "Windsurf",
            "base": "官方文档未提供自定义 OpenAI 兼容地址",
            "key": "—",
            "model": "—",
            "notes": "Windsurf 官方文档没有自定义第三方 OpenAI 兼容端点/自带密钥的配置说明；请在 Windsurf 内用 Cline、Roo Code 或 Continue 插件接入本站。"
          }
        }
      },
      "fields": {
        "title": "通用字段参考",
        "columns": {
          "field": "配置项",
          "meaning": "含义与填写方式",
          "example": "示例"
        },
        "rows": {
          "base": {
            "field": "base_url",
            "meaning": "通常为站点 API 根地址加 /v1，不要重复添加 /v1；以密钥页「使用」生成的对应客户端配置为准，特别是需要专用端点的分组。",
            "example": "本站 api_base_url + /v1（如 https://api.your-domain.com/v1）"
          },
          "key": {
            "field": "api_key",
            "meaning": "填写完整密钥，HTTP 使用 Bearer 后接空格和密钥；控制台登录密码不是 API Key。",
            "example": "Authorization: Bearer sk-your-api-key"
          },
          "model": {
            "field": "model",
            "meaning": "原样填写同一密钥目录返回的 id；显示名称不一定是请求 ID。",
            "example": "MODEL_ID_FROM_LIST"
          },
          "stream": {
            "field": "stream",
            "meaning": "true：HTTP 流式响应（SSE）；省略或 false：普通完整响应。curl 加 -N，SDK 逐块读取。",
            "example": "\"stream\": true"
          }
        }
      }
    },
    "notes": {
      "streaming": "供应商流没有通用断点续传：流已经开始（状态码已固化为 200）后，中断只会就地补一个标准终止帧（/v1/responses 为 response.failed，Messages 与 Chat Completions 为 error 帧），不会悄悄换上游续写，也不会把半截内容当成完整答案重放。中断后先查看用量记录，再用新请求重试；带 Idempotency-Key 的重试不会重复启动生成（会返回 409 与 X-Idempotency-State）。GET /v1/responses 支持 WebSocket 接入，但可用性取决于分组、账号和管理员 WS 模式；聊天 SSE 与 WebSocket 不同，未确认支持时使用 HTTP/SSE。",
      "mediaEndpoints": "以上客户端配置都是文本/视觉输入协议（Messages、Responses、Chat Completions）。图片、视频、语音等生成类请求请改用 /v1/images/generations、/v1/images/edits、/v1/videos/generations、/v1/tts、/v1/stt 等媒体入口；具体可用性取决于分组平台与分组开关。",
      "clientSources": "新增客户端的字段名称取自各自第一方文档（Cline 文档：OpenAI Compatible；Roo Code 仓库文档：Using OpenAI Compatible Providers；Continue 文档：How to Configure OpenAI Models；JetBrains 帮助：Use third-party and local models）。Windsurf 官方文档未提供自定义 OpenAI 兼容端点，故列出为不可用说明。",
      "antigravity": "Antigravity 的协议与工具支持取决于实际模型和分组；混合客户端工具时，部分上游内置工具可能被省略，请勿依赖内置搜索或代码执行一定可用。"
    }
  },
  "usage": {
    "title": "用量与额度",
    "steps": {},
    "tables": {
      "fields": {
        "title": "余额与额度字段",
        "columns": {
          "field": "字段",
          "meaning": "含义",
          "notes": "示例与注意"
        },
        "rows": {
          "available": {
            "field": "可用余额",
            "meaning": "顶部显示的当前可消费余额。",
            "notes": "可用 8 美元：目前最多可用这 8 美元；不足时充值或兑换。"
          },
          "frozen": {
            "field": "冻结余额",
            "meaning": "暂时占用的余额，不能再次消费。",
            "notes": "展开顶部余额信息查看，例如冻结 2 美元。"
          },
          "total": {
            "field": "总额",
            "meaning": "可用余额 + 冻结余额。",
            "notes": "可用 8 + 冻结 2 = 总额 10 美元；总额不等于当前可花费金额。"
          },
          "windows": {
            "field": "密钥用量窗口",
            "meaning": "用户密钥可设置 5 小时、1 天、7 天消费上限（rate_limit_5h / rate_limit_1d / rate_limit_7d）。",
            "notes": "以密钥页面显示的用量与重置时间为准；这些限制与订阅日、周、月额度及上游账号限制分别计算。"
          },
          "limits": {
            "field": "额度",
            "meaning": "密钥消费上限、订阅日/周/月限额、上游账号窗口分别约束请求，不能相加当成统一余额。",
            "notes": "订阅额度耗尽需续订、调整合适分组或等显示的重置时间；增加密钥上限不会充值。"
          },
          "charges": {
            "field": "请求 / token / 实际费用",
            "meaning": "在「我的用量」查看；模型定价和分组倍率影响账单。",
            "notes": "不能只凭 token 数推算扣费。"
          }
        }
      },
      "states": {
        "title": "窗口状态解释",
        "columns": {
          "state": "状态",
          "meaning": "含义",
          "action": "处理"
        },
        "rows": {
          "exhausted": {
            "state": "耗尽",
            "meaning": "当前有效窗口达到 100%。",
            "action": "检查对应限额及页面显示的重置时间。"
          },
          "stale": {
            "state": "可能已过期",
            "meaning": "缓存太旧或已跨重置时间。",
            "action": "刷新确认当前状态。"
          },
          "unknown": {
            "state": "未知 / 未获取 / 获取失败 / 不支持查询",
            "meaning": "无法确定当前额度，不等于零或无限。",
            "action": "刷新可查询数据，仍无法确认时联系管理员。"
          }
        }
      }
    },
    "notes": {}
  },
  "billing": {
    "title": "订阅、充值与兑换",
    "steps": {},
    "tables": {
      "options": {
        "title": "购买与兑换选项",
        "columns": {
          "option": "选项",
          "steps": "操作步骤",
          "notes": "注意事项"
        },
        "rows": {
          "topup": {
            "option": "充值",
            "steps": "1. 打开「充值/订阅」，选择充值金额。\n2. 核对价格，创建订单并按指引支付。\n3. 回到订单页确认状态，再检查余额。",
            "notes": "成功返回页不代表到账；以支付通知和订单处理确认为准。"
          },
          "subscription": {
            "option": "订阅套餐",
            "steps": "1. 选择套餐，核对分组、价格、有效期和额度。\n2. 创建订单并完成支付，确认订单状态。\n3. 在「我的订阅」查看生效套餐与期限。",
            "notes": "使用订阅时密钥需关联对应分组。"
          },
          "redemption": {
            "option": "兑换码",
            "steps": "1. 打开「兑换」，粘贴完整代码并提交。\n2. 按类型检查余额、并发或订阅权益变化。\n3. 余额码检查顶部余额；订阅码检查「我的订阅」和密钥分组。",
            "notes": "失败时核对拼写、是否已使用与有效状态，向发码方核实；不要公开兑换码。"
          }
        }
      }
    },
    "notes": {
      "availability": "站点可只开充值、只开订阅或关闭在线购买，导航名称和可选内容可能不同。",
      "pending": "订单仍待支付或处理中：刷新并稍后重查，保留订单号联系管理员，避免重复下单。"
    }
  },
  "faq": {
    "title": "常见问题",
    "steps": {
      "capture": {
        "title": "记录并隐藏敏感信息",
        "body": "记录请求时间、接口路径、模型 ID、HTTP 状态和错误正文；反馈管理员前隐藏 Authorization 和密钥。同一状态可有多种原因，以实际错误代码与消息为准。"
      },
      "diagnose": {
        "title": "按顺序检查配置",
        "body": "API 地址 → 完整密钥与状态 → 密钥分组与模型目录 → 余额 / 订阅 / 密钥限额 → 上游状态。重试前检查已有用量或结果，避免重复消费。"
      }
    },
    "tables": {
      "errors": {
        "title": "HTTP 状态与错误码",
        "columns": {
          "status": "HTTP 状态",
          "code": "错误码",
          "meaning": "含义",
          "action": "处理"
        },
        "rows": {
          "auth": {
            "status": "401",
            "code": "API_KEY_REQUIRED / INVALID_API_KEY / API_KEY_DISABLED",
            "meaning": "密钥缺失、无效或已停用。",
            "action": "核对 Bearer 格式、完整密钥、状态和客户端实际读取的配置。"
          },
          "user": {
            "status": "401",
            "code": "USER_INACTIVE / USER_NOT_FOUND",
            "meaning": "密钥关联用户停用或不存在。",
            "action": "联系管理员核实账号状态。"
          },
          "ip": {
            "status": "403",
            "code": "ACCESS_DENIED",
            "meaning": "请求来源 IP 不符合访问限制。",
            "action": "核对实际出口 IP 和白名单 / 黑名单。"
          },
          "expiry": {
            "status": "403",
            "code": "API_KEY_EXPIRED",
            "meaning": "密钥有效期结束。",
            "action": "检查有效期，编辑或替换密钥。"
          },
          "balance": {
            "status": "403",
            "code": "INSUFFICIENT_BALANCE",
            "meaning": "账户余额不足。",
            "action": "检查可用余额并充值或兑换。"
          },
          "subscription": {
            "status": "403",
            "code": "SUBSCRIPTION_NOT_FOUND / SUBSCRIPTION_INVALID",
            "meaning": "当前分组无有效订阅或订阅无效。",
            "action": "检查分组权益、套餐状态与有效期。"
          },
          "model": {
            "status": "400 / 404",
            "code": "以响应正文为准",
            "meaning": "模型不支持、名称错误或接口路径不匹配。",
            "action": "重新获取目录，核对 model 和聊天 / Responses 协议，不要猜模型名。"
          },
          "quota": {
            "status": "429",
            "code": "API_KEY_QUOTA_EXHAUSTED / insufficient_quota",
            "meaning": "密钥消费上限用尽；部分 Responses 入口返回 insufficient_quota。",
            "action": "检查密钥额度，等待几秒或快速重试不会恢复消费上限。"
          },
          "window": {
            "status": "429",
            "code": "USAGE_LIMIT_EXCEEDED",
            "meaning": "订阅日 / 周 / 月窗口限额耗尽。",
            "action": "检查对应额度和重置时间，按需续订或选择合适分组。"
          },
          "rate": {
            "status": "429",
            "code": "以响应正文为准",
            "meaning": "并发或速率受限。",
            "action": "降低并发，按响应提示退避；先区分限流与消费额度耗尽。"
          },
          "upstream": {
            "status": "502 / 503",
            "code": "以响应正文为准",
            "meaning": "上游出错、临时无可用账号或服务不可用。",
            "action": "稍后有限重试；持续失败时携带请求信息联系管理员。"
          }
        }
      }
    },
    "notes": {}
  }
}
