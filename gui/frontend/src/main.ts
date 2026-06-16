import { Events, Window } from "@wailsio/runtime";
import "katex/dist/katex.min.css";
import "./styles/themes.css";
import "./styles/app.css";
import "./styles/markdown.css";
import "./styles/alerts.css";
import "./styles/code.css";
import { api, type InitInfo, type DocFileDTO } from "./bridge";
import { render } from "./render";
import { extractFrontmatter, renderFrontmatter } from "./frontmatter";
import { renderMermaid, renderMermaidSource } from "./mermaidRunner";
import { initTheme, toggleTheme, setTheme, isDark, onThemeChange, type ThemeMode } from "./theme";
import { initZoom, zoomIn, zoomOut, zoomReset } from "./zoom";
import { initSearch, showSearch, clearSearch } from "./search";
import { buildTOC, trackActiveHeading } from "./toc";

// --- application state ------------------------------------------------------

interface HistoryEntry {
  path: string;
  scroll: number;
}

let info: InitInfo;
let workspace: DocFileDTO[] = [];
let currentPath = "";
let currentDir = "";
let history: HistoryEntry[] = [];
let labelMode: "filename" | "title" = "filename";
let detachScrollSpy: (() => void) | null = null;

const $ = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T;

const els = {
  content: $("content"),
  contentWrap: $<HTMLElement>("content-wrap"),
  toolbar: $<HTMLElement>("toolbar"),
  sidebar: $("sidebar"),
  navList: $("nav-list"),
  navFilter: $<HTMLInputElement>("nav-filter"),
  toc: $("toc"),
  tocList: $("toc-list"),
  backlinksList: $("backlinks-list"),
  docTitle: $("doc-title"),
  statusLeft: $("status-left"),
  statusMid: $("status-mid"),
  statusRight: $("status-right"),
  btnBack: $<HTMLButtonElement>("btn-back"),
  btnHistory: $<HTMLButtonElement>("btn-history"),
  historyMenu: $("history-menu"),
  contextMenu: $("context-menu"),
  searchBar: $("search-bar"),
  searchInput: $<HTMLInputElement>("search-input"),
};

// --- bootstrap --------------------------------------------------------------

async function boot(): Promise<void> {
  info = await api.init();
  workspace = info.workspace ?? [];
  labelMode = info.config.navLabelMode === "title" ? "title" : "filename";

  applyConfigStyles();
  initTheme((info.config.theme as ThemeMode) || "system");
  onThemeChange(() => rerenderMermaidForTheme());

  initZoom({
    min: info.config.minZoom || 0.5,
    max: info.config.maxZoom || 3,
    step: info.config.zoomStep || 0.1,
    onChange: (pct) => (els.statusRight.textContent = `${pct}%`),
  });

  initSearch({
    bar: els.searchBar,
    input: els.searchInput,
    count: $("search-count"),
    next: $("search-next"),
    prev: $("search-prev"),
    close: $("search-close"),
  });

  buildSidebar();
  wireToolbar();
  wireMenuEvents();
  wireContextMenu();
  wireLiveReload();

  if (info.update?.available) {
    els.statusMid.innerHTML = `Update ${escapeHtml(info.update.latest)} available — <a href="#" id="upd-link">download</a>`;
    $("upd-link")?.addEventListener("click", (e) => {
      e.preventDefault();
      api.openExternal(info.update.downloadUrl);
    });
  }

  if (info.kind === "file") {
    await openDocument(info.path, false);
  } else {
    showFolderWelcome();
  }
}

// --- rendering --------------------------------------------------------------

async function openDocument(path: string, pushHistory: boolean): Promise<void> {
  const doc = await api.read(path);
  if (doc.error) {
    els.content.innerHTML = `<div class="error">Cannot open <code>${escapeHtml(path)}</code>: ${escapeHtml(
      doc.error
    )}</div>`;
    return;
  }

  if (pushHistory && currentPath) {
    history.push({ path: currentPath, scroll: els.contentWrap.scrollTop });
  }
  currentPath = doc.path;
  currentDir = doc.dir;
  api.watch(currentPath);

  await renderInto(doc.markdown, doc.name, path);
  els.contentWrap.scrollTop = 0;
  updateChrome();
  highlightActiveNav();
}

