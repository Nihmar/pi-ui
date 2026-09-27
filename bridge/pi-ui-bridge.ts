/**
 * pi-ui-bridge — extension loaded into every `pi --mode rpc` child by pi-ui.
 *
 * Role: exercise the RPC extension UI subprotocol (fire-and-forget `notify` plus
 * answerable `confirm`) and gate `bash` tool calls on configured patterns.
 * Contract: docs/spike-interfaces.md §10 (behaviour) and §5.7 (`PI_UI_BRIDGE_CONFIG`).
 *
 * The TypeScript source is the artifact — pi executes it through jiti, no build step.
 * Diagnostics go to stderr only: stdout is reserved for the RPC protocol stream.
 */

import { readFileSync } from "node:fs";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";

import { readMcpDocument, registerMcpServers, type McpToolSource } from "./mcp.ts";
import {
  GOAL_HELP,
  nextRound,
  parseGoalCommand,
  roundPrompt,
  startGoal,
  statusLine,
  stopGoal,
  type GoalState,
} from "./goal.ts";

/** Exact strings relied on by the fixtures and the WS `ext.notify` tests. */
const READY_NOTIFY = "pi-ui-bridge ready";
const CONFIRM_TITLE = "pi-ui-bridge: confirm command";
const BLOCKED_NOTIFY = "Command blocked by pi-ui-bridge";
const BLOCK_REASON = "blocked by pi-ui-bridge";

/** Defaults from docs/spike-interfaces.md §5.7, used when the config omits them. */
const DEFAULT_PATTERNS = ["rm -rf", "git push --force", "sudo"];

type ApprovalsMode = "confirm" | "off";

interface Approvals {
  readonly mode: ApprovalsMode;
  readonly patterns: readonly string[];
}

interface BridgeConfig {
  readonly sessionId?: string;
  readonly approvals: Approvals;
  /**
   * Path of the MCP configuration the server maintains (`GET/PUT /api/v1/mcp`).
   *
   * A path and not the configuration: one document serves every session, and a change
   * takes effect at the next spawn.
   */
  readonly mcpConfig?: string;
  /**
   * Goal mode: a long-running objective the session keeps working on, round by round.
   *
   * Off unless the configuration turns it on (PLAN.md E14: a feature flag, because a
   * loop that drives itself is not what every deployment wants).
   */
  readonly goal?: { readonly maxRounds?: number };
}

const OFF: Approvals = { mode: "off", patterns: DEFAULT_PATTERNS };

