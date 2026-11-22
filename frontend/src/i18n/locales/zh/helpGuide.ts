export default {
  "title": "配置与使用手册",
  "subtitle": "从密钥和分组开始，选择协议、复制请求、配置客户端，再核对结果、用量与账单。",
  "eyebrow": "使用手册 · 配置与入门",
  "contents": "本页目录",
  "copy": "复制",
  "copied": "已复制到剪贴板",
  "copyFailed": "复制失败，请选中代码后手动复制。",
  "next": "前往相关页面",
  "placeholder": "示例使用本站公布的 API 地址。将 sk-your-api-key 替换为完整密钥，将 MODEL_ID_FROM_LIST 替换为同一密钥目录中的模型 ID。Bash 与 PowerShell 示例可分别复制；Python 和 Node 示例从环境变量读取密钥。",
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
            "content": "OpenAI 兼容 / Messages / Google / Codex 目录",
            "purpose": "按客户端协议选择目录与模型：Chat、Messages、Responses 或 Google；Codex 使用 Responses。",
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
            "notes": "OpenAI 兼容客户端使用 data[].id。Codex 请使用专用目录 GET /backend-api/codex/models。"
          },
          "codex": {
            "endpoint": "GET /backend-api/codex/models",
            "purpose": "Codex 兼容客户端",
            "notes": "使用同一密钥鉴权，从返回目录选择 Codex 模型 ID。"
          },
          "catalog": {
            "endpoint": "GET /v1/models/catalog",
            "purpose": "富能力目录",
            "notes": "使用 next_cursor 作为下一页 cursor；保留 guaranteed/available/unknown 的区别。"
          },
          "google": {
            "endpoint": "GET /v1beta/models",
            "purpose": "Google 模型目录",
            "notes": "使用返回的 nextPageToken 继续分页；请求模型 ID 不含开头的 models/。"
          },
          "single": {
            "endpoint": "GET /v1/models/{'{'}encoded_id{'}'}",
            "purpose": "查询一个模型",
            "notes": "路径段中的 ID 需编码，不要删去供应商前缀。"
          }
        }
      }
    },
    "notes": {
      "availability": "目录列出当前分组可见的模型，不保证此刻有额度、可调度账号或支持所有端点；换密钥或分组后请刷新列表。",
      "metadata": "普通目录可含 context_window、max_output_tokens、supports_reasoning、supports_function_calling 等顶层保证值，未知时省略。富目录的 endpoints 与能力资料不保证实时空闲账户；available 不等于所有路由都保证。Messages 目录带 anthropic-version，可用 after_id/before_id 分页（不要同时传）；Google 使用 pageToken。"
    }
  },
  "clients": {
    "title": "客户端与工作流",
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
            "base": "$OMNIHIVE_BASE_URL",
            "key": "Authorization: Bearer $OMNIHIVE_API_KEY",
            "model": "model: MODEL_ID_FROM_LIST",
            "notes": "聊天调用 chat/completions；流式示例见下方，首次请求见快速开始。"
          },
          "python": {
            "client": "Python · OpenAI SDK",
            "base": "base_url = OMNIHIVE_BASE_URL",
            "key": "api_key = OMNIHIVE_API_KEY",
            "model": "model = MODEL_ID_FROM_LIST",
            "notes": "先安装 openai 包；SDK 自动追加接口路径。"
          },
          "cursor": {
            "client": "Cursor",
            "base": "自定义 OpenAI API 地址",
            "key": "本站创建的完整密钥",
            "model": "MODEL_ID_FROM_LIST",
            "notes": "自定义 URL 覆盖依客户端版本而定；官方 BYOK 文档未明确通用第三方地址。使用支持该设置的版本，并确认地址能被 Cursor 的服务访问；Tab 等功能不一定使用此密钥。"
          },
          "codex": {
            "client": "Codex",
            "base": "model_providers.omnihive.base_url",
            "key": "env_key = \"OMNIHIVE_API_KEY\"",
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
          },
          "claude": {
            "client": "Claude Code CLI",
            "base": "ANTHROPIC_BASE_URL = gateway root",
            "key": "ANTHROPIC_API_KEY",
            "model": "--model MODEL_ID_FROM_LIST",
            "notes": "CLI 自动添加 /v1/messages；桌面版为不同配置入口。需要分组与模型支持 Messages。"
          },
          "opencode": {
            "client": "OpenCode (经典配置)",
            "base": "provider.<id>.options.baseURL = root + /v1",
            "key": "options.apiKey: {'{'}env:OMNIHIVE_API_KEY{'}'}",
            "model": "<provider>/MODEL_ID_FROM_LIST",
            "notes": "示例使用 {'@'}ai-sdk/openai-compatible 发送 Chat。OpenCode 2 配置格式不同；不要混用。"
          },
          "dsh": {
            "client": "DeepSeek Harness (DSH)",
            "base": "baseURL = root + /v1",
            "key": "apiKeyEnv: OMNIHIVE_API_KEY",
            "model": "MODEL_ID_FROM_LIST",
            "notes": "在提供商设置中选择 openai-completions；openai-responses 和 anthropic-messages 是独立协议选项。按安装版本的向导保存。"
          },
          "pi": {
            "client": "Pi",
            "base": "baseUrl = root + /v1",
            "key": "apiKey: $OMNIHIVE_API_KEY",
            "model": "models[].id: MODEL_ID_FROM_LIST",
            "notes": "在 ~/.pi/agent/models.json 中配置 openai-completions；打开 /model 重载并选择。"
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
            "example": "本站公布的根地址 + /v1"
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
            "meaning": "Chat、Messages、Responses 支持时设 true 开启 SSE，省略或 false 请求完整响应；Gemini 使用 streamGenerateContent。curl 使用 -N。",
            "example": "\"stream\": true"
          }
        }
      }
    },
    "notes": {
      "streaming": "流式请求、工具结果回传与推理设置见对应章节。Codex 示例使用 HTTP Responses；WebSocket 需要分组、账户和站点配置共同允许，未确认前不要启用。",
      "mediaEndpoints": "以上客户端配置使用文本/视觉协议（Messages、Responses、Chat Completions）。图像与视频使用 /v1/images/generations、/v1/images/edits 和 /v1/videos/generations；语音合成、转写与翻译分别使用 /v1/audio/speech、/v1/audio/transcriptions 和 /v1/audio/translations。可用性和请求格式取决于分组平台、模型及已启用功能。/v1/tts 与 /v1/stt 有独立平台契约，不能与标准音频端点互换。",
      "clientSources": "新增客户端的字段名称取自各自第一方文档（Cline 文档：OpenAI Compatible；Roo Code 仓库文档：Using OpenAI Compatible Providers；Continue 文档：How to Configure OpenAI Models；JetBrains 帮助：Use third-party and local models）。Windsurf 官方文档未提供自定义 OpenAI 兼容端点，故列出为不可用说明。",
      "antigravity": "Antigravity 的协议与工具支持取决于实际模型和分组；混合客户端工具时，部分上游内置工具可能被省略，请勿依赖内置搜索或代码执行一定可用。",
      "workflow": "工作流工具先选择 HTTP 方法、完整 URL、认证头和 JSON 请求体；发送一次简单请求，读取结果和 usage，再接入循环或并发。不要把客户端能配置某协议当作所有模型均支持。"
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
            "status": "403 / 429",
            "code": "INSUFFICIENT_BALANCE / insufficient_quota",
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
            "action": "检查额度与余额；余额或额度不足不能按短暂拥塞无限重试，也不一定有 Retry-After。"
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
          },
          "missing": {
            "status": "404",
            "code": "以响应正文为准",
            "meaning": "模型不可见、动作未开放或资源不归当前密钥。",
            "action": "核对模型目录、功能权限与原始任务密钥，不要只修改 URL。"
          },
          "conflict": {
            "status": "409",
            "code": "以响应正文为准",
            "meaning": "任务尚未完成或内容冲突。",
            "action": "检查任务状态与错误码，勿直接重复生成。"
          },
          "large": {
            "status": "413",
            "code": "以响应正文为准",
            "meaning": "请求或上传过大。",
            "action": "按对应文本、图片或音频限制缩小请求。"
          }
        }
      }
    },
    "notes": {
      "shape": "同一失败在不同协议中的响应结构可能不同：Chat/Responses 通常读取 error；Messages 还含 type: error；Gemini 读取 error.code、error.status 和 error.message。保留原始状态码与错误体，不能只按一条字符串判断。"
    }
  },
  "groups": {
    "start": "开始使用",
    "api": "调用 API",
    "calls": "请求期间",
    "media": "图像与音视频",
    "apps": "客户端与工作流",
    "account": "账户与排错"
  },
  "capability": "先确认密钥分组允许目标协议和模型。目录可见、功能提示和当前请求成功是不同的事实；工具、推理、缓存与媒体能力还取决于实际模型。以下为常用字段的最小子集，并非所有上游参数均可用。",
  "addresses": {
    "title": "API 地址与认证",
    "steps": {},
    "tables": {
      "formats": {
        "title": "选择正确的地址",
        "columns": {
          "field": "用途",
          "value": "地址形式",
          "notes": "说明"
        },
        "rows": {
          "root": {
            "field": "网关根地址 (Gateway root)",
            "value": "api_base_url",
            "notes": "移除末尾 /v1 或 /v1beta，保留部署前缀。Claude Code 使用此根地址。"
          },
          "base": {
            "field": "OpenAI 兼容基础地址 (OpenAI-compatible base)",
            "value": "root + /v1",
            "notes": "客户端添加端点路径；不要重复 /v1 或填入 /chat/completions。"
          },
          "full": {
            "field": "完整 HTTP 请求地址 (HTTP request URL)",
            "value": "root + /v1/chat/completions",
            "notes": "curl、PowerShell 和 fetch 使用完整 URL。Gemini 使用 /v1beta/models/…:generateContent。"
          }
        }
      }
    },
    "notes": {
      "auth": "Chat、Responses 与媒体使用 Authorization: Bearer；Messages 使用 x-api-key 和 anthropic-version: 2023-06-01；Gemini 使用 x-goog-api-key。不要在查询字符串传密钥。面板密码不是 API 密钥。"
    }
  },
  "cc-switch": {
    "title": "CC Switch（可选）",
    "steps": {
      "import": {
        "title": "从密钥页面导入",
        "body": "创建密钥后在一次性显示密钥的区域选择导入，或在密钥操作中使用已有入口。选择 Claude 或 Gemini，确认目标应用和地址后再导入。"
      }
    },
    "tables": {},
    "notes": {
      "optional": "直接填写 URL、密钥和模型即可使用；CC Switch 不是必需依赖。导入包含密钥，不要分享导入链接。未配置本站 API 地址时不应生成导入链接。",
      "address": "分组可能改变导入的应用及地址形式：根地址、/v1 或 /antigravity。请在 CC Switch 中核对最终地址，不能把一个分组的设置视为所有分组通用。",
      "conversion": "CC Switch 也可以运行本地协议转换代理，例如把客户端的 Responses 请求转换成上游 Chat 请求。wire_api = responses 描述客户端一侧的协议，单凭此字段不能证明供应商原生支持 /responses。直连上游 Responses 与本地转 Chat 是两种不同配置。",
      "gateway": "调用 OmniHive 时，客户端始终填写本站地址和本站 Key。本站可在符合条件的分组 / 账号路由上转换 /v1/messages、/v1/chat/completions、/v1/responses 或 /v1beta/models/… 请求。CC Switch 本地转换和网关转换属于不同层，应核对实际选用协议，不要凭名称默认叠加转换。"
    }
  },
  "chat": {
    "title": "Chat Completions 对话",
    "endpoint": "POST /v1/chat/completions",
    "intro": "适用于发送 OpenAI 兼容消息历史的客户端。选择密钥分组中可调用 Chat 的模型。",
    "steps": {},
    "tables": {
      "request": {
        "title": "请求字段",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "model": {
            "field": "model",
            "type": "string · 必填",
            "meaning": "目录中的精确 ID，保留供应商前缀。"
          },
          "messages": {
            "field": "messages",
            "type": "array · 必填",
            "meaning": "按顺序提交 role/content 消息；后续请求重新提交上下文。"
          },
          "stream": {
            "field": "stream",
            "type": "boolean · 可选",
            "meaning": "true 开启 SSE；默认示例请求完整 JSON。"
          }
        }
      },
      "result": {
        "title": "读取结果",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "choices_message": {
            "field": "choices[].message",
            "type": "对象 (object)",
            "meaning": "文本位于 content；工具调用时可能只有 tool_calls。"
          },
          "choices_finish_reason_usage": {
            "field": "choices[].finish_reason / usage",
            "type": "字符串 / 对象 (string / object)",
            "meaning": "检查结束原因和已报告用量。"
          }
        }
      }
    },
    "notes": {
      "behavior": "不要未经转换就把 Messages system 块或 Responses input 项作为 Chat 消息。"
    }
  },
  "messages": {
    "title": "Messages 对话",
    "endpoint": "POST /v1/messages",
    "intro": "选择支持 Messages 协议的模型和分组。使用 x-api-key 与 anthropic-version 请求头。",
    "steps": {},
    "tables": {
      "request": {
        "title": "请求字段",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "model": {
            "field": "model",
            "type": "string · 必填",
            "meaning": "目录中的请求 ID。"
          },
          "max_tokens": {
            "field": "max_tokens",
            "type": "integer · 必填",
            "meaning": "输出预算，不超过所选模型限制。"
          },
          "messages": {
            "field": "messages",
            "type": "array · 必填",
            "meaning": "user/assistant 消息；content 可以是文本或内容块。"
          },
          "system": {
            "field": "system",
            "type": "string / array · 可选",
            "meaning": "顶层系统指令，与 messages 分开。"
          }
        }
      },
      "result": {
        "title": "读取结果",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "content": {
            "field": "content[]",
            "type": "数组 (array)",
            "meaning": "按类型读取 text、thinking 或 tool_use 内容块。"
          },
          "stop_reason_usage": {
            "field": "stop_reason / usage",
            "type": "字符串 / 对象 (string / object)",
            "meaning": "检查停止原因与输入/输出用量。"
          }
        }
      }
    },
    "notes": {
      "behavior": "POST /v1/messages/count_tokens 是依模型而定的计数或估算，不证明生成可用。"
    }
  },
  "responses": {
    "title": "Responses 响应",
    "endpoint": "POST /v1/responses",
    "intro": "Codex 等 Responses 客户端需使用允许 Responses 的分组与模型。默认采用 HTTP 接入。",
    "steps": {},
    "tables": {
      "request": {
        "title": "请求字段",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "model": {
            "field": "model",
            "type": "string · 必填",
            "meaning": "精确模型 ID。"
          },
          "input": {
            "field": "input",
            "type": "string / array · 必填",
            "meaning": "文本或按顺序排列的输入项。"
          },
          "instructions": {
            "field": "instructions",
            "type": "string · 可选",
            "meaning": "本次请求的指令。"
          },
          "max_output_tokens": {
            "field": "max_output_tokens",
            "type": "integer · 可选",
            "meaning": "按模型设置输出预算。"
          }
        }
      },
      "result": {
        "title": "读取结果",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "output": {
            "field": "output[]",
            "type": "数组 (array)",
            "meaning": "按 message、reasoning、function_call 等类型解析。"
          },
          "status_incomplete_details_error": {
            "field": "status / incomplete_details / error",
            "type": "状态与错误对象字段",
            "meaning": "仅 completed 表示完整完成；检查 incomplete 或 failed 的详情。"
          },
          "usage": {
            "field": "usage",
            "type": "对象 (object)",
            "meaning": "已报告用量与最终结算状态分开。"
          }
        }
      }
    },
    "notes": {
      "behavior": "未确认路由支持状态续接时，应重新提交完整历史。previous_response_id 受归属与账户绑定约束；store:true 不保证状态已保存。",
      "compatibility": "GET /v1/responses 是有条件开放的 WebSocket 入口，不是读取已保存响应。"
    }
  },
  "gemini": {
    "title": "Gemini / Google 协议",
    "endpoint": "POST /v1beta/models/{'{'}model{'}'}:generateContent",
    "intro": "将模型 ID 放入 URL，并使用 x-goog-api-key 认证。分组和模型必须允许对应 Google 动作。",
    "steps": {},
    "tables": {
      "request": {
        "title": "请求字段",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "model_URL": {
            "field": "model (URL)",
            "type": "编码路径段 · 必填",
            "meaning": "使用请求 ID；去掉目录 name 开头的 models/ 后，对剩余 ID 编码一次。"
          },
          "contents": {
            "field": "contents",
            "type": "array · 必填",
            "meaning": "包含 parts，例如 {'{'}text:\"Hello\"{'}'}；role 可为 user。"
          },
          "generationConfig": {
            "field": "generationConfig",
            "type": "object · 可选",
            "meaning": "仅使用所选模型支持的控制项。"
          }
        }
      },
      "result": {
        "title": "读取结果",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "candidates_content_parts": {
            "field": "candidates[].content.parts",
            "type": "数组 (array)",
            "meaning": "逐个读取内容；安全阻断可能没有文本。"
          },
          "finishReason_usageMetadata": {
            "field": "finishReason / usageMetadata",
            "type": "结束原因与用量字段",
            "meaning": "检查结束原因与已报告用量。"
          }
        }
      }
    },
    "notes": {
      "behavior": "流式使用 :streamGenerateContent?alt=sse，而不是 Chat 的 stream 标志。:countTokens 依模型而定；:embedContent 与 :batchEmbedContents 需要可用的向量路由。"
    }
  },
  "retrieval": {
    "title": "向量与重排",
    "endpoint": "POST /v1/embeddings · POST /v1/rerank",
    "intro": "从有权限的分组选择专用向量或重排模型，不能用任意聊天模型替代。",
    "steps": {},
    "tables": {
      "request": {
        "title": "请求字段",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "model": {
            "field": "model",
            "type": "string · 必填",
            "meaning": "用于对应检索端点的模型。"
          },
          "input_embeddings": {
            "field": "input (embeddings)",
            "type": "string · 示例中必填",
            "meaning": "待编码文本；维数与编码选项依模型而定。"
          },
          "query_documents_rerank": {
            "field": "query / documents (rerank)",
            "type": "string / string[] · 必填",
            "meaning": "查询文本与候选文档。"
          },
          "top_n_rerank": {
            "field": "top_n (rerank)",
            "type": "正整数 · 可选",
            "meaning": "请求返回的排序结果数量。"
          }
        }
      },
      "result": {
        "title": "读取结果",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "data_embedding_index": {
            "field": "data[].embedding / index",
            "type": "向量 / 整数",
            "meaning": "向量与输入位置；检查向量维数一致。"
          },
          "index_relevance_score": {
            "field": "index / relevance_score",
            "type": "重排结果条目",
            "meaning": "重排结果条目；外层结构因路由而异。"
          },
          "usage": {
            "field": "usage",
            "type": "对象 · 有上报时返回",
            "meaning": "缺失计数表示未知；重排可能按查询而非 token 计费。"
          }
        }
      }
    },
    "notes": {
      "behavior": "这些端点不代表支持流式或工具。OpenAI 适配器未提供 rerank，需使用具有重排路由的分组。"
    }
  },
  "streaming": {
    "title": "流式输出与中断",
    "endpoint": "POST · 各协议专用 SSE 流",
    "intro": "所选协议的流式能力需分组与模型支持。使用 curl -N 或可逐段读取 SSE 的 SDK。",
    "steps": {},
    "tables": {
      "request": {
        "title": "请求字段",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "stream": {
            "field": "stream",
            "type": "布尔值 · Chat / Messages / Responses",
            "meaning": "在 JSON 请求中设为 true。"
          },
          "alt_sse": {
            "field": "alt=sse",
            "type": "查询参数 · Gemini",
            "meaning": "与 :streamGenerateContent 一起使用。"
          }
        }
      },
      "result": {
        "title": "读取结果",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "Chat": {
            "field": "Chat",
            "type": "choices[].delta 增量帧",
            "meaning": "拼接 delta；通常以 [DONE] 结束。错误或提前 EOF 不代表完成。"
          },
          "Messages": {
            "field": "Messages",
            "type": "event + data 事件帧",
            "meaning": "读取 content_block 增量直到 message_stop；event:error 表示失败。"
          },
          "Responses": {
            "field": "Responses",
            "type": "类型化事件帧",
            "meaning": "response.completed 为成功；response.incomplete 和 response.failed 需检查原因。"
          },
          "Gemini": {
            "field": "Gemini",
            "type": "Google JSON 事件帧",
            "meaning": "读取 candidates 和最终 finishReason/usage，不等待 Chat 的 [DONE]。"
          }
        }
      }
    },
    "notes": {
      "behavior": "按空行分隔 SSE 事件，忽略保活注释。HTTP 200 后仍可能失败；工具参数片段需保留到调用完整。",
      "compatibility": "中断输出不会续成一份完整答案。重发前先查用量或已有任务；并非所有重试都具备幂等性。PowerShell 快速开始仅展示非流式 JSON。"
    }
  },
  "tools": {
    "title": "函数工具与结果回传",
    "endpoint": "POST · Chat / Messages / Responses / Gemini",
    "intro": "选择分组允许且支持工具的模型。客户端函数由你的应用执行并回传结果；模型也可能不调用工具。",
    "steps": {},
    "tables": {
      "request": {
        "title": "请求字段",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "Chat_tools": {
            "field": "Chat tools[]",
            "type": "type:function + function",
            "meaning": "function 包含 name、description 和 parameters（JSON Schema）。"
          },
          "Messages_tools": {
            "field": "Messages tools[]",
            "type": "name + input_schema",
            "meaning": "使用 Messages 的工具定义结构。"
          },
          "Responses_tools": {
            "field": "Responses tools[]",
            "type": "type:function + name + parameters",
            "meaning": "函数字段直接位于工具对象上。"
          },
          "Gemini_tools": {
            "field": "Gemini tools",
            "type": "functionDeclarations",
            "meaning": "使用 Google 函数声明。"
          }
        }
      },
      "result": {
        "title": "读取结果",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "Chat_result": {
            "field": "Chat 工具结果",
            "type": "role:tool / tool_call_id / content",
            "meaning": "匹配真实调用 ID，并保留 assistant 的 tool_calls 消息。"
          },
          "Messages_result": {
            "field": "Messages 工具结果",
            "type": "tool_result / tool_use_id",
            "meaning": "将结果放入 user 内容块。"
          },
          "Responses_result": {
            "field": "Responses 工具结果",
            "type": "function_call_output / call_id / output",
            "meaning": "匹配 call_id 并保留前序输入/输出项。"
          },
          "Gemini_result": {
            "field": "Gemini 工具结果",
            "type": "functionResponse",
            "meaning": "在对话上下文中返回函数结果。"
          }
        }
      }
    },
    "notes": {
      "behavior": "保留真实调用 ID、参数及不透明推理/签名。不要编造 ID 或丢弃前序 assistant 项。函数工具能力不代表内置搜索、代码执行或所有服务端工具可用。"
    }
  },
  "reasoning": {
    "title": "推理控制",
    "endpoint": "POST · 模型专用思考控制",
    "intro": "仅使用模型声明且分组路由支持的档位。目录未说明时省略控制项，不默认 high 或 max 可用。",
    "steps": {},
    "tables": {
      "request": {
        "title": "请求字段",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "reasoning_effort": {
            "field": "reasoning_effort",
            "type": "Chat · 可选",
            "meaning": "值取自所选模型支持的档位。"
          },
          "reasoning_effort_reasoning_summary": {
            "field": "reasoning.effort / reasoning.summary",
            "type": "Responses · 可选",
            "meaning": "推理档位和摘要行为依模型而定。"
          },
          "thinking_output_config_effort": {
            "field": "thinking / output_config.effort",
            "type": "Messages · 可选",
            "meaning": "支持时 thinking 使用 type 与 budget_tokens。"
          },
          "generationConfig_thinkingConfig": {
            "field": "generationConfig.thinkingConfig",
            "type": "Gemini · 可选",
            "meaning": "Google 控制结构，不能与其他协议互换。"
          }
        }
      },
      "result": {
        "title": "读取结果",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "reasoning_thinking": {
            "field": "reasoning / thinking",
            "type": "各协议专用思考输出",
            "meaning": "可能含摘要或不透明续接数据；工具往返时保留这些内容。"
          }
        }
      }
    },
    "notes": {
      "behavior": "档位不是 token 预算、延迟或质量保证。temperature、并行工具等可能与模型推理模式冲突。"
    }
  },
  "cache": {
    "title": "提示缓存与用量",
    "endpoint": "POST · 模型专用缓存控制",
    "intro": "缓存取决于模型、分组路由和上游策略。保持上下文前缀稳定；目录中的缓存提示不保证命中或折扣。",
    "steps": {},
    "tables": {
      "request": {
        "title": "请求字段",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "cache_control": {
            "field": "cache_control",
            "type": "Messages 内容块 · 可选",
            "meaning": "支持的 Messages 路由可接收 ephemeral 缓存提示，路由可能调整它。"
          }
        }
      },
      "result": {
        "title": "读取结果",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "cache_creation_input_tokens": {
            "field": "cache_creation_input_tokens",
            "type": "数值 · 有上报时返回",
            "meaning": "创建缓存的 token，与缓存读取分开。"
          },
          "cache_read_input_tokens": {
            "field": "cache_read_input_tokens",
            "type": "数值 · 有上报时返回",
            "meaning": "从缓存读取的 token；以实际用量和最终账单核对。"
          }
        }
      }
    },
    "notes": {
      "behavior": "缺失缓存字段表示未知，不是零。不同协议不存在通用的 TTL、缓存键或价格倍率。"
    }
  },
  "images": {
    "title": "图像生成与编辑",
    "endpoint": "POST /v1/images/generations · /v1/images/edits · /v1/images/variations",
    "intro": "使用允许对应图像动作的分组与模型。生成、编辑和变体是不同能力。",
    "steps": {},
    "tables": {
      "request": {
        "title": "请求字段",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "model_prompt": {
            "field": "model / prompt",
            "type": "JSON 字符串 · 图像生成",
            "meaning": "选择图像模型并描述画面。"
          },
          "image_model_prompt": {
            "field": "image / model / prompt",
            "type": "multipart · OpenAI 或千问编辑示例",
            "meaning": "使用 curl -F 上传 reference.png，由 curl 设置 multipart boundary。"
          },
          "mask": {
            "field": "mask",
            "type": "multipart · 仅限 OpenAI 子集",
            "meaning": "仅限所选模型接受时使用；千问编辑不接受 mask。"
          }
        }
      },
      "result": {
        "title": "读取结果",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "data_url_data_b64_json": {
            "field": "data[].url / data[].b64_json",
            "type": "字符串 (string)",
            "meaning": "依路由和格式返回 URL 或 base64 图像数据。"
          }
        }
      }
    },
    "notes": {
      "behavior": "OpenRouter 仅支持同步位图生成：b64_json 与 png/jpeg/webp，不支持 URL 输出、SSE、编辑或变体。千问编辑接受 1–3 张上传图，每张最多 10 MiB，仅输出 b64_json。",
      "compatibility": "其他路由可能要求不同的 JSON 或 multipart 字段，不要跨平台照搬高级参数。"
    }
  },
  "async-images": {
    "title": "异步图像任务",
    "endpoint": "POST /v1/images/generations/async · /v1/images/edits/async",
    "intro": "仅在功能开启、分组解析为符合条件的 OpenAI/Grok 图像模型时可用。禁用时提交不会创建任务；已接受任务仍可能可查。",
    "steps": {},
    "tables": {
      "request": {
        "title": "请求字段",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "request_body": {
            "field": "请求体 (request body)",
            "type": "与同步动作相同",
            "meaning": "使用生成 JSON 或对应编辑上传格式；不接受 stream。"
          }
        }
      },
      "result": {
        "title": "读取结果",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "HTTP_202_task_id_poll_url": {
            "field": "HTTP 202 / task_id / poll_url",
            "type": "已受理任务",
            "meaning": "保存任务 ID，按 Retry-After（当前 3 秒）间隔查询，不要重复提交。"
          },
          "status": {
            "field": "status",
            "type": "任务状态（processing / completed / failed）",
            "meaning": "使用原密钥 GET /v1/images/tasks/{'{'}task_id{'}'}；completed 或 failed 后停止轮询。"
          },
          "result_http_status_error": {
            "field": "result / http_status / error",
            "type": "终态详情",
            "meaning": "完成后读取 result 中的图片 URL；失败时检查状态和 error。"
          }
        }
      }
    },
    "notes": {
      "behavior": "任务同时绑定用户和密钥，同一用户的另一把密钥也不能查询。单次异步图像没有公共取消、删除或独立二进制下载端点。",
      "compatibility": "图像批次是独立的 Gemini 分组功能，有专用目录、权限和生命周期，请查看批量生图指南。"
    }
  },
  "media-jobs": {
    "title": "视频任务",
    "endpoint": "POST /v1/videos/generations · GET /v1/videos/{'{'}id{'}'} · GET /v1/videos/{'{'}id{'}'}/content",
    "intro": "视频是任务流程，不是聊天响应。示例限定符合条件的 OpenRouter 视频模型与分组；时长需符合所选模型。",
    "steps": {},
    "tables": {
      "request": {
        "title": "请求字段",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "model_prompt": {
            "field": "model / prompt",
            "type": "JSON 字符串 · OpenRouter 示例",
            "meaning": "视频模型与描述；具体模型可能有额外要求。"
          },
          "duration": {
            "field": "duration",
            "type": "正整数 · OpenRouter",
            "meaning": "示例为 4 秒，不代表所有模型允许此值或实际交付时长。"
          }
        }
      },
      "result": {
        "title": "读取结果",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "id_status": {
            "field": "id / status",
            "type": "OpenAI / OpenRouter 任务状态",
            "meaning": "保存返回的公开 ID；queued → in_progress → completed，或失败终态。"
          },
          "content": {
            "field": "/content",
            "type": "完成后下载媒体内容",
            "meaning": "完成后使用原密钥下载；打开前核对 HTTP 状态和 Content-Type。"
          }
        }
      }
    },
    "notes": {
      "behavior": "OpenAI 编辑/延长使用 video:{'{'}id:...{'}'} 引用本密钥已完成的 OpenAI 视频。OpenRouter 不支持编辑、延长或取消。千问/千帆支持的取消为独立动作；收到确认不代表已最终取消或退款。",
      "compatibility": "不要使用上游任务 ID 或远程 URL 代替本站返回的 ID；其他平台状态结构可能不同。"
    }
  },
  "audio": {
    "title": "语音合成、转写与翻译",
    "endpoint": "POST /v1/audio/speech · /v1/audio/transcriptions · /v1/audio/translations",
    "intro": "每个动作都需选择具备音频能力的模型与授权分组。示例区分 OpenRouter MP3、千问 WAV 和 OpenAI 音频翻译。",
    "steps": {},
    "tables": {
      "request": {
        "title": "请求字段",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "model_input_voice": {
            "field": "model / input / voice",
            "type": "语音合成 JSON",
            "meaning": "文本及模型支持的音色；千问要求 voice，文本为 1–600 个 Unicode 字符。"
          },
          "response_format": {
            "field": "response_format",
            "type": "语音合成 · 按路由而定",
            "meaning": "OpenRouter：mp3/pcm，不支持 stream；千问：仅 wav。"
          },
          "file_model": {
            "field": "file / model",
            "type": "音频翻译 multipart · OpenAI API-key 路由",
            "meaning": "选择支持翻译的模型；翻译与转写为不同动作。"
          }
        }
      },
      "result": {
        "title": "读取结果",
        "columns": {
          "field": "字段",
          "type": "类型 / 要求",
          "meaning": "说明"
        },
        "rows": {
          "speech_response": {
            "field": "语音合成响应",
            "type": "二进制音频流或模型专用 JSON 信封",
            "meaning": "检查 Content-Type。Mistral 可能返回 audio_data base64 JSON，不要把所有响应当 MP3。"
          },
          "transcription_response": {
            "field": "音频转写响应",
            "type": "示例返回 JSON",
            "meaning": "读取文本及已提供用量；输出格式因路由而异。"
          }
        }
      }
    },
    "notes": {
      "behavior": "千问转写仅接受 file/model/response_format，输出为 json；OpenRouter 支持 json/verbose_json。/v1/tts 与 /v1/stt 有专用契约，不能通用套用 OpenAI 请求体。"
    }
  },
  "references": "客户端官方参考（配置随版本变化）",
  "resources": {
    "title": "文件、批次与专用接口",
    "intro": "这些接口需单独确认分组、模型与动作权限。已注册的路径不代表当前密钥可用，通用对话示例不能直接作为它们的请求体。",
    "steps": {},
    "tables": {
      "paths": {
        "title": "资源定位",
        "columns": {
          "endpoint": "方法 / 路径",
          "purpose": "用途与边界"
        },
        "rows": {
          "files": {
            "endpoint": "POST /v1/files · GET /v1/files · GET/DELETE /v1/files/{'{'}id{'}'} · GET /v1/files/{'{'}id{'}'}/content",
            "purpose": "上传、列举、查看或下载归属文件；删除需明确执行。"
          },
          "batches": {
            "endpoint": "POST/GET /v1/batches · GET /v1/batches/{'{'}id{'}'} · POST /v1/batches/{'{'}id{'}'}/cancel",
            "purpose": "文本批处理；提交一次、保存 ID、再查询。与图像批次不同。"
          },
          "messages": {
            "endpoint": "POST/GET /v1/messages/batches · GET/DELETE /v1/messages/batches/{'{'}id{'}'} · POST …/{'{'}id{'}'}/cancel · GET …/{'{'}id{'}'}/results",
            "purpose": "Messages 批次生命周期和结果需对应路由支持。"
          },
          "images": {
            "endpoint": "POST/GET /v1/images/batches · GET …/models · GET …/{'{'}id{'}'} · GET …/{'{'}id{'}'}/items · GET …/{'{'}id{'}'}/download",
            "purpose": "Gemini 分组图像批次具有专用模型目录及密钥/分组归属。单项下载、取消、输出或记录删除请查看专用指南。"
          },
          "moderations": {
            "endpoint": "POST /v1/moderations",
            "purpose": "内容审核是独立动作，不是聊天结果。"
          },
          "realtime": {
            "endpoint": "GET /v1/realtime · POST /v1/live · GET /v1/live/{'{'}call_id{'}'}",
            "purpose": "实时/Live 会话需匹配协议的客户端。千问实时保留 DashScope 事件，不能全部按 OpenAI Realtime 解析。"
          },
          "search": {
            "endpoint": "POST /v1/alpha/search · /v1/web_search · /v1/x_search · /v1/tools/search · /v1/tools/search_pro",
            "purpose": "专用搜索动作；函数工具能力不代表可用这些路由。"
          },
          "voices": {
            "endpoint": "POST/GET /v1/custom-voices · GET/PATCH/DELETE /v1/custom-voices/{'{'}voice_id{'}'} · GET …/{'{'}voice_id{'}'}/audio",
            "purpose": "音色管理具有平台、方法及归属限制；不能从语音合成能力推断可用。"
          }
        }
      }
    },
    "notes": {
      "native": "/api/v1/services/* 等 DashScope 原生路径、/compatible-mode/v1/files 与 batches 有各自契约，不能把通配路径当作任意供应商 API 代理。需要专用调用时请先确认该模型对应的请求格式。"
    }
  },
  "suppliers": {
    "title": "供应商、套餐与访问范围",
    "intro": "供应商是管理员配置的上游账号来源。客户端使用本站 API Key 连接本站；目录里的供应商名称不代表另一个公开网关地址。",
    "steps": {},
    "tables": {
      "scope": {
        "title": "各项配置应该填在哪里？",
        "columns": {
          "setting": "配置项",
          "owner": "配置位置",
          "meaning": "作用与边界"
        },
        "rows": {
          "public": {
            "setting": "公开 URL + 本站 API Key",
            "owner": "你的客户端或工作流",
            "meaning": "OmniHive 公开 Base URL：https://api.omnihive.io/v1。Chat 与 Responses 客户端自行追加操作路径；Claude Code 使用根地址 https://api.omnihive.io；Gemini 使用 /v1beta/models/…:generateContent 或 :streamGenerateContent。"
          },
          "supplier": {
            "setting": "供应商账号 + 区域 / 套餐密钥",
            "owner": "管理员的账号配置",
            "meaning": "选择供应商、区域、按量 / Coding / Token Plan、上游协议及匹配凭据。供应商 API 地址和账号密钥只填在管理员上游配置，不填进调用本站的客户端。"
          },
          "group": {
            "setting": "本站 Key 分组 + 精确模型 ID",
            "owner": "API 密钥页与模型目录",
            "meaning": "分组决定可用路由、模型、账号选择和计费范围。用同一把 Key 查询 /v1/models 和 /v1/models/catalog；增加供应商配置不等于所有用户自动获得权限。"
          },
          "unknown": {
            "setting": "未知的模型发现、额度或价格",
            "owner": "模型目录、用量与价格页面",
            "meaning": "没有模型发现接口不等于没有模型权限；额度缺失不等于零或无限；价格缺失不等于免费。未知时请确认模型映射和本站销售价格。"
          },
          "capabilities": {
            "setting": "工具、流式、思考与缓存",
            "owner": "模型与协议的能力元数据",
            "meaning": "分别核对模型和实际路由。文本转换不能证明所有工具结构、推理设置、缓存折扣、多模态输入或上游原生媒体生成可用。"
          }
        }
      }
    },
    "notes": {
      "coverage": "供应商系列、地区、产品与套餐以当前后台目录为准，账号与分组权限决定实际访问范围。目录包含 Astron Token Plan、小米 MiMo 中国/欧洲/新加坡等独立产品和集群；相似名称不代表密钥互通。Copilot / Nous 支持手工凭据子集：fine_grained_pat、invocation_token、official_proxy，无自动 OAuth 或 refresh。上游仅 Chat，本站 Messages / Responses / Gemini 桥接经过离线合同测试，不是供应商在线认证；真实可用性需运营方授权 key 验证。内部 payg 路由不表示供应商按量计费。RelayCode 身份未解决，与已实现的 RelaxyCode 分开；ETok.ai 是历史移除的预设，不是停服声明。",
      "plans": "区域根地址、套餐 key 与未知元数据分别保留。Tencent Intl 个人套餐不能从共享主机继承企业 Responses；企业需明确配置 Responses 端点。Copilot 使用 Copilot-Integration-Id: omnihive，根 /models 与 /v1/chat/completions 分开。Nous invocation token 到期手工替换；official_proxy 需显式可移植的运营方 URL 前缀（不自动加 /v1），入口 key 可选。通过单账号编辑切换 auth_mode（包括密钥引用账号），必须新 key 或在无 key 代理时明确清空；同模式编辑保留密钥引用。不读取客户端 token 存储或共享 refresh grants。消费者订阅、Coding 套餐不自动授予 API、共享或自动化权限。",
      "media": "图片、视频和音频路由分别见对应章节。增加兼容文本供应商不等于新增原生媒体适配器；调用前须确认具体媒体动作、分组和模型。",
      "evidence": "手工钱包查询支持 Novita / PPIO（USD / CNY，除以 10000）、SiliconFlow CN / Global（CNY / USD，不缩放）、SoleAPI（remaining Credits，不折算现金）及 AtlasCloud PAYG（账户余额、返回币种，需读取权限）。胜算云 common 返回 CNY 钱包，enterprise 返回 project_budget，保留预算周期与无限状态；不是个人钱包。查询结果附时间，不支持或失败不是零余额。CC Switch 固定提交 793e67d9b3210eb527e7d36f2c866a56fae9dec7 是配置证据，不是运行认证；供应商文档不自动成为本站价格或功能承诺。",
      "selection": "管理员按认证方式、套餐类型、协议核对账号。有套餐目录时选择并保存 billing_plan：AICodeMirror 提供 PRO / MAX / ULTRA / 按量付费，CCSub 提供月度订阅 / 按量付费；编辑会回显，清除移除选择。未选保持未设置，选择不购买额度、不改变模型权限或传输地址。AtlasCloud Coding、七牛订阅继续使用独立接入模式与专用 Key。本站用户订阅与上游供应商套餐分别管理。",
      "usage": "七牛 PAYG 手工查询 consumed_usage：今天、近 7 天或近 30 天；day 粒度、明确时区的 RFC3339 起止、最多 31 天，默认 Asia/Shanghai。按模型/计费项保留 kToken 等原单位，不合计不同单位、不当作剩余额度。钱包余额、上游套餐额度、项目预算和本站账本消耗分别核对；本站记录不包含绕过本站的调用。保存 plan 套餐会阻止未验证的 PAYG 查询；AtlasCloud Coding、七牛订阅没有对应已接入的套餐余额查询。 Atlas 日模型消耗保留独立余额查询；Azure 展示订阅区域配额，GitHub 展示个人 premium request/AI credit 账单，Together 展示组织月账单（需 beta 权限）。腾讯国内企业 Token 套餐需独立 Secret ID/Key、地域、团队；套餐/周期/账单数量保留精确字符串，call_count 仍是次数。MiniMax 新国内 Token Plan 仅适用明确官方档案。MiniMax 当前中国 api.minimax.cn 与 Global api.minimax.io 的 API Key 按量账号可手动查余额，后台验证 Key 类别；旧 minimaxi.com、Coding、自定义和跨地区配置不借用余额接口。上游余额不缩放，未知币种不默认人民币或美元，真实零保留，失败不补零。TheRouter 展示总 credits 与总使用量，不相减推测余额。查询配置可选，不阻碍保存推理，缺配置零出站。真实身份变化清旧读数；普通刷新及同身份失败保留带错误提示的历史值。",
      "queryErrors": "查询未接入、当前模式不可查、自定义地址不匹配、密钥无效、权限不足、代理不可用、限流、上游失败与格式错误分别处理；未知不等于零或无限。先看成功状态与时间，再看数值；不要把旧钱包快照当实时余额。这些查询在管理员账号列表操作，普通本站模型 Key 不具备管理查询权限。",
      "configuration": "直接 URL + Key：客户端使用本站地址、本站 Key 与当前组可见的精确模型 ID。CC Switch 是可选配置工具，其预设、模型映射和本地协议转换需单独核对，不能代替网关授权。模型发现/同步与实际调用分开；没有发现接口时仍可由管理员维护已确认的模型映射。",
      "volcengineQuota": "火山 Ark Coding 个人/团队套餐：管理员另配用量查询 AK/SK，与推理 Key 分开；密钥不回显，编辑留空保留，明确清除才删除。旧账号缺省 personal；team 必须明确保存席位，缺失零出站，不调用 GetSeatInfo。5h / weekly / monthly 只显示已用百分比和重置时间，不填造 used/total、Token 或现金；重置不等于订阅到期。缺凭据不影响原推理配置。国内团队 Ark Agent Plan 需明确席位，复用独立 AK/SK，支持每日及 5h/周/月实际返回窗口；个人 Agent 与 BytePlus 个人查询未接入。BytePlus 团队沿用 billing_plan=team，另配 byteplus_access_key_id / byteplus_secret_access_key 和 byteplus_seat_id，不借用国内密钥或主机。",
      "quotaStatus": "火山查询先看 success / status 和查询时间：ok、missing_credentials、invalid_credentials、permission_denied、unsupported_profile、no_verified_query、not_subscribed、no_seat、invalid_response、upstream_error、url_rejected 分别代表成功、缺凭据、凭据错误、权限不足、档案不支持、未核实查询、未订阅、缺席位配置或上游无该席位数据、响应无效、上游失败、出站策略拒绝。HTTP 200 不等于业务成功；普通失败不当零、保留历史；identity_changed 清除旧身份观测，旧读数须结合更新时间判断，缺窗口也不补零。",
      "modelSync": "模型同步或保存失败保留原模型 ID、别名映射、白名单、选中项与上次成功目录。discovered_models 才是上游实际发现；candidate_models / 平台预设仍是候选，不证明账号可用，手工选择不等于发现成功。候选不能单独使目录 ready 或写入 eligible_models。discovery=none 默认尚未确认列表接口，不等于官方确认没有接口；保持关闭，不猜路径。 AiHubMix、DMX 中国站、Qiniu PAYG 已接入验证的模型列表；其他地区/订阅模式不继承。反重力编辑保存并重开保留别名与直接模型 ID 的区别。",
      "modelSyncErrors": "模型同步错误以 upstream_model_sync_ 原因码区分：configuration 配置、credentials 凭据、account_type 账号类型、not_adapted 未适配、discovery_unconfirmed 未确认、endpoint_not_found 上游404路径、method_not_allowed 上游405方法、permission 上游403权限、rate_limited 上游429限流、network 网络、upstream 上游、internal 内部。403 没有明确认证错误码时不直接认定 Key 失效；未知原因显示同步失败，不归为账号类型。不要为修复错误清空配置；同步成功仍需验证实际调用。"
    }
  }
}
