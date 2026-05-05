import { readFile } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";
import type { AgentSessionState, PluginConfig, PluginRuntimeContext } from "./types.js";
import { pinchtabFetch } from "./client.js";

const instanceReadyRetryDelayMs = 500;
const instanceReadyMaxWaitMs = 12000;

const agentSessions = new Map<string, AgentSessionState>();
let discoveredConfig: { baseUrl?: string; token?: string } | null | undefined;

function resolveSessionStateKey(context?: PluginRuntimeContext): string {
  return context?.agentId || context?.sessionId || "global";
}

export function rememberRuntimeContext(context?: PluginRuntimeContext): AgentSessionState {
  const key = resolveSessionStateKey(context);
  const existing = agentSessions.get(key);
  const next: AgentSessionState = {
    key,
    agentId: context?.agentId ?? existing?.agentId,
    agentName: context?.agentName ?? existing?.agentName,
    sessionId: context?.sessionId ?? existing?.sessionId,
    sessionKey: context?.sessionKey ?? existing?.sessionKey,
    lastTabId: existing?.lastTabId,
    updatedAt: Date.now(),
  };
  agentSessions.set(key, next);
  return next;
}

export function getAgentSessionState(context?: PluginRuntimeContext): AgentSessionState | undefined {
  return agentSessions.get(resolveSessionStateKey(context));
}

export function normalizeDiscoveredHost(bind: string): string {
  if (bind === "0.0.0.0") return "127.0.0.1";
  if (bind === "::") return "::1";
  return bind;
}

export function formatHostForUrl(host: string): string {
  if (host.includes(":") && !host.startsWith("[") && !host.endsWith("]")) {
    return `[${host}]`;
  }
  return host;
}

export function isLocalHost(baseUrl: string): boolean {
  try {
    const url = new URL(baseUrl);
    const host = url.hostname.toLowerCase();
    return host === "localhost" || host === "127.0.0.1" || host === "::1" || host === "[::1]";
  } catch {
    return false;
  }
}

export function getLastTabId(context?: PluginRuntimeContext): string | undefined {
  return getAgentSessionState(context)?.lastTabId;
}

export function setLastTabId(tabId: string | undefined, context?: PluginRuntimeContext): void {
  const state = rememberRuntimeContext(context);
  state.lastTabId = tabId;
  state.updatedAt = Date.now();
  agentSessions.set(state.key, state);
}

export function resolveProfile(
  cfg: PluginConfig,
  profile?: string,
  context?: PluginRuntimeContext,
): { instanceId?: string; attach?: boolean } {
  rememberRuntimeContext(context);
  const name = profile || cfg.defaultProfile || "openclaw";
  if (cfg.profiles?.[name]) {
    return cfg.profiles[name];
  }
  if (name === "user") {
    return { attach: true };
  }
  return {};
}

async function discoverPinchtabConfig(): Promise<{ baseUrl?: string; token?: string } | null> {
  if (discoveredConfig !== undefined) {
    return discoveredConfig;
  }

  try {
    const path = join(homedir(), ".pinchtab", "config.json");
    const raw = await readFile(path, "utf8");
    const parsed = JSON.parse(raw);
    const bind = parsed?.server?.bind || "127.0.0.1";
    const port = parsed?.server?.port;
    const token = parsed?.server?.token;

    let baseUrl: string | undefined;
    if (port) {
      const host = formatHostForUrl(normalizeDiscoveredHost(bind));
      baseUrl = `http://${host}:${port}`;
    }

    discoveredConfig = {
      baseUrl,
      token: typeof token === "string" && token ? token : undefined,
    };
    return discoveredConfig;
  } catch {
    discoveredConfig = null;
    return discoveredConfig;
  }
}

export function formatDiscoveredBaseUrl(bind: string, port: string | number): string {
  return `http://${formatHostForUrl(normalizeDiscoveredHost(bind))}:${port}`;
}

export async function resolveEffectiveConfig(cfg: PluginConfig): Promise<PluginConfig> {
  const discovered = await discoverPinchtabConfig();
  return {
    ...cfg,
    baseUrl: cfg.baseUrl || discovered?.baseUrl || "http://localhost:9867",
    token: cfg.token || discovered?.token,
  };
}

