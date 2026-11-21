import { curlResourceExample, jsonRequestExample, powershellChatExample, requestExample, shellQuote } from './quickstart'

export function guideExamples(root: string, siteName = 'OmniHive'): Record<string, { label: string; code: string }[]> {
  const base = root ? `${root}/v1` : ''
  if (!base) return {}
  return {
    ...apiExamples(root),
    quick: [
      { label: 'Bash · environment & models', code: String.raw`export OMNIHIVE_BASE_URL=${shellQuote(base)}
export OMNIHIVE_API_KEY="sk-your-api-key"
curl "$OMNIHIVE_BASE_URL/models" \
  -H "Authorization: Bearer $OMNIHIVE_API_KEY"` },
      requestExample(root, 'MODEL_ID_FROM_LIST', 'chat/completions').examples[0],
      powershellChatExample(root, 'MODEL_ID_FROM_LIST')
    ],
    models: [curlResourceExample(root, '/backend-api/codex/models', 'Bash · Codex catalog'), curlResourceExample(root, '/v1/models/catalog?limit=100', 'Bash · rich catalog')],
    clients: [
      ...clientExamples(root, siteName),
      { label: 'Text · Cursor', code: String.raw`OpenAI Base URL: ${base}
OpenAI API Key: sk-your-api-key
Model: MODEL_ID_FROM_LIST` },
      { label: 'Cline / Roo Code · OpenAI Compatible', code: String.raw`API Provider: OpenAI Compatible
Base URL: ${base}
API Key: sk-your-api-key
Model: MODEL_ID_FROM_LIST` },
      { label: 'Continue · config.yaml', code: String.raw`name: ${JSON.stringify(siteName)}
version: 0.0.1
schema: v1
models:
  - name: MODEL_ID_FROM_LIST
    provider: openai
    model: MODEL_ID_FROM_LIST
    apiBase: ${base}
    apiKey: sk-your-api-key` },
      { label: 'JetBrains AI Assistant · OpenAI Compatible', code: String.raw`Settings | Tools | AI Assistant | Providers & API keys
Provider: OpenAI-compatible endpoint
URL: ${base}
API Key: sk-your-api-key` },
      { label: 'Python · OpenAI SDK', code: String.raw`# python -m pip install openai
import os
from openai import OpenAI

client = OpenAI(
    base_url=os.environ["OMNIHIVE_BASE_URL"],
    api_key=os.environ["OMNIHIVE_API_KEY"],
)
response = client.chat.completions.create(
    model="MODEL_ID_FROM_LIST",
    messages=[{"role": "user", "content": "Hello"}],
)
print(response.choices[0].message.content)` },
      { label: 'Python · Anthropic Messages SDK', code: String.raw`# python -m pip install anthropic
import os
from anthropic import Anthropic

client = Anthropic(
    base_url=${JSON.stringify(root)},
    api_key=os.environ["OMNIHIVE_API_KEY"],
)
message = client.messages.create(
    model="MESSAGES_MODEL_ID_FROM_LIST",
    max_tokens=256,
    messages=[{"role": "user", "content": "Hello"}],
)
print(message.content[0].text)` },
      { label: 'TypeScript · Google Gemini SDK', code: String.raw`// npm install @google/genai
import { GoogleGenAI } from "@google/genai";

const ai = new GoogleGenAI({
  apiKey: process.env.OMNIHIVE_API_KEY,
  httpOptions: { baseUrl: ${JSON.stringify(root)} },
});
const response = await ai.models.generateContent({
  model: "GOOGLE_MODEL_ID_FROM_LIST",
  contents: "Hello",
});
console.log(response.text);` },
      { label: 'TypeScript · 429 Rate-Limit & Retry-After Backoff', code: String.raw`// Respect conditional Retry-After / RPM headers; avoid blind retries on 504/timeout to prevent duplicate charges.
async function callWithBackoff(url: string, init: RequestInit, maxAttempts = 3) {
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    const res = await fetch(url, init);
    if (res.status !== 429 || attempt === maxAttempts) return res;
    const retryAfterSec = Number(res.headers.get("Retry-After"));
    const waitMs = Number.isFinite(retryAfterSec) && retryAfterSec > 0
      ? retryAfterSec * 1000
      : Math.min(1000 * 2 ** (attempt - 1), 8000);
    await new Promise((resolve) => setTimeout(resolve, waitMs));
  }
  throw new Error("Exhausted 429 retries");
}` },
      { label: 'TOML · ~/.codex/config.toml', code: String.raw`model_provider = "omnihive"
model = "MODEL_ID_FROM_CODEX_LIST"

[model_providers.omnihive]
name = ${JSON.stringify(siteName)}
base_url = "${base}"
env_key = "OMNIHIVE_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false` },

    ]
  }
}


