import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const rootDir = fileURLToPath(new URL(".", import.meta.url));
const workspaceRoot = path.resolve(rootDir, "..");
const packageJSONPath = path.resolve(rootDir, "package.json");

function formatBuildTime(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  const timezoneOffsetMinutes = -date.getTimezoneOffset();
  const timezoneSign = timezoneOffsetMinutes >= 0 ? "+" : "-";
  const timezoneHours = Math.floor(Math.abs(timezoneOffsetMinutes) / 60);
  const timezoneMinutes = Math.abs(timezoneOffsetMinutes) % 60;

  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(
    date.getHours()
  )}:${pad(date.getMinutes())}:${pad(date.getSeconds())} ${timezoneSign}${pad(timezoneHours)}:${pad(
    timezoneMinutes
  )}`;
}

function readFrontendVersion(): string {
  const packageJSON = JSON.parse(readFileSync(packageJSONPath, "utf8")) as {
    version?: string;
  };

  return packageJSON.version?.trim() || "0.0.0";
}

function readGitCommit(): string {
  try {
    const commit = execSync("git rev-parse --short=12 HEAD", {
      cwd: workspaceRoot,
      stdio: ["ignore", "pipe", "ignore"]
    })
      .toString()
      .trim();

    return commit || "unknown";
  } catch {
    // Allow source archive builds without a .git directory; the UI will show "unknown" for commit.
    return "unknown";
  }
}

const frontendBuildInfo = {
  version: readFrontendVersion(),
  commit: readGitCommit(),
  buildTime: formatBuildTime(new Date())
};

export default defineConfig({
  plugins: [react()],
  cacheDir: path.resolve(rootDir, "../build/.cache/vite"),
  define: {
    __MEETING_WEB_VERSION__: JSON.stringify(frontendBuildInfo.version),
    __MEETING_WEB_COMMIT__: JSON.stringify(frontendBuildInfo.commit),
    __MEETING_WEB_BUILD_TIME__: JSON.stringify(frontendBuildInfo.buildTime)
  },
  build: {
    outDir: path.resolve(rootDir, "../build/frontend"),
    emptyOutDir: true
  },
  server: {
    port: 5188,
    host: "0.0.0.0",
    proxy: {
      "/api": "http://127.0.0.1:5180",
      "/healthz": "http://127.0.0.1:5180"
    }
  }
});
