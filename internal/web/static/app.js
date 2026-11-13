// PrintSvr 现代化前端交互逻辑 (纯原生无外部依赖，局域网离线极速运行)

const State = {
  token: localStorage.getItem("printsvr_token") || "",
  user: null,
  activeTab: "print", // 用户视图: print, queue, profile; 管理后台: dashboard, admin-queue, admin-printers, admin-users, admin-settings
  isAdminMode: false,
  publicInfo: {},
  pollTimer: null,
  printers: [],
  selectedPrinterId: null,
};

// 工具函数: API 请求封装
async function api(path, options = {}) {
  const headers = options.headers || {};
  if (State.token) {
    headers["Authorization"] = `Bearer ${State.token}`;
  }
  if (!(options.body instanceof FormData) && options.body && typeof options.body === "object") {
    headers["Content-Type"] = "application/json";
    options.body = JSON.stringify(options.body);
  }

  try {
    const res = await fetch(path, { ...options, headers });
    const data = await res.json().catch(() => ({}));
    if (res.status === 401) {
      logout(false);
      showToast("登录已失效，请重新登录", "error");
      throw new Error("Unauthorized");
    }
    if (!res.ok) {
      throw new Error(data.error || "请求失败");
    }
    return data;
  } catch (err) {
    if (err.message !== "Unauthorized") {
      showToast(err.message, "error");
    }
    throw err;
  }
}

// Toast 提示
function showToast(message, type = "info") {
  const container = document.getElementById("toast-container");
  if (!container) return;

  const toast = document.createElement("div");
  toast.className = `toast ${type}`;
  let icon = "ℹ️";
  if (type === "success") icon = "✅";
  if (type === "error") icon = "❌";

  toast.innerHTML = `<span>${icon}</span> <span>${message}</span>`;
  container.appendChild(toast);

  setTimeout(() => {
    toast.style.opacity = "0";
    toast.style.transition = "opacity 0.3s";
    setTimeout(() => toast.remove(), 300);
  }, 3500);
}

// 格式化文件大小
function formatBytes(bytes) {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
}

// 格式化时间
function formatTime(isoStr) {
  if (!isoStr) return "-";
  const d = new Date(isoStr);
  return d.toLocaleString("zh-CN", { hour12: false });
}

// 获取状态对应渲染标签
function renderStatusBadge(status, progress) {
  const map = {
    pending: { label: "排队中", class: "status-pending" },
    processing: { label: `打印中 ${progress || 0}%`, class: "status-processing" },
    completed: { label: "已完成", class: "status-completed" },
    failed: { label: "失败", class: "status-failed" },
    cancelled: { label: "已取消", class: "status-cancelled" },
  };
  const item = map[status] || { label: status, class: "status-cancelled" };
  return `<span class="status-badge ${item.class}"><span class="status-dot"></span>${item.label}</span>`;
}

// 初始化
document.addEventListener("DOMContentLoaded", async () => {
  await loadPublicInfo();
  if (State.token) {
    try {
      await fetchCurrentUser();
      initAppView();
    } catch (e) {
      showAuthModal("login");
    }
  } else {
    showAuthModal("login");
  }

  // 绑定全局拖拽阻止
  window.addEventListener("dragover", (e) => e.preventDefault());
  window.addEventListener("drop", (e) => e.preventDefault());
});

// 加载公开站点信息
async function loadPublicInfo() {
  try {
    const data = await api("/api/public/info");
    State.publicInfo = data;
    if (data.site_name) {
      document.title = data.site_name;
      document.querySelectorAll(".site-title-text").forEach(el => el.innerText = data.site_name);
    }
    if (data.announcement) {
      const banner = document.getElementById("announcement-text");
      if (banner) banner.innerText = data.announcement;
    }
    // 控制登录页演示测试账号卡片显示开关
    const demoCard = document.getElementById("demo-accounts-card");
    if (demoCard) {
      demoCard.style.display = data.show_demo_accounts === false ? "none" : "block";
    }
  } catch (e) {}
}

// 获取当前登录用户
async function fetchCurrentUser() {
  const data = await api("/api/auth/me");
  State.user = data;
  updateUserBadge();
}

function updateUserBadge() {
  const container = document.getElementById("nav-user-area");
  if (!container || !State.user) return;

  const isAdmin = State.user.role === "admin";
  container.innerHTML = `
    <div class="user-badge">
      <span>👤 ${State.user.display_name || State.user.username}</span>
      <span class="role-tag">${isAdmin ? "管理员" : "普通用户"}</span>
      <span style="font-size:0.75rem; color:#64748b;">配额: ${State.user.quota === -1 ? "无限制" : `${State.user.used_quota}/${State.user.quota}页`}</span>
    </div>
    ${isAdmin ? `
      <button class="btn btn-sm ${State.isAdminMode ? "btn-primary" : "btn-outline"}" onclick="toggleAdminMode()">
        ${State.isAdminMode ? "🖥️ 切换为用户界面" : "🛠️ 进入管理控制台"}
      </button>
    ` : ""}
    <button class="btn btn-sm btn-outline" onclick="logout(true)" title="退出登录">退出</button>
  `;
}

// 切换管理后台/用户视图
function toggleAdminMode() {
  State.isAdminMode = !State.isAdminMode;
  State.activeTab = State.isAdminMode ? "dashboard" : "print";
  initAppView();
}

// 初始化主界面
function initAppView() {
  document.getElementById("auth-modal").classList.remove("active");
  document.getElementById("app-main").style.display = "block";
  updateUserBadge();
  renderNavigation();
  switchTab(State.activeTab);
}

// 渲染导航菜单
function renderNavigation() {
  const navContainer = document.getElementById("nav-tabs-container");
  if (!navContainer) return;

  let tabs = [];
  if (State.isAdminMode) {
    tabs = [
      { id: "dashboard", label: "📊 系统概览", fn: "switchTab('dashboard')" },
      { id: "admin-queue", label: "📑 全局打印队列", fn: "switchTab('admin-queue')" },
      { id: "admin-printers", label: "🖨️ 打印机设备", fn: "switchTab('admin-printers')" },
      { id: "admin-users", label: "👥 用户配额", fn: "switchTab('admin-users')" },
      { id: "admin-settings", label: "⚙️ 系统设置", fn: "switchTab('admin-settings')" },
    ];
  } else {
    tabs = [
      { id: "print", label: "🚀 我要打印", fn: "switchTab('print')" },
      { id: "queue", label: "📋 我的打印作业", fn: "switchTab('queue')" },
      { id: "profile", label: "👤 个人中心", fn: "switchTab('profile')" },
    ];
  }

  navContainer.innerHTML = tabs.map(t => `
    <button class="nav-tab-btn ${State.activeTab === t.id ? "active" : ""}" onclick="${t.fn}">
      ${t.label}
    </button>
  `).join("");
}

// 切换标签页
function switchTab(tabId) {
  State.activeTab = tabId;
  renderNavigation();

  // 停止先前的轮询
  if (State.pollTimer) {
    clearInterval(State.pollTimer);
    State.pollTimer = null;
  }

  const contentArea = document.getElementById("view-content-area");
  if (!contentArea) return;

  switch (tabId) {
    case "print":
      renderPrintView(contentArea);
      break;
    case "queue":
      renderQueueView(contentArea);
      State.pollTimer = setInterval(() => loadUserQueue(false), 3000);
      break;
    case "profile":
      renderProfileView(contentArea);
      break;
    case "dashboard":
      renderDashboardView(contentArea);
      State.pollTimer = setInterval(() => loadDashboardData(false), 5000);
      break;
    case "admin-queue":
      renderAdminQueueView(contentArea);
      State.pollTimer = setInterval(() => loadAdminQueue(false), 3000);
      break;
    case "admin-printers":
      renderAdminPrintersView(contentArea);
      break;
    case "admin-users":
      renderAdminUsersView(contentArea);
      break;
    case "admin-settings":
      renderAdminSettingsView(contentArea);
      break;
  }
}

