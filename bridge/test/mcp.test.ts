/**
 * Tests of the MCP client: a real child process speaks JSON-RPC over stdio, so the
 * handshake, the tool listing, a tool call and a shutdown are exercised end to end
 * without depending on an MCP server being installed.
 *
 * Run with `npm test` (the Node test runner executes the TypeScript directly; there is
 * no build step in this package).
 */

import { strict as assert } from "node:assert";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, before, test } from "node:test";
import type { ExtensionAPI, ToolDefinition } from "@earendil-works/pi-coding-agent";
import type { TSchema } from "typebox";

import {
  McpConnection,
  readMcpDocument,
  registerMcpServers,
  stdioServers,
  toolResult,
} from "../mcp.ts";

/** A minimal MCP server: initialize, tools/list, tools/call. */
const FAKE_SERVER = `
let buffer = "";
const reply = (id, result) => process.stdout.write(JSON.stringify({ jsonrpc: "2.0", id, result }) + "\\n");
process.stdin.on("data", (chunk) => {
  buffer += chunk;
  let index = buffer.indexOf("\\n");
  while (index >= 0) {
    const line = buffer.slice(0, index).trim();
    buffer = buffer.slice(index + 1);
    if (line !== "") {
      const message = JSON.parse(line);
      if (message.id !== undefined) {
        if (message.method === "initialize") {
          reply(message.id, { protocolVersion: "2024-11-05", capabilities: {}, serverInfo: { name: "fake", version: "1" } });
        } else if (message.method === "tools/list") {
          reply(message.id, { tools: [{ name: "echo", description: "Echo the text", inputSchema: { type: "object", properties: { text: { type: "string" } }, required: ["text"] } }] });
        } else if (message.method === "tools/call") {
          if (message.params.name === "echo") {
            reply(message.id, { content: [{ type: "text", text: "echo: " + message.params.arguments.text }] });
          } else {
            reply(message.id, { content: [{ type: "text", text: "unknown tool" }], isError: true });
          }
        } else {
          reply(message.id, {});
        }
      }
    }
    index = buffer.indexOf("\\n");
  }
});
`;

/** The command of the fake server, as an MCP entry. */
function fakeServerEntry(): { command: string; args: string[] } {
  return { command: process.execPath, args: ["-e", FAKE_SERVER] };
}

/** Writes one MCP document into a temporary file. */
function writeDocument(document: unknown): string {
  const path = join(mkdtempSync(join(tmpdir(), "piui-mcp-")), "mcp.json");
  writeFileSync(path, JSON.stringify(document), "utf8");
  return path;
}

let connection: McpConnection | undefined;

after(async () => {
  await connection?.close();
});

test("a document names its enabled stdio servers", () => {
  const path = writeDocument({
    version: 1,
    servers: {
      b: fakeServerEntry(),
      a: { ...fakeServerEntry(), enabled: false },
      remote: { url: "https://mcp.example/sse" },
    },
  });

  const document = readMcpDocument(path);
  const servers = stdioServers(document);
  assert.deepEqual(
    servers.map((entry) => entry.name),
    ["b"],
    "disabled and remote entries are not started",
  );
});

test("a missing or broken configuration is empty, never a throw", () => {
  assert.deepEqual(readMcpDocument(undefined), {});
  const path = writeDocument({ servers: {} });
  writeFileSync(path, "not json", "utf8");
  assert.deepEqual(readMcpDocument(path), {}, "an unreadable file degrades to no servers");
});

test("the handshake lists the tools and a call returns their content", async () => {
  connection = new McpConnection("fake", () => {});
  connection.start(fakeServerEntry());

  const tools = await connection.initialize();
  assert.equal(tools.length, 1);
  assert.equal(tools[0]?.name, "echo");

  const result = toolResult(await connection.callTool("echo", { text: "hello" }));
  assert.deepEqual(result.content, [{ type: "text", text: "echo: hello" }]);

  // An MCP tool that failed answers successfully at the JSON-RPC level; the failure is
  // in the result, and it is `toolResult` that turns it into pi's thrown-error contract.
  const failed = await connection.callTool("nope", {});
  assert.equal(failed.isError, true);
  assert.throws(() => toolResult(failed), /unknown tool/);
});

test("closing is idempotent and ends the child", async () => {
  const one = new McpConnection("fake", () => {});
  one.start(fakeServerEntry());
  await one.initialize();
  await one.close();
  await one.close();
  await assert.rejects(() => one.callTool("echo", { text: "late" }), /gone|closed/);
});

test("registering connects the servers and names the tools after them", async () => {
  const path = writeDocument({ servers: { fake: fakeServerEntry(), broken: { command: "/nonexistent-mcp-server" } } });
  const registered: ToolDefinition<TSchema, unknown>[] = [];
  const pi = {
    registerTool: (tool: ToolDefinition<TSchema, unknown>) => {
      registered.push(tool);
    },
  } as unknown as ExtensionAPI;

  const lines: string[] = [];
  const connections = await registerMcpServers(pi, readMcpDocument(path), (line) => lines.push(line));
  try {
    assert.equal(registered.length, 1, "the broken server is skipped, not fatal");
    assert.equal(registered[0]?.name, "mcp_fake_echo");
    assert.ok(lines.some((line) => line.includes("broken")), "the failure is reported on stderr");

    const executed = await registered[0]!.execute(
      "call-1",
      { text: "via pi" },
      undefined,
      undefined,
      {} as never,
    );
    assert.deepEqual(executed.content, [{ type: "text", text: "echo: via pi" }]);
  } finally {
    await Promise.all(connections.map((entry) => entry.close()));
  }
});
