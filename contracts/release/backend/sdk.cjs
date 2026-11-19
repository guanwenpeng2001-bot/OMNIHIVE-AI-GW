const fs = require("node:fs");
const path = require("node:path");
const { createRequire } = require("node:module");

async function main() {
  const load = createRequire(path.resolve(process.env.BE24_SDK_ROOT, "package.json"));
  const version = load("openai/version").VERSION;
  const OpenAI = load("openai");
  const { key, model } = JSON.parse(fs.readFileSync(0, "utf8"));
  const client = new OpenAI({ apiKey: key, baseURL: "http://127.0.0.1:18080/v1", maxRetries: 0, timeout: 12000 });
  const messages = [{ role: "user", content: "Fixture ping" }];
  const chat = await client.chat.completions.create({ model, messages });
  const stream = await client.chat.completions.create({ model, messages, stream: true });
  let content = "";
  let stops = 0;
  for await (const frame of stream) {
    const choice = frame.choices[0];
    content += choice?.delta?.content ?? "";
    if (choice?.finish_reason === "stop") stops++;
  }
  const result = {
    sdk: "openai", version, chat: chat.choices[0]?.message?.content === "Fixture pong",
    stream: content === "Fixture pong", stream_stop_frames: stops,
  };
  result.status = version === "4.104.0" && result.chat && result.stream && stops === 1 ? "pass" : "fail";
  process.stdout.write(JSON.stringify(result));
}

main().catch((error) => {
  process.stdout.write(JSON.stringify({ status: "fail", error_class: error.name, http: error.status ?? null }));
});