// ==========================================
// 1. 用户视图: 我要打印 (Print View)
// ==========================================
async function renderPrintView(container) {
  container.innerHTML = `
    <div class="card">
      <div class="card-header">
        <h2 class="card-title">🖨️ 提交打印作业</h2>
        <span style="font-size: 0.85rem; color: var(--text-muted)">支持文件: PDF, Word, Excel, 图片, TXT 等</span>
      </div>

      <form id="print-form" onsubmit="handlePrintSubmit(event)">
        <!-- 步骤1: 上传文件 -->
        <div class="form-group">
          <label class="form-label">1. 选择或拖拽待打印文件</label>
          <div class="dropzone" id="file-dropzone" onclick="document.getElementById('file-input').click()">
            <svg class="dropzone-icon" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12" />
            </svg>
            <p style="font-weight: 600; margin-bottom: 0.25rem;">点击上传，或直接将文件拖拽至此处</p>
            <p style="font-size: 0.8rem; color: var(--text-muted);">单文件支持最大 50MB</p>
            <input type="file" id="file-input" style="display: none" onchange="handleFileSelected(this.files[0])" />
          </div>
          <div id="file-preview" style="display:none;" class="file-preview-card">
            <div>
              <span id="preview-filename" style="font-weight: 600;"></span>
              <span id="preview-filesize" style="font-size:0.8rem; color:#64748b; margin-left: 0.5rem;"></span>
            </div>
            <button type="button" class="btn btn-sm btn-outline" onclick="removeSelectedFile()">重新选择</button>
          </div>
        </div>

        <!-- 步骤2: 选择目标打印机 -->
        <div class="form-group">
          <label class="form-label">2. 选择打印机设备</label>
          <div class="printer-grid" id="printers-select-grid">
            <div style="color:#64748b; padding:1rem;">正在加载局域网打印机...</div>
          </div>
        </div>

        <!-- 步骤3: 打印参数设置 -->
        <div class="form-group">
          <label class="form-label">3. 打印参数配置</label>
          <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 1rem;">
            <div>
              <label class="form-label" style="font-weight:normal;">打印份数</label>
              <input type="number" id="job-copies" class="form-control" value="1" min="1" max="100" />
            </div>
            <div>
              <label class="form-label" style="font-weight:normal;">色彩模式</label>
              <select id="job-color" class="form-control">
                <option value="false">黑白打印 (省墨推荐)</option>
                <option value="true">彩色打印</option>
              </select>
            </div>
            <div>
              <label class="form-label" style="font-weight:normal;">单双面</label>
              <select id="job-duplex" class="form-control">
                <option value="simplex">单面打印</option>
                <option value="duplex_long">双面打印 (长边翻转/常规装订)</option>
                <option value="duplex_short">双面打印 (短边翻转)</option>
              </select>
            </div>
            <div>
              <label class="form-label" style="font-weight:normal;">纸张类型</label>
              <select id="job-papersize" class="form-control">
                <option value="A4">A4 (常用)</option>
                <option value="A3">A3</option>
                <option value="B5">B5</option>
              </select>
            </div>
          </div>
        </div>

        <div class="form-group">
          <label class="form-label" style="font-weight:normal;">备注说明 (选填)</label>
          <input type="text" id="job-remark" class="form-control" placeholder="例: 部门周例会材料，加急使用" />
        </div>

        <div style="display: flex; justify-content: flex-end; margin-top: 1.5rem;">
          <button type="submit" class="btn btn-primary" id="btn-submit-job" style="padding: 0.75rem 2rem; font-size: 1rem;">
            🚀 发送至打印队列
          </button>
        </div>
      </form>
    </div>
  `;

  // 绑定拖拽
  setupDropzone();
  // 加载打印机选项
  await loadPrintersForSelect();
}

let currentSelectedFile = null;

function setupDropzone() {
  const dropzone = document.getElementById("file-dropzone");
  if (!dropzone) return;

  dropzone.addEventListener("dragover", (e) => {
    e.preventDefault();
    dropzone.classList.add("dragover");
  });
  dropzone.addEventListener("dragleave", () => dropzone.classList.remove("dragover"));
  dropzone.addEventListener("drop", (e) => {
    e.preventDefault();
    dropzone.classList.remove("dragover");
    if (e.dataTransfer.files.length > 0) {
      handleFileSelected(e.dataTransfer.files[0]);
    }
  });
}

function handleFileSelected(file) {
  if (!file) return;
  currentSelectedFile = file;
  document.getElementById("file-dropzone").style.display = "none";
  const preview = document.getElementById("file-preview");
  preview.style.display = "flex";
  document.getElementById("preview-filename").innerText = file.name;
  document.getElementById("preview-filesize").innerText = `(${formatBytes(file.size)})`;
}

function removeSelectedFile() {
  currentSelectedFile = null;
  document.getElementById("file-input").value = "";
  document.getElementById("file-preview").style.display = "none";
  document.getElementById("file-dropzone").style.display = "block";
}

async function loadPrintersForSelect() {
  try {
    const res = await api("/api/printers");
    State.printers = res.data || [];
    const grid = document.getElementById("printers-select-grid");
    if (!grid) return;

    if (State.printers.length === 0) {
      grid.innerHTML = `<div style="color:#ef4444; padding:1rem;">暂无可用打印机，请联系管理员添加或导入打印机。</div>`;
      return;
    }

    // 默认选中第一台或 is_default
    let defaultPrinter = State.printers.find(p => p.is_default) || State.printers[0];
    State.selectedPrinterId = defaultPrinter.id;

    grid.innerHTML = State.printers.map(p => `
      <div class="printer-card ${p.id === State.selectedPrinterId ? "selected" : ""}" id="pcard-${p.id}" onclick="selectPrinter(${p.id})">
        <div class="printer-card-header">
          <div class="printer-title">🖨️ ${p.name}</div>
          ${p.is_default ? `<span class="tag" style="background:#e0e7ff; color:#3730a3;">默认</span>` : ""}
        </div>
        <div class="printer-loc">📍 位置: ${p.location || "局域网共享"}</div>
        <div style="font-size:0.75rem; color:#64748b; margin-bottom:0.4rem;">
          当前状态: <strong style="color:${p.status === 'idle' ? '#10b981' : '#f59e0b'}">${p.status === 'idle' ? '🟢 空闲就绪' : '🟠 正在打印中'}</strong>
          (排队: ${p.pending_jobs_count} 个任务)
        </div>
        <div class="printer-tags">
          ${p.color_supported ? `<span class="tag" style="background:#fce7f3; color:#be185d;">🎨 彩色</span>` : `<span class="tag">黑白</span>`}
          ${p.duplex_supported ? `<span class="tag" style="background:#ecfdf5; color:#047857;">📄 双面</span>` : ""}
          ${p.is_simulation ? `<span class="tag" style="background:#fef3c7; color:#b45309;">仿真模式</span>` : `<span class="tag" style="background:#e0f2fe; color:#0369a1;">物理硬件</span>`}
        </div>
      </div>
    `).join("");
  } catch (e) {}
}

function selectPrinter(id) {
  State.selectedPrinterId = id;
  document.querySelectorAll(".printer-card").forEach(el => el.classList.remove("selected"));
  const card = document.getElementById(`pcard-${id}`);
  if (card) card.classList.add("selected");
}

async function handlePrintSubmit(e) {
  e.preventDefault();
  if (!currentSelectedFile) {
    showToast("请先选择待打印的文件", "error");
    return;
  }
  if (!State.selectedPrinterId) {
    showToast("请选择一台打印机", "error");
    return;
  }

  const btn = document.getElementById("btn-submit-job");
  btn.disabled = true;
  btn.innerText = "⏳ 正在上传并排队...";

  const formData = new FormData();
  formData.append("file", currentSelectedFile);
  formData.append("printer_id", State.selectedPrinterId);
  formData.append("copies", document.getElementById("job-copies").value);
  formData.append("is_color", document.getElementById("job-color").value);
  formData.append("duplex", document.getElementById("job-duplex").value);
  formData.append("paper_size", document.getElementById("job-papersize").value);
  formData.append("remark", document.getElementById("job-remark").value);

  try {
    const res = await api("/api/jobs", {
      method: "POST",
      body: formData,
    });
    showToast(`作业提交成功！任务号: ${res.job_number}，已进入打印队列`, "success");
    // 切换到我的作业标签页查看进度
    switchTab("queue");
  } catch (err) {
    btn.disabled = false;
    btn.innerText = "🚀 发送至打印队列";
  }
}

