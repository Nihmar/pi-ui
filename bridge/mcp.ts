/**
 * pi-ui-bridge — the MCP client half.
 *
 * The bridge connects to the MCP servers the server lists in the file named by
 * `mcpConfig` in `PI_UI_BRIDGE_CONFIG` (`GET/PUT /api/v1/mcp`), lists their tools and
 * registers each one with pi, so an MCP tool is a pi tool with a `mcp_<server>_<tool>`
 * name.
 *
 * Why here and not in the Go server: MCP is a protocol between a model and a tool
 * server, and pi is the process that owns the model and its tools. The server stores
 * and validates the configuration; the child speaks the protocol.
 *
 * Transport: stdio, one child process per enabled server. A remote (`url`) entry is
 * accepted by the configuration but not connected here yet — it is reported on stderr and
 * skipped, which is what "degrade with a clear message" means for a transport this
 * version does not implement.
 *
 * Nothing here ever writes to stdout: that stream is the RPC protocol.
 */

import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { readFileSync } from "node:fs";
import { Unsafe, type TSchema } from "typebox";
import type {
  AgentToolResult,
  ExtensionAPI,
  ExtensionContext,
  ToolDefinition,
} from "@earendil-works/pi-coding-agent";

/** The MCP revision this client speaks. */
const PROTOCOL_VERSION = "2024-11-05";

/** How long a request may take before the tool call fails. */
const REQUEST_TIMEOUT_MS = 60_000;

/** How long a server gets to exit after SIGTERM before SIGKILL. */
const SHUTDOWN_GRACE_MS = 2_000;

/** Prefix of every tool this client registers, so a name collision is impossible. */
const TOOL_PREFIX = "mcp";

/** One MCP server entry, as `internal/mcp` stores it. */
export interface McpServer {
  readonly command?: string;
  readonly args?: readonly string[];
  readonly env?: Readonly<Record<string, string>>;
  readonly url?: string;
  readonly headers?: Readonly<Record<string, string>>;
  readonly enabled?: boolean;
}

/** The document, as `internal/mcp` stores it. */
export interface McpDocument {
  readonly version?: number;
  readonly servers?: Readonly<Record<string, McpServer>>;
}

/** A tool as `tools/list` reports it. */
interface McpTool {
  readonly name: string;
  readonly description?: string;
  readonly inputSchema?: unknown;
}

/** One content block of a `tools/call` result. */
interface McpContent {
  readonly type: string;
  readonly text?: string;
  readonly data?: string;
  readonly mimeType?: string;
}

/** The result of `tools/call`. */
interface McpCallResult {
  readonly content?: readonly McpContent[];
  readonly isError?: boolean;
}

/** Reads the MCP document, or returns an empty one when there is nothing to read. */
export function readMcpDocument(path: string | undefined): McpDocument {
  if (!path) {
    return {};
  }
  try {
    const text = readFileSync(path, "utf8");
    const parsed: unknown = JSON.parse(text);
    if (typeof parsed !== "object" || parsed === null) {
      throw new Error("the document is not a JSON object");
    }
    return parsed as McpDocument;
  } catch (error) {
    process.stderr.write(`pi-ui-bridge: MCP configuration ${path}: ${String(error)}\n`);
    return {};
  }
}

/** The enabled stdio servers of a document, in a stable order. */
export function stdioServers(
  document: McpDocument,
): Array<{ name: string; server: McpServer }> {
  const entries = Object.entries(document.servers ?? {});
  const enabled = entries.filter(([, server]) => server.enabled !== false);
  const usable = enabled.filter(([, server]) => (server.command ?? "") !== "");
  for (const [name, server] of enabled) {
    if ((server.command ?? "") === "" && (server.url ?? "") !== "") {
      process.stderr.write(
        `pi-ui-bridge: MCP server ${name} is remote (${server.url}); this version connects to stdio servers only\n`,
      );
    }
  }
  return usable
    .sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0))
    .map(([name, server]) => ({ name, server }));
}

/** One JSON-RPC message. */
interface JsonRpcMessage {
  readonly jsonrpc?: string;
  readonly id?: number | string;
  readonly method?: string;
  readonly params?: unknown;
  readonly result?: unknown;
  readonly error?: { readonly code?: number; readonly message?: string };
}

/**
 * A stdio MCP connection: JSON-RPC 2.0 over the child's stdin/stdout, one line per
 * message, with an id-keyed promise per request.
 *
 * It is deliberately small and synchronous in shape: a request returns a promise that
 * settles on the matching response, a notification is written and forgotten, and a
 * server that dies rejects everything in flight instead of hanging a tool call.
 */
