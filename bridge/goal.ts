/**
 * pi-ui-bridge — goal mode: a long-running objective the session keeps working on.
 *
 * The loop is a round driver: after every turn pi settles, the driver decides whether
 * another round is due and, if so, sends the next one as a user message. The decision is
 * deliberately here and not in the model's hands alone:
 *
 *   - the round budget is the server's (`maxRounds`), so a goal cannot loop forever;
 *   - every round starts with a review prompt that asks for the goal state, so the model
 *     works from "where are we" instead of re-reading the objective;
 *   - `goal stop` (or the budget) ends it, and the state is published as a custom entry
 *     so the app can show it without asking.
 *
 * This module is pure on purpose: no pi, no clock, no I/O. `pi-ui-bridge.ts` is the thin
 * adapter that feeds it events, and `test/goal.test.ts` drives it directly.
 */

/** What a goal is doing right now. */
export interface GoalState {
  /** The objective, verbatim from the user. */
  readonly objective: string;
  /** Rounds already started, 1-based while active. */
  readonly round: number;
  /** How many rounds this goal may start. */
  readonly maxRounds: number;
  /** False once the goal finished, was stopped or ran out of rounds. */
  readonly active: boolean;
  /** Why it is no longer active: `done`, `stopped` or `budget`. */
  readonly endedBy?: "done" | "stopped" | "budget";
}

/** What a `/goal` invocation asked for. */
export type GoalCommand =
  | { readonly action: "start"; readonly objective: string }
  | { readonly action: "stop" }
  | { readonly action: "status" }
  | { readonly action: "help" };

/** The defaults a deployment gets when its configuration says nothing. */
export const DEFAULT_MAX_ROUNDS = 10;

/** The largest budget a configuration may ask for: a runaway loop is not a goal. */
export const HARD_MAX_ROUNDS = 50;

/**
 * Parses one `/goal` command line.
 *
 * `undefined` means the line is not a goal command at all (the caller leaves it alone).
 * A bare `/goal` and `/goal help` are help, so a typo or a question never starts a loop:
 * the only way to start one is to write `start` with an objective.
 */
export function parseGoalCommand(input: unknown): GoalCommand | undefined {
  if (typeof input !== "string") return undefined;
  const trimmed = input.trim();
  if (!trimmed.startsWith("/goal")) return undefined;
  const rest = trimmed.slice("/goal".length).trim();
  if (rest === "" || rest === "help" || rest === "?") return { action: "help" };
  if (rest === "status") return { action: "status" };
  if (rest === "stop") return { action: "stop" };

  const startMatch = /^start\b\s*([\s\S]*)$/.exec(rest);
  if (startMatch === null) return { action: "help" };
  const objective = (startMatch[1] ?? "").trim();
  if (objective === "") return { action: "help" };
  return { action: "start", objective };
}

/** Starts a goal, clamping the budget into what a server may ask for. */
export function startGoal(objective: string, maxRounds: number = DEFAULT_MAX_ROUNDS): GoalState {
  return {
    objective: objective.trim(),
    round: 1,
    maxRounds: clampRounds(maxRounds),
    active: true,
  };
}

/** Ends a goal because somebody asked. */
export function stopGoal(state: GoalState): GoalState {
  return { ...state, active: false, endedBy: "stopped" };
}

/** Ends a goal because the model reported it done. */
export function finishGoal(state: GoalState): GoalState {
  return { ...state, active: false, endedBy: "done" };
}

/**
 * The prompt of one round.
 *
 * It carries the objective, the round number and the review instruction, and it asks for
 * a machine-readable last line so the driver can tell "done" from "keep going" without a
 * second model. A goal that cannot be judged this way still terminates: the budget ends it.
 */
export function roundPrompt(state: GoalState): string {
  return [
    `Goal (round ${state.round} of ${state.maxRounds}): ${state.objective}`,
    "",
    "First review the work already done towards this goal: what is finished, what is",
    "missing, what you are about to do next. Then continue the work with the next",
    "concrete step.",
  ].join("\n");
}

/** The marker a round may end with to say the goal is reached. */
export const GOAL_DONE_MARKER = "GOAL_DONE";

/**
 * Decides whether another round is due.
 *
 * A round that ends with the done marker finishes the goal; otherwise the next round
 * starts until the budget is spent. The result is the state to store and the prompt to
 * send, or just the state when nothing more should happen.
 */
export function nextRound(
  state: GoalState,
  settledOutput: string,
): { readonly state: GoalState; readonly prompt?: string } {
  if (!state.active) return { state };
  if (settledOutput.includes(GOAL_DONE_MARKER)) {
    return { state: finishGoal(state) };
  }
  if (state.round >= state.maxRounds) {
    return { state: { ...state, active: false, endedBy: "budget" } };
  }
  const next: GoalState = { ...state, round: state.round + 1 };
  return { state: next, prompt: roundPrompt(next) };
}

/** The one-line status the app shows (`setStatus` and the goal entry). */
export function statusLine(state: GoalState): string {
  if (state.active) {
    return `goal ${state.round}/${state.maxRounds}: ${shorten(state.objective)}`;
  }
  const reason = state.endedBy === "budget" ? "rounds spent" : (state.endedBy ?? "finished");
  return `goal ${reason} after ${state.round} round(s): ${shorten(state.objective)}`;
}

/** The help text a user gets: the commands are the whole surface. */
export const GOAL_HELP = [
  "/goal start <objective> — work on an objective, round by round",
  "/goal status — what the current goal is doing",
  "/goal stop — end it",
].join("\n");

/** Clamps a configured budget into the allowed range. */
function clampRounds(maxRounds: number): number {
  if (!Number.isFinite(maxRounds) || maxRounds < 1) return 1;
  return Math.min(Math.floor(maxRounds), HARD_MAX_ROUNDS);
}

/** Keeps a status line short enough for a UI bar. */
function shorten(text: string, limit = 80): string {
  const flat = text.replace(/\s+/g, " ").trim();
  return flat.length <= limit ? flat : `${flat.slice(0, limit - 1)}…`;
}
