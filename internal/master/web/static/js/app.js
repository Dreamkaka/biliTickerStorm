import { api, apiError, getToken, setToken, clearToken } from "./api.js";
import { toast, confirmDialog, showDialog, fieldValue, setFieldValue } from "./ui.js";
import { esc, badge, fmtTime, kvHTML, emptyRow } from "./format.js";

const $ = (s, root = document) => root.querySelector(s);
const $$ = (s, root = document) => [...root.querySelectorAll(s)];

const WS_STR = [
  "pushplus_token", "bark_token", "serverchan_key", "serverchan3_api_url",
  "telegram_bot_token", "telegram_chat_id", "telegram_http_proxy",
  "proxy_list", "proxy_api_url", "proxy_api_scheme", "ticket_time_start",
];
const WS_NUM = [
  "interval_ms", "rate_limit_delay_ms", "risk_local_retries",
  "risk_cooldown_base_ms", "risk_cooldown_max_sec",
  "proxy_max_fails", "proxy_cooldown_sec", "proxy_max_backoff_sec",
  "proxy_api_count", "conn_per_host", "create_batch_size",
];

let metaCache = null;
let allEvents = [];
let projectCtx = null;
let projectIdLoaded = null;
let pollTimer = null;
let currentTab = "cluster";

/* ---------- theme ---------- */
function applyTheme(theme) {
  const t = theme === "light" ? "light" : "dark";
  localStorage.setItem("bts_theme", t);
  if (window.mdui?.setTheme) {
    window.mdui.setTheme(t);
  } else {
    document.documentElement.classList.toggle("mdui-theme-dark", t === "dark");
    document.documentElement.classList.toggle("mdui-theme-light", t === "light");
  }
  const btn = $("#btn-theme");
  if (btn) btn.icon = t === "dark" ? "light_mode" : "dark_mode";
}
applyTheme(localStorage.getItem("bts_theme") || "dark");
function toggleTheme() {
  const cur = localStorage.getItem("bts_theme") || "dark";
  applyTheme(cur === "dark" ? "light" : "dark");
}
$("#btn-theme")?.addEventListener("click", toggleTheme);
$("#btn-theme-gate")?.addEventListener("click", toggleTheme);

/* ---------- auth gate ---------- */
let appReady = false;
let pollIntervalId = null;

function showAuthGate(version) {
  $("#auth-gate")?.classList.remove("hidden");
  $("#app-shell")?.classList.add("hidden");
  if (version) $("#auth-gate-version").textContent = version;
  const field = $("#auth-token");
  if (field) field.value = getToken();
  $("#auth-error")?.classList.add("hidden");
}

function showAppShell() {
  $("#auth-gate")?.classList.add("hidden");
  $("#app-shell")?.classList.remove("hidden");
}

async function enterWithToken(raw) {
  const t = String(raw || "").trim();
  if (!t) {
    const err = $("#auth-error");
    if (err) {
      err.textContent = "请输入 WEB_TOKEN";
      err.classList.remove("hidden");
    }
    return false;
  }
  try {
    await api("/api/v1/meta", { token: t });
    setToken(t);
    $("#auth-error")?.classList.add("hidden");
    await bootApp();
    return true;
  } catch (e) {
    const err = $("#auth-error");
    if (err) {
      err.textContent = e.message === "unauthorized" ? "Token 无效" : e.message;
      err.classList.remove("hidden");
    }
    return false;
  }
}

$("#btn-auth-enter")?.addEventListener("click", () => enterWithToken($("#auth-token")?.value));
$("#auth-token")?.addEventListener("keydown", (e) => {
  if (e.key === "Enter") {
    e.preventDefault();
    enterWithToken($("#auth-token")?.value);
  }
});

$("#btn-logout")?.addEventListener("click", async () => {
  clearToken();
  appReady = false;
  if (pollIntervalId) {
    clearInterval(pollIntervalId);
    pollIntervalId = null;
  }
  showAuthGate($("#hdr-version")?.textContent || "—");
});

