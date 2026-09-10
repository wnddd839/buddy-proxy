// One-off upstream probe for CodeBuddy daily check-in APIs (local debugging).
// Usage: node scripts/probe-checkin.js [path/to/proxy-accounts.json]
// Does not print tokens. IDE version should match config.DefaultIDEVersion when upstream rejects stale UA.
const fs = require("fs");
const https = require("https");
const os = require("os");
const path = require("path");

const defaultPath = path.join(os.homedir(), ".codebuddy", "proxy-accounts.json");
const accountsPath = process.argv[2] || defaultPath;
const IDE = "2.117.2";

function post(url, token, userId, site) {
  return new Promise((resolve, reject) => {
    const u = new URL(url);
    const domain =
      site === "global"
        ? "www.codebuddy.ai"
        : u.hostname.includes("codebuddy.cn")
          ? "www.codebuddy.cn"
          : "copilot.tencent.com";
    const headers = {
      Accept: "application/json",
      "Content-Type": "application/json",
      Authorization: "Bearer " + token,
      "X-Requested-With": "XMLHttpRequest",
      "X-Agent-Intent": "craft",
      "X-IDE-Type": "CLI",
      "X-IDE-Name": "CLI",
      "X-IDE-Version": IDE,
      "User-Agent": "CLI/" + IDE + " CodeBuddy/" + IDE,
      "X-Product": "SaaS",
      "X-User-Id": userId || "probe",
      "X-Domain": domain,
    };
    const body = "{}";
    const r = https.request(
      {
        hostname: u.hostname,
        path: u.pathname,
        method: "POST",
        headers: { ...headers, "Content-Length": Buffer.byteLength(body) },
      },
      (res) => {
        let d = "";
        res.on("data", (c) => (d += c));
        res.on("end", () => {
          let j = null;
          try {
            j = JSON.parse(d);
          } catch {
            j = { raw: d.slice(0, 200) };
          }
          resolve({ status: res.statusCode, code: j?.code, msg: j?.msg || j?.message, data: j?.data });
        });
      }
    );
    r.on("error", reject);
    r.write(body);
    r.end();
  });
}

const paths = [
  "checkin-activity-status",
  "checkin-status",
  "daily-checkin",
];

const bases = {
  domestic: [
    "https://copilot.tencent.com",
    "https://www.codebuddy.cn",
  ],
  global: ["https://www.codebuddy.ai"],
};

(async () => {
  const store = JSON.parse(fs.readFileSync(accountsPath, "utf8"));
  for (const acc of store.accounts || []) {
    if (!acc.bearerToken) continue;
    const site = acc.site || "domestic";
    const uid = acc.authStatus?.userId || acc.authStatus?.userName || "anonymous";
    console.log("===", acc.label, site, "uid=" + (uid ? uid.slice(0, 8) + "…" : "?"), "===");
    for (const base of bases[site] || bases.domestic) {
      for (const p of paths) {
        const url = base + "/v2/billing/meter/" + p;
        try {
          const { status, code, msg, data } = await post(url, acc.bearerToken, uid, site);
          const extra =
            data && typeof data === "object"
              ? JSON.stringify(data).slice(0, 120)
              : "";
          console.log(
            base.replace("https://", ""),
            p,
            "HTTP",
            status,
            "apiCode",
            code,
            msg || "",
            extra
          );
        } catch (e) {
          console.log(base.replace("https://", ""), p, "ERR", e.message);
        }
      }
    }
  }
})();
