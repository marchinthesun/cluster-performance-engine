(() => {
  const $ = (sel) => document.querySelector(sel);

  const sessionId =
    globalThis.crypto?.randomUUID?.() ??
    "sess-" + Date.now().toString(36) + "-" + Math.random().toString(36).slice(2, 10);

  let execSeq = 0;

  /** @type {{ stdout:string, stderr:string, exit_code:number, duration_ms:number, error?:string, request_id?:string, server_time?:string } | null} */
  let lastResp = null;

  /** @type {any | null} */
  let lastTopologySnap = null;

  function isoNow() {
    return new Date().toISOString();
  }

  function activateTab(name) {
    document.querySelectorAll(".tab").forEach((t) => {
      t.classList.toggle("active", t.dataset.tab === name);
      t.setAttribute("aria-selected", t.dataset.tab === name ? "true" : "false");
    });
    document.querySelectorAll(".tab-panel").forEach((p) => {
      const on = p.id === "panel-" + name;
      p.classList.toggle("active", on);
      p.hidden = !on;
    });
  }

  function activateSubtab(name) {
    document.querySelectorAll(".subtab").forEach((t) => {
      t.classList.toggle("active", t.dataset.sub === name);
    });
    document.querySelectorAll("#panel-output .subpanel").forEach((p) => {
      const on = p.id === "sub-" + name;
      p.classList.toggle("active", on);
      p.toggleAttribute("hidden", !on);
    });
  }

  function openOutputAndFocus() {
    activateTab("output");
    setTimeout(() => {
      $("#cli-input").focus();
      $("#cli-input").select?.();
    }, 30);
  }

  function openControlTab() {
    activateTab("control");
    setTimeout(() => {
      document.querySelector("#nf-run-cmd")?.focus?.();
    }, 30);
  }

  const tokInput = $("#api-token");
  if (tokInput) {
    tokInput.value = sessionStorage.getItem("nf-api-token") || "";
    tokInput.addEventListener("change", () => {
      sessionStorage.setItem("nf-api-token", tokInput.value.trim());
    });
  }

  $("#sess-id").textContent = sessionId;
  $("#footer-sess-hint").textContent =
    "nexusflow dashboard · " + sessionId.slice(0, 8) + "… · Output · Topology · Control · Guide";

  $("#cli-events").textContent = "[" + isoNow() + "] UI ready\n";
  $("#cli-out-stdout").textContent = "(empty)";
  $("#cli-out-stderr").textContent = "(empty)";
  $("#cli-log").textContent =
    "[" +
    isoNow() +
    "] Session started. Commands POST to /api/exec (whitelist roots only).\n";

  const sessWallStart = Date.now();
  function tickSession() {
    const s = Math.floor((Date.now() - sessWallStart) / 1000);
    const h = String(Math.floor(s / 3600)).padStart(2, "0");
    const m = String(Math.floor((s % 3600) / 60)).padStart(2, "0");
    const sec = String(s % 60).padStart(2, "0");
    $("#sess-time").textContent = `${h}:${m}:${sec}`;
  }
  setInterval(tickSession, 1000);
  tickSession();

  function appendActivity(summary, ok) {
    const ul = $("#activity-feed");
    const li = document.createElement("li");
    li.className = "activity-row" + (ok === false ? " activity-fail" : "");
    li.textContent = `${isoNow().slice(11, 23)} · ${summary}`;
    ul.insertBefore(li, ul.firstChild);
    while (ul.children.length > 24) ul.removeChild(ul.lastChild);
  }

  function splitArgv(line) {
    const parts = [];
    let cur = "";
    let q = null;
    for (let i = 0; i < line.length; i++) {
      const c = line[i];
      if (q) {
        if (c === q) q = null;
        else cur += c;
        continue;
      }
      if (c === '"' || c === "'") {
        q = c;
        continue;
      }
      if (/\s/.test(c)) {
        if (cur.length) {
          parts.push(cur);
          cur = "";
        }
        continue;
      }
      cur += c;
    }
    if (cur.length) parts.push(cur);
    return parts.filter(Boolean);
  }

  function argvToCliLine(argv) {
    return argv
      .map((w) => {
        if (/[\s'"\\]/.test(w)) return '"' + w.replace(/\\/g, "\\\\").replace(/"/g, '\\"') + '"';
        return w;
      })
      .join(" ");
  }

  function readTimeoutSec() {
    const el = $("#exec-timeout-sec");
    if (!el) return 0;
    const n = parseInt(String(el.value).trim(), 10);
    return Number.isFinite(n) && n > 0 ? n : 0;
  }

  const execTimeoutEl = $("#exec-timeout-sec");
  if (execTimeoutEl) {
    const saved = sessionStorage.getItem("nf-exec-timeout");
    if (saved != null && saved !== "") execTimeoutEl.value = saved;
    execTimeoutEl.addEventListener("change", () => {
      sessionStorage.setItem("nf-exec-timeout", execTimeoutEl.value);
    });
  }

  function fillCliFromArgv(argv) {
    $("#cli-input").value = argvToCliLine(argv);
    openOutputAndFocus();
  }

  /** Single-line tail: argv-split like CLI. Multiple lines: pinned `bash -lc` script */
  function normalizeRunTailArgv(text) {
    const normalized = text.replace(/\r\n/g, "\n");
    const trimmed = normalized.trim();
    if (!trimmed) return null;
    const endsTrimmed = normalized.trimEnd();
    if (!/\n/.test(endsTrimmed)) return splitArgv(trimmed);
    return ["bash", "-lc", trimmed];
  }

  function buildRunArgv() {
    const cpus = $("#nf-run-cpus").value.trim();
    const numa = $("#nf-run-numa").value.trim();
    const pri = $("#nf-run-priority").value.trim();
    const membind = $("#nf-run-membind")?.checked !== false;
    const tailArgv = normalizeRunTailArgv($("#nf-run-cmd").value);
    if (!tailArgv) return null;
    const out = ["run"];
    if (cpus !== "") out.push("--cpus", cpus);
    if (numa !== "") out.push("--numa", numa);
    if (pri !== "") out.push("--priority", pri);
    if (!membind) out.push("--membind=false");
    out.push("--", ...tailArgv);
    return out;
  }

  function buildDagArgv() {
    const file = $("#nf-dag-file").value.trim();
    if (!file) return null;
    const argv = ["dag", "run", "--file", file];
    const prom = $("#nf-dag-prom").value.trim();
    if (prom) argv.push("--prom-file", prom);
    return argv;
  }

  function buildShmArgv() {
    const size = $("#nf-shm-size").value.trim();
    if (!size) return null;
    const argv = ["shm", "create", "--size", size];
    const name = $("#nf-shm-name").value.trim();
    if (name) argv.push("--name", name);
    return argv;
  }

  function buildPlasmaArgv() {
    const listen = $("#nf-plasma-listen").value.trim();
    const file = $("#nf-plasma-file").value.trim();
    const shmName = $("#nf-plasma-shm-name").value.trim();
    const idle = $("#nf-plasma-idle").value.trim();
    if (!listen || !file || !shmName || !idle) return null;
    const sz = parseInt(String($("#nf-plasma-shm-size").value).trim(), 10);
    if (!Number.isFinite(sz) || sz < 1) return null;
    return [
      "plasma",
      "run",
      "--listen",
      listen,
      "--file",
      file,
      "--shm-name",
      shmName,
      "--shm-size",
      String(sz),
      "--idle-exit",
      idle,
    ];
  }

  function buildPerfArgv() {
    const ms = parseInt(String($("#nf-perf-ms").value).trim(), 10);
    const kind = $("#nf-perf-kind").value.trim() || "cycles";
    if (!Number.isFinite(ms) || ms < 1) return null;
    return ["perf", "sample", "--sleep-ms", String(ms), "--kind", kind];
  }

  function buildHugepagesArgv() {
    const size = $("#nf-hp-size").value.trim() || "2M";
    const pages = parseInt(String($("#nf-hp-pages").value).trim(), 10);
    if (!Number.isFinite(pages) || pages < 0) return null;
    return ["hugepages", "set", "--size", size, "--pages", String(pages)];
  }

  function tryParseTopologyJSON(stdout) {
    const t = stdout.trim();
    try {
      return JSON.parse(t);
    } catch (_) {}
    const i = t.indexOf("{");
    const j = t.lastIndexOf("}");
    if (i >= 0 && j > i) {
      try {
        return JSON.parse(t.slice(i, j + 1));
      } catch (_) {}
    }
    return null;
  }

  function parseTSVRows(text) {
    const lines = text.replace(/\r\n/g, "\n").split("\n");
    const rows = [];
    for (const line of lines) {
      if (!line.trim() || line.trim().startsWith("#")) continue;
      if (!line.includes("\t")) continue;
      rows.push(line.split("\t").map((c) => c.trim()));
    }
    if (rows.length < 2) return null;
    const ncol = rows[0].length;
    if (ncol < 2) return null;
    let ok = 0;
    for (const r of rows) {
      if (r.length === ncol) ok++;
    }
    if (ok < Math.ceil(rows.length * 0.6)) return null;
    return rows;
  }

  function renderParsedTable(rows) {
    const host = $("#parsed-table-wrap");
    if (!host || !rows.length) return;
    const table = document.createElement("table");
    table.className = "data-table";
    const thead = document.createElement("thead");
    const trh = document.createElement("tr");
    rows[0].forEach((c) => {
      const th = document.createElement("th");
      th.textContent = c;
      trh.appendChild(th);
    });
    thead.appendChild(trh);
    table.appendChild(thead);
    const tbody = document.createElement("tbody");
    for (let r = 1; r < rows.length; r++) {
      const tr = document.createElement("tr");
      rows[r].forEach((c) => {
        const td = document.createElement("td");
        td.textContent = c;
        tr.appendChild(td);
      });
      tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    host.innerHTML = "";
    host.appendChild(table);
  }

  function renderNumaChart(snap) {
    const host = $("#chart-numa");
    if (!host) return;
    host.innerHTML = "";
    const nodes = snap?.numa_nodes || [];
    if (!nodes.length) {
      host.innerHTML = '<p class="muted small">No NUMA nodes in JSON snapshot.</p>';
      return;
    }
    const counts = nodes.map((n) => ({
      id: n.id,
      n: (n.cpus && n.cpus.length) || 0,
    }));
    const max = Math.max(...counts.map((x) => x.n), 1);
    const wrap = document.createElement("div");
    wrap.className = "bar-list";
    for (const { id, n } of counts) {
      const row = document.createElement("div");
      row.className = "bar-row";
      const label = document.createElement("span");
      label.className = "bar-label";
      label.textContent = `NUMA ${id}`;
      const track = document.createElement("div");
      track.className = "bar-track";
      const fill = document.createElement("div");
      fill.className = "bar-fill";
      fill.style.width = `${(100 * n) / max}%`;
      track.appendChild(fill);
      const val = document.createElement("span");
      val.className = "bar-val mono";
      val.textContent = String(n);
      row.appendChild(label);
      row.appendChild(track);
      row.appendChild(val);
      wrap.appendChild(row);
    }
    host.appendChild(wrap);
  }

  function updateDataHint(snap, tsvOk) {
    const el = $("#data-hint");
    if (!el) return;
    const parts = [];
    if (snap) parts.push("Topology JSON detected — bars and CPU table updated.");
    if (tsvOk) parts.push("Tab-separated table rendered from stdout.");
    if (!parts.length)
      el.textContent =
        "No structured parse yet. Run topology JSON or a command that prints TSV (e.g. topology matrix).";
    else el.textContent = parts.join(" ");
  }

  function applyTopologySnap(snap) {
    const cpus = snap.cpus || [];
    const nodes = snap.numa_nodes || [];

    $("#row-count").textContent = String(cpus.length);
    $("#m-cpus").textContent = `CPUs ${cpus.length}`;
    $("#m-numa").textContent = `NUMA ${nodes.length}`;

    const tbody = $("#cpu-tbody");
    tbody.innerHTML = "";
    cpus.forEach((c, i) => {
      const tr = document.createElement("tr");
      const siblings = (c.thread_siblings || []).join(", ");
      tr.innerHTML = `
        <td>${i + 1}</td>
        <td class="pkg-cell">${escapeHtml(String(c.package ?? ""))}</td>
        <td>${escapeHtml(String(c.core ?? ""))}</td>
        <td>${escapeHtml(String(c.numa_node ?? ""))}</td>
        <td><span class="sev-tag pill-med">${escapeHtml(String(c.id ?? ""))}</span></td>
        <td class="muted">${escapeHtml(siblings)}</td>`;
      tbody.appendChild(tr);
    });

    renderNumaChart(snap);
  }

  function parseTopologyIntoUI(stdout) {
    const snap = tryParseTopologyJSON(stdout);
    if (snap) {
      lastTopologySnap = snap;
      applyTopologySnap(snap);
      return snap;
    }
    if (!lastTopologySnap) {
      $("#cpu-tbody").innerHTML =
        '<tr><td colspan="6" class="muted">No topology JSON yet — run <span class="accent-dim">topology --json</span>.</td></tr>';
      $("#row-count").textContent = "0";
      $("#m-cpus").textContent = "CPUs —";
      $("#m-numa").textContent = "NUMA —";
      $("#chart-numa").innerHTML =
        '<p class="muted small">No snapshot — run topology JSON first.</p>';
    }
    return null;
  }

  function refreshStructuredViews(stdout) {
    const snap = parseTopologyIntoUI(stdout);
    const rows = parseTSVRows(stdout);
    const host = $("#parsed-table-wrap");
    if (rows) {
      renderParsedTable(rows);
    } else if (snap) {
      if (host) {
        host.innerHTML =
          '<p class="muted small pad-table">Snapshot updated from JSON. Run <span class="accent-dim">topology matrix</span> for a CI table in this panel.</p>';
      }
    } else if (host && !host.querySelector("table")) {
      host.innerHTML =
        '<p class="muted small pad-table">No TSV rows detected in stdout.</p>';
    }
    updateDataHint(snap, !!rows);
  }

  function escapeHtml(s) {
    return s
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }

  async function copyText(text, okMsg) {
    try {
      await navigator.clipboard.writeText(text);
      appendActivity(okMsg || "copied", true);
    } catch {
      appendActivity("copy failed", false);
    }
  }

  async function runArgv(argv, opts = {}) {
    execSeq += 1;
    const seq = execSeq;
    $("#exec-count").textContent = String(seq);
    $("#m-seq").textContent = `EXEC #${seq}`;

    $("#ctx-target").textContent = `nexusflow ${argv.join(" ")}`;

    const log = $("#cli-log");
    const clientTs = isoNow();
    const t0 = performance.now();

    let res;
    try {
      const hdr = { "Content-Type": "application/json" };
      const tok = (($("#api-token") && $("#api-token").value) || "").trim();
      if (tok) hdr.Authorization = "Bearer " + tok;

      const ts = opts.timeoutSec != null ? opts.timeoutSec : readTimeoutSec();
      const payload = { argv };
      if (ts > 0) payload.timeout_sec = ts;

      res = await fetch("/api/exec", {
        method: "POST",
        headers: hdr,
        body: JSON.stringify(payload),
      });
    } catch (err) {
      const msg = String(err?.message || err);
      $("#last-http").textContent = "fetch error";
      $("#last-reqid").textContent = "—";
      $("#cli-events").textContent += `[${isoNow()}] #${seq} NETWORK · ${msg}\n`;
      log.textContent += `\n!!! #${seq} fetch failed: ${msg}\n`;
      $("#ctx-badges").innerHTML = '<span class="badge badge-alert">network</span>';
      appendActivity(`#${seq} NET FAIL ${argv[0]}`, false);
      return null;
    }

    const roundTrip = Math.round(performance.now() - t0);
    $("#last-http").textContent = `${res.status} · ${roundTrip} ms RT`;

    const raw = await res.text();
    let data;
    try {
      data = JSON.parse(raw || "{}");
    } catch {
      data = {
        stdout: "",
        stderr: raw.slice(0, 8000),
        exit_code: -1,
        duration_ms: 0,
        error: "response not JSON",
      };
    }

    lastResp = data;
    const hdrId = res.headers.get("X-Request-ID") || "";
    const reqId = data.request_id || hdrId || "—";
    $("#last-reqid").textContent = reqId;

    const sep =
      `\n══════════════════════════════════════════════════════════════════════
 exec #${seq} │ ${clientTs}
 HTTP ${res.status} │ browser RT ${roundTrip} ms │ subprocess ${data.duration_ms} ms
 request-id ${reqId}
 server_time ${data.server_time || "—"}
══════════════════════════════════════════════════════════════════════
> nexusflow ${argv.join(" ")}\n`;

    log.textContent += sep;

    const out = data.stdout || "";
    const err = data.stderr || "";
    if (out) log.textContent += out + (out.endsWith("\n") ? "" : "\n");
    if (err) log.textContent += "[stderr]\n" + err + (err.endsWith("\n") ? "" : "\n");

    let tail = `--- exit=${data.exit_code}`;
    if (data.error) tail += ` api_error=${data.error}`;
    tail += " ---\n";
    log.textContent += tail;

    $("#cli-out-stdout").textContent = out || "(empty)";
    $("#cli-out-stderr").textContent = err || "(empty)";

    const ev = $("#cli-events");
    ev.textContent += `[${isoNow()}] #${seq} HTTP ${res.status} exit=${data.exit_code} ${argv[0]} rid=${reqId}\n`;
    if (ev.textContent.length > 24000) ev.textContent = ev.textContent.slice(-24000);

    updateMetrics(argv, data);
    refreshStructuredViews(out);

    const apiErr = data.error || "";
    if (apiErr) {
      $("#ctx-badges").innerHTML = '<span class="badge badge-alert">api error</span>';
      appendActivity(`#${seq} ✗ ${argv.slice(0, 4).join(" ")} · ${apiErr.slice(0, 60)}`, false);
    } else if (!res.ok) {
      $("#ctx-badges").innerHTML = '<span class="badge badge-alert">HTTP ' + res.status + "</span>";
      appendActivity(`#${seq} ✗ HTTP ${res.status} ${argv[0]}`, false);
    } else if (data.exit_code !== 0) {
      $("#ctx-badges").innerHTML = '<span class="badge badge-alert">non-zero exit</span>';
      appendActivity(`#${seq} ✗ exit ${data.exit_code} ${argv[0]}`, false);
    } else {
      $("#ctx-badges").innerHTML = '<span class="badge badge-info">ok</span>';
      appendActivity(`#${seq} ✓ ${argv.slice(0, 4).join(" ")}`, true);
    }

    log.scrollTop = log.scrollHeight;
    ev.scrollTop = ev.scrollHeight;
    $("#cli-out-stdout").parentElement && ($("#cli-out-stdout").scrollTop = 0);

    return data;
  }

  function updateMetrics(argv, data) {
    $("#m-exit").textContent = `EXIT ${data.exit_code}`;
    $("#m-ms").textContent = `${data.duration_ms} ms`;

    const pillExit = $("#m-exit");
    pillExit.classList.remove("pill-crit", "pill-high", "pill-low");
    if (data.exit_code !== 0) pillExit.classList.add("pill-crit");
    else pillExit.classList.add("pill-low");

    $("#m-args").textContent = `ARGS ${argv.length}`;
  }

  document.querySelectorAll(".tab").forEach((tab) => {
    tab.addEventListener("click", () => {
      activateTab(tab.dataset.tab);
      if (tab.dataset.tab === "output") openOutputAndFocus();
    });
  });

  document.querySelectorAll(".subtab").forEach((st) => {
    st.addEventListener("click", () => activateSubtab(st.dataset.sub));
  });

  $("#btn-open-control").addEventListener("click", () => openControlTab());

  document.querySelectorAll("#panel-control [data-act]").forEach((btn) => {
    btn.addEventListener("click", async () => {
      const act = btn.dataset.act;
      let argv = null;
      /** @type {{ timeoutSec?: number }} */
      let opts = {};
      switch (act) {
        case "run-fill":
          argv = buildRunArgv();
          if (!argv) return;
          fillCliFromArgv(argv);
          return;
        case "run-run":
          argv = buildRunArgv();
          break;
        case "dag-fill":
          argv = buildDagArgv();
          if (!argv) return;
          fillCliFromArgv(argv);
          return;
        case "dag-run":
          argv = buildDagArgv();
          break;
        case "shm-fill":
          argv = buildShmArgv();
          if (!argv) return;
          fillCliFromArgv(argv);
          return;
        case "shm-run":
          argv = buildShmArgv();
          break;
        case "plasma-fill":
          argv = buildPlasmaArgv();
          if (!argv) return;
          fillCliFromArgv(argv);
          return;
        case "plasma-run":
          argv = buildPlasmaArgv();
          opts = { timeoutSec: Math.max(readTimeoutSec(), 600) };
          break;
        case "perf-fill":
          argv = buildPerfArgv();
          if (!argv) return;
          fillCliFromArgv(argv);
          return;
        case "perf-run":
          argv = buildPerfArgv();
          break;
        case "hp-fill":
          argv = buildHugepagesArgv();
          if (!argv) return;
          fillCliFromArgv(argv);
          return;
        case "hp-run":
          argv = buildHugepagesArgv();
          break;
        default:
          return;
      }
      if (!argv) return;
      $("#cli-input").value = argvToCliLine(argv);
      activateTab("output");
      await runArgv(argv, opts);
    });
  });

  const presetSelect = $("#preset-select");
  function currentPresetLine() {
    return (presetSelect && presetSelect.value) || "";
  }

  $("#preset-fill").addEventListener("click", () => {
    const line = currentPresetLine();
    if (!line) return;
    $("#cli-input").value = line;
    openOutputAndFocus();
  });

  $("#preset-run").addEventListener("click", async (ev) => {
    const line = currentPresetLine();
    if (!line) return;
    $("#cli-input").value = line;
    const argv = splitArgv(line.trim());
    if (!argv.length) return;
    if (!ev.shiftKey) activateTab("output");
    await runArgv(argv);
  });

  $("#btn-run").addEventListener("click", async () => {
    const argv = splitArgv($("#cli-input").value.trim());
    if (!argv.length) return;
    await runArgv(argv);
  });

  $("#cli-input").addEventListener("keydown", (e) => {
    if (e.key === "Enter") $("#btn-run").click();
  });

  $("#btn-clear").addEventListener("click", () => {
    $("#cli-log").textContent = "";
    $("#cli-events").textContent += `[${isoNow()}] transcript cleared\n`;
  });

  $("#btn-copy-stdout").addEventListener("click", () => {
    void copyText(($("#cli-out-stdout").textContent || "").replace(/^\(empty\)$/, ""), "stdout copied");
  });
  $("#btn-copy-stderr").addEventListener("click", () => {
    void copyText(($("#cli-out-stderr").textContent || "").replace(/^\(empty\)$/, ""), "stderr copied");
  });

  $("#btn-topology-json").addEventListener("click", async () => {
    await runArgv(["topology", "--json", "--source", "auto"]);
    activateTab("output");
    activateSubtab("data");
  });

  $("#btn-help-cmd").addEventListener("click", async () => {
    await runArgv(["help"]);
    activateTab("output");
    activateSubtab("transcript");
  });

  activateTab("output");
  activateSubtab("transcript");
  parseTopologyIntoUI("");
})();