async function bootstrap() {
  // 先无 token 探测：未配置 WEB_TOKEN 时 meta 可匿名访问
  try {
    const meta = await api("/api/v1/meta", { token: "" });
    if (!meta.web_auth_enabled) {
      clearToken();
      await bootApp();
      return;
    }
  } catch {
    // 需要鉴权或网络错误
  }

  // 已启用鉴权：若本地有 token 则校验
  if (getToken()) {
    try {
      await api("/api/v1/meta");
      await bootApp();
      return;
    } catch {
      clearToken();
    }
  }

  // 拉版本号（health 免鉴权）
  let ver = "—";
  try {
    const h = await fetch("/api/v1/health").then((r) => r.json());
    ver = h.version || "—";
  } catch { /* ignore */ }
  showAuthGate(ver);
}

async function bootApp() {
  showAppShell();
  if (!appReady) {
    appReady = true;
    await refreshAll();
    if (!pollIntervalId) {
      pollIntervalId = setInterval(() => {
        if (currentTab !== "cluster" && currentTab !== "tasks") return;
        loadOverview(true).catch(() => {});
        loadWorkers(true).catch(() => {});
        loadTasks(true).catch(() => {});
      }, 3000);
    }
  } else {
    await refreshAll();
  }
}

/* ---------- navigation ---------- */
function switchTab(tab) {
  if (!tab) return;
  currentTab = tab;
  $$(".page").forEach((p) => p.classList.toggle("active", p.id === "tab-" + tab));

  const rail = $("#rail");
  if (rail && rail.value !== tab) rail.value = tab;

  $$("#drawer-list mdui-list-item").forEach((item) => {
    item.active = item.getAttribute("value") === tab;
  });

  const drawer = $("#drawer");
  if (drawer?.open) drawer.open = false;

  if (tab === "login") loadAccounts();
  if (tab === "events") loadEvents();
  if (tab === "settings" || tab === "update") {
    loadMeta();
    if (tab === "settings") loadWorkerSettings();
  }
}

function onNavChange(el) {
  const v = el?.value;
  if (v && v !== currentTab) switchTab(v);
}

$("#rail")?.addEventListener("change", () => onNavChange($("#rail")));
$("#btn-menu")?.addEventListener("click", () => {
  const drawer = $("#drawer");
  if (drawer) drawer.open = !drawer.open;
});
$$("#drawer-list mdui-list-item").forEach((item) => {
  item.addEventListener("click", () => switchTab(item.getAttribute("value")));
});

/* ---------- meta ---------- */
async function loadMeta() {
  try {
    metaCache = await api("/api/v1/meta");
    $("#hdr-version").textContent = metaCache.version || "dev";
    const authOn = !!metaCache.web_auth_enabled;
    const pill = $("#auth-pill");
    const logoutBtn = $("#btn-logout");
    if (authOn) {
      pill.textContent = "鉴权: 已登录";
      pill.className = "chip ok";
      logoutBtn?.classList.remove("hidden");
    } else {
      pill.textContent = "鉴权: 关闭";
      pill.className = "chip";
      logoutBtn?.classList.add("hidden");
    }

    const runtime = $("#runtime-meta");
    if (runtime) {
      runtime.innerHTML = kvHTML({
        version: metaCache.version,
        build_time: metaCache.build_time,
        git_commit: metaCache.git_commit,
        config_path: metaCache.config_path,
        web_addr: metaCache.web_addr,
        web_auth: metaCache.web_auth_enabled,
        grpc_port: metaCache.grpc_port,
        heartbeat_timeout_sec: metaCache.heartbeat_timeout_sec,
        task_timeout_sec: metaCache.task_timeout_sec,
        ban_timeout_sec: metaCache.ban_timeout_sec,
        accounts: metaCache.accounts_count,
        active_account: metaCache.active_account || "-",
      });
    }

    const vb = $("#version-box");
    if (vb) {
      vb.innerHTML = kvHTML({
        version: metaCache.version,
        build_time: metaCache.build_time,
        git_commit: metaCache.git_commit,
      });
    }
    if (metaCache.release_url) $("#link-releases")?.setAttribute("href", metaCache.release_url);
    if (metaCache.repo_url) $("#link-repo")?.setAttribute("href", metaCache.repo_url);
    if (metaCache.buy_repo_url) $("#link-buy")?.setAttribute("href", metaCache.buy_repo_url);
  } catch (e) {
    const pill = $("#auth-pill");
    if (pill) {
      pill.textContent = "鉴权: " + e.message;
      pill.className = "chip warn";
    }
    if (e.message === "unauthorized" || /unauthor/i.test(e.message)) {
      clearToken();
      showAuthGate($("#hdr-version")?.textContent || "—");
    }
  }
}