function diagnostic(detail: string): void {
  console.error(`pi-ui-bridge: ${detail}`);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/**
 * True only for arrays whose every entry is a string. `Array.isArray` narrows to
 * `any[]`, so the entries are re-typed as `unknown` before the guard proves each
 * one: no entry — and no array element — escapes as `any`.
 */
function isStringArray(value: unknown): value is readonly string[] {
  if (!Array.isArray(value)) return false;
  const entries: readonly unknown[] = value;
  return entries.every((entry): entry is string => typeof entry === "string");
}

function offConfig(reason: string): BridgeConfig {
  diagnostic(reason);
  return { approvals: OFF };
}

/**
 * Validate the `approvals` block. A missing block or missing fields are valid and
 * take the defaults; a present-but-wrong type is a bad shape and degrades to `off`.
 */
function readApprovals(value: unknown, source: string): Approvals | undefined {
  if (value === undefined) return { mode: "confirm", patterns: DEFAULT_PATTERNS };
  if (!isRecord(value)) {
    diagnostic(`config ${source}: "approvals" is not an object`);
    return undefined;
  }

  const mode = value.mode;
  if (mode !== undefined && mode !== "confirm" && mode !== "off") {
    diagnostic(`config ${source}: "approvals.mode" must be "confirm" or "off"`);
    return undefined;
  }

  const patterns = value.patterns;
  if (patterns === undefined) {
    return { mode: mode ?? "confirm", patterns: DEFAULT_PATTERNS };
  }
  if (!isStringArray(patterns)) {
    diagnostic(`config ${source}: "approvals.patterns" must be an array of strings`);
    return undefined;
  }
  return { mode: mode ?? "confirm", patterns };
}

/**
 * Read `PI_UI_BRIDGE_CONFIG` defensively: a missing variable, an unreadable file,
 * invalid JSON or a bad shape all degrade to `off` (startup notify still happens).
 */
function readConfig(): BridgeConfig {
  const path = process.env.PI_UI_BRIDGE_CONFIG;
  if (path === undefined || path.trim() === "") {
    return offConfig("PI_UI_BRIDGE_CONFIG is not set");
  }

  let text: string;
  try {
    text = readFileSync(path, "utf8");
  } catch (error) {
    return offConfig(`cannot read config ${path} (${describe(error)})`);
  }

  // `JSON.parse` is typed `any`; narrowing it to `unknown` keeps every later read checked.
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch (error) {
    return offConfig(`invalid JSON in config ${path} (${describe(error)})`);
  }
  if (!isRecord(parsed)) {
    return offConfig(`config ${path} is not a JSON object`);
  }

  const approvals = readApprovals(parsed.approvals, path);
  if (approvals === undefined) return { approvals: OFF };

  const sessionId = parsed.sessionId;
  if (sessionId !== undefined && typeof sessionId !== "string") {
    return offConfig(`config ${path}: "sessionId" must be a string`);
  }
  const mcpConfig = parsed.mcpConfig;
  if (mcpConfig !== undefined && typeof mcpConfig !== "string") {
    return offConfig(`config ${path}: "mcpConfig" must be a string`);
  }
  const goal = readGoal(parsed.goal, path);
  return {
    approvals,
    ...(sessionId === undefined ? {} : { sessionId }),
    ...(mcpConfig === undefined || mcpConfig.trim() === "" ? {} : { mcpConfig }),
    ...(goal === undefined ? {} : { goal }),
  };
}

/**
 * Read the `goal` block: present means enabled.
 *
 * A wrong shape is not a reason to degrade the whole bridge — approvals and MCP still
 * work — so it is reported and the block is dropped (goal mode off).
 */
function readGoal(value: unknown, source: string): { maxRounds?: number } | undefined {
  if (value === undefined) return undefined;
  if (!isRecord(value)) {
    diagnostic(`config ${source}: "goal" is not an object; goal mode is off`);
    return undefined;
  }
  const maxRounds = value.maxRounds;
  if (maxRounds !== undefined && typeof maxRounds !== "number") {
    diagnostic(`config ${source}: "goal.maxRounds" must be a number; goal mode is off`);
    return undefined;
  }
  return maxRounds === undefined ? {} : { maxRounds };
}

/** The text of the last assistant message of a turn, for the goal driver. */
function lastAssistantText(messages: readonly unknown[]): string {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const message = messages[index];
    if (!isRecord(message) || message.role !== "assistant") continue;
    const content = message.content;
    if (typeof content === "string") return content;
    if (!Array.isArray(content)) continue;
    const texts: string[] = [];
    for (const block of content as readonly unknown[]) {
      if (isRecord(block) && block.type === "text" && typeof block.text === "string") {
        texts.push(block.text);
      }
    }
    if (texts.length > 0) return texts.join("\n");
  }
  return "";
}