// ==========================================
// 2. 用户视图: 我的打印作业 (User Queue)
// ==========================================
function renderQueueView(container) {
  container.innerHTML = `
    <div class="card">
      <div class="card-header">
        <h2 class="card-title">📋 我的打印队列与任务历史</h2>
        <div style="display:flex; gap:0.5rem; align-items:center;">
          <select id="user-status-filter" class="form-control" style="width: auto; padding: 0.35rem 0.75rem;" onchange="loadUserQueue(true)">
            <option value="all">全部状态</option>
            <option value="pending">排队中</option>
            <option value="processing">打印中</option>
            <option value="completed">已完成</option>
            <option value="failed">失败</option>
            <option value="cancelled">已取消</option>
          </select>
          <button class="btn btn-sm btn-outline" onclick="loadUserQueue(true)">🔄 刷新</button>
        </div>
      </div>

      <div class="table-responsive">
        <table class="custom-table">
          <thead>
            <tr>
              <th>任务流水号</th>
              <th>文件名</th>
              <th>目标打印机</th>
              <th>份数/页数</th>
              <th>打印参数</th>
              <th>状态与进度</th>
              <th>提交时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody id="user-jobs-tbody">
            <tr><td colspan="8" style="text-align:center; padding:2rem; color:#64748b;">正在加载打印作业列表...</td></tr>
          </tbody>
        </table>
      </div>
    </div>
  `;

  loadUserQueue(true);
}

async function loadUserQueue(showLoading = false) {
  const tbody = document.getElementById("user-jobs-tbody");
  if (!tbody) return;

  const filter = document.getElementById("user-status-filter")?.value || "all";
  try {
    const res = await api(`/api/jobs?status=${filter}`);
    const jobs = res.data || [];

    if (jobs.length === 0) {
      tbody.innerHTML = `<tr><td colspan="8" style="text-align:center; padding:2rem; color:#64748b;">暂无打印作业记录</td></tr>`;
      return;
    }

    tbody.innerHTML = jobs.map(j => `
      <tr>
        <td><code>${j.job_number}</code></td>
        <td>
          <div style="font-weight: 600;">${j.file_name}</div>
          <div style="font-size: 0.75rem; color: #64748b;">${formatBytes(j.file_size)} • ${j.file_type.toUpperCase()}</div>
        </td>
        <td>${j.printer?.name || '未知设备'}</td>
        <td>${j.copies} 份 / 约 ${j.pageCount || j.page_count} 页</td>
        <td>
          <span class="tag">${j.is_color ? '🎨彩色' : '黑白'}</span>
          <span class="tag">${j.duplex === 'simplex' ? '单面' : '双面'}</span>
          <span class="tag">${j.paper_size}</span>
        </td>
        <td style="min-width: 140px;">
          ${renderStatusBadge(j.status, j.progress)}
          ${j.status === 'processing' ? `
            <div class="progress-bar-container">
              <div class="progress-bar-fill" style="width: ${j.progress}%;"></div>
            </div>
          ` : ""}
          ${j.error_message ? `<div style="font-size:0.75rem; color:#ef4444; margin-top:2px;">${j.error_message}</div>` : ""}
        </td>
        <td style="font-size: 0.8rem; color: #64748b;">${formatTime(j.created_at)}</td>
        <td>
          <div style="display:flex; gap:0.35rem;">
            ${j.status === 'pending' ? `
              <button class="btn btn-sm btn-danger" onclick="cancelJob(${j.id})" title="取消任务">取消</button>
            ` : ""}
            ${(j.status === 'completed' || j.status === 'failed' || j.status === 'cancelled') ? `
              <button class="btn btn-sm btn-outline" onclick="retryJob(${j.id})" title="重新排队打印">重打</button>
            ` : ""}
            <a href="/api/jobs/${j.id}/download?token=${State.token}" target="_blank" class="btn btn-sm btn-outline" title="预览/下载原文件">查看</a>
          </div>
        </td>
      </tr>
    `).join("");
  } catch (e) {}
}

async function cancelJob(id) {
  if (!confirm("确定要取消此打印任务吗？")) return;
  try {
    await api(`/api/jobs/${id}/cancel`, { method: "POST" });
    showToast("任务已取消", "success");
    loadUserQueue(true);
  } catch (e) {}
}

async function retryJob(id) {
  try {
    await api(`/api/jobs/${id}/retry`, { method: "POST" });
    showToast("任务已重新加入打印队列！", "success");
    loadUserQueue(true);
  } catch (e) {}
}

// ==========================================
// 3. 用户视图: 个人中心 (Profile View)
// ==========================================
function renderProfileView(container) {
  const isLocked = State.user.is_password_locked;
  container.innerHTML = `
    <div style="max-width: 600px; margin: 0 auto;">
      <div class="card">
        <div class="card-header">
          <h2 class="card-title">👤 个人账号信息</h2>
        </div>
        <div class="form-group">
          <label class="form-label">登录用户名</label>
          <input type="text" class="form-control" value="${State.user.username}" disabled />
        </div>
        <div class="form-group">
          <label class="form-label">姓名 / 显示昵称</label>
          <input type="text" class="form-control" value="${State.user.display_name || '-'}" disabled />
        </div>
        <div class="form-group">
          <label class="form-label">所属部门 / 办公室</label>
          <input type="text" class="form-control" value="${State.user.department || '-'}" disabled />
        </div>
        <div class="form-group">
          <label class="form-label">打印配额统计</label>
          <div style="padding: 0.75rem; background: #f8fafc; border-radius: 8px;">
            <div>已打印页数: <strong>${State.user.used_quota}</strong> 页</div>
            <div>总可用额度: <strong>${State.user.quota === -1 ? '不限制' : State.user.quota + ' 页'}</strong></div>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-header">
          <h2 class="card-title">🔐 修改个人密码</h2>
          ${isLocked ? `<span class="tag" style="background:#fee2e2; color:#b91c1c; font-weight:600;">🔒 已锁定</span>` : ''}
        </div>
        ${isLocked ? `
          <div style="background:#fffbeb; border:1px solid #fde68a; color:#92400e; padding:0.75rem 1rem; border-radius:8px; font-size:0.875rem; margin-bottom:1.25rem; display:flex; align-items:center; gap:0.5rem;">
            <span>⚠️</span>
            <span>该账号已被系统管理员设定为<strong>公共锁定账号</strong>，禁止自行修改密码。如需更改请联系管理员。</span>
          </div>
        ` : ''}
        <form onsubmit="handleChangePassword(event)">
          <div class="form-group">
            <label class="form-label">原密码</label>
            <input type="password" id="old-pwd" class="form-control" placeholder="请输入当前密码" required ${isLocked ? 'disabled' : ''} />
          </div>
          <div class="form-group">
            <label class="form-label">新密码 (至少6位)</label>
            <input type="password" id="new-pwd" class="form-control" placeholder="请输入新密码" required minlength="6" ${isLocked ? 'disabled' : ''} />
          </div>
          <div class="form-group">
            <label class="form-label">确认新密码</label>
            <input type="password" id="confirm-pwd" class="form-control" placeholder="请再次输入新密码" required minlength="6" ${isLocked ? 'disabled' : ''} />
          </div>
          <button type="submit" class="btn btn-primary" style="width: 100%;" ${isLocked ? 'disabled style="background:#94a3b8; cursor:not-allowed;"' : ''}>
            ${isLocked ? '🔒 密码已被锁定，不可修改' : '确认修改密码'}
          </button>
        </form>
      </div>
    </div>
  `;
}