/* ---------- cluster ---------- */
async function loadOverview(quiet = false) {
  const ov = await api("/api/v1/overview", { quiet });
  const w = ov.workers || {};
  const t = ov.tasks || {};
  $("#overview-cards").innerHTML = [
    ["Workers", w.total], ["Idle", w.idle], ["Working", w.working], ["Risking", w.risking],
    ["Tasks", t.total], ["Pending", t.pending], ["Doing", t.doing], ["Done", t.done],
  ]
    .map(
      ([label, value]) =>
        `<div class="stat"><div class="label">${label}</div><div class="value">${value ?? 0}</div></div>`
    )
    .join("");
}

async function loadWorkers(quiet = false) {
  const list = await api("/api/v1/workers", { quiet });
  $("#workers-table tbody").innerHTML =
    (list || [])
      .map(
        (w) => `
      <tr>
        <td><code>${esc(w.worker_id)}</code></td>
        <td>${esc(w.address)}</td>
        <td>${badge(w.status)}</td>
        <td>${esc(w.status_detail || "-")}</td>
        <td><code>${esc(w.proxy_label || "none")}</code></td>
        <td>${esc(w.task_assigned || "-")}</td>
        <td>${fmtTime(w.update_time)}</td>
        <td>${w.ban_remain_sec ? w.ban_remain_sec + "s" : "-"}</td>
      </tr>`
      )
      .join("") || emptyRow(8, "暂无 worker");
}

async function loadTasks(quiet = false) {
  const list = await api("/api/v1/tasks", { quiet });
  $("#tasks-mini tbody").innerHTML =
    (list || [])
      .slice(0, 20)
      .map(
        (t) => `
      <tr>
        <td>${esc(t.task_name)}</td>
        <td>${badge(t.status)}</td>
        <td>${esc(t.assigned_to || "-")}</td>
        <td>${t.retry_count}</td>
      </tr>`
      )
      .join("") || emptyRow(4, "暂无任务");

  const tb = $("#tasks-table tbody");
  tb.innerHTML =
    (list || [])
      .map(
        (t) => `
      <tr>
        <td>${esc(t.task_name)}<br><code class="muted">${esc(t.id)}</code></td>
        <td>${badge(t.status)}</td>
        <td>${esc(t.assigned_to || "-")}</td>
        <td>${esc(t.preview || "")}</td>
        <td>${t.retry_count}</td>
        <td>${fmtTime(t.updated_at)}</td>
        <td>
          <div class="action-row">
            <mdui-button variant="text" data-act="requeue" data-id="${esc(t.id)}">重入队</mdui-button>
            <mdui-button variant="text" data-act="view" data-id="${esc(t.id)}">查看</mdui-button>
            <mdui-button variant="text" data-act="del" data-id="${esc(t.id)}">删除</mdui-button>
          </div>
        </td>
      </tr>`
      )
      .join("") || emptyRow(7, "暂无任务");

  tb.querySelectorAll("[data-act]").forEach((btn) => {
    btn.addEventListener("click", async () => {
      const id = btn.dataset.id;
      try {
        if (btn.dataset.act === "requeue") {
          await api("/api/v1/tasks/" + id + "/requeue", { method: "POST" });
          toast("已重入队");
        } else if (btn.dataset.act === "del") {
          const ok = await confirmDialog({ title: "删除任务", message: "确认删除该任务？", okText: "删除" });
          if (!ok) return;
          const delFile = await confirmDialog({
            title: "删除磁盘文件",
            message: "是否同时删除磁盘上的 JSON 配置文件？",
            okText: "删除文件",
          });
          await api("/api/v1/tasks/" + id + "?delete_file=" + (delFile ? "true" : "false"), { method: "DELETE" });
          toast("已删除");
        } else if (btn.dataset.act === "view") {
          const data = await api("/api/v1/tasks/" + id);
          showDialog({
            title: "任务配置（脱敏）",
            body: data.content || JSON.stringify(data, null, 2),
          });
          return;
        }
        await refreshAll();
      } catch (e) {
        apiError(e);
      }
    });
  });
}

