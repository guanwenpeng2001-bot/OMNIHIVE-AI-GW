#!/usr/bin/env node
import { readFileSync, writeFileSync } from "node:fs";
import { MODELS } from "../tools/pi-catalog/node_modules/@mariozechner/pi-ai/dist/models.generated.js";

const sourcePackage = "@mariozechner/pi-ai";
const readJSON = (path) => JSON.parse(readFileSync(new URL(path, import.meta.url), "utf8"));
const packageVersion = readJSON(`../tools/pi-catalog/node_modules/${sourcePackage}/package.json`).version;
const dependencyVersion = readJSON("../tools/pi-catalog/package.json").dependencies[sourcePackage];
if (packageVersion !== dependencyVersion) {
  throw new Error(`Expected ${sourcePackage}@${dependencyVersion}, found ${packageVersion}. Run npm ci in tools/pi-catalog.`);
}

// Order sets precedence when a pair of source providers share a target key.
const providers = new Map(Object.entries({
  "amazon-bedrock": "bedrock",
  anthropic: "anthropic",
  "azure-openai-responses": "azure-openai-responses",
  cerebras: "cerebras",
  "cloudflare-ai-gateway": "cloudflare-ai-gateway",
  "cloudflare-workers-ai": "cloudflare-workers-ai",
  deepseek: "deepseek",
  fireworks: "fireworks",
  "github-copilot": "github-copilot",
  google: "gemini",
  "google-vertex": "vertex_ai",
  groq: "groq",
  huggingface: "huggingface",
  "kimi-coding": "kimi-for-coding",
  minimax: "minimax",
  "minimax-cn": "minimax-cn",
  mistral: "mistral",
  moonshotai: "moonshot",
  "moonshotai-cn": "moonshotai-cn",
  openai: "openai",
  "openai-codex": "openai",
  "opencode-go": "opencode-go",
  opencode: "opencode-go",
  openrouter: "openrouter",
  "vercel-ai-gateway": "vercel-ai-gateway",
  xai: "xai",
  xiaomi: "xiaomi",
  "xiaomi-token-plan-ams": "xiaomi-token-plan-ams",
  "xiaomi-token-plan-cn": "xiaomi-token-plan-cn",
  "xiaomi-token-plan-sgp": "xiaomi-token-plan-sgp",
  zai: "zai",
}));

const scopedPricingProviders = new Set([
  "azure-openai-responses",
  "cloudflare-ai-gateway",
  "github-copilot",
  "opencode-go",
  "openrouter",
  "vercel-ai-gateway",
  "vertex_ai",
]);

function capabilityProtocol(provider, model) {
  switch (model.api) {
    case "openai-completions":
    case "mistral-conversations":
      return ["chat", ["chat_completions"]];
    case "openai-responses": {
      const responsesOnly = provider !== "openai" || /-codex(?:-|$)|-(?:pro|deep-research)$/.test(model.id);
      return ["responses", responsesOnly ? ["responses"] : ["responses", "chat_completions"]];
    }
    case "azure-openai-responses":
    case "openai-codex-responses":
      return ["responses", ["responses"]];
    case "anthropic-messages":
      return ["chat", ["messages"]];
    case "google-generative-ai":
    case "google-vertex":
      return ["chat", ["generateContent"]];
    case "bedrock-converse-stream":
      return ["chat", ["converse"]];
    default:
      throw new Error(`Unmapped protocol ${model.api} for ${provider}/${model.id}`);
  }
}

// Shift the decimal representation and emit a JSON number with no float division
// or rounding at a fixed decimal precision.
function perToken(perMillion) {
  if (perMillion === 0) return 0;
  const [mantissa, exponent = "0"] = String(perMillion).split("e");
  const [whole, fraction = ""] = mantissa.split(".");
  const digits = whole + fraction;
  const point = whole.length + Number(exponent) - 6;
  let decimal;
  if (point <= 0) {
    decimal = `0.${"0".repeat(-point)}${digits}`;
  } else if (point >= digits.length) {
    decimal = digits.padEnd(point, "0");
  } else {
    decimal = `${digits.slice(0, point)}.${digits.slice(point)}`;
  }
  decimal = decimal.replace(/^0+(?=\d)/, "");
  if (decimal.includes(".")) decimal = decimal.replace(/0+$/, "").replace(/\.$/, "");
  return JSON.rawJSON(decimal);
}

function writeJSON(path, value) {
  writeFileSync(new URL(path, import.meta.url), JSON.stringify(value, null, 2) + "\n");
}