async function renderInto(markdown: string, name: string, path: string): Promise<void> {
  clearSearch();

  // `.mmd` files render as a standalone diagram.
  if (/\.mmd$/i.test(path)) {
    await renderMermaidSource(els.content, markdown, isDark());
    els.docTitle.textContent = name;
    return;
  }

  const fm = extractFrontmatter(markdown);
  const result = render(fm.body);
  els.content.innerHTML = renderFrontmatter(fm.data) + result.html;

  els.docTitle.textContent = chooseTitle(name, result.headings[0]?.text, fm.data);

  await postProcess(result.headings);
}

async function postProcess(headings: { level: number; text: string; slug: string }[]): Promise<void> {
  injectHeadingAnchors();
  resolveWikilinks();
  fillAdoTocPlaceholders(headings);
  wireCodeCopy();
  await renderMermaid(els.content, isDark());

  buildTOC(els.tocList, headings, (slug) => scrollToSlug(slug, true));
  detachScrollSpy?.();
  detachScrollSpy = trackActiveHeading(els.contentWrap, els.tocList);

  await loadBacklinks();
}

function rerenderMermaidForTheme(): void {
  // Re-render diagrams to match the new theme by reloading the current doc.
  if (currentPath) void openDocument(currentPath, false);
}

// --- wikilinks & ado toc ----------------------------------------------------

function injectHeadingAnchors(): void {
  for (const h of Array.from(
    els.content.querySelectorAll<HTMLElement>("h1[id],h2[id],h3[id],h4[id],h5[id],h6[id]")
  )) {
    if (h.querySelector(".heading-anchor")) continue;
    const a = document.createElement("a");
    a.className = "heading-anchor";
    a.href = `#${h.id}`;
    a.textContent = "#";
    a.setAttribute("aria-hidden", "true");
    h.insertBefore(a, h.firstChild);
  }
}

async function resolveWikilinks(): Promise<void> {
  const links = Array.from(els.content.querySelectorAll<HTMLAnchorElement>("a[data-wikilink]"));
  for (const a of links) {
    const target = a.getAttribute("data-wikilink") || "";
    const res = await api.resolveLink(`[[${target}]]`, currentDir);
    if (res.kind === "broken") {
      a.classList.add("wikilink-broken");
      a.title = `Unresolved: ${target}`;
    } else {
      a.dataset.resolvedKind = res.kind;
      a.dataset.resolved = res.resolved;
      a.dataset.fragment = res.fragment;
      a.title = res.display;
    }
  }
}

function fillAdoTocPlaceholders(headings: { level: number; text: string; slug: string }[]): void {
  const holders = Array.from(els.content.querySelectorAll<HTMLElement>("[data-ado-toc]"));
  if (holders.length === 0) return;
  const html = headings
    .map(
      (h) =>
        `<a class="inline-toc-item" href="#${h.slug}" style="margin-left:${(h.level - 1) * 12}px">${escapeHtml(
          h.text
        )}</a>`
    )
    .join("");
  for (const el of holders) {
    el.innerHTML = `<div class="inline-toc">${html}</div>`;
    el.removeAttribute("data-ado-toc");
  }
}

// --- navigation -------------------------------------------------------------