async function handleChangePassword(e) {
  e.preventDefault();
  const oldPassword = document.getElementById("old-pwd").value;
  const newPassword = document.getElementById("new-pwd").value;
  const confirmPassword = document.getElementById("confirm-pwd").value;

  if (newPassword !== confirmPassword) {
    showToast("两次输入的新密码不一致，请重新检查！", "error");
    document.getElementById("confirm-pwd").focus();
    return;
  }

  if (oldPassword === newPassword) {
    showToast("新密码不能与原密码相同", "error");
    return;
  }

  try {
    await api("/api/auth/password", {
      method: "POST",
      body: { old_password: oldPassword, new_password: newPassword, confirm_password: confirmPassword },
    });
    showToast("密码修改成功，请使用新密码重新登录", "success");
    logout(false);
  } catch (e) {}
}

// ==========================================
// 4. 管理后台: 系统概览 (Admin Dashboard)
// ==========================================
function renderDashboardView(container) {
  container.innerHTML = `
    <div>
      <div class="stats-grid" id="dashboard-stats-grid">
        <div class="stat-card">
          <div class="stat-icon" style="background:#e0e7ff; color:#4f46e5;">📋</div>
          <div><div class="stat-value" id="stat-today-jobs">-</div><div class="stat-label">今日打印任务</div></div>
        </div>
        <div class="stat-card">
          <div class="stat-icon" style="background:#fef3c7; color:#d97706;">⏳</div>
          <div><div class="stat-value" id="stat-pending-jobs">-</div><div class="stat-label">排队待打印</div></div>
        </div>
        <div class="stat-card">
          <div class="stat-icon" style="background:#ecfdf5; color:#059669;">✅</div>
          <div><div class="stat-value" id="stat-completed-jobs">-</div><div class="stat-label">已成功完成</div></div>
        </div>
        <div class="stat-card">
          <div class="stat-icon" style="background:#e0f2fe; color:#0284c7;">📄</div>
          <div><div class="stat-value" id="stat-total-pages">-</div><div class="stat-label">系统累计消耗页数</div></div>
        </div>
      </div>

      <div style="display:grid; grid-template-columns: 2fr 1fr; gap: 1.5rem;">
        <div class="card">
          <div class="card-header">
            <h3 class="card-title">📈 近 7 日局域网打印趋势</h3>
          </div>
          <div class="trend-chart-container" id="trend-chart">
            <div style="color:#64748b; margin:auto;">加载趋势图中...</div>
          </div>
        </div>

        <div class="card">
          <div class="card-header">
            <h3 class="card-title">🖨️ 打印机负载分布</h3>
          </div>
          <div id="printer-loads-list" style="display:flex; flex-direction:column; gap:0.75rem;">
            <div style="color:#64748b;">加载中...</div>
          </div>
        </div>
      </div>
    </div>
  `;

  loadDashboardData(true);
}

async function loadDashboardData(showLoading = false) {
  try {
    const res = await api("/api/admin/dashboard");
    const d = res.data;
    if (!d) return;

    document.getElementById("stat-today-jobs").innerText = d.total_jobs_today;
    document.getElementById("stat-pending-jobs").innerText = d.pending_jobs;
    document.getElementById("stat-completed-jobs").innerText = d.completed_jobs;
    document.getElementById("stat-total-pages").innerText = d.total_pages_printed || 0;

    // 渲染近 7 天趋势柱状图
    const trendContainer = document.getElementById("trend-chart");
    if (trendContainer && d.recent_trend) {
      const maxCount = Math.max(...d.recent_trend.map(t => t.count), 1);
      trendContainer.innerHTML = d.recent_trend.map(t => {
        const heightPct = Math.max(8, (t.count / maxCount) * 100);
        return `
          <div class="chart-bar-col">
            <div style="font-size:0.7rem; color:#64748b; margin-bottom:2px;">${t.count}</div>
            <div class="chart-bar" style="height: ${heightPct}%;" title="${t.date}: ${t.count} 次打印"></div>
            <div class="chart-label">${t.date}</div>
          </div>
        `;
      }).join("");
    }

    // 渲染打印机负载
    const loadsContainer = document.getElementById("printer-loads-list");
    if (loadsContainer && d.printer_loads) {
      loadsContainer.innerHTML = d.printer_loads.map(pl => `
        <div style="display:flex; justify-content:space-between; align-items:center; padding:0.5rem; background:#f8fafc; border-radius:6px;">
          <span style="font-weight:500; font-size:0.85rem;">🖨️ ${pl.printer_name}</span>
          <span class="tag" style="background:#e0e7ff; color:#3730a3; font-weight:600;">${pl.job_count} 任务</span>
        </div>
      `).join("");
    }
  } catch (e) {}
}

// ==========================================
// 5. 管理后台: 全局打印队列调度 (Admin Queue)
// ==========================================
function renderAdminQueueView(container) {
  container.innerHTML = `
    <div class="card">
      <div class="card-header">
        <h2 class="card-title">📑 全站局域网打印队列监管调度</h2>
        <div style="display:flex; gap:0.5rem; align-items:center; flex-wrap:wrap;">
          <input type="text" id="admin-search-input" class="form-control" placeholder="搜索文件名或流水号..." style="width:180px; padding:0.35rem 0.65rem;" />
          <select id="admin-status-filter" class="form-control" style="width: auto; padding: 0.35rem 0.65rem;" onchange="loadAdminQueue(true)">
            <option value="all">全部状态</option>
            <option value="pending">排队中</option>
            <option value="processing">打印中</option>
            <option value="completed">已完成</option>
            <option value="failed">失败</option>
          </select>
          <button class="btn btn-sm btn-outline" onclick="loadAdminQueue(true)">🔍 查询</button>
          <button class="btn btn-sm btn-danger" onclick="clearHistoryJobs()">🧹 清理历史归档</button>
        </div>
      </div>

      <div class="table-responsive">
        <table class="custom-table">
          <thead>
            <tr>
              <th>流水号/优先级</th>
              <th>提交用户</th>
              <th>文件名</th>
              <th>目标设备</th>
              <th>参数配置</th>
              <th>队列状态与进度</th>
              <th>提交时间</th>
              <th>调度操作</th>
            </tr>
          </thead>
          <tbody id="admin-jobs-tbody">
            <tr><td colspan="8" style="text-align:center; padding:2rem; color:#64748b;">正在加载全站作业...</td></tr>
          </tbody>
        </table>
      </div>
    </div>
  `;

  document.getElementById("admin-search-input").addEventListener("keydown", (e) => {
    if (e.key === "Enter") loadAdminQueue(true);
  });

  loadAdminQueue(true);
}

async function loadAdminQueue(showLoading = false) {
  const tbody = document.getElementById("admin-jobs-tbody");
  if (!tbody) return;

  const filter = document.getElementById("admin-status-filter")?.value || "all";
  const search = document.getElementById("admin-search-input")?.value || "";

  try {
    const res = await api(`/api/admin/jobs?status=${filter}&search=${encodeURIComponent(search)}`);
    const jobs = res.data || [];

    if (jobs.length === 0) {
      tbody.innerHTML = `<tr><td colspan="8" style="text-align:center; padding:2rem; color:#64748b;">当前队列为空</td></tr>`;
      return;
    }

    tbody.innerHTML = jobs.map(j => `
      <tr>
        <td>
          <code>${j.job_number}</code>
          ${j.priority >= 100 ? `<span class="tag" style="background:#fee2e2; color:#b91c1c; font-weight:bold;">⚡加急置顶</span>` : ""}
        </td>
        <td>
          <div style="font-weight:600;">${j.user?.display_name || j.user?.username || '未知'}</div>
          <div style="font-size:0.75rem; color:#64748b;">${j.user?.department || '通用'}</div>
        </td>
        <td>
          <div style="font-weight: 500;">${j.file_name}</div>
          <div style="font-size: 0.75rem; color: #64748b;">${formatBytes(j.file_size)}</div>
        </td>
        <td>${j.printer?.name || '未知设备'}</td>
        <td>
          <div>${j.copies}份 / 约${j.pageCount || j.page_count}页</div>
          <div style="font-size:0.75rem; color:#64748b;">${j.is_color ? '彩色' : '黑白'} • ${j.duplex === 'simplex' ? '单面' : '双面'}</div>
        </td>
        <td>
          ${renderStatusBadge(j.status, j.progress)}
          ${j.status === 'processing' ? `
            <div class="progress-bar-container">
              <div class="progress-bar-fill" style="width: ${j.progress}%;"></div>
            </div>
          ` : ""}
          ${j.error_message ? `<div style="font-size:0.75rem; color:#ef4444;">${j.error_message}</div>` : ""}
        </td>
        <td style="font-size: 0.8rem; color: #64748b;">${formatTime(j.created_at)}</td>
        <td>
          <div style="display:flex; gap:0.35rem; flex-wrap:wrap;">
            ${j.status === 'pending' ? `
              <button class="btn btn-sm btn-primary" onclick="prioritizeJob(${j.id})" title="提升至最先打印">⚡ 置顶</button>
              <button class="btn btn-sm btn-danger" onclick="adminCancelJob(${j.id})">终止</button>
            ` : ""}
            ${(j.status === 'completed' || j.status === 'failed' || j.status === 'cancelled') ? `
              <button class="btn btn-sm btn-outline" onclick="retryJob(${j.id})">重打</button>
            ` : ""}
            <a href="/api/jobs/${j.id}/download?token=${State.token}" target="_blank" class="btn btn-sm btn-outline">预览</a>
          </div>
        </td>
      </tr>
    `).join("");
  } catch (e) {}
}