async function loadConfigs() {
  const list = await api("/api/v1/configs");
  const all = $("#cfg-select-all");
  if (all) all.checked = false;
  $("#configs-table tbody").innerHTML =
    (list || [])
      .map((c) => {
        const queued = !!c.has_task;
        const check = queued
          ? `<input type="checkbox" disabled title="已在任务队列" />`
          : `<input type="checkbox" class="cfg-pick" data-name="${esc(c.name)}" />`;
        const act = queued
          ? `<span class="muted">已入队</span>`
          : `<mdui-button variant="text" class="cfg-enqueue-one" data-name="${esc(c.name)}">入队</mdui-button>`;
        return `
      <tr>
        <td>${check}</td>
        <td><code>${esc(c.name)}.json</code></td>
        <td>${c.size}</td>
        <td>${queued ? badge(c.task_status) : '<span class="muted">未入队</span>'}</td>
        <td>${fmtTime(c.mod_time)}</td>
        <td>${act}</td>
      </tr>`;
      })
      .join("") || emptyRow(6, "目录为空");

  $$("#configs-table .cfg-enqueue-one").forEach((btn) => {
    btn.addEventListener("click", async () => {
      const name = btn.dataset.name;
      if (!name) return;
      try {
        const res = await api("/api/v1/tasks/reload", {
          method: "POST",
          body: JSON.stringify({ names: [name] }),
        });
        toast("已入队 " + (res.added || 0) + " 个");
        await refreshAll();
      } catch (e) {
        apiError(e);
      }
    });
  });
}

function selectedConfigNames() {
  return $$("#configs-table .cfg-pick:checked")
    .map((el) => el.dataset.name)
    .filter(Boolean);
}

$("#cfg-select-all")?.addEventListener("change", () => {
  const on = !!$("#cfg-select-all")?.checked;
  $$("#configs-table .cfg-pick").forEach((el) => {
    el.checked = on;
  });
});

$("#btn-configs-refresh")?.addEventListener("click", async () => {
  try {
    await loadConfigs();
    toast("列表已刷新");
  } catch (e) {
    apiError(e);
  }
});

$("#btn-configs-enqueue")?.addEventListener("click", async () => {
  const names = selectedConfigNames();
  if (!names.length) {
    toast("请先勾选要入队的配置", { error: true });
    return;
  }
  try {
    const res = await api("/api/v1/tasks/reload", {
      method: "POST",
      body: JSON.stringify({ names }),
    });
    toast("已入队 " + (res.added || 0) + " 个任务");
    await refreshAll();
  } catch (e) {
    apiError(e);
  }
});

/* ---------- events ---------- */
async function loadEvents() {
  try {
    allEvents = (await api("/api/v1/events?limit=200")) || [];
    renderEvents();
  } catch (e) {
    $("#events-list").innerHTML = `<div class="empty-state">${esc(e.message)}</div>`;
  }
}

function renderEvents() {
  const level = $("#event-level")?.value || "";
  const kw = (fieldValue($("#event-filter")) || "").trim().toLowerCase();
  const filtered = allEvents.filter((e) => {
    if (level && (e.level || "") !== level) return false;
    if (kw && !(e.message || "").toLowerCase().includes(kw)) return false;
    return true;
  });
  $("#events-list").innerHTML =
    filtered
      .slice()
      .reverse()
      .map(
        (e) => `
      <div class="event ${esc(e.level || "")}">
        <span class="t">${fmtTime(e.time)}</span>
        <span class="lvl">${esc(e.level || "info")}</span>
        ${esc(e.message)}
      </div>`
      )
      .join("") || `<div class="empty-state">暂无事件</div>`;
}

