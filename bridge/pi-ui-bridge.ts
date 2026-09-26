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
  return sessionId === undefined ? { approvals } : { sessionId, approvals };
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

  pi.on("session_start", (_event, ctx: ExtensionContext) => {
    ctx.ui.notify(READY_NOTIFY, "info");
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