async function prioritizeJob(id) {
  try {
    await api(`/api/admin/jobs/${id}/prioritize`, { method: "POST" });
    showToast("任务已成功提升为最高优先级并置顶！", "success");
    loadAdminQueue(true);
  } catch (e) {}
}

async function adminCancelJob(id) {
  if (!confirm("确定要强制终止该任务吗？")) return;
  try {
    await api(`/api/admin/jobs/${id}/cancel`, { method: "POST" });
    showToast("作业已终止", "success");
    loadAdminQueue(true);
  } catch (e) {}
}

async function clearHistoryJobs() {
  if (!confirm("确定要清理已完成、失败和已取消的历史记录吗？")) return;
  try {
    const res = await api("/api/admin/jobs/history", { method: "DELETE" });
    showToast(res.message, "success");
    loadAdminQueue(true);
  } catch (e) {}
}

// ==========================================
// 6. 管理后台: 打印机设备管理 (Admin Printers)
// ==========================================
function renderAdminPrintersView(container) {
  container.innerHTML = `
    <div class="card">
      <div class="card-header">
        <h2 class="card-title">🖨️ 局域网共享打印机设备管理</h2>
        <div style="display:flex; gap:0.5rem;">
          <button class="btn btn-primary" onclick="openDiscoverPrintersModal()">
            🔍 自动扫描 Windows 系统打印机
          </button>
          <button class="btn btn-outline" onclick="openAddPrinterModal()">
            ➕ 手动添加打印机
          </button>
        </div>
      </div>

      <div class="table-responsive">
        <table class="custom-table">
          <thead>
            <tr>
              <th>打印机显示名称</th>
              <th>系统设备标识</th>
              <th>物理位置</th>
              <th>模式与特性</th>
              <th>当前状态</th>
              <th>排队中</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody id="admin-printers-tbody">
            <tr><td colspan="7" style="text-align:center; padding:2rem; color:#64748b;">正在加载打印机列表...</td></tr>
          </tbody>
        </table>
      </div>
    </div>
  `;

  loadAdminPrinters();
}

async function loadAdminPrinters() {
  const tbody = document.getElementById("admin-printers-tbody");
  if (!tbody) return;

  try {
    const res = await api("/api/printers");
    const printers = res.data || [];

    if (printers.length === 0) {
      tbody.innerHTML = `<tr><td colspan="7" style="text-align:center; padding:2rem; color:#64748b;">暂未配置打印机，请点击上方按钮扫描或添加</td></tr>`;
      return;
    }

    tbody.innerHTML = printers.map(p => `
      <tr>
        <td>
          <div style="font-weight:600;">🖨️ ${p.name}</div>
          ${p.is_default ? `<span class="tag" style="background:#e0e7ff; color:#3730a3;">默认打印机</span>` : ""}
          <div style="font-size:0.75rem; color:#64748b;">${p.description || ''}</div>
        </td>
        <td><code>${p.system_name || '-'}</code></td>
        <td>${p.location || '未标注'}</td>
        <td>
          ${p.color_supported ? `<span class="tag" style="background:#fce7f3; color:#be185d;">彩色</span>` : `<span class="tag">黑白</span>`}
          ${p.duplex_supported ? `<span class="tag" style="background:#ecfdf5; color:#047857;">双面</span>` : ""}
          ${p.is_simulation ? `<span class="tag" style="background:#fef3c7; color:#b45309;">仿真模式</span>` : `<span class="tag" style="background:#e0f2fe; color:#0369a1;">物理打印机</span>`}
        </td>
        <td>
          <span class="status-badge ${p.status === 'idle' ? 'status-completed' : 'status-pending'}">
            <span class="status-dot"></span>${p.status === 'idle' ? '就绪/闲置' : '打印中'}
          </span>
        </td>
        <td><strong>${p.pending_jobs_count || 0}</strong></td>
        <td>
          <div style="display:flex; gap:0.35rem;">
            <button class="btn btn-sm btn-outline" onclick="sendTestPrintPage(${p.id})">自检测试</button>
            <button class="btn btn-sm btn-outline" onclick="openEditPrinterModal(${p.id})">编辑</button>
            <button class="btn btn-sm btn-danger" onclick="deletePrinter(${p.id})">删除</button>
          </div>
        </td>
      </tr>
    `).join("");
  } catch (e) {}
}

async function sendTestPrintPage(printerId) {
  try {
    const res = await api(`/api/printers/${printerId}/test`, { method: "POST" });
    showToast(res.message, "success");
  } catch (e) {}
}

async function deletePrinter(id) {
  if (!confirm("确定要删除该打印机吗？")) return;
  try {
    await api(`/api/printers/${id}`, { method: "DELETE" });
    showToast("打印机已删除", "success");
    loadAdminPrinters();
  } catch (e) {}
}

// 自动扫描与一键导入模态框
async function openDiscoverPrintersModal() {
  openModal(`
    <div class="card-header" style="margin-bottom:1rem;">
      <h3 class="card-title">🔍 自动扫描 Windows 系统物理与虚拟打印机</h3>
    </div>
    <div id="discover-results-area" style="max-height:350px; overflow-y:auto;">
      <div style="text-align:center; padding:2rem; color:#64748b;">正在调用系统接口扫描已安装打印机...</div>
    </div>
    <div style="display:flex; justify-content:flex-end; gap:0.5rem; margin-top:1.5rem;">
      <button class="btn btn-secondary" onclick="closeModal()">关闭</button>
    </div>
  `);

  try {
    const res = await api("/api/printers/discover");
    const list = res.data || [];
    const area = document.getElementById("discover-results-area");
    if (!area) return;

    if (list.length === 0) {
      area.innerHTML = `<div style="padding:1.5rem; color:#64748b;">未扫描到操作系统打印机，或非 Windows 宿主环境。</div>`;
      return;
    }

    area.innerHTML = `
      <div style="display:flex; flex-direction:column; gap:0.75rem;">
        ${list.map(item => `
          <div style="border:1px solid #e2e8f0; border-radius:8px; padding:0.75rem 1rem; display:flex; justify-content:space-between; align-items:center;">
            <div>
              <div style="font-weight:600; font-size:0.95rem;">🖨️ ${item.Name}</div>
              <div style="font-size:0.75rem; color:#64748b;">端口: ${item.PortName || '未知'} • 驱动: ${item.DriverName || '通用'}</div>
            </div>
            <div>
              ${item.already_imported ? `
                <span class="tag" style="background:#ecfdf5; color:#047857;">已录入</span>
              ` : `
                <button class="btn btn-sm btn-primary" onclick="quickImportPrinter('${encodeURIComponent(item.Name)}', '${encodeURIComponent(item.Name)}')">
                  ➕ 导入此打印机
                </button>
              `}
            </div>
          </div>
        `).join("")}
      </div>
    `;
  } catch (e) {
    const area = document.getElementById("discover-results-area");
    if (area) area.innerHTML = `<div style="color:#ef4444; padding:1rem;">扫描失败: ${e.message}</div>`;
  }
}