els.content.addEventListener("click", async (e) => {
  const a = (e.target as HTMLElement).closest("a") as HTMLAnchorElement | null;
  if (!a) return;
  e.preventDefault();

  // Wikilink resolved earlier.
  if (a.hasAttribute("data-wikilink")) {
    if (a.classList.contains("wikilink-broken")) return;
    const kind = a.dataset.resolvedKind;
    if (kind === "markdown" || kind === "wikilink") {
      await openDocument(a.dataset.resolved!, true);
      if (a.dataset.fragment) scrollToSlug(a.dataset.fragment);
    } else if (kind === "anchor") {
      scrollToSlug((a.dataset.resolved || "").replace(/^#/, ""), true);
    }
    return;
  }

  const href = a.getAttribute("href") || "";
  if (!href) return;

  if (href.startsWith("#")) {
    scrollToSlug(href.slice(1), true);
    return;
  }

  const res = await api.resolveLink(href, currentDir);
  switch (res.kind) {
    case "markdown":
      await openDocument(res.resolved, true);
      if (res.fragment) scrollToSlug(res.fragment);
      break;
    case "anchor":
      scrollToSlug(res.resolved.replace(/^#/, ""), true);
      break;
    case "http":
    case "mailto":
    case "file":
      await api.openExternal(res.resolved);
      break;
    case "broken":
      flashStatus(`Broken link: ${res.raw}`);
      break;
  }
});

// Hover shows the resolved target in the status bar (Chrome-like).
els.content.addEventListener("mouseover", async (e) => {
  const a = (e.target as HTMLElement).closest("a") as HTMLAnchorElement | null;
  if (!a) return;
  if (a.hasAttribute("data-wikilink")) {
    els.statusLeft.textContent = a.title || a.getAttribute("data-wikilink") || "";
    return;
  }
  const href = a.getAttribute("href") || "";
  if (href.startsWith("#")) {
    els.statusLeft.textContent = href;
    return;
  }
  els.statusLeft.textContent = href;
});
els.content.addEventListener("mouseout", () => (els.statusLeft.textContent = currentPath));

function scrollToSlug(slug: string, record = false): void {
  const target = els.content.querySelector<HTMLElement>(`[id="${cssEscape(slug)}"]`);
  if (target) {
    // Record the current position so Back returns to the previous section.
    if (record && currentPath) {
      history.push({ path: currentPath, scroll: els.contentWrap.scrollTop });
      updateChrome();
    }
    target.scrollIntoView({ behavior: "smooth", block: "start" });
    target.classList.add("anchor-flash");
    setTimeout(() => target.classList.remove("anchor-flash"), 1200);
  } else {
    flashStatus(`Anchor not found: #${slug}`);
  }
}

function goBack(): void {
  const entry = history.pop();
  if (!entry) return;
  // Same-document entries (in-page navigation) just restore the scroll position.
  if (entry.path === currentPath) {
    els.contentWrap.scrollTo({ top: entry.scroll, behavior: "smooth" });
    updateChrome();
    return;
  }
  void openDocument(entry.path, false).then(() => {
    els.contentWrap.scrollTop = entry.scroll;
  });
}

// --- sidebar ----------------------------------------------------------------

function buildSidebar(): void {
  renderNav(workspace);
  els.navFilter.addEventListener("input", () => {
    const q = els.navFilter.value.toLowerCase();
    renderNav(
      workspace.filter(
        (d) => d.name.toLowerCase().includes(q) || (d.title || "").toLowerCase().includes(q)
      )
    );
  });
}

function renderNav(items: DocFileDTO[]): void {
  els.navList.innerHTML = "";
  for (const d of items) {
    const a = document.createElement("a");
    a.className = "nav-item";
    a.dataset.path = d.path;
    a.textContent = labelMode === "title" && d.title ? d.title : d.name;
    a.title = d.rel || d.path;
    a.addEventListener("click", (e) => {
      e.preventDefault();
      void openDocument(d.path, true);
    });
    a.addEventListener("contextmenu", (e) => showContextMenu(e, d.path, true));
    els.navList.appendChild(a);
  }
  highlightActiveNav();
}

function highlightActiveNav(): void {
  for (const a of Array.from(els.navList.querySelectorAll<HTMLElement>(".nav-item"))) {
    a.classList.toggle("active", a.dataset.path === currentPath);
  }
}

function showFolderWelcome(): void {
  const count = workspace.length;
  els.content.innerHTML = `<div class="welcome"><h1>${escapeHtml(info.appName)}</h1>
    <p>${count} markdown document${count === 1 ? "" : "s"} in this folder.</p>
    <p class="muted">Select a document from the sidebar to begin.</p></div>`;
  els.docTitle.textContent = info.dir;
  els.tocList.innerHTML = "";
  els.backlinksList.innerHTML = "";
}

// --- backlinks --------------------------------------------------------------

async function loadBacklinks(): Promise<void> {
  if (!currentPath) {
    els.backlinksList.innerHTML = "";
    return;
  }
  const links = (await api.backlinks(currentPath)) || [];
  if (links.length === 0) {
    els.backlinksList.innerHTML = '<div class="toc-empty">None</div>';
    return;
  }
  els.backlinksList.innerHTML = "";
  for (const bl of links) {
    const item = document.createElement("a");
    item.className = "backlink-item";
    item.innerHTML = `<div class="backlink-name">${escapeHtml(
      bl.sourceTitle || bl.sourceName
    )}</div><div class="backlink-snippet">${escapeHtml(bl.snippet)}</div>`;
    item.addEventListener("click", (e) => {
      e.preventDefault();
      void openDocument(bl.sourcePath, true);
    });
    els.backlinksList.appendChild(item);
  }
}

// --- toolbar / chrome -------------------------------------------------------

function updateChrome(): void {
  els.btnBack.disabled = history.length === 0;
  els.btnHistory.disabled = history.length === 0;
  els.statusLeft.textContent = currentPath;
}

function wireToolbar(): void {
  els.btnBack.addEventListener("click", goBack);
  els.btnHistory.addEventListener("click", toggleHistoryMenu);
  $("btn-sidebar").addEventListener("click", () => els.sidebar.classList.toggle("collapsed"));
  $("btn-toc").addEventListener("click", () => els.toc.classList.toggle("hidden"));
  $("btn-theme").addEventListener("click", () => toggleTheme());
  $("btn-labels").addEventListener("click", toggleLabels);
  $("btn-mono").addEventListener("click", () => document.body.classList.toggle("mono"));

  // Double-clicking the title-bar area performs the OS window action (zoom on
  // macOS, maximise/restore elsewhere), matching native window behaviour.
  els.toolbar.addEventListener("dblclick", (e) => {
    if ((e.target as HTMLElement).closest("button, input, a, .history-menu")) return;
    void titleBarAction();
  });
}

// titleBarAction mirrors the platform's title-bar double-click: macOS uses the
// native zoom, other platforms toggle maximise.
async function titleBarAction(): Promise<void> {
  try {
    if (navigator.userAgent.includes("Macintosh")) {
      await Window.Zoom();
    } else {
      await Window.ToggleMaximise();
    }
  } catch {
    /* window control unavailable; ignore */
  }
}

function toggleLabels(): void {
  labelMode = labelMode === "title" ? "filename" : "title";
  renderNav(currentFilter());
}

function currentFilter(): DocFileDTO[] {
  const q = els.navFilter.value.toLowerCase();
  if (!q) return workspace;
  return workspace.filter(
    (d) => d.name.toLowerCase().includes(q) || (d.title || "").toLowerCase().includes(q)
  );
}

function toggleHistoryMenu(): void {
  const menu = els.historyMenu;
  menu.classList.toggle("hidden");
  if (menu.classList.contains("hidden")) return;
  menu.innerHTML = "";
  for (let i = history.length - 1; i >= 0; i--) {
    const entry = history[i];
    const item = document.createElement("div");
    item.className = "history-item";
    item.textContent = baseName(entry.path);
    item.addEventListener("click", () => {
      history = history.slice(0, i);
      menu.classList.add("hidden");
      void openDocument(entry.path, false);
    });
    menu.appendChild(item);
  }
}

// --- menu + context menu ----------------------------------------------------

function wireMenuEvents(): void {
  const on = (name: string, fn: () => void) => Events.On(name, fn);
  on("menu:back", goBack);
  on("menu:reload", () => currentPath && openDocument(currentPath, false));
  on("menu:toggle-sidebar", () => els.sidebar.classList.toggle("collapsed"));
  on("menu:toggle-toc", () => els.toc.classList.toggle("hidden"));
  on("menu:toggle-backlinks", () => els.toc.classList.toggle("hidden"));
  on("menu:toggle-theme", () => toggleTheme());
  on("menu:toggle-labels", toggleLabels);
  on("menu:toggle-mono", () => document.body.classList.toggle("mono"));
  on("menu:zoom-in", zoomIn);
  on("menu:zoom-out", zoomOut);
  on("menu:zoom-reset", zoomReset);
  on("menu:find", () => showSearch(els.searchBar, els.searchInput));
  on("menu:new-window", () => currentPath && api.openNewWindow(currentPath));
}

function wireContextMenu(): void {
  els.content.addEventListener("contextmenu", (e) => {
    const a = (e.target as HTMLElement).closest("a") as HTMLAnchorElement | null;
    let mdPath = "";
    if (a) {
      if (a.dataset.resolvedKind === "markdown" || a.dataset.resolvedKind === "wikilink") {
        mdPath = a.dataset.resolved || "";
      }
    }
    showContextMenu(e, mdPath, !!mdPath);
  });
  document.addEventListener("click", () => els.contextMenu.classList.add("hidden"));
}

function showContextMenu(e: MouseEvent, mdPath: string, canOpenWindow: boolean): void {
  e.preventDefault();
  const menu = els.contextMenu;
  menu.innerHTML = "";
  const add = (label: string, fn: () => void) => {
    const item = document.createElement("div");
    item.className = "ctx-item";
    item.textContent = label;
    item.addEventListener("click", () => {
      fn();
      menu.classList.add("hidden");
    });
    menu.appendChild(item);
  };

  const sel = window.getSelection()?.toString();
  if (sel) add("Copy", () => navigator.clipboard.writeText(sel));
  if (canOpenWindow && mdPath) add("Open in New Window", () => api.openNewWindow(mdPath));
  add("Reload", () => currentPath && openDocument(currentPath, false));

  menu.style.left = `${e.clientX}px`;
  menu.style.top = `${e.clientY}px`;
  menu.classList.remove("hidden");
}

function wireCodeCopy(): void {
  for (const btn of Array.from(els.content.querySelectorAll<HTMLElement>(".code-copy"))) {
    btn.addEventListener("click", () => {
      const code = btn.parentElement?.querySelector("code")?.textContent || "";
      navigator.clipboard.writeText(code);
      btn.textContent = "Copied";
      setTimeout(() => (btn.textContent = "Copy"), 1200);
    });
  }
}

function wireLiveReload(): void {
  Events.On("file:changed", (ev: { data: string }) => {
    if (ev.data === currentPath) {
      const scroll = els.contentWrap.scrollTop;
      void openDocument(currentPath, false).then(() => (els.contentWrap.scrollTop = scroll));
    }
  });
}

// --- helpers ----------------------------------------------------------------

function applyConfigStyles(): void {
  const c = info.config;
  const root = document.documentElement;
  if (c.fontFamily) root.style.setProperty("--font-family", c.fontFamily);
  if (c.fontSizePx) root.style.setProperty("--font-size", `${c.fontSizePx}px`);
  if (c.lineHeight) root.style.setProperty("--line-height", String(c.lineHeight));
  if (c.contentWidthPx) root.style.setProperty("--content-width", `${c.contentWidthPx}px`);
  if (c.monospace) document.body.classList.add("mono");
  if (c.codeTheme) document.body.dataset.codeTheme = c.codeTheme.toLowerCase().replace(/\s+/g, "-");
}

function chooseTitle(name: string, h1?: string, fm?: Record<string, unknown> | null): string {
  const fmTitle = fm && typeof fm.title === "string" ? fm.title : "";
  return fmTitle || h1 || name;
}

function flashStatus(msg: string): void {
  els.statusMid.textContent = msg;
  setTimeout(() => (els.statusMid.textContent = ""), 2500);
}

function baseName(p: string): string {
  return p.split(/[\\/]/).pop() || p;
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function cssEscape(s: string): string {
  return s.replace(/["\\]/g, "\\$&");
}

// Keyboard shortcuts not covered by the native menu.
document.addEventListener("keydown", (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key === "f") {
    e.preventDefault();
    showSearch(els.searchBar, els.searchInput);
  } else if (e.key === "Escape") {
    els.contextMenu.classList.add("hidden");
    els.historyMenu.classList.add("hidden");
  }
});

void boot();
