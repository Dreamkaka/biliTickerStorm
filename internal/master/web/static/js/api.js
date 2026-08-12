import { toast, setLoading } from "./ui.js";

/** 内存中的 token；鉴权门写入，getToken 优先读它 */
let memoryToken = (localStorage.getItem("bts_token") || "").trim();

export function getToken() {
  return memoryToken || (localStorage.getItem("bts_token") || "").trim();
}

export function setToken(t) {
  memoryToken = String(t || "").trim();
  if (memoryToken) localStorage.setItem("bts_token", memoryToken);
  else localStorage.removeItem("bts_token");
}

export function clearToken() {
  memoryToken = "";
  localStorage.removeItem("bts_token");
}

export async function api(path, opts = {}) {
  const { quiet, headers: optHeaders, token: overrideToken, ...fetchOpts } = opts;
  const headers = Object.assign({}, optHeaders || {});
  const t = overrideToken != null ? String(overrideToken).trim() : getToken();
  if (t) headers["Authorization"] = "Bearer " + t;
  if (fetchOpts.body && !(fetchOpts.body instanceof FormData)) {
    headers["Content-Type"] = "application/json";
  }
  if (!quiet) setLoading(true);
  try {
    const res = await fetch(path, { ...fetchOpts, headers });
    const text = await res.text();
    let data;
    try {
      data = text ? JSON.parse(text) : null;
    } catch {
      data = { raw: text };
    }
    if (!res.ok) {
      const msg = (data && data.error) || res.statusText || "request failed";
      throw new Error(msg);
    }
    return data;
  } finally {
    if (!quiet) setLoading(false);
  }
}

export function apiError(e, prefix = "") {
  const msg = prefix ? prefix + e.message : e.message;
  toast(msg, { error: true });
  console.error(e);
}