async function quickImportPrinter(name, sysName) {
  try {
    await api("/api/printers/import", {
      method: "POST",
      body: {
        name: decodeURIComponent(name),
        system_name: decodeURIComponent(sysName),
        location: "主机直连端口",
        color_supported: false,
        duplex_supported: true,
        is_simulation: false,
        description: "由系统打印机自动扫描导入",
      },
    });
    showToast("打印机导入成功！", "success");
    closeModal();
    loadAdminPrinters();
  } catch (e) {}
}

function openAddPrinterModal() {
  openModal(`
    <div class="card-header" style="margin-bottom:1rem;">
      <h3 class="card-title">➕ 添加新打印机设备</h3>
    </div>
    <form onsubmit="handleAddPrinterSubmit(event)">
      <div class="form-group">
        <label class="form-label">打印机显示名称</label>
        <input type="text" id="add-p-name" class="form-control" placeholder="例: 1号会议室激光打印机" required />
      </div>
      <div class="form-group">
        <label class="form-label">操作系统识别名 / 网络共享名</label>
        <input type="text" id="add-p-sysname" class="form-control" placeholder="例: HP LaserJet 1020" required />
      </div>
      <div class="form-group">
        <label class="form-label">放置位置</label>
        <input type="text" id="add-p-loc" class="form-control" placeholder="例: 总部 3F 西侧复印区" />
      </div>
      <div style="display:flex; gap:1.5rem; margin-bottom:1.25rem;">
        <label><input type="checkbox" id="add-p-color" /> 支持彩色打印</label>
        <label><input type="checkbox" id="add-p-duplex" checked /> 支持双面打印</label>
        <label><input type="checkbox" id="add-p-sim" /> 启用仿真模拟模式</label>
      </div>
      <div style="display:flex; justify-content:flex-end; gap:0.5rem;">
        <button type="button" class="btn btn-secondary" onclick="closeModal()">取消</button>
        <button type="submit" class="btn btn-primary">保存添加</button>
      </div>
    </form>
  `);
}

async function handleAddPrinterSubmit(e) {
  e.preventDefault();
  try {
    await api("/api/printers/import", {
      method: "POST",
      body: {
        name: document.getElementById("add-p-name").value,
        system_name: document.getElementById("add-p-sysname").value,
        location: document.getElementById("add-p-loc").value,
        color_supported: document.getElementById("add-p-color").checked,
        duplex_supported: document.getElementById("add-p-duplex").checked,
        is_simulation: document.getElementById("add-p-sim").checked,
      },
    });
    showToast("打印机添加成功", "success");
    closeModal();
    loadAdminPrinters();
  } catch (e) {}
}

async function openEditPrinterModal(id) {
  try {
    const res = await api("/api/printers");
    const printers = res.data || [];
    const p = printers.find(item => item.id === id);
    if (!p) {
      showToast("未找到该打印机信息", "error");
      return;
    }

    openModal(`
      <div class="card-header" style="margin-bottom:1rem;">
        <h3 class="card-title">✏️ 编辑打印机设备配置: ${p.name}</h3>
      </div>
      <form onsubmit="handleUpdatePrinterSubmit(event, ${id})">
        <div class="form-group">
          <label class="form-label">打印机显示名称</label>
          <input type="text" id="edit-p-name" class="form-control" value="${p.name || ''}" required />
        </div>
        <div class="form-group">
          <label class="form-label">操作系统识别名 / 网络共享名</label>
          <input type="text" id="edit-p-sysname" class="form-control" value="${p.system_name || ''}" required />
        </div>
        <div class="form-group">
          <label class="form-label">物理放置位置</label>
          <input type="text" id="edit-p-loc" class="form-control" value="${p.location || ''}" />
        </div>
        <div class="form-group">
          <label class="form-label">支持纸张规格 (逗号分隔)</label>
          <input type="text" id="edit-p-papers" class="form-control" value="${p.paper_sizes || 'A4,A3,B5'}" />
        </div>
        <div class="form-group">
          <label class="form-label">当前运行状态</label>
          <select id="edit-p-status" class="form-control">
            <option value="idle" ${p.status === 'idle' ? 'selected' : ''}>🟢 就绪/闲置 (idle)</option>
            <option value="busy" ${p.status === 'busy' ? 'selected' : ''}>🟠 正在作业 (busy)</option>
            <option value="offline" ${p.status === 'offline' ? 'selected' : ''}>⚪ 离线脱机 (offline)</option>
            <option value="error" ${p.status === 'error' ? 'selected' : ''}>🔴 故障报警 (error)</option>
          </select>
        </div>
        <div style="display:grid; grid-template-columns: 1fr 1fr; gap:0.6rem; margin-bottom:1.25rem;">
          <label><input type="checkbox" id="edit-p-default" ${p.is_default ? 'checked' : ''} /> 设为系统默认打印机</label>
          <label><input type="checkbox" id="edit-p-color" ${p.color_supported ? 'checked' : ''} /> 支持彩色打印</label>
          <label><input type="checkbox" id="edit-p-duplex" ${p.duplex_supported ? 'checked' : ''} /> 支持双面打印</label>
          <label><input type="checkbox" id="edit-p-sim" ${p.is_simulation ? 'checked' : ''} /> 启用虚拟仿真模式</label>
        </div>
        <div class="form-group">
          <label class="form-label">设备备注说明</label>
          <input type="text" id="edit-p-desc" class="form-control" value="${p.description || ''}" />
        </div>
        <div style="display:flex; justify-content:flex-end; gap:0.5rem;">
          <button type="button" class="btn btn-secondary" onclick="closeModal()">取消</button>
          <button type="submit" class="btn btn-primary">保存修改</button>
        </div>
      </form>
    `);
  } catch (e) {
    showToast("加载打印机详情失败", "error");
  }
}

async function handleUpdatePrinterSubmit(e, id) {
  e.preventDefault();
  try {
    await api(`/api/printers/${id}`, {
      method: "PUT",
      body: {
        name: document.getElementById("edit-p-name").value,
        system_name: document.getElementById("edit-p-sysname").value,
        location: document.getElementById("edit-p-loc").value,
        paper_sizes: document.getElementById("edit-p-papers").value,
        status: document.getElementById("edit-p-status").value,
        is_default: document.getElementById("edit-p-default").checked,
        color_supported: document.getElementById("edit-p-color").checked,
        duplex_supported: document.getElementById("edit-p-duplex").checked,
        is_simulation: document.getElementById("edit-p-sim").checked,
        description: document.getElementById("edit-p-desc").value,
      },
    });
    showToast("打印机配置已成功保存！", "success");
    closeModal();
    loadAdminPrinters();
  } catch (e) {}
}

// ==========================================
// 7. 管理后台: 用户配额管理 (Admin Users)
// ==========================================
function renderAdminUsersView(container) {
  container.innerHTML = `
    <div class="card">
      <div class="card-header">
        <h2 class="card-title">👥 用户与打印配额管理</h2>
        <button class="btn btn-primary" onclick="openCreateUserModal()">➕ 新建用户账号</button>
      </div>

      <div class="table-responsive">
        <table class="custom-table">
          <thead>
            <tr>
              <th>用户名</th>
              <th>姓名/昵称</th>
              <th>系统角色</th>
              <th>所属部门</th>
              <th>打印配额 (已用 / 总额)</th>
              <th>账号状态</th>
              <th>密码权限</th>
              <th>注册时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody id="admin-users-tbody">
            <tr><td colspan="9" style="text-align:center; padding:2rem; color:#64748b;">正在加载用户列表...</td></tr>
          </tbody>
        </table>
      </div>
    </div>
  `;

  loadAdminUsers();
}

