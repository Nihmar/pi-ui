/**
 * Tests of the goal driver: the state machine that decides when a long-running objective
 * gets another round, when it is done, and when the budget ends it.
 *
 * It is pure, so the whole loop is testable without a model, a session or a clock — which
 * is the point of keeping it out of `pi-ui-bridge.ts`.
 */

import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  DEFAULT_MAX_ROUNDS,
  GOAL_DONE_MARKER,
  HARD_MAX_ROUNDS,
  finishGoal,
  nextRound,
  parseGoalCommand,
  roundPrompt,
  startGoal,
  statusLine,
  stopGoal,
} from "../goal.ts";

test("a goal command is parsed only when it is one", () => {
  assert.deepEqual(parseGoalCommand("/goal start ship the file browser"), {
    action: "start",
    objective: "ship the file browser",
  });
  assert.deepEqual(parseGoalCommand("  /goal stop  "), { action: "stop" });
  assert.deepEqual(parseGoalCommand("/goal status"), { action: "status" });
  assert.deepEqual(parseGoalCommand("/goal"), { action: "help" });
  assert.deepEqual(parseGoalCommand("/goal help"), { action: "help" });
  // A typo or a missing objective is help, never a loop that starts by accident.
  assert.deepEqual(parseGoalCommand("/goal startt do something"), { action: "help" });
  assert.deepEqual(parseGoalCommand("/goal start   "), { action: "help" });
  assert.equal(parseGoalCommand("just a message"), undefined);
  assert.equal(parseGoalCommand(undefined), undefined);
  assert.equal(parseGoalCommand(42), undefined);
});

test("a round prompt carries the objective, the round and the review", () => {
  const state = startGoal("green the tests", 3);
  const prompt = roundPrompt(state);
  assert.match(prompt, /green the tests/);
  assert.match(prompt, /round 1 of 3/);
  assert.match(prompt, /review/i);
});

test("the marker ends a goal, the budget ends a runaway", () => {
  const state = startGoal("green the tests", 2);

  const first = nextRound(state, "still working");
  assert.equal(first.state.active, true);
  assert.equal(first.state.round, 2);
  assert.ok(first.prompt, "a second round is due");

  const done = nextRound(first.state, `all tests pass\n${GOAL_DONE_MARKER}`);
  assert.equal(done.state.active, false);
  assert.equal(done.state.endedBy, "done");
  assert.equal(done.prompt, undefined);

  const spent = nextRound(first.state, "still working");
  assert.equal(spent.state.active, false);
  assert.equal(spent.state.endedBy, "budget");
  assert.equal(spent.prompt, undefined);
});

test("an inactive goal never starts another round", () => {
  const stopped = stopGoal(startGoal("x", 5));
  assert.deepEqual(nextRound(stopped, "anything"), { state: stopped });

  const finished = finishGoal(startGoal("x", 5));
  assert.deepEqual(nextRound(finished, "anything"), { state: finished });
});

test("the budget is clamped on both ends", () => {
  assert.equal(startGoal("x", 0).maxRounds, 1);
  assert.equal(startGoal("x", -3).maxRounds, 1);
  assert.equal(startGoal("x", 999).maxRounds, HARD_MAX_ROUNDS);
  assert.equal(startGoal("x").maxRounds, DEFAULT_MAX_ROUNDS);
  assert.equal(startGoal("x", 2.9).maxRounds, 2);
});

test("the status line says what a bar needs", () => {
  const active = startGoal("a very long objective ".repeat(10), 4);
  const line = statusLine(active);
  assert.match(line, /goal 1\/4: /);
  assert.ok(line.length <= 100, `too long: ${line}`);
  assert.match(statusLine(stopGoal(active)), /stopped/);
  assert.match(statusLine({ ...startGoal("x", 1), active: false, endedBy: "budget" }), /rounds spent/);
});