const presets = readJSON("../backend/internal/service/platform_model_presets.json");
const presetKeys = new Set(presets.flatMap((preset) =>
  preset.models.flatMap(({ id }) => {
    const profile = {
      native: preset.platform,
      google: "gemini",
      "vertex-gemini": "vertex_ai",
      "vertex-anthropic": "vertex_ai",
    }[preset.provider_profile] ?? preset.provider_profile;
    return [`${preset.platform}/${id}`, `${profile}/${id}`];
  }),
));

const capabilities = new Map();
const qualifiedPrices = new Map();
const barePrices = new Map();
const seen = new Set();
const upstreamProviders = Object.keys(MODELS);
for (const provider of upstreamProviders) {
  if (!providers.has(provider)) throw new Error(`Unmapped provider ${provider}`);
}
let upstreamCount = 0;

for (const [sourceProvider, provider] of providers) {
  if (!Object.hasOwn(MODELS, sourceProvider)) continue;
  for (const [id, model] of Object.entries(MODELS[sourceProvider])) {
    upstreamCount += 1;
    if (model.id !== id || model.provider !== sourceProvider || !id || !model.name ||
        !Number.isSafeInteger(model.contextWindow) || model.contextWindow <= 0 ||
        !Number.isSafeInteger(model.maxTokens) || model.maxTokens <= 0 ||
        typeof model.reasoning !== "boolean" ||
        !Array.isArray(model.input) || !model.input.length ||
        !model.input.every((modality) => typeof modality === "string" && modality.length)) {
      throw new Error(`Invalid model record ${sourceProvider}/${id}`);
    }
    const [mode, methods] = capabilityProtocol(provider, model);
    const key = `${provider}/${id}`;
    if (seen.has(key)) continue;
    seen.add(key);

    if (!presetKeys.has(key)) {
      capabilities.set(key, {
        provider,
        profile: provider,
        model_id: id,
        display_name: model.name,
        mode,
        supported_methods: methods,
        context_window: model.contextWindow,
        context_limit_kind: "total",
        max_output_tokens: model.maxTokens,
        supports_reasoning: model.reasoning,
        supports_vision: model.input.includes("image"),
        input_modalities: model.input,
        output_modalities: ["text"],
      });
    }

    const { input, output, cacheRead, cacheWrite } = model.cost;
    const rates = [input, output, cacheRead, cacheWrite];
    if (!rates.every(Number.isFinite)) throw new Error(`Invalid token rates for ${key}`);
    // All-zero and negative sentinel rates do not establish a token tariff.
    const hasRate = rates.every((rate) => rate >= 0) && (input > 0 || output > 0);
    const price = hasRate ? {
      litellm_provider: provider,
      mode,
      input_cost_per_token: perToken(input),
      output_cost_per_token: perToken(output),
      cache_read_input_token_cost: perToken(cacheRead),
      cache_creation_input_token_cost: perToken(cacheWrite),
      max_input_tokens: model.contextWindow,
      max_output_tokens: model.maxTokens,
    } : null;
    const pricingID = id.toLowerCase();
    const pricingKey = `${provider}/${pricingID}`;
    if (price) {
      if (qualifiedPrices.has(pricingKey)) throw new Error(`Duplicate normalized price key ${pricingKey}`);
      qualifiedPrices.set(pricingKey, price);
    }
    if (!scopedPricingProviders.has(provider)) {
      // Bare names are usable only when they identify one unscoped provider.
      barePrices.set(pricingID, !barePrices.has(pricingID) ? price : null);
    }
  }
}

for (const [id, price] of barePrices) {
  if (price && !qualifiedPrices.has(id)) qualifiedPrices.set(id, price);
}

if (capabilities.size === 0 || qualifiedPrices.size === 0) {
  throw new Error(`No usable models or rates found in ${sourcePackage}@${packageVersion}`);
}

writeJSON("../backend/resources/model-capabilities/pi-supplement.json", {
  schema_version: 1,
  source_package: sourcePackage,
  source_version: packageVersion,
  records: [...capabilities.keys()].sort().map((key) => capabilities.get(key)),
});
writeJSON("../backend/resources/model-pricing/pi-prices-supplement.json",
  Object.fromEntries([...qualifiedPrices.keys()].sort().map((key) => [key, qualifiedPrices.get(key)])),
);
console.log(`Synced ${sourcePackage}@${packageVersion} (${upstreamProviders.length} providers, ${upstreamCount} upstream models)`);
console.log(`Generated ${capabilities.size} capability records and ${qualifiedPrices.size} price keys`);