function describe(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/** The bash command, or `undefined` when the input carries no usable string. */
function readCommand(input: unknown): string | undefined {
  if (!isRecord(input)) return undefined;
  const command = input.command;
  return typeof command === "string" ? command : undefined;
}

/**
 * Match by case-insensitive substring — deliberately not regex: patterns come from
 * config, and a substring cannot fail to compile or backtrack.
 */
function matchPattern(command: string, patterns: readonly string[]): string | undefined {
  const haystack = command.toLowerCase();
  for (const pattern of patterns) {
    const needle = pattern.toLowerCase();
    if (needle !== "" && haystack.includes(needle)) return pattern;
  }
  return undefined;
}

export default function (pi: ExtensionAPI): void {
  const config: BridgeConfig = readConfig();
  const session = config.sessionId === undefined ? "" : `, session ${config.sessionId}`;
  diagnostic(`approvals mode: ${config.approvals.mode}${session}`);

  // The MCP servers are connected once per child, and their tools are registered before
  // the first turn: `pi.registerTool` in an async handler is what the extension API
  // documents as "close session-scoped resources from an idempotent session_shutdown
  // handler", which is the other half of this pair.
  let mcpConnections: readonly McpToolSource[] = [];

  pi.on("session_start", (_event, ctx: ExtensionContext) => {
    ctx.ui.notify(READY_NOTIFY, "info");
    if (config.mcpConfig === undefined) return;
    const document = readMcpDocument(config.mcpConfig);
    void registerMcpServers(pi, document).then(
      (connections) => {
        mcpConnections = connections;
      },
      (error: unknown) => diagnostic(`MCP setup failed: ${describe(error)}`),
    );
  });

  // Goal mode: one objective at a time, driven by the pure state machine in goal.ts.
  let goal: GoalState | undefined;
  let lastTurnText = "";

  const publishGoal = (ctx: ExtensionContext): void => {
    if (goal === undefined) {
      ctx.ui.setStatus("goal", undefined);
      return;
    }
    const line = statusLine(goal);
    ctx.ui.setStatus("goal", line);
    // The entry is what the app renders on the timeline: a status line with the goal's
    // state, appended once per transition rather than per round.
    pi.appendEntry("goal", goal);
    diagnostic(`goal: ${line}`);
  };

  if (config.goal !== undefined) {
    pi.registerCommand("goal", {
      description: "Work on an objective round by round (start, status, stop)",
      handler: async (args: string, ctx) => {
        const command = parseGoalCommand(`/goal ${args}`.trim());
        if (command === undefined || command.action === "help") {
          ctx.ui.notify(GOAL_HELP, "info");
          return;
        }
        if (command.action === "status") {
          ctx.ui.notify(goal === undefined ? "no goal is running" : statusLine(goal), "info");
          return;
        }
        if (command.action === "stop") {
          if (goal === undefined || !goal.active) {
            ctx.ui.notify("no goal is running", "info");
            return;
          }
          goal = stopGoal(goal);
          publishGoal(ctx);
          ctx.ui.notify(statusLine(goal), "info");
          return;
        }

        goal = startGoal(command.objective, config.goal?.maxRounds);
        publishGoal(ctx);
        ctx.ui.notify(statusLine(goal), "info");
        pi.sendUserMessage(roundPrompt(goal));
      },
    });
  }

  // The turn's text is kept so the driver can see the goal marker: `agent_settled`
  // carries no messages, `agent_end` does.
  pi.on("agent_end", (event) => {
    lastTurnText = lastAssistantText(event.messages);
  });

  pi.on("agent_settled", (_event, ctx: ExtensionContext) => {
    if (goal === undefined || !goal.active) return;
    const decision = nextRound(goal, lastTurnText);
    goal = decision.state;
    publishGoal(ctx);
    if (decision.prompt !== undefined) {
      // A user message, not a custom one: a round must reach the model as a request.
      pi.sendUserMessage(decision.prompt);
    }
  });

  pi.on("session_shutdown", async () => {
    // Idempotent on purpose: cancellation, a session replacement and process exit can
    // all converge here, and a connection that is already closed is a no-op.
    const closing = mcpConnections;
    mcpConnections = [];
    await Promise.all(closing.map((connection) => connection.close()));
  });

  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName !== "bash") return undefined;

    const command = readCommand(event.input);
    if (command === undefined) return undefined;
    if (config.approvals.mode !== "confirm") return undefined;

    const pattern = matchPattern(command, config.approvals.patterns);
    if (pattern === undefined || !ctx.hasUI) return undefined;

    const confirmed = await ctx.ui.confirm(
      CONFIRM_TITLE,
      `pi-ui-bridge matched the pattern "${pattern}".\n\n${command}`,
    );
    if (confirmed) return undefined;

    ctx.ui.notify(BLOCKED_NOTIFY, "warning");
    return { block: true, reason: BLOCK_REASON };
  });
}