export function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c])
  );
}

export function badge(status) {
  return `<span class="badge ${esc(status || "")}">${esc(status || "-")}</span>`;
}

export function fmtTime(t) {
  if (!t) return "-";
  try {
    return new Date(t).toLocaleString();
  } catch {
    return String(t);
  }
}

export function kvHTML(obj) {
  return Object.entries(obj)
    .map(
      ([k, v]) =>
        `<div class="kv-row"><span class="k">${esc(k)}</span><span class="v">${esc(
          typeof v === "object" ? JSON.stringify(v) : v
        )}</span></div>`
    )
    .join("");
}

export function emptyRow(cols, text = "暂无数据") {
  return `<tr><td colspan="${cols}" class="empty">${esc(text)}</td></tr>`;
}
