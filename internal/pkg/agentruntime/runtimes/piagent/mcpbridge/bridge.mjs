// agentre-mcp-bridge：把注入的 HTTP MCP server 翻成 pi 一等工具。
// 由 agentre piagent runtime 经 --extension 加载；server 列表经
// AGENTRE_PI_MCP_CONFIG 指向的 JSON 文件传入。pi 无原生 MCP（仅 read/write/edit/
// bash），加工具只能走扩展。仅实现 Streamable-HTTP 的 JSON 响应（对齐 agentre 自家
// 网关 server），不做完整多事件 SSE 流。
import { readFileSync } from "node:fs";

export default async function agentreMcpBridge(pi) {
  const cfgPath = process.env.AGENTRE_PI_MCP_CONFIG;
  if (!cfgPath) return;
  let cfg;
  try {
    cfg = JSON.parse(readFileSync(cfgPath, "utf8"));
  } catch (e) {
    console.error(`[agentre-mcp-bridge] read config failed: ${e}`);
    return;
  }
  const toParameters = await makeParamConverter();
  for (const server of cfg.servers ?? []) {
    try {
      await registerServer(pi, server, toParameters);
    } catch (e) {
      // 单个 server 失败只跳过它，不拖垮整轮 pi。
      console.error(`[agentre-mcp-bridge] server ${server?.name} skipped: ${e}`);
    }
  }
}

async function registerServer(pi, server, toParameters) {
  await rpc(server, "initialize", { protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "agentre-pi-bridge", version: "1" } });
  await notify(server, "notifications/initialized", {});
  const listed = await rpc(server, "tools/list", {});
  const allow = new Set(server.tools ?? []);
  for (const t of listed.tools ?? []) {
    if (allow.size > 0 && !allow.has(t.name)) continue; // 按角色裁剪
    pi.registerTool({
      name: t.name,                 // 裸名，对齐 group system prompt 与 MCPServerSpec.Tools
      label: t.name,
      description: t.description ?? "",
      parameters: toParameters(t.inputSchema),
      execute: async (_toolCallId, params) => {
        const res = await rpc(server, "tools/call", { name: t.name, arguments: params ?? {} });
        return { content: normalizeContent(res.content), details: res };
      },
    });
  }
}

// makeParamConverter：pi 的 parameters 期望 TypeBox TSchema。优先用裸 JSON Schema
// （spike 已证实 pi 直接接受）；能加载 typebox 时用 Type.Unsafe 包一层更稳。
async function makeParamConverter() {
  try {
    const { Type } = await import("typebox");
    return (schema) => Type.Unsafe(schema ?? { type: "object" });
  } catch {
    return (schema) => schema ?? { type: "object" };
  }
}

function normalizeContent(content) {
  if (Array.isArray(content) && content.length > 0) return content;
  return [{ type: "text", text: "" }];
}

let _id = 0;

// post 发一个 JSON-RPC 帧到 server。Accept: application/json 让规范的
// Streamable-HTTP server 走 JSON 响应；若 server 仍回 SSE，parseBody 兜底取单帧。
function post(server, body) {
  return fetch(server.url, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json", ...(server.headers ?? {}) },
    body: JSON.stringify(body),
  });
}

async function rpc(server, method, params) {
  const res = await post(server, { jsonrpc: "2.0", id: ++_id, method, params });
  if (!res.ok) throw new Error(`${method} HTTP ${res.status}`);
  const data = await parseBody(res);
  if (data && data.error) throw new Error(`${method}: ${data.error.message ?? "rpc error"}`);
  return (data && data.result) ?? {};
}

async function notify(server, method, params) {
  // 通知无 id、不关心响应体；显式排空 body 让连接干净关闭，避免 socket 半开噪音。
  const res = await post(server, { jsonrpc: "2.0", method, params });
  await res.body?.cancel();
}

async function parseBody(res) {
  const ct = res.headers.get("content-type") ?? "";
  const text = await res.text();
  if (ct.includes("text/event-stream")) {
    for (const line of text.split("\n")) {
      const s = line.trim();
      if (s.startsWith("data:")) return JSON.parse(s.slice(5).trim());
    }
    return {};
  }
  return text ? JSON.parse(text) : {};
}