export class McpConnection {
  /** The server's name, as the configuration lists it. */
  readonly name: string;

  private child?: ChildProcessWithoutNullStreams;
  private readonly pending = new Map<number, { resolve: (value: unknown) => void; reject: (error: Error) => void; timer: NodeJS.Timeout }>();
  private nextId = 1;
  private closed = false;
  private readonly onStderr: (line: string) => void;
  private readonly spawnFn: typeof spawn;

  /**
   * The fields are assigned explicitly and not through constructor parameter
   * properties: this file is executed by pi's loader and by the Node test runner, and
   * the latter strips types without transforming them, where a parameter property is a
   * syntax error. Plain fields work everywhere.
   */
  constructor(name: string, onStderr: (line: string) => void, spawnFn: typeof spawn = spawn) {
    this.name = name;
    this.onStderr = onStderr;
    this.spawnFn = spawnFn;
  }

  /**
   * Starts the child process of one configured server.
   *
   * It is separate from the constructor because the process is what may fail: a
   * connection exists first, then starts, so its owner can report a failure and close it
   * without a half-constructed object.
   */
  start(server: McpServer): void {
    if (this.child !== undefined) {
      throw new Error(`MCP server ${this.name} is already started`);
    }
    const child = this.spawnFn(server.command ?? "", [...(server.args ?? [])], {
      env: { ...process.env, ...(server.env ?? {}) },
      stdio: ["pipe", "pipe", "pipe"],
    }) as ChildProcessWithoutNullStreams;
    this.child = child;

    let buffer = "";
    child.stdout.setEncoding("utf8");
    child.stdout.on("data", (chunk: string) => {
      buffer += chunk;
      let newline = buffer.indexOf("\n");
      while (newline >= 0) {
        const line = buffer.slice(0, newline).trim();
        buffer = buffer.slice(newline + 1);
        if (line !== "") {
          this.handleLine(line);
        }
        newline = buffer.indexOf("\n");
      }
    });
    child.stderr.setEncoding("utf8");
    child.stderr.on("data", (chunk: string) => {
      for (const line of chunk.split("\n")) {
        if (line.trim() !== "") {
          this.onStderr(line.trimEnd());
        }
      }
    });
    child.on("error", (error) => this.fail(new Error(`${error.message}`)));
    child.on("exit", (code) => this.fail(new Error(`MCP server ${this.name} exited (${code ?? "signal"})`)));
  }

  /** Performs the handshake and returns the server's tools. */
  async initialize(): Promise<McpTool[]> {
    await this.request("initialize", {
      protocolVersion: PROTOCOL_VERSION,
      capabilities: {},
      clientInfo: { name: "pi-ui-bridge", version: "0.1.0" },
    });
    this.notify("notifications/initialized", {});
    const result = await this.request("tools/list", {});
    const tools = (result as { tools?: readonly McpTool[] }).tools ?? [];
    return [...tools];
  }

  /** Calls one tool and returns its content blocks. */
  async callTool(name: string, args: unknown): Promise<McpCallResult> {
    const result = await this.request("tools/call", { name, arguments: args ?? {} });
    return result as McpCallResult;
  }

  /** Sends a request and waits for its response. */
  request(method: string, params: unknown): Promise<unknown> {
    if (this.closed) {
      return Promise.reject(new Error(`MCP server ${this.name} is gone`));
    }
    const id = this.nextId++;
    return new Promise<unknown>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`MCP server ${this.name} did not answer ${method} in ${REQUEST_TIMEOUT_MS}ms`));
      }, REQUEST_TIMEOUT_MS);
      timer.unref?.();
      this.pending.set(id, { resolve, reject, timer });
      this.write({ jsonrpc: "2.0", id, method, params });
    });
  }

  /** Sends a notification: no id, no answer. */
  notify(method: string, params: unknown): void {
    if (!this.closed) {
      this.write({ jsonrpc: "2.0", method, params });
    }
  }

  /** Closes the connection: SIGTERM, then SIGKILL, then the pipes. Idempotent. */
  async close(): Promise<void> {
    if (this.closed) {
      return;
    }
    this.closed = true;
    const child = this.child;
    if (child !== undefined) {
      const exited = new Promise<void>((resolve) => {
        if (child.exitCode !== null || child.signalCode !== null) {
          resolve();
          return;
        }
        child.once("exit", () => resolve());
      });
      child.kill("SIGTERM");
      const killed = new Promise<void>((resolve) => {
        const timer = setTimeout(() => {
          child.kill("SIGKILL");
          resolve();
        }, SHUTDOWN_GRACE_MS);
        timer.unref?.();
        void exited.then(() => {
          clearTimeout(timer);
          resolve();
        });
      });
      await Promise.race([exited, killed]);
    }
    this.fail(new Error(`MCP server ${this.name} was closed`));
  }

  /** Writes one message. */
  private write(message: JsonRpcMessage): void {
    this.child?.stdin.write(`${JSON.stringify(message)}\n`);
  }

  /** Routes one inbound line: a response resolves a pending request. */
  private handleLine(line: string): void {
    let message: JsonRpcMessage;
    try {
      message = JSON.parse(line) as JsonRpcMessage;
    } catch {
      this.onStderr(`pi-ui-bridge: MCP server ${this.name} wrote a line that is not JSON`);
      return;
    }
    if (message.id === undefined) {
      return;
    }
    const pending = this.pending.get(Number(message.id));
    if (pending === undefined) {
      return;
    }
    this.pending.delete(Number(message.id));
    clearTimeout(pending.timer);
    if (message.error !== undefined) {
      pending.reject(new Error(message.error.message ?? `MCP error ${message.error.code ?? "?"}`));
      return;
    }
    pending.resolve(message.result);
  }

  /** Rejects everything in flight: the server is gone. */
  private fail(error: Error): void {
    for (const [, pending] of this.pending) {
      clearTimeout(pending.timer);
      pending.reject(error);
    }
    this.pending.clear();
  }
}

