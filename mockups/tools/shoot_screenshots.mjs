// Renders every mockup screen to a PNG over the Chrome DevTools Protocol.
//
// The mockup is the visual contract and a review often happens on a machine without a
// display: this turns mockups/screens/*.html into mockups/screenshots/<theme>/*.png, one
// image per screen and theme, cropped to the device frame with the mockup toolbar and the
// review notes left out. No dependency beyond Node 22 (built-in fetch and WebSocket) and a
// Chromium binary.
//
// Usage:
//   node mockups/tools/shoot_screenshots.mjs [--out DIR] [--themes dark,light]
//                                            [--scale 2] [--only 08-chat] [--chrome PATH]
//
// Chromium comes from --chrome, then $PIUI_CHROME, then the Playwright cache, then PATH.
import { spawn, spawnSync } from "node:child_process";
import { existsSync, readdirSync } from "node:fs";
import { mkdir, readFile, readdir, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import path from "node:path";

const MOCKUPS = path.resolve(import.meta.dirname, "..");
const SCREENS = path.join(MOCKUPS, "screens");

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

/** Reads `--flag value` pairs, keeping the defaults for whatever is absent. */
function options(argv) {
  const parsed = {
    out: path.join(MOCKUPS, "screenshots"),
    themes: ["dark", "light"],
    scale: 2,
    only: undefined,
    chrome: undefined,
    port: 9333,
  };
  for (let index = 0; index < argv.length; index += 2) {
    const [flag, value] = [argv[index], argv[index + 1]];
    if (flag === "--out") parsed.out = path.resolve(value);
    else if (flag === "--themes") parsed.themes = value.split(",");
    else if (flag === "--scale") parsed.scale = Number(value);
    else if (flag === "--only") parsed.only = value;
    else if (flag === "--chrome") parsed.chrome = path.resolve(value);
    else if (flag === "--port") parsed.port = Number(value);
    else throw new Error(`unknown flag ${flag}`);
  }
  return parsed;
}

/** The Chromium to drive: flag, environment, Playwright cache, then PATH. */
function findChrome(given) {
  if (given !== undefined) return given;
  if (process.env.PIUI_CHROME) return process.env.PIUI_CHROME;
  const cache = path.join(homedir(), ".cache", "ms-playwright");
  if (existsSync(cache)) {
    const builds = readdirSync(cache).filter((name) => name.startsWith("chromium-"));
    const binaries = [
      "chrome-linux64/chrome",
      "chrome-linux/chrome",
      "chrome-headless-shell-linux64/headless_shell",
    ];
    for (const build of builds) {
      for (const binary of binaries) {
        const candidate = path.join(cache, build, binary);
        if (existsSync(candidate)) return candidate;
      }
    }
  }
  const names = ["chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome"];
  for (const name of names) {
    if (spawnSync("which", [name], { stdio: "ignore" }).status === 0) return name;
  }
  throw new Error("no Chromium found: pass --chrome PATH or set $PIUI_CHROME");
}

/** Minimal CDP client over the browser-level WebSocket. */
class Cdp {
  constructor(ws) {
    this.ws = ws;
    this.nextId = 1;
    this.pending = new Map();
    ws.onmessage = (event) => this.#onMessage(event.data);
  }

  static async connect(url) {
    const ws = new WebSocket(url);
    await new Promise((resolve, reject) => {
      ws.onopen = resolve;
      ws.onerror = () => reject(new Error(`cannot open ${url}`));
    });
    return new Cdp(ws);
  }

  send(method, params = {}) {
    // sessionId is a sibling of method/params on the wire, not a parameter: inside params
    // the call reaches the browser target, which answers "wasn't found".
    const { sessionId, ...rest } = params;
    const id = this.nextId++;
    const answer = new Promise((resolve, reject) => this.pending.set(id, { resolve, reject }));
    const message = { id, method, params: rest };
    if (sessionId !== undefined) message.sessionId = sessionId;
    this.ws.send(JSON.stringify(message));
    return answer;
  }

  #onMessage(data) {
    const message = JSON.parse(data);
    if (message.id === undefined) return;
    const entry = this.pending.get(message.id);
    if (entry === undefined) return;
    this.pending.delete(message.id);
    if (message.error) entry.reject(new Error(message.error.message));
    else entry.resolve(message.result);
  }
}

