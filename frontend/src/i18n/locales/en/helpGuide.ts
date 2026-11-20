export default {
  "title": "Configuration and usage manual",
  "subtitle": "Start with a key and group, choose a protocol, copy a request, configure your client, and verify results, usage and billing.",
  "eyebrow": "USER MANUAL · CONFIGURATION & GETTING STARTED",
  "contents": "On this page",
  "copy": "Copy",
  "copied": "Copied to clipboard",
  "copyFailed": "Copy failed. Select the code and copy it manually.",
  "next": "Related pages",
  "placeholder": "Examples use this site’s published API address. Replace sk-your-api-key with your complete key and MODEL_ID_FROM_LIST with an ID from that key’s catalog. Copy the Bash or PowerShell variant for your shell; Python and Node examples read the key from the environment.",
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
            "content": "OpenAI-compatible / Messages / Google / Codex catalogs",
            "purpose": "Choose the catalog and model for the client’s protocol: Chat, Messages, Responses or Google; Codex uses Responses.",
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
            "notes": "Use data[].id for OpenAI-compatible clients. For Codex, use its dedicated catalog at GET /backend-api/codex/models."
          },
          "codex": {
            "endpoint": "GET /backend-api/codex/models",
            "purpose": "Codex-compatible clients",
            "notes": "Authenticate with the same key and select a Codex model ID from the response."
          },
          "catalog": {
            "endpoint": "GET /v1/models/catalog",
            "purpose": "Rich capability catalog",
            "notes": "Pass next_cursor as cursor for the next page; preserve the distinction between guaranteed, available and unknown."
          },
          "google": {
            "endpoint": "GET /v1beta/models",
            "purpose": "Google model catalog",
            "notes": "Continue using nextPageToken; the request model ID omits the leading models/."
          },
          "single": {
            "endpoint": "GET /v1/models/{'{'}encoded_id{'}'}",
            "purpose": "Inspect one model",
            "notes": "Encode the ID as one path segment; do not strip its provider prefix."
          }
        }
      }
    },
    "notes": {
      "availability": "The catalogue lists models visible to this group; it does not guarantee current quota, account availability or support for every endpoint. Refresh after changing keys or groups.",
      "metadata": "The ordinary catalog may include top-level guarantees such as context_window, max_output_tokens, supports_reasoning and supports_function_calling; unknown values are omitted. Rich catalog endpoints and capabilities do not guarantee live capacity; available does not mean guaranteed on every route. Messages catalogs use anthropic-version and after_id/before_id pagination (not both); Google uses pageToken."
    }
  },
  "clients": {
    "title": "Clients & workflows",
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
            "base": "$OMNIHIVE_BASE_URL",
            "key": "Authorization: Bearer $OMNIHIVE_API_KEY",
            "model": "model: MODEL_ID_FROM_LIST",
            "notes": "Chat uses chat/completions; see streaming below and the first request in Quick start."
          },
          "python": {
            "client": "Python · OpenAI SDK",
            "base": "base_url = OMNIHIVE_BASE_URL",
            "key": "api_key = OMNIHIVE_API_KEY",
            "model": "model = MODEL_ID_FROM_LIST",
            "notes": "Install openai first; the SDK appends the endpoint path."
          },
          "cursor": {
            "client": "Cursor",
            "base": "Custom OpenAI API URL",
            "key": "Complete key created on this site",
            "model": "MODEL_ID_FROM_LIST",
            "notes": "The URL override depends on the client version; official BYOK documentation does not establish arbitrary custom URLs. Use a build exposing this setting and an address reachable from Cursor’s servers; features such as Tab may not use this key."
          },
          "codex": {
            "client": "Codex",
            "base": "model_providers.omnihive.base_url",
            "key": "env_key = \"OMNIHIVE_API_KEY\"",
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
          },
          "claude": {
            "client": "Claude Code CLI",
            "base": "ANTHROPIC_BASE_URL = gateway root",
            "key": "ANTHROPIC_API_KEY",
            "model": "--model MODEL_ID_FROM_LIST",
            "notes": "The CLI appends /v1/messages; desktop configuration is separate. Requires a group and model that accept Messages."
          },
          "opencode": {
            "client": "OpenCode (classic config)",
            "base": "provider.<id>.options.baseURL = root + /v1",
            "key": "options.apiKey: {'{'}env:OMNIHIVE_API_KEY{'}'}",
            "model": "<provider>/MODEL_ID_FROM_LIST",
            "notes": "The example uses the {'@'}ai-sdk/openai-compatible package for Chat. OpenCode 2 has a different schema; do not mix them."
          },
          "dsh": {
            "client": "DeepSeek Harness (DSH)",
            "base": "baseURL = root + /v1",
            "key": "apiKeyEnv: OMNIHIVE_API_KEY",
            "model": "MODEL_ID_FROM_LIST",
            "notes": "Select openai-completions in provider settings; openai-responses and anthropic-messages are separate protocol choices. Save through your installed version’s setup flow."
          },
          "pi": {
            "client": "Pi",
            "base": "baseUrl = root + /v1",
            "key": "apiKey: $OMNIHIVE_API_KEY",
            "model": "models[].id: MODEL_ID_FROM_LIST",
            "notes": "Configure openai-completions in ~/.pi/agent/models.json; open /model to reload and select it."
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
            "example": "Published gateway root + /v1"
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
            "meaning": "For supported Chat, Messages and Responses calls, true requests SSE; omitted or false requests a complete response. Gemini uses streamGenerateContent. Use curl -N.",
            "example": "\"stream\": true"
          }
        }
      }
    },
    "notes": {
      "streaming": "See the streaming, tools and reasoning sections for request behavior. The Codex example uses HTTP Responses; WebSocket requires compatible group, account and site settings, so leave it disabled until confirmed.",
      "mediaEndpoints": "The client configurations above use text/vision protocols (Messages, Responses, Chat Completions). For media, use /v1/images/generations, /v1/images/edits and /v1/videos/generations; for speech synthesis, transcription and translation, use /v1/audio/speech, /v1/audio/transcriptions and /v1/audio/translations respectively. Availability and request formats depend on the group platform, model and enabled features. /v1/tts and /v1/stt have separate platform contracts and are not interchangeable with the standard audio endpoints.",
      "clientSources": "Field names for the added clients come from each vendor's first-party documentation (Cline docs: OpenAI Compatible; Roo Code repository docs: Using OpenAI Compatible Providers; Continue docs: How to Configure OpenAI Models; JetBrains help: Use third-party and local models). Windsurf is listed as unsupported because its official documentation does not describe a custom OpenAI-compatible endpoint.",
      "antigravity": "Antigravity protocol and tool support depend on the model and group. Some upstream built-in tools may be omitted when mixed with client tools; do not assume built-in search or code execution is available.",
      "workflow": "For a workflow tool, configure the HTTP method, full URL, authentication header and JSON body. Send a simple request, inspect the result and usage, then add loops or concurrency. A client’s protocol setting does not grant every model that capability."
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
            "status": "403 / 429",
            "code": "INSUFFICIENT_BALANCE / insufficient_quota",
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
            "action": "Check quota and balance; exhaustion is not transient congestion and may have no Retry-After. Do not retry indefinitely."
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
          },
          "missing": {
            "status": "404",
            "code": "Read the response body",
            "meaning": "Invisible model, unavailable action or a resource not owned by this key.",
            "action": "Check the catalog, feature access and original task key, not just the URL."
          },
          "conflict": {
            "status": "409",
            "code": "Read the response body",
            "meaning": "Resource not ready or a content conflict.",
            "action": "Inspect task state and error code before generating again."
          },
          "large": {
            "status": "413",
            "code": "Read the response body",
            "meaning": "Request or upload too large.",
            "action": "Reduce the request according to that text, image or audio route’s limits."
          }
        }
      }
    },
    "notes": {
      "shape": "Error envelopes differ by protocol: Chat/Responses commonly expose error; Messages also uses type: error; Gemini uses error.code, error.status and error.message. Preserve the original HTTP status and body instead of matching one string."
    }
  },
  "groups": {
    "start": "Getting started",
    "api": "Calling the API",
    "calls": "During a request",
    "media": "Images, video & audio",
    "apps": "Apps & workflows",
    "account": "Account & troubleshooting"
  },
  "capability": "First confirm that the key’s group permits the protocol and model. Catalog visibility, capability hints, and a successful request are different facts; tools, reasoning, cache and media also depend on the selected model. The tables describe a minimal common subset, not support for every upstream parameter.",
  "addresses": {
    "title": "API addresses & authentication",
    "steps": {},
    "tables": {
      "formats": {
        "title": "Choose the correct address",
        "columns": {
          "field": "Use",
          "value": "Address shape",
          "notes": "Notes"
        },
        "rows": {
          "root": {
            "field": "Gateway root",
            "value": "api_base_url",
            "notes": "Remove a trailing /v1 or /v1beta; keep the deployment prefix. Claude Code uses this root."
          },
          "base": {
            "field": "OpenAI-compatible base",
            "value": "root + /v1",
            "notes": "The client appends the endpoint path; do not duplicate /v1 or include /chat/completions."
          },
          "full": {
            "field": "HTTP request URL",
            "value": "root + /v1/chat/completions",
            "notes": "curl, PowerShell and fetch use the full URL. Gemini uses /v1beta/models/…:generateContent."
          }
        }
      }
    },
    "notes": {
      "auth": "Chat, Responses and media use Authorization: Bearer; Messages uses x-api-key and anthropic-version: 2023-06-01; Gemini uses x-goog-api-key. Do not put keys in query strings. Your panel password is not an API key."
    }
  },
  "cc-switch": {
    "title": "CC Switch (optional)",
    "steps": {
      "import": {
        "title": "Import from API Keys",
        "body": "Use the import action when the newly created key is shown, or the available key action. Choose Claude or Gemini and review the target app and address before importing."
      }
    },
    "tables": {},
    "notes": {
      "optional": "Direct URL, key and model configuration is sufficient; CC Switch is optional. Imports contain a key, so do not share import links. No import link should be generated without a configured public API address.",
      "address": "The group can change the imported app and address shape: root, /v1 or /antigravity. Confirm the final address in CC Switch; one group’s settings do not apply to every group.",
      "conversion": "CC Switch can also run a local protocol-conversion proxy: a Responses-speaking client can be converted locally to an upstream Chat endpoint. wire_api = responses describes the client-facing protocol; it alone does not prove the supplier implements /responses. Direct upstream Responses and local Chat conversion are distinct configurations.",
      "gateway": "When calling OmniHive, keep the site URL and site key in the client configuration. The site may separately convert /v1/messages, /v1/chat/completions, /v1/responses or /v1beta/models/… requests on an eligible group/account route. Local CC Switch conversion and gateway conversion are separate layers; inspect the actual selected protocol instead of enabling both by assumption."
    }
  },
  "chat": {
    "title": "Chat Completions",
    "endpoint": "POST /v1/chat/completions",
    "intro": "Use for clients that send an OpenAI-compatible message history. Choose a chat-capable model in the key’s group.",
    "steps": {},
    "tables": {
      "request": {
        "title": "Request fields",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "model": {
            "field": "model",
            "type": "string · required",
            "meaning": "Exact catalog ID, including any provider prefix."
          },
          "messages": {
            "field": "messages",
            "type": "array · required",
            "meaning": "Ordered role/content messages; resend conversation context."
          },
          "stream": {
            "field": "stream",
            "type": "boolean · optional",
            "meaning": "Set true for SSE; default examples request one JSON response."
          }
        }
      },
      "result": {
        "title": "Read the result",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "choices_message": {
            "field": "choices[].message",
            "type": "object",
            "meaning": "Text is in content; tool_calls may replace text."
          },
          "choices_finish_reason_usage": {
            "field": "choices[].finish_reason / usage",
            "type": "string / object",
            "meaning": "Inspect completion reason and reported usage."
          }
        }
      }
    },
    "notes": {
      "behavior": "Do not send Messages system blocks or Responses input items as Chat messages without conversion."
    }
  },
  "messages": {
    "title": "Messages",
    "endpoint": "POST /v1/messages",
    "intro": "Select a model and group accepting the Messages protocol. Use x-api-key and the anthropic-version header.",
    "steps": {},
    "tables": {
      "request": {
        "title": "Request fields",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "model": {
            "field": "model",
            "type": "string · required",
            "meaning": "Catalog request ID."
          },
          "max_tokens": {
            "field": "max_tokens",
            "type": "integer · required",
            "meaning": "Output budget within the selected model’s limits."
          },
          "messages": {
            "field": "messages",
            "type": "array · required",
            "meaning": "User/assistant messages; content may be text or content blocks."
          },
          "system": {
            "field": "system",
            "type": "string / array · optional",
            "meaning": "Top-level instructions, separate from messages."
          }
        }
      },
      "result": {
        "title": "Read the result",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "content": {
            "field": "content[]",
            "type": "array",
            "meaning": "Read text, thinking or tool_use blocks by type."
          },
          "stop_reason_usage": {
            "field": "stop_reason / usage",
            "type": "string / object",
            "meaning": "Check the stop reason and input/output usage."
          }
        }
      }
    },
    "notes": {
      "behavior": "POST /v1/messages/count_tokens is a separate model-dependent count or estimate; it does not prove generation access."
    }
  },
  "responses": {
    "title": "Responses",
    "endpoint": "POST /v1/responses",
    "intro": "For Responses clients such as Codex, use a model and group that permit Responses. HTTP is the default integration path.",
    "steps": {},
    "tables": {
      "request": {
        "title": "Request fields",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "model": {
            "field": "model",
            "type": "string · required",
            "meaning": "Exact model ID."
          },
          "input": {
            "field": "input",
            "type": "string / array · required",
            "meaning": "Text or ordered input items."
          },
          "instructions": {
            "field": "instructions",
            "type": "string · optional",
            "meaning": "Instructions for this request."
          },
          "max_output_tokens": {
            "field": "max_output_tokens",
            "type": "integer · optional",
            "meaning": "Model-specific output budget."
          }
        }
      },
      "result": {
        "title": "Read the result",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "output": {
            "field": "output[]",
            "type": "array",
            "meaning": "Inspect item types: message, reasoning, function_call and others."
          },
          "status_incomplete_details_error": {
            "field": "status / incomplete_details / error",
            "type": "object fields",
            "meaning": "Only a completed response is fully complete; inspect incomplete or failed outcomes."
          },
          "usage": {
            "field": "usage",
            "type": "object",
            "meaning": "Reported usage is separate from final billing status."
          }
        }
      }
    },
    "notes": {
      "behavior": "Resend full history unless your route explicitly supports state continuation. previous_response_id is owner/account-bound; store:true alone does not promise retained state.",
      "compatibility": "GET /v1/responses is conditional WebSocket ingress, not retrieval of a saved response."
    }
  },
  "gemini": {
    "title": "Gemini / Google protocol",
    "endpoint": "POST /v1beta/models/{'{'}model{'}'}:generateContent",
    "intro": "Put the model ID in the URL and authenticate with x-goog-api-key. The group and model must allow the requested Google action.",
    "steps": {},
    "tables": {
      "request": {
        "title": "Request fields",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "model_URL": {
            "field": "model (URL)",
            "type": "encoded path segment · required",
            "meaning": "Use the request ID; remove a catalog name’s leading models/ and encode the remaining ID once."
          },
          "contents": {
            "field": "contents",
            "type": "array · required",
            "meaning": "Content with parts, such as {'{'}text:\"Hello\"{'}'}; role may be user."
          },
          "generationConfig": {
            "field": "generationConfig",
            "type": "object · optional",
            "meaning": "Only controls supported by the selected model."
          }
        }
      },
      "result": {
        "title": "Read the result",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "candidates_content_parts": {
            "field": "candidates[].content.parts",
            "type": "array",
            "meaning": "Read each part; safety blocking may produce no text."
          },
          "finishReason_usageMetadata": {
            "field": "finishReason / usageMetadata",
            "type": "fields",
            "meaning": "Inspect finish reason and reported usage."
          }
        }
      }
    },
    "notes": {
      "behavior": "Streaming uses :streamGenerateContent?alt=sse, not a Chat stream flag. :countTokens is model-dependent; :embedContent and :batchEmbedContents require an eligible embeddings route."
    }
  },
  "retrieval": {
    "title": "Embeddings & rerank",
    "endpoint": "POST /v1/embeddings · POST /v1/rerank",
    "intro": "Select a dedicated embedding or reranking model from an eligible group. A chat model is not an interchangeable substitute.",
    "steps": {},
    "tables": {
      "request": {
        "title": "Request fields",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "model": {
            "field": "model",
            "type": "string · required",
            "meaning": "Model for the specific retrieval endpoint."
          },
          "input_embeddings": {
            "field": "input (embeddings)",
            "type": "string · required in example",
            "meaning": "Text to encode; dimensions and encoding support vary by model."
          },
          "query_documents_rerank": {
            "field": "query / documents (rerank)",
            "type": "string / string[] · required",
            "meaning": "Search query and candidate documents."
          },
          "top_n_rerank": {
            "field": "top_n (rerank)",
            "type": "positive integer · optional",
            "meaning": "Number of ranked results requested."
          }
        }
      },
      "result": {
        "title": "Read the result",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "data_embedding_index": {
            "field": "data[].embedding / index",
            "type": "vector / integer",
            "meaning": "Embedding and input position; verify consistent vector dimensions."
          },
          "index_relevance_score": {
            "field": "index / relevance_score",
            "type": "ranked entries",
            "meaning": "Rerank result entries; the outer envelope varies by route."
          },
          "usage": {
            "field": "usage",
            "type": "object · when reported",
            "meaning": "Missing counts are unknown; rerank may be priced per query rather than tokens."
          }
        }
      }
    },
    "notes": {
      "behavior": "These endpoints do not imply streaming or tool support. OpenAI’s adapter does not supply rerank; ask for a group with a rerank route."
    }
  },
  "streaming": {
    "title": "Streaming & interrupted calls",
    "endpoint": "POST · protocol-specific SSE",
    "intro": "The group and model must support streaming on the chosen protocol. Use curl -N or an SDK that reads SSE incrementally.",
    "steps": {},
    "tables": {
      "request": {
        "title": "Request fields",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "stream": {
            "field": "stream",
            "type": "boolean · Chat / Messages / Responses",
            "meaning": "Set true in the JSON request."
          },
          "alt_sse": {
            "field": "alt=sse",
            "type": "query · Gemini",
            "meaning": "Use with :streamGenerateContent."
          }
        }
      },
      "result": {
        "title": "Read the result",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "Chat": {
            "field": "Chat",
            "type": "choices[].delta",
            "meaning": "Assemble deltas; normally ends with [DONE]. An error or premature EOF is not completion."
          },
          "Messages": {
            "field": "Messages",
            "type": "event + data",
            "meaning": "Read content_block deltas through message_stop; event:error means failure."
          },
          "Responses": {
            "field": "Responses",
            "type": "typed events",
            "meaning": "response.completed succeeds; response.incomplete and response.failed require inspection."
          },
          "Gemini": {
            "field": "Gemini",
            "type": "Google JSON events",
            "meaning": "Read candidates and final finishReason/usage; do not wait for Chat [DONE]."
          }
        }
      }
    },
    "notes": {
      "behavior": "Split SSE events on blank lines and ignore keepalive comments. HTTP 200 can precede a later failure. Preserve partial tool arguments until the tool call is complete.",
      "compatibility": "Interrupted output is not resumed as a finished answer. Check usage or existing tasks before sending a new request; retries are not universally idempotent. The PowerShell quickstart demonstrates non-streaming JSON only."
    }
  },
  "tools": {
    "title": "Function tools & results",
    "endpoint": "POST · Chat / Messages / Responses / Gemini",
    "intro": "Choose a tool-capable model in the allowed group. Your application executes client functions and sends the result back; the model may choose not to call a tool.",
    "steps": {},
    "tables": {
      "request": {
        "title": "Request fields",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "Chat_tools": {
            "field": "Chat tools[]",
            "type": "type:function + function",
            "meaning": "function contains name, description and parameters (JSON Schema)."
          },
          "Messages_tools": {
            "field": "Messages tools[]",
            "type": "name + input_schema",
            "meaning": "Use the Messages tool definition shape."
          },
          "Responses_tools": {
            "field": "Responses tools[]",
            "type": "type:function + name + parameters",
            "meaning": "Function fields live directly on the tool object."
          },
          "Gemini_tools": {
            "field": "Gemini tools",
            "type": "functionDeclarations",
            "meaning": "Use Google function declarations."
          }
        }
      },
      "result": {
        "title": "Read the result",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "Chat_result": {
            "field": "Chat result",
            "type": "role:tool / tool_call_id / content",
            "meaning": "Match the actual tool call ID and retain the assistant tool_calls message."
          },
          "Messages_result": {
            "field": "Messages result",
            "type": "tool_result / tool_use_id",
            "meaning": "Place the result in a user content block."
          },
          "Responses_result": {
            "field": "Responses result",
            "type": "function_call_output / call_id / output",
            "meaning": "Match call_id and retain preceding input/output items."
          },
          "Gemini_result": {
            "field": "Gemini result",
            "type": "functionResponse",
            "meaning": "Return the function response in the conversation context."
          }
        }
      }
    },
    "notes": {
      "behavior": "Keep real call IDs, arguments and opaque reasoning/signatures. Do not invent IDs or discard previous assistant items. Tool support does not grant built-in search, code execution or every server tool."
    }
  },
  "reasoning": {
    "title": "Reasoning controls",
    "endpoint": "POST · model-specific controls",
    "intro": "Use only levels declared for the model and supported by its group’s route. If the catalog is silent, omit the control instead of assuming high or max.",
    "steps": {},
    "tables": {
      "request": {
        "title": "Request fields",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "reasoning_effort": {
            "field": "reasoning_effort",
            "type": "Chat · optional",
            "meaning": "Effort value from the selected model’s supported levels."
          },
          "reasoning_effort_reasoning_summary": {
            "field": "reasoning.effort / reasoning.summary",
            "type": "Responses · optional",
            "meaning": "Effort and summary behavior are model-specific."
          },
          "thinking_output_config_effort": {
            "field": "thinking / output_config.effort",
            "type": "Messages · optional",
            "meaning": "thinking uses type and budget_tokens where supported."
          },
          "generationConfig_thinkingConfig": {
            "field": "generationConfig.thinkingConfig",
            "type": "Gemini · optional",
            "meaning": "Google control shape; not interchangeable with other protocols."
          }
        }
      },
      "result": {
        "title": "Read the result",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "reasoning_thinking": {
            "field": "reasoning / thinking",
            "type": "protocol-specific output",
            "meaning": "May contain summaries or opaque continuation data; retain it when continuing a tool exchange."
          }
        }
      }
    },
    "notes": {
      "behavior": "Effort is not a guaranteed token budget, latency or answer quality. Temperature and parallel tools may conflict with a model’s reasoning mode."
    }
  },
  "cache": {
    "title": "Prompt caching & usage",
    "endpoint": "POST · model-specific caching",
    "intro": "Caching depends on the model, group route and upstream policy. Keep stable context prefixes; a catalog cache hint does not guarantee a cache hit or discount.",
    "steps": {},
    "tables": {
      "request": {
        "title": "Request fields",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "cache_control": {
            "field": "cache_control",
            "type": "Messages content block · optional",
            "meaning": "Supported Messages routes may accept an ephemeral cache hint; routing can adjust it."
          }
        }
      },
      "result": {
        "title": "Read the result",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "cache_creation_input_tokens": {
            "field": "cache_creation_input_tokens",
            "type": "number · when reported",
            "meaning": "Tokens used to create cache; separate from cache reads."
          },
          "cache_read_input_tokens": {
            "field": "cache_read_input_tokens",
            "type": "number · when reported",
            "meaning": "Tokens read from cache; inspect actual usage and the final bill."
          }
        }
      }
    },
    "notes": {
      "behavior": "Missing cache fields mean unknown, not zero. There is no universal TTL, cache key or pricing multiplier across protocols."
    }
  },
  "images": {
    "title": "Image generation & editing",
    "endpoint": "POST /v1/images/generations · /v1/images/edits · /v1/images/variations",
    "intro": "Use a model and group authorized for the specific image action. Generation, editing and variations are separate capabilities.",
    "steps": {},
    "tables": {
      "request": {
        "title": "Request fields",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "model_prompt": {
            "field": "model / prompt",
            "type": "JSON strings · generation",
            "meaning": "Select an image model and describe the image."
          },
          "image_model_prompt": {
            "field": "image / model / prompt",
            "type": "multipart · OpenAI or Qianwen edit example",
            "meaning": "Upload reference.png with curl -F; let curl set the multipart boundary."
          },
          "mask": {
            "field": "mask",
            "type": "multipart · OpenAI subset only",
            "meaning": "Optional where the selected model accepts it; Qianwen edits reject masks."
          }
        }
      },
      "result": {
        "title": "Read the result",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "data_url_data_b64_json": {
            "field": "data[].url / data[].b64_json",
            "type": "string",
            "meaning": "URL or base64 image data, depending on route and format."
          }
        }
      }
    },
    "notes": {
      "behavior": "OpenRouter supports synchronous raster generation only: b64_json, png/jpeg/webp; no URL output, SSE, edits or variations. Qianwen edit accepts 1–3 uploads up to 10 MiB each and only b64_json output.",
      "compatibility": "Other routes may require different JSON or multipart fields. Do not copy one platform’s advanced options to another."
    }
  },
  "async-images": {
    "title": "Asynchronous image tasks",
    "endpoint": "POST /v1/images/generations/async · /v1/images/edits/async",
    "intro": "Available only when enabled and the group resolves to an eligible OpenAI/Grok image model. Disabled submission creates no task; accepted tasks may remain readable.",
    "steps": {},
    "tables": {
      "request": {
        "title": "Request fields",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "request_body": {
            "field": "request body",
            "type": "same as synchronous action",
            "meaning": "Use generation JSON or the applicable edit upload; stream is rejected."
          }
        }
      },
      "result": {
        "title": "Read the result",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "HTTP_202_task_id_poll_url": {
            "field": "HTTP 202 / task_id / poll_url",
            "type": "accepted task",
            "meaning": "Save the task ID. Follow Retry-After (currently 3 seconds) rather than submitting again."
          },
          "status": {
            "field": "status",
            "type": "processing / completed / failed",
            "meaning": "GET /v1/images/tasks/{'{'}task_id{'}'} with the original key. Stop on completed or failed."
          },
          "result_http_status_error": {
            "field": "result / http_status / error",
            "type": "terminal details",
            "meaning": "On completion read image URLs in result; on failure inspect status/error."
          }
        }
      }
    },
    "notes": {
      "behavior": "Tasks belong to both the user and the key; another key from the same user cannot poll them. No public cancel/delete or separate binary-download endpoint exists for these single-image tasks.",
      "compatibility": "Batch images are a separate Gemini-group feature with their own catalog, permissions and lifecycle; use the Batch Images guide."
    }
  },
  "media-jobs": {
    "title": "Video jobs",
    "endpoint": "POST /v1/videos/generations · GET /v1/videos/{'{'}id{'}'} · GET /v1/videos/{'{'}id{'}'}/content",
    "intro": "Video is a job workflow, not a chat response. The example is for an eligible OpenRouter video model and group; choose a duration allowed by that model.",
    "steps": {},
    "tables": {
      "request": {
        "title": "Request fields",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "model_prompt": {
            "field": "model / prompt",
            "type": "JSON strings · OpenRouter example",
            "meaning": "Video model and description; additional model requirements may apply."
          },
          "duration": {
            "field": "duration",
            "type": "positive integer · OpenRouter",
            "meaning": "Example uses 4 seconds, not a universal allowed duration or delivered duration."
          }
        }
      },
      "result": {
        "title": "Read the result",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "id_status": {
            "field": "id / status",
            "type": "OpenAI / OpenRouter job",
            "meaning": "Keep the returned public ID; queued → in_progress → completed, or a terminal failure."
          },
          "content": {
            "field": "/content",
            "type": "download after completion",
            "meaning": "Use the original key; verify HTTP status and Content-Type before opening the file."
          }
        }
      }
    },
    "notes": {
      "behavior": "OpenAI edits/extensions use video:{'{'}id:...{'}'} for an owned completed OpenAI video. OpenRouter has no edit/extend/cancel support. Cancellation on supported Qianwen/Qianfan routes is a separate action; acknowledgement is not final cancellation or a refund.",
      "compatibility": "Do not substitute an upstream task ID or remote URL for the returned site ID. Other platforms may expose different status envelopes."
    }
  },
  "audio": {
    "title": "Speech, transcription & translation",
    "endpoint": "POST /v1/audio/speech · /v1/audio/transcriptions · /v1/audio/translations",
    "intro": "Choose an audio-capable model and an authorized group for each action. The examples separate OpenRouter MP3, Qianwen WAV, and OpenAI audio translation.",
    "steps": {},
    "tables": {
      "request": {
        "title": "Request fields",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "model_input_voice": {
            "field": "model / input / voice",
            "type": "speech JSON",
            "meaning": "Text and a voice supported by the model. Qianwen requires voice and 1–600 Unicode characters."
          },
          "response_format": {
            "field": "response_format",
            "type": "speech · route-specific",
            "meaning": "OpenRouter: mp3/pcm, no stream. Qianwen: wav only."
          },
          "file_model": {
            "field": "file / model",
            "type": "translation multipart · OpenAI API-key route",
            "meaning": "Use a translation-capable model; this is a separate action from transcription."
          }
        }
      },
      "result": {
        "title": "Read the result",
        "columns": {
          "field": "Field",
          "type": "Type / requirement",
          "meaning": "Meaning"
        },
        "rows": {
          "speech_response": {
            "field": "speech response",
            "type": "binary or model-specific envelope",
            "meaning": "Check Content-Type. Mistral may return audio_data base64 JSON; do not assume every response is MP3."
          },
          "transcription_response": {
            "field": "transcription response",
            "type": "JSON in the examples",
            "meaning": "Read text and available usage; output formats differ by route."
          }
        }
      }
    },
    "notes": {
      "behavior": "Qianwen transcription accepts only file/model/response_format and json output. OpenRouter supports json/verbose_json. /v1/tts and /v1/stt have dedicated contracts, not universal OpenAI bodies."
    }
  },
  "references": "Official client references (configuration varies by version)",
  "resources": {
    "title": "Files, batches & specialized APIs",
    "intro": "Confirm group, model and action eligibility separately for these resources. A registered path does not grant the current key access, and a chat body is not their request schema.",
    "steps": {},
    "tables": {
      "paths": {
        "title": "Resource directory",
        "columns": {
          "endpoint": "Method / path",
          "purpose": "Purpose & boundary"
        },
        "rows": {
          "files": {
            "endpoint": "POST /v1/files · GET /v1/files · GET/DELETE /v1/files/{'{'}id{'}'} · GET /v1/files/{'{'}id{'}'}/content",
            "purpose": "Upload, list, inspect or download owned files; deletion is explicit."
          },
          "batches": {
            "endpoint": "POST/GET /v1/batches · GET /v1/batches/{'{'}id{'}'} · POST /v1/batches/{'{'}id{'}'}/cancel",
            "purpose": "Text batch jobs; submit once, keep the ID, then query. Distinct from image batches."
          },
          "messages": {
            "endpoint": "POST/GET /v1/messages/batches · GET/DELETE /v1/messages/batches/{'{'}id{'}'} · POST …/{'{'}id{'}'}/cancel · GET …/{'{'}id{'}'}/results",
            "purpose": "Messages batch lifecycle and results require a supporting route."
          },
          "images": {
            "endpoint": "POST/GET /v1/images/batches · GET …/models · GET …/{'{'}id{'}'} · GET …/{'{'}id{'}'}/items · GET …/{'{'}id{'}'}/download",
            "purpose": "Gemini-group image batches have a dedicated model list and owner key/group. Use the linked guide for item downloads, cancellation and output/record deletion."
          },
          "moderations": {
            "endpoint": "POST /v1/moderations",
            "purpose": "Content moderation is a separate action, not a chat result."
          },
          "realtime": {
            "endpoint": "GET /v1/realtime · POST /v1/live · GET /v1/live/{'{'}call_id{'}'}",
            "purpose": "Real-time/Live sessions require protocol-specific clients. Qianwen realtime retains DashScope events; not every route speaks OpenAI Realtime."
          },
          "search": {
            "endpoint": "POST /v1/alpha/search · /v1/web_search · /v1/x_search · /v1/tools/search · /v1/tools/search_pro",
            "purpose": "Dedicated search actions; function-tool support does not grant these routes."
          },
          "voices": {
            "endpoint": "POST/GET /v1/custom-voices · GET/PATCH/DELETE /v1/custom-voices/{'{'}voice_id{'}'} · GET …/{'{'}voice_id{'}'}/audio",
            "purpose": "Voice management has platform, method and ownership restrictions; do not infer access from speech support."
          }
        }
      }
    },
    "notes": {
      "native": "DashScope native paths such as /api/v1/services/* and compatible-mode files/batches have separate contracts; wildcard paths are not arbitrary provider API proxies. Confirm the model-specific request format before using a specialized integration."
    }
  },
  "suppliers": {
    "title": "Suppliers, plans & access",
    "intro": "The supplier is the upstream account selected by an administrator. Your client connects to this site with a site API key; a supplier name in the catalog is not a separate public gateway address.",
    "steps": {},
    "tables": {
      "scope": {
        "title": "Which configuration belongs where?",
        "columns": {
          "setting": "Setting",
          "owner": "Where to configure it",
          "meaning": "What it controls"
        },
        "rows": {
          "public": {
            "setting": "Public URL + site API key",
            "owner": "Your client or workflow",
            "meaning": "OmniHive public Base URL: https://api.omnihive.io/v1. Chat and Responses clients append their operation path. Claude Code uses the root https://api.omnihive.io; Gemini uses /v1beta/models/…:generateContent or :streamGenerateContent."
          },
          "supplier": {
            "setting": "Supplier account + regional / plan key",
            "owner": "Administrator account setup",
            "meaning": "Choose the supplier, region, PAYG / coding / token plan, upstream protocol and matching credential. Supplier API bases and account keys belong in administrator setup, not in a client calling this site."
          },
          "group": {
            "setting": "Site key group + exact model ID",
            "owner": "API Keys and model catalog",
            "meaning": "The group controls available routes, models, account selection and billing scope. Query /v1/models and /v1/models/catalog with the same key; changing supplier configuration does not grant every user access."
          },
          "unknown": {
            "setting": "Unknown discovery, quota or price",
            "owner": "Catalog, usage and pricing pages",
            "meaning": "No discovery connector does not mean no model access. Missing quota is not zero or unlimited; missing price is not free. Ask for a confirmed model mapping and site price when the value is unknown."
          },
          "capabilities": {
            "setting": "Tools, stream, thinking and cache",
            "owner": "Model and protocol capability metadata",
            "meaning": "Check the selected model and route separately. A text bridge does not certify all tool schemas, reasoning controls, cache discounts, multimodal inputs or upstream-native media generation."
          }
        }
      }
    },
    "notes": {
      "coverage": "Supplier families, regions, products and plans follow the current admin catalog; account and group permissions determine access. Astron Token Plan and Xiaomi MiMo China/Europe/Singapore are distinct products or clusters; similar names do not imply interchangeable keys. Copilot and Nous support manual credentials: fine_grained_pat, invocation_token and official_proxy; no automatic OAuth or refresh. Their upstream is Chat; our Messages / Responses / Gemini bridges have offline contract tests, not live vendor certification. Operator-authorized keys are required to establish live availability. Internal payg routing does not mean vendor PAYG billing. RelayCode remains unresolved separately from implemented RelaxyCode; ETok.ai is a historically removed preset, not a shutdown claim.",
      "plans": "Regional roots, plan keys and unknown metadata remain separate. Tencent Intl personal cannot inherit enterprise Responses from a shared host; enterprise needs an explicit Responses endpoint. Copilot uses Copilot-Integration-Id: omnihive and root /models, distinct from /v1/chat/completions. Nous invocation tokens expire and require manual replacement; official_proxy needs an explicit portable operator URL prefix (no automatic /v1) and an optional frontdoor key. Use single-account editing to change auth_mode, including referenced credentials: provide a fresh key or explicit clearing for a keyless proxy; same-mode edits preserve secret references. Do not read client token stores or share refresh grants. Consumer subscriptions and coding plans do not automatically grant API, sharing or automation rights.",
      "media": "Image, video and audio routes are documented in their own chapters. Adding a compatible text supplier does not add native media adapters. Confirm the specific media action, group and model before calling it.",
      "evidence": "Manual wallets support Novita / PPIO (USD / CNY, divide by 10000), SiliconFlow CN / Global (CNY / USD, no scaling), SoleAPI (remaining Credits, no cash conversion) and AtlasCloud PAYG (account balance, returned currency, read permission required). Shengsuanyun common returns a CNY wallet; enterprise returns project_budget with its period and unlimited state, not a personal wallet. Readings carry timestamps. Unsupported or failed is not zero. CC Switch commit 793e67d9b3210eb527e7d36f2c866a56fae9dec7 is configuration evidence, not runtime certification; vendor documentation does not promise site pricing or features.",
      "selection": "Administrators check authentication, plan type, then protocol. Where a plan catalog exists, select and save billing_plan: AICodeMirror offers PRO / MAX / ULTRA / PAYG; CCSub offers monthly subscription / PAYG. Editing restores the selection; clearing removes it. Unselected stays unset. Selection does not purchase quota, grant models or change transport bases. AtlasCloud Coding and Qiniu subscription retain separate access modes and dedicated keys. Site user subscriptions and upstream supplier plans are separate.",
      "usage": "Qiniu PAYG manually queries consumed_usage for today, 7 days or 30 days: day granularity, RFC3339 boundaries with explicit timezone offsets, at most 31 days, default Asia/Shanghai. Preserve each model/item unit, including kToken; do not add unlike units or label consumption as remaining quota. Upstream wallets, plan quotas, project budgets and the site ledger are separate; site records exclude calls made outside this gateway. Saved plan selections block unverified PAYG queries; AtlasCloud Coding and Qiniu subscription have no integrated plan-balance query. Atlas daily model usage keeps its separate wallet; Azure shows subscription/region allocation, GitHub personal premium-request/AI-credit reports, and Together organization monthly billing (beta access required). Tencent domestic enterprise Token Plan uses independent Secret ID/Key, region and team. Package, cycle and billing quantities retain exact decimal strings; call_count stays a count. MiniMax new CN Token Plan only uses its exact official profile. Current api.minimax.cn and api.minimax.io PAYG API-key accounts support manual balance queries, with key class checked on the server. Legacy minimaxi.com, Coding, custom and mixed-region profiles do not inherit that balance endpoint. Values are not scaled, unknown currency never defaults to CNY or USD, genuine zero remains visible and errors never become zero. TheRouter shows total credits and total usage without subtracting a wallet balance. Optional query credentials do not block inference saves; missing setup sends no upstream request. Identity changes clear old readings; ordinary updates and same-identity failures preserve history with an error notice.",
      "queryErrors": "Distinguish no integrated query, unsupported mode, custom origin mismatch, invalid credentials, permission denied, unavailable proxy, rate limit, upstream failure and invalid response. Unknown is neither zero nor unlimited. Check success and time before reading amounts; an old wallet snapshot is not a live balance. Queries run from administrator account rows; ordinary site model keys do not authorize admin queries.",
      "configuration": "Direct URL + key: use the site address, site key and exact model ID visible to the current group. CC Switch is optional configuration tooling; its presets, model mappings and local protocol conversion need separate checks and cannot replace gateway authorization. Discovery/synchronization and inference are separate; administrators may maintain confirmed model mappings when discovery is unavailable.",
      "volcengineQuota": "Volcengine Ark Coding personal/team plans: administrators supply separate query AK/SK, independent of the inference key. Secrets are not echoed; blank editing preserves them and explicit clearing removes them. Older accounts default to personal; team requires an explicit saved seat; missing seats send no request and never call GetSeatInfo. 5h / weekly / monthly show used percentages and reset times only, never fabricated used/total, tokens or cash; reset is not subscription expiry. Missing query credentials do not affect existing inference configuration. Domestic team Ark Agent Plan requires an explicit seat and supports reported daily / 5h / weekly / monthly windows using the same AK/SK. Personal Agent and BytePlus personal queries are not integrated. BytePlus Team reuses billing_plan=team, separate byteplus_access_key_id / byteplus_secret_access_key and byteplus_seat_id; it never uses domestic credentials or hosts.",
      "quotaStatus": "For Volcengine queries check success / status and time: ok, missing_credentials, invalid_credentials, permission_denied, unsupported_profile, no_verified_query, not_subscribed, no_seat, invalid_response, upstream_error and url_rejected distinguish success, missing/invalid credentials, permission, profile, unverified query, subscription, missing saved seat or upstream seat data, response, upstream and outbound-policy outcomes. HTTP 200 is not business success. Failures never become zero; ordinary failures preserve history, while identity_changed clears observations from the old identity; read old snapshots with their timestamps, and do not fill missing windows with zero.",
      "modelSync": "Failed model synchronization or saving preserves existing model IDs, aliases, whitelist, selections and the last successful catalog. Only discovered_models represent actual upstream discovery; candidate_models and platform presets remain candidates, not account eligibility. Selecting a candidate manually is not discovery success. Candidates alone cannot make the catalog ready or populate eligible_models. discovery=none defaults to an unconfirmed listing contract, not officially confirmed absence; keep it closed without guessing paths. AiHubMix, DMX China and Qiniu PAYG now support verified model listing; other regions/subscription modes do not inherit it. Reopening an Antigravity account retains aliases separately from direct model IDs.",
      "modelSyncErrors": "Model synchronization uses upstream_model_sync_ reasons: configuration, credentials, account_type, not_adapted, discovery_unconfirmed, endpoint_not_found (upstream 404), method_not_allowed (405), permission (403), rate_limited (429), network, upstream and internal. A 403 without an explicit authentication error does not establish an invalid key. Unknown reasons use generic sync failure, not account type. Preserve configuration while fixing errors; successful discovery still requires separate inference validation."
    }
  }
}
