export type ClientSnippetId = 'codex' | 'opencode' | 'cursor' | 'claude' | 'curl' | 'python';

export interface ClientSnippetOption {
  id: ClientSnippetId;
  label: string;
  hint: string;
}

export const CLIENT_SNIPPET_OPTIONS: ClientSnippetOption[] = [
  { id: 'codex', label: 'Codex CLI', hint: '通过 /v1/responses 直连，支持工具调用与 web_search' },
  { id: 'opencode', label: 'OpenCode', hint: '通过 OpenAI Compatible Provider 直连' },
  { id: 'cursor', label: 'Cursor', hint: '在 Models 设置中覆盖 OpenAI Base URL' },
  { id: 'claude', label: 'Claude Code', hint: '需配合 Anthropic → OpenAI 适配器使用' },
  { id: 'curl', label: 'cURL', hint: '终端最小联通验证命令' },
  { id: 'python', label: 'Python SDK', hint: '标准 OpenAI Python 客户端' },
];

export function buildClientSnippet(
  id: ClientSnippetId,
  baseUrl: string,
  model: string,
  apiKeyHint: string,
): string {
  const base = (baseUrl || 'http://127.0.0.1:32126/v1').replace(/\/$/, '');
  const m = model || 'auto';
  const key = apiKeyHint || '你的网关APIKey';

  switch (id) {
    case 'codex':
      return `# ~/.codex/config.toml
model_provider = "buddy-proxy"
model = "${m}"

[model_providers.buddy-proxy]
name = "Buddy Proxy"
base_url = "${base}"
wire_api = "responses"
env_key = "CODEBUDDY_PROXY_API_KEY"

# 终端执行：
# export CODEBUDDY_PROXY_API_KEY="${key}"
# codex "帮我看看这个仓库的结构"`;

    case 'opencode':
      return `// ~/.config/opencode/opencode.json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "buddy-proxy": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Buddy Proxy",
      "options": {
        "baseURL": "${base}",
        "apiKey": "{env:CODEBUDDY_PROXY_API_KEY}"
      },
      "models": {
        "${m}": { "name": "${m}" }
      }
    }
  },
  "model": "buddy-proxy/${m}"
}`;

    case 'cursor':
      return `// Cursor Settings -> Models -> OpenAI API Key
Override OpenAI Base URL: ${base}
OpenAI API Key:           ${key}
Model:                    ${m}（或「模型与路由」编目中的任意模型 ID）`;

    case 'claude':
      return `# Claude Code 原生使用 Anthropic Messages 协议。
# 本网关提供 OpenAI Chat / Responses，不直接提供 /v1/messages。
# 请使用你信任的 Anthropic → OpenAI 适配器，并将适配器上游设为：
#
#   Upstream Base URL: ${base}
#   Upstream API Key:  ${key}
#
# 并在「System Prompt」页选择 auto 或 strip 模式以规避 11128 拦截。`;

    case 'curl':
      return `curl ${base}/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer ${key}" \\
  -d '{
    "model": "${m}",
    "stream": true,
    "messages": [{"role": "user", "content": "你好，请做个连通性测试"}]
  }'`;

    case 'python':
      return `from openai import OpenAI

client = OpenAI(
    base_url="${base}",
    api_key="${key}",
)

resp = client.chat.completions.create(
    model="${m}",
    messages=[{"role": "user", "content": "Hello Buddy Proxy!"}],
)
print(resp.choices[0].message.content)`;
  }
}