export async function getEnhancedHealth(cfg: PluginConfig, context?: PluginRuntimeContext): Promise<any> {
  const effectiveCfg = await resolveEffectiveConfig(cfg);
  const base = effectiveCfg.baseUrl || "http://localhost:9867";
  const health = await pinchtabFetch(effectiveCfg, "/health", {}, context);
  const serverOk = !health?.error;
  const agentSession = getAgentSessionState(context);

  const result: any = {
    server: serverOk ? "ok" : "unreachable",
    baseUrl: base,
    defaultProfile: effectiveCfg.defaultProfile || "openclaw",
    policies: {
      allowEvaluate: effectiveCfg.allowEvaluate === true,
      allowDownloads: effectiveCfg.allowDownloads === true,
      allowUploads: effectiveCfg.allowUploads === true,
      allowedDomains: effectiveCfg.allowedDomains?.length ? effectiveCfg.allowedDomains : "all",
    },
  };

  if (agentSession) {
    result.agentSession = {
      agentId: agentSession.agentId,
      agentName: agentSession.agentName,
      sessionId: agentSession.sessionId,
      sessionKey: agentSession.sessionKey,
      lastTabId: agentSession.lastTabId,
    };
  }

  if (serverOk) {
    result.serverHealth = health;
    if (health?.version) result.serverVersion = health.version;
  } else {
    result.error = health?.error;
    if (isLocalHost(base)) {
      result.hint = `Pinchtab is not reachable at ${base}. Start it with: pinchtab server`;
    }
  }

  const warnings: string[] = [];
  if (effectiveCfg.allowEvaluate) warnings.push("evaluate enabled - JS execution allowed");
  if (!effectiveCfg.allowedDomains?.length) warnings.push("no domain restrictions");
  if (warnings.length) result.warnings = warnings;

  return result;
}

export async function ensureServerRunning(
  cfg: PluginConfig,
  context?: PluginRuntimeContext,
): Promise<{ ok: boolean; error?: string; autoStarted?: boolean }> {
  const effectiveCfg = await resolveEffectiveConfig(cfg);
  const base = effectiveCfg.baseUrl || "http://localhost:9867";
  const healthCheck = await pinchtabFetch(effectiveCfg, "/health", {}, context);
  if (!healthCheck?.error) {
    return { ok: true };
  }

  const hint = isLocalHost(base)
    ? ` Pinchtab is not running at ${base}. Start it with: pinchtab server`
    : "";
  return { ok: false, error: `${healthCheck.error}${hint}` };
}

export async function waitForInstanceReady(
  cfg: PluginConfig,
  instanceId?: string,
  context?: PluginRuntimeContext,
): Promise<{ ok: boolean; error?: string }> {
  const effectiveCfg = await resolveEffectiveConfig(cfg);
  const start = Date.now();
  let lastError = "instance not ready";

  while (Date.now() - start < instanceReadyMaxWaitMs) {
    const health = await pinchtabFetch(effectiveCfg, "/health", {}, context);
    if (!health?.error) {
      const instances = await pinchtabFetch(effectiveCfg, "/instances", {}, context);
      const list = Array.isArray(instances?.value)
        ? instances.value
        : Array.isArray(instances)
          ? instances
          : [];
      const running = instanceId
        ? list.find((instance: any) => instance?.id === instanceId && instance?.status === "running")
        : list.find((instance: any) => instance?.status === "running" && instance?.id);
      if (running) {
        return { ok: true };
      }
      if (instanceId && list.some((instance: any) => instance?.id === instanceId)) {
        lastError = `instance ${instanceId} not ready`;
      }
    } else {
      lastError = health.error || lastError;
    }

    if (instanceId) {
      await new Promise((resolve) => setTimeout(resolve, instanceReadyRetryDelayMs));
      continue;
    }

    const tabs = await pinchtabFetch(effectiveCfg, "/tabs", {}, context);
    if (!tabs?.error) {
      return { ok: true };
    }

    const text = `${tabs?.error || ""} ${tabs?.body || ""}`.toLowerCase();
    lastError = tabs?.error || lastError;

    if (!text.includes("instance not ready") && !text.includes("may be restarting") && !text.includes("503")) {
      return { ok: false, error: tabs?.error || "unknown readiness error" };
    }

    await new Promise((resolve) => setTimeout(resolve, instanceReadyRetryDelayMs));
  }

  return { ok: false, error: `Timed out waiting for Pinchtab instance readiness: ${lastError}` };
}