function apiExamples(root: string) {
  const chat = requestExample(root, 'MODEL_ID_FROM_LIST', 'chat/completions')
  const request = (path: string, body: Record<string, unknown>, label: string, output?: string) => ({
    label, code: jsonRequestExample(root, path, body).examples[0].code + (output ? ` --output ${shellQuote(output)}` : '')
  })
  return {
    addresses: [{ label: 'API addresses', code: `Gateway root: ${root}
OpenAI-compatible base: ${root}/v1
Chat request URL: ${chat.url}` }],
    chat: chat.examples,
    messages: requestExample(root, 'MESSAGES_MODEL_ID_FROM_LIST', 'messages').examples,
    responses: requestExample(root, 'RESPONSES_MODEL_ID_FROM_LIST', 'responses').examples,
    gemini: requestExample(root, 'GOOGLE_MODEL_ID_FROM_LIST', 'gemini').examples,
    retrieval: [
      request('/v1/embeddings', { model: 'EMBEDDING_MODEL_ID_FROM_LIST', input: 'A quiet coast' }, 'Bash · embeddings'),
      request('/v1/rerank', { model: 'RERANK_MODEL_ID_FROM_LIST', query: 'coast', documents: ['A coastal lighthouse', 'A mountain trail'], top_n: 1 }, 'Bash · rerank')
    ],
    streaming: [request('/v1/chat/completions', { ...chat.body, stream: true }, 'Bash · Chat SSE')],
    tools: [request('/v1/chat/completions', {
      model: 'TOOL_CAPABLE_CHAT_MODEL_ID_FROM_LIST',
      messages: [{ role: 'user', content: 'What is the weather in Shanghai?' }],
      tools: [{ type: 'function', function: { name: 'get_weather', description: 'Get the weather for a city', parameters: { type: 'object', properties: { city: { type: 'string' } }, required: ['city'] } } }]
    }, 'Bash · function declaration')],
    // No invented effort enum: use an actual value supplied by the catalog.
    reasoning: [{ label: 'Node · Responses reasoning', code: `const key = process.env.OMNIHIVE_API_KEY;
const effort = process.env.MODEL_REASONING_EFFORT; // Set to a supported catalog level.
if (!key || !effort) throw new Error("Set OMNIHIVE_API_KEY and MODEL_REASONING_EFFORT");
const response = await fetch(${JSON.stringify(`${root}/v1/responses`)}, {
  method: "POST",
  headers: { "Authorization": "Bearer " + key, "Content-Type": "application/json" },
  body: JSON.stringify({ model: "REASONING_MODEL_ID_FROM_LIST", input: "Explain your conclusion briefly.", reasoning: { effort } }),
});
if (!response.ok) throw new Error(\`HTTP \${response.status}\`);
console.log(await response.json());` }],
    cache: [jsonRequestExample(root, '/v1/messages', {
      model: 'CACHE_CAPABLE_MESSAGES_MODEL_ID_FROM_LIST', max_tokens: 256,
      messages: [{ role: 'user', content: [{ type: 'text', text: 'Stable context for a repeated question.', cache_control: { type: 'ephemeral' } }] }]
    }, 'x-api-key', { 'Content-Type': 'application/json', 'anthropic-version': '2023-06-01' }).examples[0]],
    images: [
      request('/v1/images/generations', { model: 'IMAGE_MODEL_ID_FROM_LIST', prompt: 'A lighthouse on a quiet coast' }, 'Bash · image generation'),
      curlResourceExample(root, '/v1/images/edits', 'Bash · OpenAI / Qianwen multipart edit', ['model=IMAGE_EDIT_MODEL_ID_FROM_LIST', 'prompt=Make the sky blue', 'image=@./reference.png;type=image/png'])
    ],
    'async-images': [
      request('/v1/images/generations/async', { model: 'ASYNC_IMAGE_MODEL_ID_FROM_LIST', prompt: 'A lighthouse on a quiet coast' }, 'Bash · submit async image'),
      curlResourceExample(root, '/v1/images/tasks/TASK_ID_FROM_SUBMIT', 'Bash · poll image task')
    ],
    'media-jobs': [
      request('/v1/videos/generations', { model: 'OPENROUTER_VIDEO_MODEL_ID_FROM_LIST', prompt: 'A slow camera move along a quiet coast', duration: 4 }, 'Bash · OpenRouter video submit'),
      curlResourceExample(root, '/v1/videos/VIDEO_ID_FROM_SUBMIT', 'Bash · poll video'),
      curlResourceExample(root, '/v1/videos/VIDEO_ID_FROM_SUBMIT/content', 'Bash · download completed video', [], 'video.mp4')
    ],
    audio: [
      request('/v1/audio/speech', { model: 'OPENROUTER_TTS_MODEL_ID_FROM_LIST', input: 'Hello', voice: 'VOICE_ID_FOR_SELECTED_MODEL', response_format: 'mp3' }, 'Bash · OpenRouter speech', 'speech.mp3'),
      request('/v1/audio/speech', { model: 'QIANWEN_TTS_MODEL_ID_FROM_LIST', input: 'Hello', voice: 'VOICE_ID_FOR_SELECTED_MODEL', response_format: 'wav' }, 'Bash · Qianwen speech', 'speech.wav'),
      curlResourceExample(root, '/v1/audio/transcriptions', 'Bash · OpenRouter / Qianwen transcription', ['model=STT_MODEL_ID_FROM_LIST', 'file=@./speech.wav;type=audio/wav', 'response_format=json']),
      curlResourceExample(root, '/v1/audio/translations', 'Bash · OpenAI audio translation', ['model=OPENAI_AUDIO_TRANSLATION_MODEL_ID_FROM_LIST', 'file=@./speech.wav;type=audio/wav'])
    ],
    usage: [curlResourceExample(root, '/v1/usage', 'Bash · current key usage')]
  }
}