$("#event-level")?.addEventListener("change", renderEvents);
$("#event-filter")?.addEventListener("input", renderEvents);
$("#btn-events-refresh")?.addEventListener("click", loadEvents);
$("#btn-events-clear")?.addEventListener("click", async () => {
  const ok = await confirmDialog({ title: "清空事件", message: "清空 master 事件缓冲？", okText: "清空" });
  if (!ok) return;
  try {
    await api("/api/v1/events/clear", { method: "POST" });
    toast("已清空");
    await loadEvents();
  } catch (e) {
    apiError(e);
  }
});

/* ---------- accounts / QR ---------- */
async function loadAccounts() {
  try {
    const data = await api("/api/v1/auth/accounts");
    const active = data.active;
    const list = data.accounts || [];
    $("#account-list").innerHTML =
      list
        .map(
          (a) => `
      <div class="list-item">
        <div>
          <strong>${esc(a.username)}</strong>
          <div class="muted">uid=${esc(a.uid)} ${a.uid === active ? "· 当前" : ""}</div>
        </div>
        <div class="actions">
          ${
            a.uid !== active
              ? `<mdui-button variant="outlined" data-act="act" data-uid="${esc(a.uid)}">启用</mdui-button>`
              : ""
          }
          <mdui-button variant="text" data-act="del" data-uid="${esc(a.uid)}">删除</mdui-button>
        </div>
      </div>`
        )
        .join("") || `<div class="empty-state">尚未登录账号</div>`;

    $("#account-list").querySelectorAll("[data-act]").forEach((btn) => {
      btn.addEventListener("click", async () => {
        try {
          if (btn.dataset.act === "act") {
            await api("/api/v1/auth/accounts/" + btn.dataset.uid + "/activate", { method: "POST" });
            toast("已切换账号");
          } else {
            const ok = await confirmDialog({
              title: "删除账号",
              message: "删除该账号本地 cookie？",
              okText: "删除",
            });
            if (!ok) return;
            await api("/api/v1/auth/accounts/" + btn.dataset.uid, { method: "DELETE" });
            toast("已删除账号");
          }
          loadAccounts();
        } catch (e) {
          apiError(e);
        }
      });
    });
  } catch (e) {
    $("#account-list").innerHTML = `<div class="empty-state">${esc(e.message)}</div>`;
  }
}

$("#btn-qr")?.addEventListener("click", async () => {
  try {
    const res = await api("/api/v1/auth/qr/start", { method: "POST" });
    $("#qr-box").classList.remove("hidden");
    $("#qr-img").src =
      "https://api.qrserver.com/v1/create-qr-code/?size=220x220&data=" + encodeURIComponent(res.url);
    $("#qr-status").textContent = "等待扫码…";
    if (pollTimer) clearInterval(pollTimer);
    pollTimer = setInterval(async () => {
      try {
        const p = await api(
          "/api/v1/auth/qr/poll?qrcode_key=" + encodeURIComponent(res.qrcode_key),
          { quiet: true }
        );
        if (p.code === 0) {
          clearInterval(pollTimer);
          $("#qr-status").textContent = "登录成功: " + (p.username || p.uid);
          toast("登录成功");
          loadAccounts();
        } else if (p.code === 86038) {
          clearInterval(pollTimer);
          $("#qr-status").textContent = "二维码已失效，请重新生成";
        } else if (p.code === 86090) {
          $("#qr-status").textContent = "已扫码，请在手机上确认";
        } else {
          $("#qr-status").textContent = p.message || "code=" + p.code;
        }
      } catch (e) {
        $("#qr-status").textContent = e.message;
      }
    }, 1500);
  } catch (e) {
    apiError(e);
  }
});

/* ---------- config generate ---------- */
function extractProjectId(input) {
  const s = (input || "").trim();
  if (!s) return null;
  if (/^\d+$/.test(s)) return parseInt(s, 10);
  const m = s.match(/id=(\d+)/) || s.match(/detail\/(\d+)/) || s.match(/(\d{4,})/);
  return m ? parseInt(m[1], 10) : null;
}

function syncCountAndSale() {
  const n = $("#buyer-options") ? $("#buyer-options").selectedOptions.length : 0;
  setFieldValue($("#cfg-count"), String(n));
  const opts = (projectCtx && projectCtx.ticket_options) || [];
  const ti = parseInt($("#ticket-options").value, 10);
  const ticket = opts[ti];
  const ss = ticket && (ticket.sale_start != null ? ticket.sale_start : ticket.saleStart);
  setFieldValue($("#cfg-sale-start"), ss != null && ss !== "" ? String(ss) : "");
}

