const host = () => document.getElementById("snackbar-host");
const loadingBar = () => document.getElementById("loading-bar");
let loadingCount = 0;

export function toast(message, { error = false, timeout = 3200 } = {}) {
  const el = document.createElement("div");
  el.className = "snackbar" + (error ? " error" : "");
  el.innerHTML = `<span class="msg"></span><button type="button">关闭</button>`;
  el.querySelector(".msg").textContent = String(message ?? "");
  const close = () => el.remove();
  el.querySelector("button").addEventListener("click", close);
  host()?.appendChild(el);
  if (timeout > 0) setTimeout(close, timeout);
}

export function setLoading(on) {
  loadingCount += on ? 1 : -1;
  if (loadingCount < 0) loadingCount = 0;
  loadingBar()?.classList.toggle("active", loadingCount > 0);
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
    const dialog = document.getElementById("confirm-dialog");
    const titleEl = document.getElementById("confirm-title");
    const msgEl = document.getElementById("confirm-message");
    const ok = document.getElementById("confirm-ok");
    const cancel = document.getElementById("confirm-cancel");
    if (!dialog) {
      resolve(window.confirm(message));
      return;
    }
    titleEl.textContent = title;
    msgEl.textContent = message;
    ok.textContent = okText;
    let settled = false;
    const finish = (v) => {
      if (settled) return;
      settled = true;
      ok.removeEventListener("click", onOk);
      cancel.removeEventListener("click", onCancel);
      dialog.removeEventListener("close", onClose);
      resolve(v);
    };
    const onOk = (e) => {
      e.preventDefault();
      finish(true);
      if (dialog.open) dialog.close();
    };
    const onCancel = (e) => {
      e.preventDefault();
      finish(false);
      if (dialog.open) dialog.close();
    };
    const onClose = () => finish(false);
    ok.addEventListener("click", onOk);
    cancel.addEventListener("click", onCancel);
    dialog.addEventListener("close", onClose);
    dialog.show();
  });
}

export function showDialog({ title = "详情", body = "" } = {}) {
  const dialog = document.getElementById("view-dialog");
  const titleEl = document.getElementById("view-title");
  const bodyEl = document.getElementById("view-body");
  const close = document.getElementById("view-close");
  if (!dialog) {
    window.alert(body);
    return;
  }
  titleEl.textContent = title;
  bodyEl.textContent = body;
  const onClose = (e) => {
    e?.preventDefault?.();
    close.removeEventListener("click", onClose);
    dialog.close();
  };
  close.addEventListener("click", onClose);
  dialog.show();
}
