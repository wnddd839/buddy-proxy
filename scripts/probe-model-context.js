// One-off upstream probe for /v3/config model context limits (local debugging).
// Usage: node scripts/probe-model-context.js [path/to/proxy-accounts.json]
// Does not print tokens. IDE version should match config.DefaultIDEVersion when upstream
// rejects stale UA. Prints context-related fields for models matching INTEREST.
const fs = require("fs");
const https = require("https");
const os = require("os");
const path = require("path");

const defaultPath = path.join(os.homedir(), ".codebuddy", "proxy-accounts.json");
const accountsPath = process.argv[2] || defaultPath;
const IDE = "2.117.2";
const INTEREST = /deepseek|glm|hy4|hunyuan/i;
const CONTEXT_KEYS = /context|token|window|length|max|limit|capacity|input|output|seq/i;

function get(url, token, userId) {
  return new Promise((resolve, reject) => {
    const u = new URL(url);
    const headers = {
      Accept: "application/json",
      Authorization: "Bearer " + token,
      "X-IDE-Type": "CLI",
      "X-IDE-Name": "CLI",
      "X-IDE-Version": IDE,
      "User-Agent": "CLI/" + IDE + " CodeBuddy/" + IDE,
      "X-Product": "SaaS",
      "X-User-Id": userId || "probe",
      "X-Domain": u.hostname,
      "X-Agent-Intent": "craft",
      "X-Requested-With": "XMLHttpRequest",
    };
    const r = https.request({ hostname: u.hostname, path: u.pathname + u.search, method: "GET", headers }, (res) => {
      let d = "";
      res.on("data", (c) => (d += c));
      res.on("end", () => resolve({ status: res.statusCode, body: d }));
    });
    r.on("error", reject);
    r.end();
  });
}

function walkModels(payload) {
  const root = payload.data || payload;
  let models = root.models;
  if (models && !Array.isArray(models) && models.models) models = models.models;
  return Array.isArray(models) ? models : [];
}

function pickContext(obj, prefix = "") {
  const out = {};
  if (!obj || typeof obj !== "object" || Array.isArray(obj)) return out;
  for (const [k, v] of Object.entries(obj)) {
    const path = prefix ? prefix + "." + k : k;
    if (CONTEXT_KEYS.test(k)) {
      if (v && typeof v === "object") out[path] = JSON.stringify(v).slice(0, 240);
      else out[path] = v;
    } else if (v && typeof v === "object" && !Array.isArray(v) && prefix.split(".").length < 2) {
      Object.assign(out, pickContext(v, path));
    }
  }
  return out;
}

(async () => {
  const store = JSON.parse(fs.readFileSync(accountsPath, "utf8"));
  const acc = (store.accounts || []).find((a) => a.bearerToken);
  if (!acc) {
    console.log("no account");
    return;
  }
  const uid = acc.authStatus?.userId || "probe";
  const url = acc.site === "global" ? "https://www.codebuddy.ai/v3/config" : "https://copilot.tencent.com/v3/config";
  console.log("site", acc.site, "url", url);
  const { status, body } = await get(url, acc.bearerToken, uid);
  let payload;
  try {
    payload = JSON.parse(body);
  } catch {
    console.log("HTTP", status, "not json", body.slice(0, 200));
    return;
  }
  console.log("HTTP", status, "apiCode", payload.code, "bodyBytes", body.length);
  const models = walkModels(payload);
  console.log("modelCount", models.length);
  if (models[0]) console.log("sampleKeys", Object.keys(models[0]).join(","));
  for (const m of models) {
    const id = String(m.id || m.modelId || "");
    if (!INTEREST.test(id) && !INTEREST.test(String(m.name || ""))) continue;
    const ctx = pickContext(m);
    console.log("---", id, m.name || "");
    console.log("  contextFields", JSON.stringify(ctx));
  }
})();