async function loadAdminUsers() {
  const tbody = document.getElementById("admin-users-tbody");
  if (!tbody) return;

  try {
    const res = await api("/api/admin/users");
    const users = res.data || [];

    tbody.innerHTML = users.map(u => {
      const isSelf = State.user && State.user.id === u.id;
      return `
      <tr>
        <td><code>${u.username}</code></td>
        <td style="font-weight:600;">${u.display_name}</td>
        <td>
          <span class="role-tag" style="background:${u.role === 'admin' ? '#4f46e5' : '#64748b'}">
            ${u.role === 'admin' ? '系统管理员' : '普通用户'}
          </span>
        </td>
        <td>${u.department || '-'}</td>
        <td>
          <strong>${u.used_quota}</strong> / ${u.quota === -1 ? '无限配额' : u.quota + ' 页'}
        </td>
        <td>
          <span class="status-badge ${u.is_active ? 'status-completed' : 'status-failed'}">
            <span class="status-dot"></span>${u.is_active ? '正常' : '已停用'}
          </span>
        </td>
        <td>
          ${u.is_password_locked ? `
            <span class="tag" style="background:#fee2e2; color:#b91c1c; font-weight:600;">🔒 密码已锁</span>
          ` : `
            <span class="tag" style="background:#ecfdf5; color:#047857;">🔓 允许修改</span>
          `}
        </td>
        <td style="font-size:0.8rem; color:#64748b;">${formatTime(u.created_at)}</td>
        <td>
          <div style="display:flex; gap:0.35rem; flex-wrap:wrap;">
            ${!isSelf ? `
              <button class="btn btn-sm ${u.is_active ? 'btn-danger' : 'btn-primary'}" onclick="toggleUserActive(${u.id})">
                ${u.is_active ? '快捷禁用' : '快捷启用'}
              </button>
              <button class="btn btn-sm btn-outline" onclick="toggleUserPasswordLock(${u.id})" title="${u.is_password_locked ? '解除密码锁定' : '锁定该账号密码(公用机推荐)'}">
                ${u.is_password_locked ? '🔓 解除锁密' : '🔒 锁定改密'}
              </button>
            ` : ''}
            <button class="btn btn-sm btn-outline" onclick="openResetPwdModal(${u.id}, '${u.username}')">重置密码</button>
            <button class="btn btn-sm btn-outline" onclick="openEditUserModal(${u.id})">编辑</button>
          </div>
        </td>
      </tr>
      `;
    }).join("");
  } catch (e) {}
}

async function toggleUserActive(id) {
  try {
    const res = await api(`/api/admin/users/${id}/toggle-active`, { method: "POST" });
    showToast(res.message, "success");
    loadAdminUsers();
  } catch (e) {}
}

async function toggleUserPasswordLock(id) {
  try {
    const res = await api(`/api/admin/users/${id}/toggle-password-lock`, { method: "POST" });
    showToast(res.message, "success");
    loadAdminUsers();
  } catch (e) {}
}

