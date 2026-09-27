/**
 * The remote half of the MCP client: servers reached over HTTP instead of a child process.
 *
 * The transport is the specification's "Streamable HTTP": every JSON-RPC message is a POST
 * to the server's URL, and the answer arrives either as one JSON object or as a
 * `text/event-stream` carrying the same objects as `data:` frames. A server that hands out a
 * session id during `initialize` gets it back on every later request, and a notification has
 * no answer to wait for.
 *
 * What it does not do: the optional GET stream for server-initiated messages. A tool call is
 * a request and its answer; sampling or logging pushed by the server are features this
 * bridge does not consume, and pretending otherwise would mean holding an idle connection
 * per server open forever.
 */

import type { McpCallResult, McpServer, McpTool, McpToolSource } from "./mcp.ts";

/** How long one request may take. */
const REQUEST_TIMEOUT_MS = 60_000;

/** One server-sent event, as far as this client reads them. */
interface SseEvent {
  readonly event?: string;
  readonly data: string;
}

/**
 * Splits a `text/event-stream` body into its events.
 *
 * Events are separated by a blank line; `data:` lines of one event are joined with newlines;
 * every other field (`event`, `id`, `retry`) is read and mostly ignored, because the
 * specification puts the payload in `data` and a client that needs the rest can add it.
 */
export function parseSse(body: string): SseEvent[] {
  const events: SseEvent[] = [];
  for (const block of body.split(/\r?\n\r?\n/)) {
    const data: string[] = [];
    let name: string | undefined;
    for (const line of block.split(/\r?\n/)) {
      if (line.startsWith(":")) {
        continue;
      }
      if (line.startsWith("event:")) {
        name = line.slice("event:".length).trim();
        continue;
      }
      if (line.startsWith("data:")) {
        data.push(line.slice("data:".length).replace(/^ /, ""));
      }
    }
    if (data.length > 0) {
      events.push(name === undefined ? { data: data.join("\n") } : { event: name, data: data.join("\n") });
    }
  }
  return events;
}

/**
 * A JSON-RPC connection over HTTP.
 *
 * One instance talks to one server: it holds the session id, counts its request ids and
 * aborts whatever is in flight when it is closed. A response carrying an `error` rejects the
 * caller's promise, which is what `registerMcpServers` reports and skips.
 */
export class McpHttpConnection implements McpToolSource {
  readonly name: string;

  private readonly url: string;
  private readonly headers: Readonly<Record<string, string>>;
  private readonly onStderr: (line: string) => void;
  private readonly fetchFn: typeof fetch;
  private sessionId: string | undefined;
  private nextId = 1;
  private closed = false;

  constructor(
    name: string,
    url: string,
    headers: Readonly<Record<string, string>> | undefined,
    onStderr: (line: string) => void,
    fetchFn: typeof fetch = fetch,
  ) {
    this.name = name;
    this.url = url;
    this.headers = headers ?? {};
    this.onStderr = onStderr;
    this.fetchFn = fetchFn;
  }

  /** Performs the handshake and returns the server's tools. */
  async initialize(): Promise<McpTool[]> {
    await this.request("initialize", {
      protocolVersion: "2024-11-05",
      capabilities: {},
      clientInfo: { name: "pi-ui-bridge", version: "0.1.0" },
    });
    await this.notify("notifications/initialized", {});
    const result = await this.request("tools/list", {});
    const tools = (result as { tools?: readonly McpTool[] }).tools ?? [];
    return [...tools];
  }

  /** Calls one tool. */
  async callTool(name: string, args: unknown): Promise<McpCallResult> {
    const result = await this.request("tools/call", { name, arguments: args ?? {} });
    return result as McpCallResult;
  }

  /** Releases the session, when the server asked for one. Idempotent. */
  async close(): Promise<void> {
    if (this.closed) {
      return;
    }
    this.closed = true;
    if (this.sessionId === undefined) {
      return;
    }
    try {
      // The specification's DELETE ends the session; a server that does not implement it
      // answers 405 and that is not this client's problem.
      await this.fetchFn(this.url, {
        method: "DELETE",
        headers: this.requestHeaders(),
      });
    } catch (error) {
      this.onStderr(`pi-ui-bridge: closing MCP server ${this.name}: ${String(error)}`);
    }
  }

  /** Sends a request and waits for its answer. */
  private async request(method: string, params: unknown): Promise<unknown> {
    const id = this.nextId++;
    const message = { jsonrpc: "2.0", id, method, params };
    const answer = await this.post(message);
    if (answer === undefined) {
      throw new Error(`MCP server ${this.name} did not answer ${method}`);
    }
    const error = (answer as { error?: { message?: string } }).error;
    if (error !== undefined) {
      throw new Error(error.message ?? `MCP error in ${method}`);
    }
    return (answer as { result?: unknown }).result;
  }

  /** Sends a notification: no id, no answer. */
  private async notify(method: string, params: unknown): Promise<void> {
    await this.post({ jsonrpc: "2.0", method, params });
  }

  /** One POST, returning the response object it carried (a notification has none). */
  private async post(message: unknown): Promise<unknown> {
    if (this.closed) {
      throw new Error(`MCP server ${this.name} is closed`);
    }
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
    timer.unref?.();
    let response: Response;
    try {
      response = await this.fetchFn(this.url, {
        method: "POST",
        headers: {
          ...this.requestHeaders(),
          "content-type": "application/json",
          // Both are accepted on purpose: the server decides whether this answer is one
          // object or a stream.
          accept: "application/json, text/event-stream",
        },
        body: JSON.stringify(message),
        signal: controller.signal,
      });
    } finally {
      clearTimeout(timer);
    }

    const session = response.headers.get("mcp-session-id");
    if (session !== null && session !== "") {
      this.sessionId = session;
    }
    if (!response.ok) {
      throw new Error(`MCP server ${this.name} answered ${response.status}`);
    }
    if (response.status === 202) {
      return undefined; // a notification was accepted
    }
    const contentType = response.headers.get("content-type") ?? "";
    const body = await response.text();
    if (contentType.includes("text/event-stream")) {
      // A stream may carry notifications before the answer: the one with the matching id is
      // this request's.
      const wanted = (message as { id?: number }).id;
      for (const event of parseSse(body)) {
        let decoded: unknown;
        try {
          decoded = JSON.parse(event.data);
        } catch {
          this.onStderr(`pi-ui-bridge: MCP server ${this.name} sent a frame that is not JSON`);
          continue;
        }
        if (wanted === undefined || (decoded as { id?: number }).id === wanted) {
          return decoded;
        }
      }
      return undefined;
    }
    if (body.trim() === "") {
      return undefined;
    }
    return JSON.parse(body);
  }

  /** The headers every request carries: the configuration's, plus the session. */
  private requestHeaders(): Record<string, string> {
    return {
      ...this.headers,
      ...(this.sessionId === undefined ? {} : { "mcp-session-id": this.sessionId }),
    };
  }
}