function clientExamples(root: string, siteName: string) {
  return [
    { label: 'Bash · Claude Code CLI', code: `export ANTHROPIC_BASE_URL=${shellQuote(root)}
export ANTHROPIC_API_KEY="$OMNIHIVE_API_KEY"
claude --model 'MESSAGES_MODEL_ID_FROM_LIST'` },
    { label: 'JSON · OpenCode · opencode.json', code: JSON.stringify({
      provider: { omnihive: { npm: '@ai-sdk/openai-compatible', name: siteName, options: { baseURL: `${root}/v1`, apiKey: '{env:OMNIHIVE_API_KEY}' }, models: { MODEL_ID_FROM_LIST: { name: 'MODEL_ID_FROM_LIST' } } } },
      model: 'omnihive/MODEL_ID_FROM_LIST'
    }, null, 2) },
    { label: 'JSON · Pi · ~/.pi/agent/models.json', code: JSON.stringify({
      providers: { omnihive: { baseUrl: `${root}/v1`, api: 'openai-completions', apiKey: '$OMNIHIVE_API_KEY', models: [{ id: 'MODEL_ID_FROM_LIST' }] } }
    }, null, 2) },
    { label: 'DSH · provider setup', code: `Protocol: openai-completions
baseURL: ${root}/v1
apiKeyEnv: OMNIHIVE_API_KEY
Model ID: MODEL_ID_FROM_LIST` }
  ]
}