// 生成随机强密码 (包含大小写字母、数字与常用安全字符)
function generateRandomPassword(length = 10) {
  const chars = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789!@#$%";
  let pwd = "";
  for (let i = 0; i < length; i++) {
    pwd += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return pwd;
}

function fillRandomResetPassword() {
  const pwd = generateRandomPassword(10);
  const input = document.getElementById("reset-new-pwd");
  if (input) {
    input.value = pwd;
    showToast("已自动生成强随机密码，提交后请复制发送给用户", "info");
  }
}

function fillRandomCreateUserPassword() {
  const pwd = generateRandomPassword(10);
  const input = document.getElementById("new-u-pwd");
  if (input) {
    input.value = pwd;
    showToast("已自动生成随机密码", "info");
  }
}

function openCreateUserModal() {
  openModal(`
    <div class="card-header" style="margin-bottom:1rem;">
      <h3 class="card-title">➕ 新建局域网用户</h3>
    </div>
    <form onsubmit="handleCreateUserSubmit(event)">
      <div class="form-group">
        <label class="form-label">登录用户名</label>
        <input type="text" id="new-u-username" class="form-control" required minlength="3" />
      </div>
      <div class="form-group">
        <label class="form-label">初始登录密码</label>
        <div style="display:flex; gap:0.5rem;">
          <input type="text" id="new-u-pwd" class="form-control" required minlength="6" value="123456" />
          <button type="button" class="btn btn-outline" style="white-space:nowrap;" onclick="fillRandomCreateUserPassword()">
            🎲 随机生成
          </button>
        </div>
      </div>
      <div class="form-group">
        <label class="form-label">姓名 / 显示名</label>
        <input type="text" id="new-u-name" class="form-control" required />
      </div>
      <div class="form-group">
        <label class="form-label">角色权限</label>
        <select id="new-u-role" class="form-control">
          <option value="user">普通用户</option>
          <option value="admin">系统管理员</option>
        </select>
      </div>
      <div class="form-group">
        <label class="form-label">所属部门</label>
        <input type="text" id="new-u-dept" class="form-control" placeholder="例: 财务部 / 研发中心" />
      </div>
      <div class="form-group">
        <label class="form-label">打印页数配额 (-1 为无限制)</label>
        <input type="number" id="new-u-quota" class="form-control" value="200" />
      </div>
      <div style="display:flex; justify-content:flex-end; gap:0.5rem;">
        <button type="button" class="btn btn-secondary" onclick="closeModal()">取消</button>
        <button type="submit" class="btn btn-primary">立即创建</button>
      </div>
    </form>
  `);
}

async function handleCreateUserSubmit(e) {
  e.preventDefault();
  try {
    await api("/api/admin/users", {
      method: "POST",
      body: {
        username: document.getElementById("new-u-username").value,
        password: document.getElementById("new-u-pwd").value,
        display_name: document.getElementById("new-u-name").value,
        role: document.getElementById("new-u-role").value,
        department: document.getElementById("new-u-dept").value,
        quota: parseInt(document.getElementById("new-u-quota").value) || 200,
      },
    });
    showToast("用户创建成功！", "success");
    closeModal();
    loadAdminUsers();
  } catch (e) {}
}

function openResetPwdModal(id, username) {
  openModal(`
    <div class="card-header" style="margin-bottom:1rem;">
      <h3 class="card-title">🔐 重置用户密码 (${username})</h3>
    </div>
    <form onsubmit="handleResetPwdSubmit(event, ${id})">
      <div class="form-group">
        <label class="form-label">请输入新的登录密码 (至少6位)</label>
        <div style="display:flex; gap:0.5rem;">
          <input type="text" id="reset-new-pwd" class="form-control" required minlength="6" value="123456" />
          <button type="button" class="btn btn-outline" style="white-space:nowrap;" onclick="fillRandomResetPassword()">
            🎲 随机生成
          </button>
        </div>
        <div style="font-size:0.75rem; color:#64748b; margin-top:0.35rem;">
          提示：点击“随机生成”可自动生成高强度密码，可直接复制发给用户。
        </div>
      </div>
      <div style="display:flex; justify-content:flex-end; gap:0.5rem;">
        <button type="button" class="btn btn-secondary" onclick="closeModal()">取消</button>
        <button type="submit" class="btn btn-danger">确认重置</button>
      </div>
    </form>
  `);
}

async function handleResetPwdSubmit(e, id) {
  e.preventDefault();
  const pwd = document.getElementById("reset-new-pwd").value;
  try {
    const res = await api(`/api/admin/users/${id}/reset-password`, {
      method: "POST",
      body: { new_password: pwd },
    });
    showToast(res.message, "success");
    closeModal();
  } catch (e) {}
}

async function openEditUserModal(id) {
  const res = await api("/api/admin/users");
  const user = (res.data || []).find(u => u.id === id);
  if (!user) return;

  openModal(`
    <div class="card-header" style="margin-bottom:1rem;">
      <h3 class="card-title">✏️ 编辑用户: ${user.username}</h3>
    </div>
    <form onsubmit="handleUpdateUserSubmit(event, ${id})">
      <div class="form-group">
        <label class="form-label">姓名/显示名</label>
        <input type="text" id="edit-u-name" class="form-control" value="${user.display_name}" required />
      </div>
      <div class="form-group">
        <label class="form-label">角色权限</label>
        <select id="edit-u-role" class="form-control">
          <option value="user" ${user.role === 'user' ? 'selected' : ''}>普通用户</option>
          <option value="admin" ${user.role === 'admin' ? 'selected' : ''}>系统管理员</option>
        </select>
      </div>
      <div class="form-group">
        <label class="form-label">打印页数配额 (-1 为无限制)</label>
        <input type="number" id="edit-u-quota" class="form-control" value="${user.quota}" />
      </div>
      <div class="form-group">
        <label class="form-label">所属部门</label>
        <input type="text" id="edit-u-dept" class="form-control" value="${user.department || ''}" />
      </div>
      <div style="display:flex; flex-direction:column; gap:0.5rem; margin-bottom:1.25rem;">
        <label><input type="checkbox" id="edit-u-active" ${user.is_active ? 'checked' : ''} /> 账号处于启用状态</label>
        <label><input type="checkbox" id="edit-u-locked" ${user.is_password_locked ? 'checked' : ''} /> 🔒 锁定密码 (公用终端推荐，禁止该用户自行改密)</label>
      </div>
      <div style="display:flex; justify-content:flex-end; gap:0.5rem;">
        <button type="button" class="btn btn-secondary" onclick="closeModal()">取消</button>
        <button type="submit" class="btn btn-primary">保存修改</button>
      </div>
    </form>
  `);
}

async function handleUpdateUserSubmit(e, id) {
  e.preventDefault();
  try {
    await api(`/api/admin/users/${id}`, {
      method: "PUT",
      body: {
        display_name: document.getElementById("edit-u-name").value,
        role: document.getElementById("edit-u-role").value,
        quota: parseInt(document.getElementById("edit-u-quota").value),
        department: document.getElementById("edit-u-dept").value,
        is_active: document.getElementById("edit-u-active").checked,
        is_password_locked: document.getElementById("edit-u-locked").checked,
      },
    });
    showToast("用户信息已更新", "success");
    closeModal();
    loadAdminUsers();
  } catch (e) {}
}

// ==========================================
// 8. 管理后台: 系统设置 (Admin Settings)
// ==========================================
async function renderAdminSettingsView(container) {
  container.innerHTML = `
    <div style="max-width: 650px; margin: 0 auto;">
      <div class="card">
        <div class="card-header">
          <h2 class="card-title">⚙️ 系统运行参数配置</h2>
        </div>
        <form onsubmit="handleSaveSettings(event)">
          <div class="form-group">
            <label class="form-label">系统平台名称</label>
            <input type="text" id="cfg-site_name" class="form-control" required />
          </div>
          <div class="form-group">
            <label class="form-label">首页置顶公告栏文字</label>
            <textarea id="cfg-announcement" class="form-control" rows="3"></textarea>
          </div>
          <div class="form-group">
            <label class="form-label">允许新用户自主在线注册</label>
            <select id="cfg-allow_register" class="form-control">
              <option value="true">开启 (局域网内任意成员均可自主注册)</option>
              <option value="false">关闭 (仅管理员后台统一分配账号)</option>
            </select>
          </div>
          <div class="form-group">
            <label class="form-label">登录页演示测试体验账号快捷卡片</label>
            <select id="cfg-show_demo_accounts" class="form-control">
              <option value="true">开启显示 (方便局域网内开箱体验)</option>
              <option value="false">关闭隐藏 (正式生产环境推荐隐藏)</option>
            </select>
          </div>
          <div class="form-group">
            <label class="form-label">新用户默认初始配额 (页)</label>
            <input type="number" id="cfg-default_quota" class="form-control" />
          </div>
          <div style="display:flex; justify-content:flex-end;">
            <button type="submit" class="btn btn-primary">保存配置</button>
          </div>
        </form>
      </div>
    </div>
  `;

  try {
    const res = await api("/api/admin/settings");
    const settings = res.data || [];
    settings.forEach(s => {
      const el = document.getElementById(`cfg-${s.key}`);
      if (el) el.value = s.value;
    });
  } catch (e) {}
}

async function handleSaveSettings(e) {
  e.preventDefault();
  const body = {
    site_name: document.getElementById("cfg-site_name").value,
    announcement: document.getElementById("cfg-announcement").value,
    allow_register: document.getElementById("cfg-allow_register").value,
    show_demo_accounts: document.getElementById("cfg-show_demo_accounts").value,
    default_quota: document.getElementById("cfg-default_quota").value,
  };
  try {
    await api("/api/admin/settings", {
      method: "POST",
      body: body,
    });
    showToast("系统设置保存成功！", "success");
    loadPublicInfo();
  } catch (e) {}
}

// ==========================================
// 模态框与登录/注册控制
// ==========================================
function openModal(htmlContent) {
  const overlay = document.getElementById("general-modal");
  const content = document.getElementById("general-modal-body");
  if (!overlay || !content) return;
  content.innerHTML = htmlContent;
  overlay.classList.add("active");
}

function closeModal() {
  const overlay = document.getElementById("general-modal");
  if (overlay) overlay.classList.remove("active");
}

function showAuthModal(mode = "login") {
  const modal = document.getElementById("auth-modal");
  const title = document.getElementById("auth-modal-title");
  const switchTip = document.getElementById("auth-switch-tip");
  const registerFields = document.querySelectorAll(".register-only");
  const submitBtn = document.getElementById("auth-submit-btn");

  if (mode === "register") {
    // 后台关闭用户注册时，前端点击自主注册时直接提示后台关闭，不要等用户提交注册时才提示
    if (State.publicInfo && State.publicInfo.allow_register === false) {
      showToast("当前系统已关闭新用户自主注册功能，请联系系统管理员分配账号！", "error");
      return;
    }
    title.innerText = "📝 新用户注册";
    registerFields.forEach(el => el.style.display = "block");
    submitBtn.innerText = "提交注册并登录";
    switchTip.innerHTML = `已有账号？<a href="javascript:void(0)" onclick="showAuthModal('login')">返回登录</a>`;
    modal.dataset.mode = "register";
  } else {
    title.innerText = "🔐 欢迎登录局域网共享打印平台";
    registerFields.forEach(el => el.style.display = "none");
    submitBtn.innerText = "立即登录";
    if (State.publicInfo && State.publicInfo.allow_register === false) {
      switchTip.innerHTML = `<span style="color:#94a3b8; font-size:0.8rem;">（当前系统已关闭自主注册，请联系管理员分配账号）</span>`;
    } else {
      switchTip.innerHTML = `还没有账号？<a href="javascript:void(0)" onclick="showAuthModal('register')">点击自主注册</a>`;
    }
    modal.dataset.mode = "login";
  }
  modal.classList.add("active");
}

async function handleAuthSubmit(e) {
  e.preventDefault();
  const modal = document.getElementById("auth-modal");
  const mode = modal.dataset.mode;

  const username = document.getElementById("auth-username").value;
  const password = document.getElementById("auth-password").value;

  if (mode === "login") {
    try {
      const res = await api("/api/auth/login", {
        method: "POST",
        body: { username, password },
      });
      State.token = res.token;
      State.user = res.user;
      localStorage.setItem("printsvr_token", res.token);
      showToast("登录成功！", "success");
      initAppView();
    } catch (e) {}
  } else {
    const displayName = document.getElementById("auth-displayname").value;
    const department = document.getElementById("auth-department").value;
    try {
      const res = await api("/api/auth/register", {
        method: "POST",
        body: { username, password, display_name: displayName, department },
      });
      State.token = res.token;
      State.user = res.user;
      localStorage.setItem("printsvr_token", res.token);
      showToast("注册成功！已为您自动登录", "success");
      initAppView();
    } catch (e) {}
  }
}

function logout(redirect = true) {
  State.token = "";
  State.user = null;
  localStorage.removeItem("printsvr_token");
  if (State.pollTimer) clearInterval(State.pollTimer);
  document.getElementById("app-main").style.display = "none";
  if (redirect) {
    showAuthModal("login");
  }
}

// 快速填入演示账号（提升体验）
function fillDemoAccount(user, pwd) {
  document.getElementById("auth-username").value = user;
  document.getElementById("auth-password").value = pwd;
}
