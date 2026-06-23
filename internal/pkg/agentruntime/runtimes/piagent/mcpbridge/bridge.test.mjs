import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { writeFileSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

function startFake() {
  const calls = [];
  const srv = createServer((req, res) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => {
      const rpc = JSON.parse(body || "{}");
      calls.push({ method: rpc.method, auth: req.headers.authorization, args: rpc.params?.arguments });
      const reply = (result) => { res.setHeader("content-type", "application/json"); res.end(JSON.stringify({ jsonrpc: "2.0", id: rpc.id, result })); };
      if (rpc.method === "initialize") return reply({ protocolVersion: "2025-06-18" });
      if (rpc.method === "notifications/initialized") { res.statusCode = 202; return res.end(); }
      if (rpc.method === "tools/list") return reply({ tools: [
        { name: "group_send", description: "send", inputSchema: { type: "object", required: ["body"], properties: { body: { type: "string" } } } },
        { name: "secret_tool", description: "nope", inputSchema: { type: "object" } },
      ] });
      if (rpc.method === "tools/call") return reply({ content: [{ type: "text", text: "sent" }] });
      res.statusCode = 404; res.end();
    });
  });
  return new Promise((resolve) => srv.listen(0, () => resolve({ srv, port: srv.address().port, calls })));
}

function fakePi() {
  const tools = [];
  return { tools, registerTool: (t) => tools.push(t) };
}

test("registers only allowed tools by bare name and proxies tools/call with auth", async () => {
  const { srv, port, calls } = await startFake();
  const cfgPath = join(mkdtempSync(join(tmpdir(), "pibridge-")), "cfg.json");
  writeFileSync(cfgPath, JSON.stringify({ servers: [{ name: "group", url: `http://127.0.0.1:${port}/`, headers: { Authorization: "Bearer tok" }, tools: ["group_send"] }] }));
  process.env.AGENTRE_PI_MCP_CONFIG = cfgPath;

  const mod = await import("./bridge.mjs");
  const pi = fakePi();
  await mod.default(pi);

  assert.deepEqual(pi.tools.map((t) => t.name), ["group_send"]);
  assert.equal(pi.tools[0].label, "group_send");

  const result = await pi.tools[0].execute("call-1", { body: "hi" });
  assert.deepEqual(result.content, [{ type: "text", text: "sent" }]);
  const callToolCall = calls.find((c) => c.method === "tools/call");
  assert.equal(callToolCall.auth, "Bearer tok");
  assert.deepEqual(callToolCall.args, { body: "hi" });

  srv.close();
});

test("tool execute throws on rpc error (pi encodes as error result)", async () => {
  const srv = createServer((req, res) => {
    let body = ""; req.on("data", (c) => (body += c));
    req.on("end", () => {
      const rpc = JSON.parse(body || "{}");
      const reply = (o) => { res.setHeader("content-type", "application/json"); res.end(JSON.stringify({ jsonrpc: "2.0", id: rpc.id, ...o })); };
      if (rpc.method === "initialize") return reply({ result: {} });
      if (rpc.method === "notifications/initialized") { res.statusCode = 202; return res.end(); }
      if (rpc.method === "tools/list") return reply({ result: { tools: [{ name: "group_send", description: "", inputSchema: { type: "object" } }] } });
      if (rpc.method === "tools/call") return reply({ error: { code: -32000, message: "forbidden" } });
      res.statusCode = 404; res.end();
    });
  });
  await new Promise((r) => srv.listen(0, r));
  const cfgPath = join(mkdtempSync(join(tmpdir(), "pibridge-")), "cfg.json");
  writeFileSync(cfgPath, JSON.stringify({ servers: [{ name: "group", url: `http://127.0.0.1:${srv.address().port}/`, headers: {}, tools: [] }] }));
  process.env.AGENTRE_PI_MCP_CONFIG = cfgPath;

  const mod = await import(`./bridge.mjs?e=${Date.now()}`);
  const pi = fakePi();
  await mod.default(pi);
  await assert.rejects(() => pi.tools[0].execute("c", {}), /forbidden/);
  srv.close();
});
