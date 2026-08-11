import { toast, setLoading } from "./ui.js";

export function getToken() {
  const el = document.getElementById("token");
  const fromField = el && "value" in el ? String(el.value || "").trim() : "";
  return fromField || (localStorage.getItem("bts_token") || "").trim();
}

export async function api(path, opts = {}) {
  const { quiet, headers: optHeaders, ...fetchOpts } = opts;
  const headers = Object.assign({}, optHeaders || {});
  const t = getToken();
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