$("#btn-load-project")?.addEventListener("click", async () => {
  const pid = extractProjectId(fieldValue($("#project-id")));
  if (!pid) {
    toast("请输入项目 ID", { error: true });
    return;
  }
  try {
    projectCtx = await api("/api/v1/project/" + pid + "/context");
    projectIdLoaded = pid;
    const name =
      (projectCtx.project && (projectCtx.project.name || projectCtx.project.project_name)) || pid;
    $("#project-meta").textContent = "项目: " + name;
    const opts = projectCtx.ticket_options || [];
    $("#ticket-options").innerHTML = opts
      .map((o, i) => `<option value="${i}">${esc(o.display || o.desc || o.id)}</option>`)
      .join("");
    const buyers = projectCtx.buyers || [];
    $("#buyer-options").innerHTML = buyers
      .map((b, i) => `<option value="${i}">${esc(b.name || "")}</option>`)
      .join("");
    const addrs = projectCtx.addresses || [];
    $("#addr-options").innerHTML =
      addrs
        .map(
          (a, i) =>
            `<option value="${i}">${esc(a.name || "")} ${esc(a.phone || a.tel || "")} — ${esc(
              a.addr || a.address || ""
            )}</option>`
        )
        .join("") || `<option value="">无地址</option>`;
    if (addrs[0]) {
      if (!fieldValue($("#cfg-buyer"))) setFieldValue($("#cfg-buyer"), addrs[0].name || "");
      if (!fieldValue($("#cfg-tel")))
        setFieldValue($("#cfg-tel"), addrs[0].phone || addrs[0].tel || "");
    }
    $("#ticket-options").onchange = syncCountAndSale;
    $("#buyer-options").onchange = syncCountAndSale;
    $("#addr-options").onchange = () => {
      const list = projectCtx.addresses || [];
      const a = list[parseInt($("#addr-options").value, 10)];
      if (a) {
        setFieldValue($("#cfg-buyer"), a.name || fieldValue($("#cfg-buyer")));
        setFieldValue($("#cfg-tel"), a.phone || a.tel || fieldValue($("#cfg-tel")));
      }
    };
    syncCountAndSale();
    toast("项目已加载");
  } catch (e) {
    apiError(e);
  }
});

$("#btn-generate")?.addEventListener("click", async () => {
  if (!projectCtx || !projectIdLoaded) {
    toast("请先加载项目", { error: true });
    return;
  }
  const ti = parseInt($("#ticket-options").value, 10);
  if (Number.isNaN(ti)) {
    toast("请选票档", { error: true });
    return;
  }
  const buyerIndices = [...$("#buyer-options").selectedOptions].map((o) => parseInt(o.value, 10));
  if (!buyerIndices.length) {
    toast("请至少选择一位购票人", { error: true });
    return;
  }
  const ai = parseInt($("#addr-options").value, 10);
  if (Number.isNaN(ai)) {
    toast("请选择收货地址", { error: true });
    return;
  }
  const buyer = fieldValue($("#cfg-buyer")).trim();
  const tel = fieldValue($("#cfg-tel")).trim();
  if (!buyer || !tel) {
    toast("请填写联系人姓名与电话", { error: true });
    return;
  }
  const body = {
    name: fieldValue($("#cfg-name")).trim(),
    project_id: projectIdLoaded,
    ticket_index: ti,
    buyer_indices: buyerIndices,
    address_index: ai,
    buyer,
    tel,
    phone: fieldValue($("#cfg-phone")).trim(),
    start_task: !!$("#cfg-start-task")?.checked,
  };
  try {
    const res = await api("/api/v1/configs/generate", { method: "POST", body: JSON.stringify(body) });
    const box = $("#generate-result");
    box.textContent = JSON.stringify(res, null, 2);
    box.classList.remove("hidden");
    toast(res.queued ? "已生成并入队" : "已保存配置（未入队）");
    await refreshAll();
  } catch (e) {
    apiError(e);
  }
});

