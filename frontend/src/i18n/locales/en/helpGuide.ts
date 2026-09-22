export default {
  "title": "Configuration and usage manual",
  "subtitle": "Account setup, key fields, model catalogs, client configuration, and quota troubleshooting. Browse the contents for settings, procedures, and error handling.",
  "eyebrow": "USER MANUAL · CONFIGURATION & GETTING STARTED",
  "contents": "On this page",
  "copy": "Copy",
  "copied": "Copied to clipboard",
  "copyFailed": "Copy failed. Select the code and copy it manually.",
  "next": "Related pages",
  "placeholder": "Example API addresses come from this deployment's configured api_base_url; replace the key with your own and use a model ID returned by the catalogue. The curl examples use Bash / zsh syntax; on Windows, use WSL or Git Bash.",
  "apiBaseMissing": "This deployment has no public API base URL configured (api_base_url), so this page cannot generate integration examples. Ask an administrator to configure it.",
  "quick": {
    "title": "Quick start",
    "steps": {
      "account": {
        "title": "Register or sign in with an existing account",
        "body": "Registration depends on site availability; ask the administrator for an account if it is closed. After signing in, open API Keys to find the API address. It may differ from the control-panel URL; do not use /keys, /login, or administration paths."
      },
      "funding": {
        "title": "Prepare balance or an active subscription",
        "body": "If a new account has no balance or active subscription, check Top-up / Subscription first; a redemption code can also grant its applicable entitlement. Available purchase options depend on this site."
      },
      "key": {
        "title": "Create and copy an API key",
        "body": "Enter a recognizable name, choose an accessible group, save, and copy the complete key. Generated keys use sk- by default; do not add or remove a prefix. Ask the administrator for access if no group is available."
      },
      "catalog": {
        "title": "Fetch the catalog for this key",
        "body": "Run the environment and model discovery commands below first, then copy an id from the returned list into the request model field."
      },
      "request": {
        "title": "Send a request and check its result",
        "body": "Run the chat request below; see the client chapter for other configurations. On failure, consult troubleshooting and check usage or existing results before retrying to avoid duplicate charges."
      }
    },
    "tables": {},
    "notes": {}
  },
  "keys": {
    "title": "Create and manage API keys",
    "steps": {},
    "tables": {
      "fields": {
        "title": "Key fields",
        "columns": {
          "field": "Setting",
          "meaning": "Meaning and action",
          "example": "Example"
        },
        "rows": {
          "name": {
            "field": "Name",
            "meaning": "Identifies a device or app. Use separate keys to inspect usage and disable access individually.",
            "example": "Personal laptop"
          },
          "group": {
            "field": "Group",
            "meaning": "Controls models, billing, and subscription scope. Refresh the catalog after editing a key or changing its group.",
            "example": "An accessible group matching your subscription"
          },
          "expiry": {
            "field": "Expiration",
            "meaning": "Set an appropriate validity period; after expiry, adjust it or replace the key.",
            "example": "An expiry matching the intended usage period"
          },
          "quota": {
            "field": "Quota",
            "meaning": "A key spending cap, not a top-up; account balance, group, and subscription limits still apply.",
            "example": "A $10 cap does not add $10 to your balance"
          },
          "ip": {
            "field": "IP allowlist / blocklist",
            "meaning": "Optionally restrict request sources; check that the client egress IP meets the rules.",
            "example": "Allowlist: your client egress IP"
          },
          "status": {
            "field": "Status",
            "meaning": "active: enabled; inactive: switched off; expired: past validity; quota_exhausted: spending cap reached.",
            "example": "active (balance and subscription limits still apply)"
          }
        }
      }
    },
    "notes": {
      "actions": "Procedure: API Keys → Create → fill fields → Save → Copy or Use. Actions include disable, enable, edit, and delete; enabling does not refill balance or renew subscriptions.",
      "security": "Deleted keys stop working; replace the key in clients before deleting it. Keep real keys out of screenshots, public repositories, and chat logs."
    }
  },
  "models": {
    "title": "Choose an available model",
    "steps": {},
    "tables": {
      "layers": {
        "title": "Three catalog layers",
        "columns": {
          "layer": "Layer",
          "content": "Content",
          "purpose": "Purpose",
          "view": "How to inspect"
        },
        "rows": {
          "account": {
            "layer": "Upstream account",
            "content": "Administrator-configured upstream accounts and model mappings",
            "purpose": "Availability depends on accounts, model mappings and upstream support.",
            "view": "Ask an administrator to check account configuration and routing if the list is empty."
          },
          "group": {
            "layer": "Key group",
            "content": "Group allowlists and member accounts",
            "purpose": "Allowlists narrow existing catalogs; composite groups combine eligible member account models.",
            "view": "Check the key group; ask the administrator to verify access. Refresh after changing keys or groups."
          },
          "protocol": {
            "layer": "Client protocol",
            "content": "OpenAI-compatible / Codex catalog",
            "purpose": "Select a protocol-specific model ID; chat uses chat/completions and Codex uses responses.",
            "view": "Query the endpoints below with the same request key; refresh the client / Agent model selector."
          }
        }
      },
      "endpoints": {
        "title": "Catalog endpoints",
        "columns": {
          "endpoint": "Endpoint",
          "purpose": "Purpose",
          "notes": "Usage and notes"
        },
        "rows": {
          "openai": {
            "endpoint": "GET /v1/models",
            "purpose": "Standard OpenAI-compatible tools",
            "notes": "/models is the root-path alias; adding the client_version query parameter selects the Codex catalog."
          },
          "codex": {
            "endpoint": "GET /backend-api/codex/models",
            "purpose": "Codex-compatible clients",
            "notes": "Authenticate with the same key and select a Codex model ID from the response."
          }
        }
      }
    },
    "notes": {
      "availability": "The catalogue lists models visible to this group; it does not guarantee current quota, account availability or support for every endpoint. Refresh after changing keys or groups."
    }
  },
  "clients": {
    "title": "Configure clients and streaming",
    "steps": {},
    "tables": {
      "matrix": {
        "title": "Client configuration matrix",
        "columns": {
          "client": "Client",
          "base": "Base URL",
          "key": "API Key",
          "model": "Model",
          "notes": "Notes"
        },
        "rows": {
          "curl": {
            "client": "curl",
            "base": "$SUB2API_BASE_URL",
            "key": "Authorization: Bearer $SUB2API_API_KEY",
            "model": "model: MODEL_ID_FROM_LIST",
            "notes": "Chat uses chat/completions; see streaming below and the first request in Quick start."
          },
          "python": {
            "client": "Python · OpenAI SDK",
            "base": "base_url = SUB2API_BASE_URL",
            "key": "api_key = SUB2API_API_KEY",
            "model": "model = MODEL_ID_FROM_LIST",
            "notes": "Install openai first; the SDK appends the endpoint path."
          },
          "cursor": {
            "client": "Cursor",
            "base": "Custom OpenAI API URL",
            "key": "Complete key created on this site",
            "model": "MODEL_ID_FROM_LIST",
            "notes": "Set the custom URL, key, and model; only features using that custom API pass through this gateway."
          },
          "codex": {
            "client": "Codex",
            "base": "model_providers.sub2api.base_url",
            "key": "env_key = \"SUB2API_API_KEY\"",
            "model": "model = \"MODEL_ID_FROM_CODEX_LIST\"",
            "notes": "Configure the provider in ~/.codex/config.toml and store the key in an environment variable; the example uses HTTP Responses."
          },
          "cline": {
            "client": "Cline (VS Code)",
            "base": "API Provider: OpenAI Compatible; Base URL: this site's /v1",
            "key": "The full key created on this site",
            "model": "MODEL_ID_FROM_LIST",
            "notes": "Configure it in the Cline settings panel (gear icon). The base URL ends at /v1, never at /chat/completions. Models that need tools must support the OpenAI tool protocol."
          },
          "roo": {
            "client": "Roo Code (VS Code)",
            "base": "API Provider: OpenAI Compatible; Base URL: this site's /v1",
            "key": "The full key created on this site",
            "model": "MODEL_ID_FROM_LIST",
            "notes": "Set API Provider, Base URL, API Key and Model in the settings panel. Roo Code uses native tool calling only, so a model without tool support cannot be used."
          },
          "continue": {
            "client": "Continue (VS Code / JetBrains)",
            "base": "apiBase in config.yaml pointing at this site's /v1",
            "key": "apiKey: sk-your-api-key",
            "model": "model: MODEL_ID_FROM_LIST",
            "notes": "Use provider openai with apiBase pointing here; configure a separate context model only if you need one."
          },
          "jetbrains": {
            "client": "JetBrains AI Assistant",
            "base": "Provider: OpenAI-compatible endpoint; URL: this site's /v1",
            "key": "The full key created on this site",
            "model": "The recognised model ID",
            "notes": "Configure under Settings | Tools | AI Assistant | Providers & API keys. Chat/coding features and AI Completion are two independent settings."
          },
          "windsurf": {
            "client": "Windsurf",
            "base": "No custom OpenAI-compatible endpoint in the official docs",
            "key": "-",
            "model": "-",
            "notes": "Windsurf's official documentation does not describe a custom third-party OpenAI-compatible endpoint or bring-your-own-key setup. Use the Cline, Roo Code or Continue extension inside Windsurf to reach this gateway."
          }
        }
      },
      "fields": {
        "title": "Common field reference",
        "columns": {
          "field": "Setting",
          "meaning": "Meaning and value",
          "example": "Example"
        },
        "rows": {
          "base": {
            "field": "base_url",
            "meaning": "Usually the site API root plus /v1; do not duplicate /v1. Prefer the client configuration generated by Use on API Keys, especially for groups requiring a dedicated endpoint.",
            "example": "This site's api_base_url + /v1 (e.g. https://api.your-domain.com/v1)"
          },
          "key": {
            "field": "api_key",
            "meaning": "Use the complete key; HTTP uses Bearer followed by a space and the key. Your control-panel password is not an API key.",
            "example": "Authorization: Bearer sk-your-api-key"
          },
          "model": {
            "field": "model",
            "meaning": "Copy the id returned by the catalog for the same key; display names may differ from request IDs.",
            "example": "MODEL_ID_FROM_LIST"
          },
          "stream": {
            "field": "stream",
            "meaning": "true: HTTP streaming (SSE); omitted or false: a complete response. Add -N to curl and iterate over SDK chunks.",
            "example": "\"stream\": true"
          }
        }
      }
    },
    "notes": {
      "streaming": "Provider streams have no universal resume: once a stream has started (the status is committed as 200), an interruption is reported in place with a standard terminal frame (/v1/responses sends response.failed; Messages and Chat Completions send an error frame). The gateway never silently switches upstream to continue, and never replays partial output as a complete answer. Check usage after an interruption, then retry with a new request; a retry carrying Idempotency-Key never starts a second generation (it returns 409 with X-Idempotency-State). GET /v1/responses supports WebSocket ingress, depending on the group, account, and administrator WS mode. Chat SSE and WebSocket differ; use HTTP/SSE unless WS support is confirmed.",
      "mediaEndpoints": "Every client configuration above speaks a text/vision protocol (Messages, Responses, Chat Completions). Send image, video and speech generation to the media endpoints instead: /v1/images/generations, /v1/images/edits, /v1/videos/generations, /v1/tts and /v1/stt. Availability depends on the group platform and its switches.",
      "clientSources": "Field names for the added clients come from each vendor's first-party documentation (Cline docs: OpenAI Compatible; Roo Code repository docs: Using OpenAI Compatible Providers; Continue docs: How to Configure OpenAI Models; JetBrains help: Use third-party and local models). Windsurf is listed as unsupported because its official documentation does not describe a custom OpenAI-compatible endpoint.",
      "antigravity": "Antigravity protocol and tool support depend on the model and group. Some upstream built-in tools may be omitted when mixed with client tools; do not assume built-in search or code execution is available."
    }
  },
  "usage": {
    "title": "Usage and quotas",
    "steps": {},
    "tables": {
      "fields": {
        "title": "Balance and quota fields",
        "columns": {
          "field": "Field",
          "meaning": "Meaning",
          "notes": "Example and notes"
        },
        "rows": {
          "available": {
            "field": "Available balance",
            "meaning": "Currently spendable balance shown in the header.",
            "notes": "$8 available: only that $8 is currently spendable; top up or redeem if insufficient."
          },
          "frozen": {
            "field": "Frozen balance",
            "meaning": "Temporarily reserved funds that cannot be spent again.",
            "notes": "Expand header balance details to inspect it, e.g. $2 frozen."
          },
          "total": {
            "field": "Total balance",
            "meaning": "Available plus frozen balance.",
            "notes": "$8 available + $2 frozen = $10 total; total is not the spendable amount."
          },
          "windows": {
            "field": "Key usage windows",
            "meaning": "User keys support 5-hour, 1-day and 7-day spending limits (rate_limit_5h / rate_limit_1d / rate_limit_7d).",
            "notes": "Follow usage and reset times on the key page; these limits are separate from subscription daily, weekly and monthly quotas and upstream account limits."
          },
          "limits": {
            "field": "Quotas",
            "meaning": "Key spending caps, subscription daily/weekly/monthly limits, and upstream windows are separate request constraints, not a combined balance.",
            "notes": "For exhausted subscription limits, renew, choose an appropriate group, or wait for the displayed reset; increasing a key cap does not top up funds."
          },
          "charges": {
            "field": "Requests / tokens / actual charges",
            "meaning": "Inspect these in My Usage; model prices and group multipliers affect billing.",
            "notes": "Token count alone is insufficient to calculate charges."
          }
        }
      },
      "states": {
        "title": "Window status reference",
        "columns": {
          "state": "Status",
          "meaning": "Meaning",
          "action": "Action"
        },
        "rows": {
          "exhausted": {
            "state": "Exhausted",
            "meaning": "A current valid window has reached 100%.",
            "action": "Check the applicable limit and displayed reset time."
          },
          "stale": {
            "state": "Possibly expired",
            "meaning": "Cached data is old or its reset time has passed.",
            "action": "Refresh to confirm the current status."
          },
          "unknown": {
            "state": "Unknown / not fetched / failed / unsupported",
            "meaning": "Current quota is undetermined, not zero or unlimited.",
            "action": "Refresh available data; ask the administrator if it remains unclear."
          }
        }
      }
    },
    "notes": {}
  },
  "billing": {
    "title": "Subscriptions, top-ups, and redemption",
    "steps": {},
    "tables": {
      "options": {
        "title": "Purchase and redemption options",
        "columns": {
          "option": "Option",
          "steps": "Steps",
          "notes": "Notes"
        },
        "rows": {
          "topup": {
            "option": "Top-up",
            "steps": "1. Open Top-up / Subscription and choose an amount.\n2. Verify the price, create an order, and follow payment instructions.\n3. Check order status, then balance.",
            "notes": "A success return page does not prove credit; payment notification and order processing confirm it."
          },
          "subscription": {
            "option": "Subscription plan",
            "steps": "1. Choose a plan; verify its group, price, validity, and limits.\n2. Create and pay the order; confirm its status.\n3. Check active plans and dates in My Subscriptions.",
            "notes": "The key must use the corresponding group to use the subscription."
          },
          "redemption": {
            "option": "Redemption code",
            "steps": "1. Open Redeem, paste the complete code, and submit.\n2. Check balance, concurrency, or subscription changes according to code type.\n3. For balance codes check the header; for subscription codes check My Subscriptions and the key group.",
            "notes": "On failure, verify spelling, prior use, and validity with the issuer; never post the code publicly."
          }
        }
      }
    },
    "notes": {
      "availability": "Sites may enable top-ups only, subscriptions only, or no online purchase; navigation labels and available options may differ.",
      "pending": "If an order is pending or processing, refresh and check later; retain its number for the administrator and avoid duplicate orders."
    }
  },
  "faq": {
    "title": "Troubleshooting",
    "steps": {
      "capture": {
        "title": "Record details and redact credentials",
        "body": "Record the request time, endpoint, model ID, HTTP status, and error body; hide Authorization and keys before reporting to the administrator. One status can have several causes; use the actual error code and message."
      },
      "diagnose": {
        "title": "Check configuration in order",
        "body": "API URL → complete key and status → key group and model catalog → balance / subscription / key limits → upstream status. Check existing usage or results before retrying to avoid duplicate charges."
      }
    },
    "tables": {
      "errors": {
        "title": "HTTP status and error codes",
        "columns": {
          "status": "HTTP status",
          "code": "Error code",
          "meaning": "Meaning",
          "action": "Action"
        },
        "rows": {
          "auth": {
            "status": "401",
            "code": "API_KEY_REQUIRED / INVALID_API_KEY / API_KEY_DISABLED",
            "meaning": "Missing, invalid, or disabled key.",
            "action": "Verify Bearer formatting, the complete key, its status, and the configuration the client actually reads."
          },
          "user": {
            "status": "401",
            "code": "USER_INACTIVE / USER_NOT_FOUND",
            "meaning": "The associated user is inactive or missing.",
            "action": "Ask the administrator to verify account status."
          },
          "ip": {
            "status": "403",
            "code": "ACCESS_DENIED",
            "meaning": "The source IP violates access restrictions.",
            "action": "Check the actual egress IP and allowlist / blocklist."
          },
          "expiry": {
            "status": "403",
            "code": "API_KEY_EXPIRED",
            "meaning": "The key has expired.",
            "action": "Check validity, then edit or replace the key."
          },
          "balance": {
            "status": "403",
            "code": "INSUFFICIENT_BALANCE",
            "meaning": "Insufficient account balance.",
            "action": "Check available balance and top up or redeem."
          },
          "subscription": {
            "status": "403",
            "code": "SUBSCRIPTION_NOT_FOUND / SUBSCRIPTION_INVALID",
            "meaning": "No active subscription for this group, or an invalid subscription.",
            "action": "Check group entitlement, plan status, and validity."
          },
          "model": {
            "status": "400 / 404",
            "code": "Read the response body",
            "meaning": "Unsupported model, wrong name, or incompatible endpoint.",
            "action": "Refresh the catalog and verify model and Chat / Responses protocol; do not guess model names."
          },
          "quota": {
            "status": "429",
            "code": "API_KEY_QUOTA_EXHAUSTED / insufficient_quota",
            "meaning": "The key spending cap is exhausted; some Responses endpoints return insufficient_quota.",
            "action": "Check the key quota; waiting a few seconds or rapid retries will not restore the spending cap."
          },
          "window": {
            "status": "429",
            "code": "USAGE_LIMIT_EXCEEDED",
            "meaning": "A subscription daily / weekly / monthly limit is exhausted.",
            "action": "Check the relevant limit and reset time; renew or select an appropriate group as needed."
          },
          "rate": {
            "status": "429",
            "code": "Read the response body",
            "meaning": "Concurrency or rate limiting.",
            "action": "Reduce concurrency and back off as instructed; distinguish rate limiting from spending cap exhaustion."
          },
          "upstream": {
            "status": "502 / 503",
            "code": "Read the response body",
            "meaning": "Upstream error, no available account, or temporary service unavailability.",
            "action": "Retry a limited number of times later; contact the administrator with request details if it persists."
          }
        }
      }
    },
    "notes": {}
  }
}