async function browserSocket(port) {
  for (let attempt = 0; attempt < 80; attempt += 1) {
    try {
      const answer = await fetch(`http://127.0.0.1:${port}/json/version`);
      const body = await answer.json();
      if (body.webSocketDebuggerUrl) return body.webSocketDebuggerUrl;
    } catch {
      // the endpoint is not up yet
    }
    await sleep(250);
  }
  throw new Error("chrome did not expose a debugging endpoint");
}

/** Runs one expression in a session and returns its value. */
async function evaluate(cdp, sessionId, expression) {
  const { result } = await cdp.send("Runtime.evaluate", {
    expression,
    returnByValue: true,
    sessionId,
  });
  return result.value;
}

/** Waits for a session's document to be complete. */
async function waitForDocument(cdp, sessionId) {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    try {
      if ((await evaluate(cdp, sessionId, "document.readyState")) === "complete") return;
    } catch {
      // the target is still being created
    }
    await sleep(100);
  }
  throw new Error("the page never finished loading");
}

/** The device frame of one screen, once the theme is applied and the chrome is hidden. */
const frame = (theme) => `(() => {
  document.documentElement.setAttribute("data-theme", ${JSON.stringify(theme)});
  for (const node of document.querySelectorAll(".toolbar, .notes")) node.style.display = "none";
  const rect = document.querySelector(".device").getBoundingClientRect();
  return JSON.stringify({ x: rect.x, y: rect.y, width: rect.width, height: rect.height });
})()`;

async function main() {
  if (typeof WebSocket !== "function" || typeof fetch !== "function") {
    throw new Error("Node 22 or newer is required (built-in WebSocket and fetch)");
  }
  const config = options(process.argv.slice(2));
  const chrome = findChrome(config.chrome);
  const files = (await readdir(SCREENS))
    .filter((name) => name.endsWith(".html"))
    .filter((name) => config.only === undefined || name.startsWith(config.only))
    .sort();
  if (files.length === 0) throw new Error(`no screen matched ${config.only ?? "*"}`);

  const child = spawn(
    chrome,
    [
      "--headless=new",
      `--remote-debugging-port=${config.port}`,
      "--no-sandbox",
      "--disable-gpu",
      "--hide-scrollbars",
      `--user-data-dir=${path.join(homedir(), ".cache", "piui-screenshots-profile")}`,
      "about:blank",
    ],
    { stdio: "ignore" },
  );

  try {
    const cdp = await Cdp.connect(await browserSocket(config.port));
    for (const file of files) {
      const source = await readFile(path.join(SCREENS, file), "utf8");
      const device = /data-device="([a-z]+)"/.exec(source)?.[1] ?? "mobile";
      // Wide enough that the device frame keeps its designed width in both layouts.
      const viewport = device === "desktop" ? 1400 : 1000;

      for (const theme of config.themes) {
        const { targetId } = await cdp.send("Target.createTarget", {
          url: `file://${path.join(SCREENS, file)}`,
        });
        const { sessionId } = await cdp.send("Target.attachToTarget", { targetId, flatten: true });
        try {
          await waitForDocument(cdp, sessionId);
          await cdp.send("Emulation.setDeviceMetricsOverride", {
            width: viewport,
            height: 1200,
            deviceScaleFactor: config.scale,
            mobile: false,
            sessionId,
          });
          await sleep(80);
          const box = JSON.parse(await evaluate(cdp, sessionId, frame(theme)));
          const shot = await cdp.send("Page.captureScreenshot", {
            format: "png",
            captureBeyondViewport: true,
            clip: { ...box, scale: config.scale },
            sessionId,
          });
          const folder = path.join(config.out, theme);
          await mkdir(folder, { recursive: true });
          const name = `${path.basename(file, ".html")}.png`;
          await writeFile(path.join(folder, name), Buffer.from(shot.data, "base64"));
          process.stdout.write(`. ${theme}/${name}\n`);
        } finally {
          await cdp.send("Target.closeTarget", { targetId });
        }
      }
    }
    process.stdout.write(`\nrendered ${files.length} screens with ${chrome}\n`);
  } finally {
    child.kill("SIGTERM");
  }
}

await main();
