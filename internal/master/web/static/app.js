(() => {
  const $ = (s) => document.querySelector(s);
  const $$ = (s) => [...document.querySelectorAll(s)];

  function token() {
    return ($("#token").value || localStorage.getItem("bts_token") || "").trim();
  }
  $("#token").value = localStorage.getItem("bts_token") || "";
  $("#token").addEventListener("change", () => {
    localStorage.setItem("bts_token", $("#token").value.trim());
    loadMeta();
  });

  async function api(path, opts = {}) {
    const headers = Object.assign({}, opts.headers || {});
    const t = token();
    if (t) headers["Authorization"] = "Bearer " + t;
    if (opts.body && !(opts.body instanceof FormData)) {
      headers["Content-Type"] = "application/json";
    }
    const res = await fetch(path, { ...opts, headers });
    const text = await res.text();
    let data;
    try { data = text ? JSON.parse(text) : null; } catch { data = { raw: text }; }
    if (!res.ok) throw new Error((data && data.error) || res.statusText || "request failed");
    return data;
  }

  function esc(s) {
    return String(s ?? "").replace(/[&<>"']/g, (c) => ({
      "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
    }[c]));
  }
  function badge(status) {
    return `<span class="badge ${status || ""}">${status || "-"}</span>`;
  }
  function fmtTime(t) {
    if (!t) return "-";
    try { return new Date(t).toLocaleString(); } catch { return t; }
  }
  function kvHTML(obj) {
    return Object.entries(obj).map(([k, v]) =>
      `<div class="kv-row"><span class="k">${esc(k)}</span><span class="v">${esc(typeof v === "object" ? JSON.stringify(v) : v)}</span></div>`
    ).join("");
  }

  $$("#tabs button").forEach((btn) => {
    btn.addEventListener("click", () => {
      $$("#tabs button").forEach((b) => b.classList.remove("active"));
      $$(".tab").forEach((t) => t.classList.remove("active"));
      btn.classList.add("active");
      $("#tab-" + btn.dataset.tab).classList.add("active");
      if (btn.dataset.tab === "login") loadAccounts();
      if (btn.dataset.tab === "events") loadEvents();
      if (btn.dataset.tab === "settings" || btn.dataset.tab === "update") {
        loadMeta();
        if (btn.dataset.tab === "settings") loadWorkerSettings();
      }
    });
  });

  const featureLabels = {
    cluster_dashboard: "集群看板",
    task_crud: "任务 CRUD",
    qr_login: "扫码登录",
    config_generate: "生成配置",
    event_log: "事件日志",
    pushplus: "PushPlus（worker）",
    proxy_pool: "代理池",
    h2_ja3: "H2/JA3",
    multi_notify: "多通道推送",
    worker_settings: "Worker WebUI 设置",
    payment_qr: "支付二维码",
    page_gate: "开售页 gate",
    local_audio: "本地音乐提醒",
  };

  let metaCache = null;
  async function loadMeta() {
    try {
      metaCache = await api("/api/v1/meta");
      $("#hdr-version").textContent = metaCache.version || "dev";
      const authOn = !!metaCache.web_auth_enabled;
      const pill = $("#auth-pill");
      pill.textContent = authOn ? (token() ? "鉴权: 已填 Token" : "鉴权: 需要 Token") : "鉴权: 关闭";
      pill.className = "pill " + (authOn ? (token() ? "ok" : "warn") : "muted");

      $("#runtime-meta").innerHTML = kvHTML({
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

      const feats = metaCache.features || {};
      $("#feature-matrix").innerHTML = Object.keys(featureLabels).map((k) => {
        const on = !!feats[k];
        return `<div class="feature ${on ? "on" : "off"}"><span class="dot"></span>${esc(featureLabels[k])}<em>${on ? "支持" : "未支持"}</em></div>`;
      }).join("");

      $("#version-box").innerHTML = kvHTML({
        version: metaCache.version,
        build_time: metaCache.build_time,
        git_commit: metaCache.git_commit,
      });
      if (metaCache.release_url) $("#link-releases").href = metaCache.release_url;
      if (metaCache.repo_url) $("#link-repo").href = metaCache.repo_url;
      if (metaCache.buy_repo_url) $("#link-buy").href = metaCache.buy_repo_url;
    } catch (e) {
      $("#auth-pill").textContent = "鉴权: " + e.message;
      $("#auth-pill").className = "pill warn";
    }
  }

  async function loadOverview() {
    const ov = await api("/api/v1/overview");
    const w = ov.workers || {};
    const t = ov.tasks || {};
    $("#overview-cards").innerHTML = [
      ["Workers", w.total], ["Idle", w.idle], ["Working", w.working], ["Risking", w.risking],
      ["Tasks", t.total], ["Pending", t.pending], ["Doing", t.doing], ["Done", t.done],
    ].map(([label, value]) =>
      `<div class="stat"><div class="label">${label}</div><div class="value">${value ?? 0}</div></div>`
    ).join("");
  }

  async function loadWorkers() {
    const list = await api("/api/v1/workers");
    $("#workers-table tbody").innerHTML = (list || []).map((w) => `
      <tr>
        <td><code>${esc(w.worker_id)}</code></td>
        <td>${esc(w.address)}</td>
        <td>${badge(w.status)}</td>
        <td>${esc(w.status_detail || "-")}</td>
        <td><code>${esc(w.proxy_label || "none")}</code></td>
        <td>${esc(w.task_assigned || "-")}</td>
        <td>${fmtTime(w.update_time)}</td>
        <td>${w.ban_remain_sec ? w.ban_remain_sec + "s" : "-"}</td>
      </tr>`).join("") || `<tr><td colspan="8" class="muted">暂无 worker</td></tr>`;
  }

  async function loadTasks() {
    const list = await api("/api/v1/tasks");
    $("#tasks-mini tbody").innerHTML = (list || []).slice(0, 20).map((t) => `
      <tr>
        <td>${esc(t.task_name)}</td>
        <td>${badge(t.status)}</td>
        <td>${esc(t.assigned_to || "-")}</td>
        <td>${t.retry_count}</td>
      </tr>`).join("") || `<tr><td colspan="4" class="muted">暂无任务</td></tr>`;

    const tb = $("#tasks-table tbody");
    tb.innerHTML = (list || []).map((t) => `
      <tr>
        <td>${esc(t.task_name)}<br><code class="muted">${esc(t.id)}</code></td>
        <td>${badge(t.status)}</td>
        <td>${esc(t.assigned_to || "-")}</td>
        <td>${esc(t.preview || "")}</td>
        <td>${t.retry_count}</td>
        <td>${fmtTime(t.updated_at)}</td>
        <td class="row" style="margin:0">
          <button class="btn sm" data-act="requeue" data-id="${esc(t.id)}">重入队</button>
          <button class="btn sm" data-act="view" data-id="${esc(t.id)}">查看</button>
          <button class="btn sm danger" data-act="del" data-id="${esc(t.id)}">删除</button>
        </td>
      </tr>`).join("") || `<tr><td colspan="7" class="muted">暂无任务</td></tr>`;

    tb.querySelectorAll("button").forEach((btn) => {
      btn.addEventListener("click", async () => {
        const id = btn.dataset.id;
        try {
          if (btn.dataset.act === "requeue") {
            await api("/api/v1/tasks/" + id + "/requeue", { method: "POST" });
          } else if (btn.dataset.act === "del") {
            if (!confirm("删除任务？")) return;
            const df = confirm("同时删除磁盘 JSON？") ? "true" : "false";
            await api("/api/v1/tasks/" + id + "?delete_file=" + df, { method: "DELETE" });
          } else if (btn.dataset.act === "view") {
            const data = await api("/api/v1/tasks/" + id);
            alert(data.content || JSON.stringify(data, null, 2));
          }
          await refreshAll();
        } catch (e) { alert(e.message); }
      });
    });
  }

  async function loadConfigs() {
    const list = await api("/api/v1/configs");
    $("#configs-table tbody").innerHTML = (list || []).map((c) => `
      <tr>
        <td>${esc(c.name)}.json</td>
        <td>${c.size}</td>
        <td>${c.has_task ? badge(c.task_status) : "-"}</td>
        <td>${fmtTime(c.mod_time)}</td>
      </tr>`).join("") || `<tr><td colspan="4" class="muted">目录为空</td></tr>`;
  }

  let allEvents = [];
  async function loadEvents() {
    try {
      allEvents = await api("/api/v1/events?limit=200") || [];
      renderEvents();
    } catch (e) {
      $("#events-list").innerHTML = `<div class="muted">${esc(e.message)}</div>`;
    }
  }
  function renderEvents() {
    const level = $("#event-level").value;
    const kw = ($("#event-filter").value || "").trim().toLowerCase();
    const filtered = allEvents.filter((e) => {
      if (level && (e.level || "") !== level) return false;
      if (kw && !(e.message || "").toLowerCase().includes(kw)) return false;
      return true;
    });
    $("#events-list").innerHTML = filtered.slice().reverse().map((e) => `
      <div class="event ${esc(e.level || "")}">
        <span class="t">${fmtTime(e.time)}</span>
        <span class="lvl">${esc(e.level || "info")}</span>
        ${esc(e.message)}
      </div>`).join("") || `<div class="muted">暂无事件</div>`;
  }
  $("#event-level")?.addEventListener("change", renderEvents);
  $("#event-filter")?.addEventListener("input", renderEvents);
  $("#btn-events-refresh")?.addEventListener("click", loadEvents);
  $("#btn-events-clear")?.addEventListener("click", async () => {
    if (!confirm("清空 master 事件缓冲？")) return;
    try {
      await api("/api/v1/events/clear", { method: "POST" });
      await loadEvents();
    } catch (e) { alert(e.message); }
  });

  async function loadAccounts() {
    const data = await api("/api/v1/auth/accounts");
    const active = data.active;
    $("#account-list").innerHTML = (data.accounts || []).map((a) => `
      <div class="list-item">
        <div>
          <strong>${esc(a.username)}</strong>
          <div class="muted">uid=${esc(a.uid)} ${a.uid === active ? "· 当前" : ""}</div>
        </div>
        <div class="row" style="margin:0">
          ${a.uid !== active ? `<button class="btn sm" data-act="act" data-uid="${esc(a.uid)}">启用</button>` : ""}
          <button class="btn sm danger" data-act="del" data-uid="${esc(a.uid)}">删除</button>
        </div>
      </div>`).join("") || `<div class="muted">尚未登录账号</div>`;

    $("#account-list").querySelectorAll("button").forEach((btn) => {
      btn.addEventListener("click", async () => {
        try {
          if (btn.dataset.act === "act") {
            await api("/api/v1/auth/accounts/" + btn.dataset.uid + "/activate", { method: "POST" });
          } else {
            await api("/api/v1/auth/accounts/" + btn.dataset.uid, { method: "DELETE" });
          }
          loadAccounts();
        } catch (e) { alert(e.message); }
      });
    });
  }

  let pollTimer = null;
  $("#btn-qr").addEventListener("click", async () => {
    try {
      const res = await api("/api/v1/auth/qr/start", { method: "POST" });
      $("#qr-box").classList.remove("hidden");
      $("#qr-img").src = "https://api.qrserver.com/v1/create-qr-code/?size=220x220&data=" + encodeURIComponent(res.url);
      $("#qr-status").textContent = "等待扫码…";
      if (pollTimer) clearInterval(pollTimer);
      pollTimer = setInterval(async () => {
        try {
          const p = await api("/api/v1/auth/qr/poll?qrcode_key=" + encodeURIComponent(res.qrcode_key));
          if (p.code === 0) {
            clearInterval(pollTimer);
            $("#qr-status").textContent = "登录成功: " + (p.username || p.uid);
            loadAccounts();
          } else if (p.code === 86038) {
            clearInterval(pollTimer);
            $("#qr-status").textContent = "二维码已失效，请重新生成";
          } else if (p.code === 86090) {
            $("#qr-status").textContent = "已扫码，请在手机上确认";
          } else {
            $("#qr-status").textContent = p.message || ("code=" + p.code);
          }
        } catch (e) { $("#qr-status").textContent = e.message; }
      }, 1500);
    } catch (e) { alert(e.message); }
  });

  function extractProjectId(input) {
    const s = (input || "").trim();
    if (!s) return null;
    if (/^\d+$/.test(s)) return parseInt(s, 10);
    const m = s.match(/id=(\d+)/) || s.match(/detail\/(\d+)/) || s.match(/(\d{4,})/);
    return m ? parseInt(m[1], 10) : null;
  }

  let projectCtx = null;
  let projectIdLoaded = null;

  function syncCountAndSale() {
    const n = $("#buyer-options") ? $("#buyer-options").selectedOptions.length : 0;
    $("#cfg-count").value = n;
    const opts = (projectCtx && projectCtx.ticket_options) || [];
    const ti = parseInt($("#ticket-options").value, 10);
    const ticket = opts[ti];
    const ss = ticket && (ticket.sale_start != null ? ticket.sale_start : ticket.saleStart);
    $("#cfg-sale-start").value = ss != null && ss !== "" ? String(ss) : "";
  }

  $("#btn-load-project").addEventListener("click", async () => {
    const pid = extractProjectId($("#project-id").value);
    if (!pid) return alert("请输入项目 ID");
    try {
      projectCtx = await api("/api/v1/project/" + pid + "/context");
      projectIdLoaded = pid;
      const name = (projectCtx.project && (projectCtx.project.name || projectCtx.project.project_name)) || pid;
      $("#project-meta").textContent = "项目: " + name;
      const opts = projectCtx.ticket_options || [];
      $("#ticket-options").innerHTML = opts.map((o, i) =>
        `<option value="${i}">${esc(o.display || o.desc || o.id)}</option>`
      ).join("");
      const buyers = projectCtx.buyers || [];
      $("#buyer-options").innerHTML = buyers.map((b, i) =>
        `<option value="${i}">${esc(b.name || "")}</option>`
      ).join("");
      const addrs = projectCtx.addresses || [];
      $("#addr-options").innerHTML = addrs.map((a, i) =>
        `<option value="${i}">${esc(a.name || "")} ${esc(a.phone || a.tel || "")} — ${esc(a.addr || a.address || "")}</option>`
      ).join("") || `<option value="">无地址</option>`;
      if (addrs[0]) {
        if (!$("#cfg-buyer").value) $("#cfg-buyer").value = addrs[0].name || "";
        if (!$("#cfg-tel").value) $("#cfg-tel").value = addrs[0].phone || addrs[0].tel || "";
      }
      $("#ticket-options").onchange = syncCountAndSale;
      $("#buyer-options").onchange = syncCountAndSale;
      $("#addr-options").onchange = () => {
        const addrs = projectCtx.addresses || [];
        const a = addrs[parseInt($("#addr-options").value, 10)];
        if (a) {
          $("#cfg-buyer").value = a.name || $("#cfg-buyer").value;
          $("#cfg-tel").value = a.phone || a.tel || $("#cfg-tel").value;
        }
      };
      syncCountAndSale();
    } catch (e) { alert(e.message); }
  });

  $("#btn-generate").addEventListener("click", async () => {
    if (!projectCtx || !projectIdLoaded) return alert("请先加载项目");
    const ti = parseInt($("#ticket-options").value, 10);
    if (Number.isNaN(ti)) return alert("请选票档");
    const buyerIndices = [...$("#buyer-options").selectedOptions].map((o) => parseInt(o.value, 10));
    if (!buyerIndices.length) return alert("请至少选择一位购票人");
    const ai = parseInt($("#addr-options").value, 10);
    if (Number.isNaN(ai)) return alert("请选择收货地址");
    const buyer = $("#cfg-buyer").value.trim();
    const tel = $("#cfg-tel").value.trim();
    if (!buyer || !tel) return alert("请填写联系人姓名与电话");
    const body = {
      name: $("#cfg-name").value.trim(),
      project_id: projectIdLoaded,
      ticket_index: ti,
      buyer_indices: buyerIndices,
      address_index: ai,
      buyer,
      tel,
      phone: $("#cfg-phone").value.trim(),
      start_task: true,
    };
    try {
      const res = await api("/api/v1/configs/generate", { method: "POST", body: JSON.stringify(body) });
      $("#generate-result").textContent = JSON.stringify(res, null, 2);
      await refreshAll();
    } catch (e) { alert(e.message); }
  });

  $("#btn-upload").addEventListener("click", async () => {
    const files = $("#upload-files").files;
    if (!files.length) return alert("请选择文件");
    const fd = new FormData();
    for (const f of files) fd.append("files", f);
    try {
      const res = await api("/api/v1/tasks", { method: "POST", body: fd });
      alert("已创建: " + JSON.stringify(res.created || res));
      await refreshAll();
    } catch (e) { alert(e.message); }
  });

  $("#btn-reload").addEventListener("click", async () => {
    try {
      const res = await api("/api/v1/tasks/reload", { method: "POST" });
      alert("新增 " + (res.added || 0) + " 个任务");
      await refreshAll();
    } catch (e) { alert(e.message); }
  });

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
    const warm = form.elements.namedItem("enable_warmup");
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
      const v = (el && el.value || "").trim();
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
    const warm = form.elements.namedItem("enable_warmup");
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

  const wsForm = $("#ws-form");
  if (wsForm) {
    wsForm.addEventListener("submit", async (ev) => {
      ev.preventDefault();
      try {
        const settings = collectWorkerSettings();
        const res = await api("/api/v1/settings/worker", {
          method: "PUT",
          body: JSON.stringify({ settings }),
        });
        fillWorkerSettingsForm(res.settings || settings);
        $("#ws-meta").textContent = "已保存 version=" + (res.version || 0) + "（worker 心跳后生效）";
        alert("已保存，worker 将在数秒内心跳拉取");
      } catch (e) {
        alert("保存失败: " + e.message);
      }
    });
  }
  const btnWsReload = $("#btn-ws-reload");
  if (btnWsReload) btnWsReload.addEventListener("click", () => loadWorkerSettings());
  const btnWsExport = $("#btn-ws-export");
  if (btnWsExport) {
    btnWsExport.addEventListener("click", async () => {
      try {
        const data = await api("/api/v1/settings/worker/export");
        const box = $("#ws-export");
        box.textContent = data.env || "# (empty)";
        box.classList.remove("hidden");
      } catch (e) {
        alert(e.message);
      }
    });
  }

  async function refreshAll() {
    try {
      await Promise.all([loadMeta(), loadOverview(), loadWorkers(), loadTasks(), loadConfigs()]);
    } catch (e) { console.error(e); }
  }

  $("#btn-refresh").addEventListener("click", refreshAll);
  refreshAll();
  setInterval(() => {
    loadOverview().catch(() => {});
    loadWorkers().catch(() => {});
    loadTasks().catch(() => {});
  }, 3000);
})();
