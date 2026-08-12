const mdui = () => window.mdui;

let loadingCount = 0;

export function toast(message, { error = false, timeout = 3200 } = {}) {
  const api = mdui();
  if (api?.snackbar) {
    api.snackbar({
      message: String(message ?? ""),
      closeable: true,
      autoCloseDelay: timeout,
      placement: "bottom",
    });
    return;
  }
  console.log(message);
}

export function setLoading(on) {
  loadingCount += on ? 1 : -1;
  if (loadingCount < 0) loadingCount = 0;
  const bar = document.getElementById("loading-bar");
  if (!bar) return;
  bar.classList.toggle("hidden", loadingCount === 0);
}

export function fieldValue(el) {
  if (!el) return "";
  if ("value" in el) return el.value ?? "";
  return "";
}

export function setFieldValue(el, v) {
  if (!el) return;
  el.value = v ?? "";
}

export function confirmDialog({ title = "确认", message = "", okText = "确定" } = {}) {
  return new Promise((resolve) => {
    const api = mdui();
    let settled = false;
    const finish = (v) => {
      if (settled) return;
      settled = true;
      resolve(v);
    };
    if (api?.confirm) {
      api.confirm({
        headline: title,
        description: message,
        confirmText: okText,
        cancelText: "取消",
        onConfirm: () => finish(true),
        onCancel: () => finish(false),
        onClose: () => finish(false),
      });
      return;
    }
    finish(window.confirm(message));
  });
}

export function showDialog({ title = "详情", body = "" } = {}) {
  const api = mdui();
  if (api?.dialog) {
    api.dialog({
      headline: title,
      description: "",
      body: `<pre style="margin:0;white-space:pre-wrap;word-break:break-word;font-family:ui-monospace,Consolas,monospace;font-size:12px;max-height:60vh;overflow:auto">${escapeHtml(body)}</pre>`,
      actions: [{ text: "关闭" }],
    });
    return;
  }
  if (api?.alert) {
    api.alert({
      headline: title,
      description: String(body).slice(0, 2000),
      confirmText: "关闭",
    });
    return;
  }
  window.alert(body);
}

function escapeHtml(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c])
  );
}