/**
 * Maps one MCP result onto what pi expects from a tool.
 *
 * A result the MCP server marked as an error is thrown instead of returned: pi's tool
 * contract is "throw on error, encode recovery in the content", and a thrown error is
 * what the model reads as a failed call.
 */
export function toolResult(result: McpCallResult): AgentToolResult<undefined> {
  const content = (result.content ?? []).map((block) => {
    if (block.type === "text") {
      return { type: "text" as const, text: block.text ?? "" };
    }
    if (block.type === "image" && block.data !== undefined) {
      return { type: "image" as const, data: block.data, mimeType: block.mimeType ?? "image/png" };
    }
    // A resource link or an embedded resource: shown as its JSON, because a model can
    // read that even when this client cannot render it.
    return { type: "text" as const, text: JSON.stringify(block) };
  });
  if (result.isError === true) {
    const reason = content.map((block) => ("text" in block ? block.text : "[image]")).join("\n");
    throw new Error(reason === "" ? "the MCP tool failed" : reason);
  }
  return { content, details: undefined };
}

/**
 * Connects every configured stdio server and registers its tools with pi.
 *
 * A server that fails to start or to list tools is reported on stderr and skipped: one
 * broken MCP server must not stop a session from starting.
 */
export async function registerMcpServers(
  pi: ExtensionAPI,
  document: McpDocument,
  stderr: (line: string) => void = (line) => process.stderr.write(`${line}\n`),
): Promise<McpConnection[]> {
  const connections: McpConnection[] = [];
  for (const { name, server } of stdioServers(document)) {
    const connection = new McpConnection(name, stderr);
    try {
      connection.start(server);
      const tools = await connection.initialize();
      for (const tool of tools) {
        pi.registerTool(mcpToolDefinition(connection, tool));
      }
      connections.push(connection);
      stderr(`pi-ui-bridge: MCP server ${name} offers ${tools.length} tool(s)`);
    } catch (error) {
      stderr(`pi-ui-bridge: MCP server ${name} is unavailable: ${String(error)}`);
      await connection.close();
    }
  }
  return connections;
}

/** The pi tool definition of one MCP tool. */
export function mcpToolDefinition(
  connection: McpConnection,
  tool: McpTool,
): ToolDefinition<TSchema, unknown> {
  return {
    name: `${TOOL_PREFIX}_${connection.name}_${tool.name}`,
    label: `${connection.name}: ${tool.name}`,
    description: tool.description ?? `MCP tool ${tool.name} of ${connection.name}`,
    // The schema comes from the server, so it is wrapped rather than rebuilt: an MCP
    // server may use any JSON Schema keyword and rebuilding it would lose one.
    parameters: Unsafe<Record<string, unknown>>(
      (tool.inputSchema as Record<string, unknown> | undefined) ?? {
        type: "object",
        properties: {},
      },
    ),
    async execute(
      _toolCallId: string,
      params: unknown,
      _signal: AbortSignal | undefined,
      _onUpdate: unknown,
      _ctx: ExtensionContext,
    ): Promise<AgentToolResult<undefined>> {
      // The failure travels as a thrown error (pi's contract), so the reason — a dead
      // server, a timeout, a tool that reported an error — reaches the model as one.
      return toolResult(await connection.callTool(tool.name, params));
    },
  };
}