/* ---------- tasks upload ---------- */
function updateUploadLabel() {
  const input = $("#upload-files");
  const label = $("#upload-files-label");
  if (!input || !label) return;
  const files = [...(input.files || [])];
  label.value = files.length ? files.map((f) => f.name).join(", ") : "";
}
$("#btn-pick-files")?.addEventListener("click", () => $("#upload-files")?.click());
$("#upload-files")?.addEventListener("change", updateUploadLabel);
$("#btn-upload")?.addEventListener("click", async () => {
  const files = $("#upload-files")?.files;
  if (!files?.length) {
    toast("请选择文件", { error: true });
    return;
  }
  const fd = new FormData();
  for (const f of files) fd.append("files", f);
  try {
    const res = await api("/api/v1/tasks", { method: "POST", body: fd });
    toast("已创建: " + JSON.stringify(res.created || res));
    if ($("#upload-files")) $("#upload-files").value = "";
    updateUploadLabel();
    await refreshAll();
  } catch (e) {
    apiError(e);
  }
});

/* ---------- worker settings ---------- */
function fillWorkerSettingsForm(settings) {
  const form = $("#ws-form");
  if (!form || !settings) return;
  for (const k of WS_STR) {
    const el = form.elements.namedItem(k);
    if (el) el.value = settings[k] || "";
  }
  for (const k of WS_NUM) {
    const el = form.elements.namedItem(k);
    if (el) el.value = settings[k] != null && settings[k] !== "" ? settings[k] : "";
  }
  const warm = $("#enable_warmup");
  if (warm) {
    if (settings.enable_warmup === false) warm.checked = false;
    else if (settings.enable_warmup === true) warm.checked = true;
    else warm.checked = true;
  }
}

function collectWorkerSettings() {
  const form = $("#ws-form");
  const s = {};
  for (const k of WS_STR) {
    const el = form.elements.namedItem(k);
    const v = ((el && el.value) || "").trim();
    if (v) s[k] = v;
  }
  for (const k of WS_NUM) {
    const el = form.elements.namedItem(k);
    const raw = el && el.value;
    if (raw !== "" && raw != null) {
      const n = Number(raw);
      if (!Number.isNaN(n)) s[k] = n;
    }
  }
  const warm = $("#enable_warmup");
  if (warm) s.enable_warmup = !!warm.checked;
  return s;
}

async function loadWorkerSettings() {
  try {
    const data = await api("/api/v1/settings/worker");
    fillWorkerSettingsForm(data.settings || {});
    $("#ws-meta").textContent = "version: " + (data.version || 0) + " · " + (data.note || "");
  } catch (e) {
    $("#ws-meta").textContent = "加载失败: " + e.message;
  }
}

$("#ws-form")?.addEventListener("submit", async (ev) => {
  ev.preventDefault();
  try {
    const settings = collectWorkerSettings();
    const res = await api("/api/v1/settings/worker", {
      method: "PUT",
      body: JSON.stringify({ settings }),
    });
    fillWorkerSettingsForm(res.settings || settings);
    $("#ws-meta").textContent = "已保存 version=" + (res.version || 0) + "（worker 心跳后生效）";
    toast("已保存，worker 将在数秒内心跳拉取");
  } catch (e) {
    apiError(e, "保存失败: ");
  }
});

$("#btn-ws-reload")?.addEventListener("click", () => loadWorkerSettings());
$("#btn-ws-export")?.addEventListener("click", async () => {
  try {
    const data = await api("/api/v1/settings/worker/export");
    const box = $("#ws-export");
    box.textContent = data.env || "# (empty)";
    box.classList.remove("hidden");
    toast("已导出 .env 片段");
  } catch (e) {
    apiError(e);
  }
});

/* ---------- refresh ---------- */
async function refreshAll() {
  try {
    await Promise.all([loadMeta(), loadOverview(), loadWorkers(), loadTasks(), loadConfigs()]);
  } catch (e) {
    console.error(e);
  }
}

$("#btn-refresh")?.addEventListener("click", () => {
  refreshAll().then(() => toast("已刷新"));
});

bootstrap();
