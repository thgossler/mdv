import { Events, Window } from "@wailsio/runtime";
import "katex/dist/katex.min.css";
import "./styles/themes.css";
import "./styles/app.css";
import "./styles/markdown.css";
import "./styles/alerts.css";
import "./styles/code.css";
import { api, type InitInfo, type DocFileDTO, type ContentMatch } from "./bridge";
import { render } from "./render";
import { extractFrontmatter, renderFrontmatter } from "./frontmatter";
import { renderMermaid, renderMermaidSource } from "./mermaidRunner";
import { initTheme, toggleTheme, setTheme, isDark, onThemeChange, type ThemeMode } from "./theme";
import { initZoom, zoomIn, zoomOut, zoomReset, refreshZoom } from "./zoom";
import { initSearch, showSearch, clearSearch, jumpToContentMatch } from "./search";
import { buildTOC, trackActiveHeading } from "./toc";
import { initFocusZones } from "./focuszones";
import { fuzzyPhraseMatch } from "./fuzzy";

// --- application state ------------------------------------------------------

interface HistoryEntry {
  path: string;
  // Fallback scroll offset, used only when the entry has no resolvable anchor
  // (e.g. a position above the first heading).
  scroll: number;
  // Title text of the document section nearest the top of the viewport when the
  // entry was recorded, shown in the history dropdown after a " - " separator.
  section: string;
  // Slug/id of that section heading. Restoring navigates to this anchor by id so
  // the position stays correct even after the document is re-rendered or edited;
  // the scroll offset is only a fallback when no anchor is available.
  anchor: string;
}

let info: InitInfo;
let workspace: DocFileDTO[] = [];
let currentPath = "";
let currentDir = "";
let history: HistoryEntry[] = [];
let forward: HistoryEntry[] = [];
let labelMode: "filename" | "title" = "filename";
let detachScrollSpy: (() => void) | null = null;
// The current document's filename and resolved title, kept so the toolbar title
// can switch between them when the nav-panel label mode is toggled.
let currentDocName = "";
let currentDocTitle = "";

// Extended ("character-stealing") inline Markdown syntax (math, sub/sup, mark,
// ins). Off by default; toggled live from the toolbar and persisted in
// state.jsonc. `lastRendered` keeps the current document's source so toggling
// can re-render it without a round-trip to the backend.
let extendedSyntax = false;
let lastRendered: { markdown: string; name: string; path: string } | null = null;

// --- content-search state ---------------------------------------------------
// When enabled, the navigator filter box searches document *content* (not just
// names): each matching document is shown with its in-document matches nested
// beneath it. `searchGen` discards results from superseded queries; results
// stream in via application events keyed by that generation.
let contentSearchMode = false;
let searchGen = 0;
let searchKeywords: string[] = [];
const searchResults = new Map<string, ContentMatch[]>();
let searchDebounce: number | undefined;
// Coalesces the (potentially rapid) streamed search results into at most one
// nav-list rebuild per animation frame so the main thread stays responsive to
// typing while results pour in.
let searchRenderRaf: number | undefined;

// --- navigator exclusion state ----------------------------------------------
// Gitignore-style patterns hide documents/folders from the navigator. The set
// of excluded absolute paths is computed by the backend (which owns the
// matching + persistence) and cached here so render passes can filter cheaply.
// `excludeEnabled` mirrors the checkbox; when off the set is empty.
let excludeEnabled = false;
let excludedPaths = new Set<string>();
let excludeDebounce: number | undefined;

const $ = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T;

const els = {
  content: $("content"),
  contentWrap: $<HTMLElement>("content-wrap"),
  toolbar: $<HTMLElement>("toolbar"),
  sidebar: $("sidebar"),
  navList: $("nav-list"),
  navFilter: $<HTMLInputElement>("nav-filter"),
  btnContentSearch: $<HTMLButtonElement>("btn-content-search"),
  navExclude: $<HTMLTextAreaElement>("nav-exclude"),
  excludeEnabled: $<HTMLInputElement>("exclude-enabled"),
  sidebarFoot: $<HTMLElement>("sidebar-foot"),
  toc: $("toc"),
  tocList: $("toc-list"),
  backlinksList: $("backlinks-list"),
  docTitle: $("doc-title"),
  statusLeft: $("status-left"),
  statusMid: $("status-mid"),
  statusRight: $("status-right"),
  btnBack: $<HTMLButtonElement>("btn-back"),
  btnForward: $<HTMLButtonElement>("btn-forward"),
  btnHistory: $<HTMLButtonElement>("btn-history"),
  btnExtended: $<HTMLButtonElement>("btn-extended"),
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
  extendedSyntax = info.extendedSyntax === true;
  els.btnExtended.classList.toggle("active", extendedSyntax);
  applyLayout(info.layout);
  initTheme((info.config.theme as ThemeMode) || "system");
  onThemeChange(() => rerenderMermaidForTheme());

  initZoom({
    min: info.config.minZoom || 0.5,
    max: info.config.maxZoom || 3,
    step: info.config.zoomStep || 0.1,
    onChange: (pct) => (els.statusRight.textContent = `${pct}%`),
  });
  $("btn-zoom-reset").addEventListener("click", () => zoomReset());
  $("btn-reset-layout").addEventListener("click", () => void resetLayout());

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
  wireResizers();
  // The navigator is most useful when browsing a folder, so start it collapsed
  // when a single file was opened and expanded when a folder was given. Done
  // before the UI is revealed so the sidebar never flashes the wrong state.
  els.sidebar.classList.toggle("collapsed", info.kind === "file");
  // Layout (panel widths) is now applied; reveal the UI so the panes never
  // flash at their default width before jumping to the persisted size.
  $("app").classList.remove("layout-pending");
  initFocusZones({
    navFilter: els.navFilter,
    navList: els.navList,
    contentWrap: els.contentWrap,
    tocList: els.tocList,
    backlinksList: els.backlinksList,
  });
  wireMenuEvents();
  wireContextMenu();
  wireLiveReload();
  wireFileDrop();

  if (info.update?.available) {
    els.statusMid.innerHTML = `Update ${escapeHtml(info.update.latest)} available - <a href="#" id="upd-link">download</a>`;
    $("upd-link")?.addEventListener("click", (e) => {
      e.preventDefault();
      api.openExternal(info.update.downloadUrl);
    });
  }

  if (info.kind === "file") {
    await openDocument(info.path, false);
    if (info.fragment) scrollToSlug(info.fragment);
  } else {
    showFolderWelcome();
  }
}

// --- rendering --------------------------------------------------------------

async function openDocument(
  path: string,
  pushHistory: boolean,
  opts: { focusContent?: boolean } = {}
): Promise<void> {
  const doc = await api.read(path);
  if (doc.error) {
    els.content.innerHTML = `<div class="error">Cannot open <code>${escapeHtml(path)}</code>: ${escapeHtml(
      doc.error
    )}</div>`;
    return;
  }

  if (pushHistory && currentPath) {
    history.push(makeEntry());
    // A fresh navigation invalidates the forward stack, matching browser behaviour.
    forward = [];
  }
  currentPath = doc.path;
  currentDir = doc.dir;
  api.watch(currentPath);

  await renderInto(doc.markdown, doc.name, path);
  els.contentWrap.scrollTop = 0;
  // Focus the content view so plain navigation keys (Home/End) are delivered to
  // it right after opening a document, without requiring a click first. Skip
  // this when the caller wants to keep keyboard focus where it is (e.g. opening
  // from the document navigator, so the user can keep arrowing through the list).
  if (opts.focusContent !== false) {
    els.contentWrap.focus({ preventScroll: true });
  }
  updateChrome();
  highlightActiveNav();
}

async function renderInto(markdown: string, name: string, path: string): Promise<void> {
  clearSearch();
  lastRendered = { markdown, name, path };

  // `.mmd` files render as a standalone diagram.
  if (/\.mmd$/i.test(path)) {
    await renderMermaidSource(els.content, markdown, isDark());
    currentDocName = name;
    currentDocTitle = name;
    applyDocTitle();
    refreshZoom();
    return;
  }

  const fm = extractFrontmatter(markdown);
  const result = render(fm.body, extendedSyntax);
  els.content.innerHTML = renderFrontmatter(fm.data) + result.html;

  currentDocName = name;
  currentDocTitle = chooseTitle(name, result.headings[0]?.text, fm.data);
  applyDocTitle();

  await postProcess(result.headings);
}

async function postProcess(headings: { level: number; text: string; slug: string }[]): Promise<void> {
  injectHeadingAnchors();
  resolveWikilinks();
  void resolveAssets();
  fillAdoTocPlaceholders(headings);
  wireCodeCopy();
  await renderMermaid(els.content, isDark());
  refreshZoom();

  buildTOC(els.tocList, headings, (slug) => scrollToSlug(slug, true));
  wireTocContextMenus();
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

// resolveAssets rewrites local/relative media references (images, <source>,
// video posters) to data URIs served by the Go bridge. The embedded webview
// asset server only serves the compiled frontend, so relative filesystem paths
// would otherwise fail to load. Absolute URLs (http(s), data:) are left as-is.
async function resolveAssets(): Promise<void> {
  const nodes = Array.from(
    els.content.querySelectorAll<HTMLElement>("img[src], source[src], video[poster]")
  );
  await Promise.all(
    nodes.map(async (el) => {
      const attr = el.tagName === "VIDEO" ? "poster" : "src";
      const ref = el.getAttribute(attr) || "";
      if (!ref || /^[a-z][a-z0-9+.-]+:/i.test(ref) || ref.startsWith("//") || ref.startsWith("#")) {
        return; // absolute URL, data URI or empty - nothing to resolve
      }
      try {
        const dataUri = await api.resolveAsset(ref, currentDir);
        if (dataUri) el.setAttribute(attr, dataUri);
      } catch {
        /* leave the original reference in place on failure */
      }
    })
  );
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
      history.push(makeEntry());
      forward = [];
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
  // Record the current position so Forward can return here.
  forward.push(makeEntry());
  // Same-document entries (in-page navigation) just restore the position.
  if (entry.path === currentPath) {
    scrollToEntry(entry, true);
    updateChrome();
    return;
  }
  void openDocument(entry.path, false, { focusContent: false }).then(() => {
    scrollToEntry(entry, false);
  });
}

function goForward(): void {
  const entry = forward.pop();
  if (!entry) return;
  // Record the current position so Back can return here.
  history.push(makeEntry());
  // Same-document entries (in-page navigation) just restore the position.
  if (entry.path === currentPath) {
    scrollToEntry(entry, true);
    updateChrome();
    return;
  }
  void openDocument(entry.path, false, { focusContent: false }).then(() => {
    scrollToEntry(entry, false);
  });
}

// makeEntry captures the current document position as a history entry, anchored
// to the nearest section heading so it survives later re-renders of the document.
function makeEntry(): HistoryEntry {
  const h = activeHeading();
  return {
    path: currentPath,
    scroll: els.contentWrap.scrollTop,
    section: h ? headingText(h) : "",
    anchor: h?.id ?? "",
  };
}

// scrollToEntry restores a history entry's position. It prefers the recorded
// section anchor (resolved by id, so it stays correct after the document
// changes) and falls back to the stored scroll offset when no anchor resolves.
function scrollToEntry(entry: HistoryEntry, smooth: boolean): void {
  const behavior: ScrollBehavior = smooth ? "smooth" : "auto";
  if (entry.anchor) {
    const target = els.content.querySelector<HTMLElement>(`[id="${cssEscape(entry.anchor)}"]`);
    if (target) {
      target.scrollIntoView({ behavior, block: "start" });
      return;
    }
  }
  els.contentWrap.scrollTo({ top: entry.scroll, behavior });
}

// --- sidebar ----------------------------------------------------------------

function buildSidebar(): void {
  renderNav(visibleWorkspace());
  els.navFilter.addEventListener("input", onNavFilterInput);
  // Escape clears the filter so the full document list is shown again.
  els.navFilter.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && els.navFilter.value !== "") {
      e.preventDefault();
      e.stopPropagation();
      els.navFilter.value = "";
      onNavFilterInput();
    }
  });
  els.btnContentSearch.addEventListener("click", toggleContentSearch);
  wireContentSearchEvents();
  wireExcludeField();
}

// --- navigator exclusion ----------------------------------------------------

// visibleWorkspace returns the document list with excluded entries removed.
// `excludedPaths` is empty whenever exclusion is disabled, so this is a no-op
// in that case.
function visibleWorkspace(): DocFileDTO[] {
  if (excludedPaths.size === 0) return workspace;
  return workspace.filter((d) => !excludedPaths.has(d.path));
}

// wireExcludeField seeds the exclusion controls from persisted layout state and
// wires the textarea (debounced) and the enable checkbox.
function wireExcludeField(): void {
  els.navExclude.value = info.layout.excludePatterns || "";
  excludeEnabled = !!info.layout.excludeEnabled;
  els.excludeEnabled.checked = excludeEnabled;
  updateExcludeDisabled();
  els.navExclude.addEventListener("input", scheduleApplyExcludes);
  els.excludeEnabled.addEventListener("change", () => {
    excludeEnabled = els.excludeEnabled.checked;
    updateExcludeDisabled();
    void applyExcludes();
  });
  // Compute the initial excluded set (and re-render) only when there is work to
  // do, so a clean startup avoids a needless backend round-trip and state write.
  if (excludeEnabled && els.navExclude.value.trim() !== "") {
    void applyExcludes();
  }
}

// updateExcludeDisabled dims the field when exclusion is switched off.
function updateExcludeDisabled(): void {
  els.sidebarFoot.classList.toggle("disabled", !excludeEnabled);
}

// scheduleApplyExcludes debounces textarea edits so rapid typing triggers a
// single re-evaluation, mirroring the 500ms filter/search debounce.
function scheduleApplyExcludes(): void {
  if (excludeDebounce !== undefined) clearTimeout(excludeDebounce);
  excludeDebounce = window.setTimeout(() => void applyExcludes(), 500);
}

// applyExcludes pushes the current patterns + enabled flag to the backend
// (which persists them and returns the excluded absolute paths) and refreshes
// the navigator view to reflect the new set.
async function applyExcludes(): Promise<void> {
  if (excludeDebounce !== undefined) {
    clearTimeout(excludeDebounce);
    excludeDebounce = undefined;
  }
  const list = (await api.applyExcludes(els.navExclude.value, excludeEnabled)) || [];
  excludedPaths = new Set(list);
  refreshNav();
}

// refreshNav rebuilds the navigator using the active mode and current filters.
function refreshNav(): void {
  if (contentSearchMode) {
    renderSearchNav();
  } else {
    renderNav(currentFilter());
  }
  // While the folder welcome is showing (no document open yet), keep its
  // excluded/visible counts in sync as the ignore patterns change.
  if (!currentPath && info.kind !== "file") {
    showFolderWelcome();
  }
}

// appendExcludeRule adds a gitignore rule from the context menu, enabling the
// filters so the action takes immediate effect, scrolling to reveal the new
// entry, and re-evaluating after the standard debounce.
function appendExcludeRule(rule: string): void {
  const ta = els.navExclude;
  const existing = ta.value.split(/\r?\n/).map((l) => l.trim());
  if (!existing.includes(rule)) {
    const trimmed = ta.value.replace(/\s+$/, "");
    ta.value = (trimmed ? trimmed + "\n" : "") + rule + "\n";
  }
  if (!excludeEnabled) {
    excludeEnabled = true;
    els.excludeEnabled.checked = true;
    updateExcludeDisabled();
  }
  // Scroll to the bottom so the freshly added rule is visible.
  ta.scrollTop = ta.scrollHeight;
  scheduleApplyExcludes();
}

// onNavFilterInput routes the navigator filter box to either name filtering or
// content search depending on the active mode.
function onNavFilterInput(): void {
  if (contentSearchMode) {
    scheduleContentSearch();
  } else {
    renderNav(currentFilter());
  }
}

// toggleContentSearch flips between name-filter and content-search modes,
// updating the toggle button, the input placeholder and the navigator view.
function toggleContentSearch(): void {
  contentSearchMode = !contentSearchMode;
  els.btnContentSearch.classList.toggle("active", contentSearchMode);
  els.btnContentSearch.title = contentSearchMode
    ? "Search document content (on)"
    : "Search document content (off)";
  els.navFilter.placeholder = contentSearchMode
    ? "Search document content…"
    : "Filter documents…";
  // Cancel any in-flight results and reset the view for the new mode.
  searchGen++;
  searchResults.clear();
  searchKeywords = [];
  cancelSearchRender();
  if (contentSearchMode) {
    onNavFilterInput();
    els.navFilter.focus();
  } else {
    renderNav(currentFilter());
  }
}
// splitKeywords lowercases and splits the query into distinct space-separated
// keywords, mirroring the backend's AND-per-document semantics.
function splitKeywords(query: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const w of query.toLowerCase().split(/\s+/)) {
    if (w && !seen.has(w)) {
      seen.add(w);
      out.push(w);
    }
  }
  return out;
}

// scheduleContentSearch debounces input so rapid typing issues a single search.
function scheduleContentSearch(): void {
  if (searchDebounce !== undefined) clearTimeout(searchDebounce);
  searchDebounce = window.setTimeout(runContentSearch, 500);
}

function runContentSearch(): void {
  const keywords = splitKeywords(els.navFilter.value);
  searchKeywords = keywords;
  searchGen++;
  searchResults.clear();
  cancelSearchRender();
  if (keywords.length === 0) {
    renderSearchNav();
    return;
  }
  // Show filename-qualifying documents immediately while content results stream.
  renderSearchNav();
  void api.searchContent(els.navFilter.value, searchGen);
}

// wireContentSearchEvents subscribes to the streaming results emitted by the
// backend, discarding any from a superseded query generation.
function wireContentSearchEvents(): void {
  Events.On("content-search:result", (ev: { data: { gen: number; result: { path: string; matches: ContentMatch[] } } }) => {
    if (!contentSearchMode || ev.data.gen !== searchGen) return;
    searchResults.set(ev.data.result.path, ev.data.result.matches || []);
    scheduleSearchRender();
  });
  Events.On("content-search:done", (ev: { data: { gen: number; count: number } }) => {
    if (!contentSearchMode || ev.data.gen !== searchGen) return;
    scheduleSearchRender();
  });
}

// scheduleSearchRender coalesces streamed result events into a single nav-list
// rebuild per animation frame. Rebuilding the whole list on every event would
// thrash the main thread and block typing while results stream in.
function scheduleSearchRender(): void {
  if (searchRenderRaf !== undefined) return;
  searchRenderRaf = requestAnimationFrame(() => {
    searchRenderRaf = undefined;
    renderSearchNav();
  });
}

// cancelSearchRender drops a pending coalesced render (used when a fresh search
// supersedes the streaming results of a previous one).
function cancelSearchRender(): void {
  if (searchRenderRaf !== undefined) {
    cancelAnimationFrame(searchRenderRaf);
    searchRenderRaf = undefined;
  }
}

// nameQualifies reports whether a document's name/title matches the current
// content-search query as a fuzzy phrase, mirroring the content-search rules.
function nameQualifies(d: DocFileDTO): boolean {
  const q = els.navFilter.value.trim();
  if (!q) return false;
  return fuzzyPhraseMatch(`${d.name} ${d.title || ""}`, q);
}

// renderSearchNav renders the content-search view: documents that have content
// matches OR whose name/title contains all keywords, in workspace order, each
// with its matches nested beneath.
function renderSearchNav(): void {
  if (searchKeywords.length === 0) {
    // Empty query: show the full document list (no matches).
    renderNav(visibleWorkspace());
    return;
  }
  entryPointPath = computeEntryPoint(visibleWorkspace());
  // Build the rows off-DOM and swap them in once to minimise reflow.
  const frag = document.createDocumentFragment();
  for (const d of visibleWorkspace()) {
    const matches = searchResults.get(d.path);
    const qualifies = (matches && matches.length > 0) || nameQualifies(d);
    if (!qualifies) continue;
    frag.appendChild(makeNavItem(d));
    if (matches) {
      for (const m of matches) frag.appendChild(makeNavMatch(d, m));
    }
  }
  els.navList.replaceChildren(frag);
  highlightActiveNav();
}

// entryPointPath holds the single most-likely documentation landing page in
// the current workspace (see computeEntryPoint). It is emphasised in the
// navigator so a reader immediately sees where to start.
let entryPointPath = "";

// Candidate file names and typical documentation folders, each in descending
// priority order. A root-level page always wins; otherwise the best depth-1
// page inside one of these folders is chosen.
const ENTRY_POINT_NAMES = ["readme.md", "index.md", "home.md"];
const ENTRY_POINT_FOLDERS = ["docs", "doc", "documentation", "wiki"];

// computeEntryPoint returns the path of the single most-probable entry point,
// or "" when none qualifies. Root README/index/home pages take precedence; if
// none exist at the root, a matching page directly inside a typical docs folder
// (depth 1 only) is used. Anything deeper never qualifies.
function computeEntryPoint(items: DocFileDTO[]): string {
  let best = "";
  let bestScore = Infinity;
  for (const d of items) {
    const lower = (d.rel || d.name).replace(/^\/+/, "").toLowerCase();
    const base = lower.slice(lower.lastIndexOf("/") + 1);
    const nameRank = ENTRY_POINT_NAMES.indexOf(base);
    if (nameRank < 0) continue;
    const slash = lower.indexOf("/");
    let score: number;
    if (slash < 0) {
      // Root level: highest priority, ordered by file-name rank.
      score = nameRank;
    } else if (lower.indexOf("/", slash + 1) < 0) {
      // Depth 1: only inside a recognised documentation folder.
      const folderRank = ENTRY_POINT_FOLDERS.indexOf(lower.slice(0, slash));
      if (folderRank < 0) continue;
      score = 10 + folderRank * ENTRY_POINT_NAMES.length + nameRank;
    } else {
      continue; // deeper than depth 1 never qualifies
    }
    if (score < bestScore) {
      bestScore = score;
      best = d.path;
    }
  }
  return best;
}

// makeNavItem builds a document entry anchor (shared by both nav views).
function makeNavItem(d: DocFileDTO): HTMLAnchorElement {
  const a = document.createElement("a");
  a.className = "nav-item";
  if (d.path === entryPointPath) a.classList.add("entry");
  a.tabIndex = -1;
  a.dataset.path = d.path;
  a.textContent =
    labelMode === "title" && d.title ? d.title : d.rel || d.name;
  a.title = d.rel || d.path;
  a.addEventListener("click", (e) => {
    e.preventDefault();
    a.focus();
    void openDocument(d.path, true, { focusContent: false });
  });
  a.addEventListener("contextmenu", (e) => {
    const rel = (d.rel || d.name).replace(/^\/+/, "");
    const items: MenuItem[] = [
      { label: "Copy", fn: () => copyText(a.textContent || "") },
      { label: "Open in New Window", fn: () => api.openNewWindow(d.path) },
      { label: "Exclude file", fn: () => appendExcludeRule("/" + rel) },
    ];
    const slash = rel.lastIndexOf("/");
    if (slash > 0) {
      const folder = rel.slice(0, slash);
      items.push({ label: "Exclude folder", fn: () => appendExcludeRule("/" + folder + "/") });
    }
    // Offer "root folder" only when it differs from the immediate parent, i.e.
    // the document is nested at least two levels deep (otherwise it would add
    // the exact same rule as "Exclude folder").
    const firstSlash = rel.indexOf("/");
    if (firstSlash > 0 && firstSlash !== slash) {
      const root = rel.slice(0, firstSlash);
      items.push({ label: "Exclude root folder", fn: () => appendExcludeRule("/" + root + "/") });
    }
    openMenu(e, items);
  });
  return a;
}

// makeNavMatch builds an indented content-match row that opens its document and
// jumps to the matching source line.
function makeNavMatch(d: DocFileDTO, m: ContentMatch): HTMLAnchorElement {
  const a = document.createElement("a");
  a.className = "nav-match";
  a.tabIndex = -1;
  a.dataset.path = d.path;
  a.dataset.line = String(m.line);
  a.textContent = m.text;
  a.title = m.text;
  a.addEventListener("click", (e) => {
    e.preventDefault();
    const keywords = searchKeywords.slice();
    const line = m.line;
    // When the document is already open (e.g. arrowing between its matches),
    // just move the highlight instead of re-reading and re-rendering it.
    if (d.path === currentPath) {
      jumpToContentMatch(keywords, line);
      return;
    }
    void openDocument(d.path, true, { focusContent: false }).then(() => {
      jumpToContentMatch(keywords, line);
    });
  });
  return a;
}

function renderNav(items: DocFileDTO[]): void {
  entryPointPath = computeEntryPoint(visibleWorkspace());
  els.navList.innerHTML = "";
  for (const d of items) {
    els.navList.appendChild(makeNavItem(d));
  }
  highlightActiveNav();
}

function highlightActiveNav(): void {
  for (const a of Array.from(els.navList.querySelectorAll<HTMLElement>(".nav-item"))) {
    a.classList.toggle("active", a.dataset.path === currentPath);
  }
}

function showFolderWelcome(): void {
  const total = workspace.length;
  const excluded = excludeEnabled ? excludedPaths.size : 0;
  const visible = total - excluded;
  // With exclusion active the count reflects how many documents remain visible
  // versus how many were hidden; otherwise the plain total reads more clearly.
  const summary =
    excluded > 0
      ? `${visible} of ${total} markdown document${total === 1 ? "" : "s"} shown - ${excluded} excluded by ignore pattern${excluded === 1 ? "" : "s"}.`
      : `${total} markdown document${total === 1 ? "" : "s"} in this folder.`;
  els.content.innerHTML = `<div class="welcome"><h1>${escapeHtml(info.appName)}</h1>
    <p>${summary}</p>
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
    item.tabIndex = -1;
    item.innerHTML = `<div class="backlink-name">${escapeHtml(
      labelMode === "title" && bl.sourceTitle ? bl.sourceTitle : bl.sourceName
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
  els.btnForward.disabled = forward.length === 0;
  els.btnHistory.disabled = history.length === 0;
  els.statusLeft.textContent = currentPath;
}

function wireToolbar(): void {
  els.btnBack.addEventListener("click", goBack);
  els.btnForward.addEventListener("click", goForward);
  els.btnHistory.addEventListener("click", toggleHistoryMenu);
  $("btn-sidebar").addEventListener("click", () => els.sidebar.classList.toggle("collapsed"));
  $("btn-toc").addEventListener("click", () => {
    const hidden = els.toc.classList.toggle("hidden");
    $("toc-resizer").classList.toggle("hidden", hidden);
  });
  $("btn-theme").addEventListener("click", () => toggleTheme());
  $("btn-labels").addEventListener("click", toggleLabels);
  $("btn-mono").addEventListener("click", () => document.body.classList.toggle("mono"));
  $("btn-width").addEventListener("click", toggleContentWidth);
  els.btnExtended.addEventListener("click", () => void toggleExtendedSyntax());

  // Double-clicking the title-bar area performs the OS window action (zoom on
  // macOS, maximise/restore elsewhere), matching native window behaviour.
  els.toolbar.addEventListener("dblclick", (e) => {
    if ((e.target as HTMLElement).closest("button, input, a, .history-menu")) return;
    void titleBarAction();
  });

  // macOS only: emulate the standard "drag a maximized window to restore it"
  // gesture. The native window drag (performWindowDragWithEvent) keeps the
  // maximized size, so we take over dragging while maximized.
  els.toolbar.addEventListener("mousedown", onTitleBarMouseDown);
}

// wireResizers makes the navigation (left) and contents (right) panes
// drag-resizable so long filenames, document titles or section headers stay
// readable. The widths drive the `--sidebar-width` / `--toc-width` CSS custom
// properties and are persisted to state.jsonc (debounced on the Go side) so the
// layout is restored on the next launch. Double-clicking a splitter resets that
// pane to the stylesheet default.
function wireResizers(): void {
  const root = document.documentElement;
  // Capture the stylesheet defaults once so a double-click can restore them.
  const defaults = getComputedStyle(root);
  const defaultSidebar = defaults.getPropertyValue("--sidebar-width").trim() || "260px";
  const defaultToc = defaults.getPropertyValue("--toc-width").trim() || "260px";

  const MIN = 150;
  const MAX = 600;
  const clamp = (px: number) => Math.max(MIN, Math.min(MAX, px));

  const setup = (
    resizerId: string,
    cssVar: string,
    measure: (el: HTMLElement) => number,
    delta: (startWidth: number, dx: number) => number,
    reset: string,
  ): void => {
    const resizer = $(resizerId);
    const pane = resizer === $("sidebar-resizer") ? els.sidebar : els.toc;

    resizer.addEventListener("pointerdown", (e: PointerEvent) => {
      if (e.button !== 0) return;
      e.preventDefault();
      const startX = e.clientX;
      const startWidth = measure(pane);
      resizer.setPointerCapture(e.pointerId);
      resizer.classList.add("dragging");
      document.body.classList.add("pane-resizing");

      const onMove = (ev: PointerEvent) => {
        root.style.setProperty(cssVar, `${clamp(delta(startWidth, ev.clientX - startX))}px`);
      };
      const onUp = (ev: PointerEvent) => {
        resizer.releasePointerCapture(ev.pointerId);
        resizer.classList.remove("dragging");
        document.body.classList.remove("pane-resizing");
        resizer.removeEventListener("pointermove", onMove);
        resizer.removeEventListener("pointerup", onUp);
        persistPanelWidths();
      };
      resizer.addEventListener("pointermove", onMove);
      resizer.addEventListener("pointerup", onUp);
    });

    // Double-click restores this pane to its default width.
    resizer.addEventListener("dblclick", () => {
      root.style.setProperty(cssVar, reset);
      persistPanelWidths();
    });
  };

  // Sidebar splitter sits to the right of the pane: dragging right widens it.
  setup(
    "sidebar-resizer",
    "--sidebar-width",
    (el) => el.getBoundingClientRect().width,
    (start, dx) => start + dx,
    defaultSidebar,
  );
  // Contents splitter sits to the left of the pane: dragging left widens it.
  setup(
    "toc-resizer",
    "--toc-width",
    (el) => el.getBoundingClientRect().width,
    (start, dx) => start - dx,
    defaultToc,
  );
}

// panelWidthPx reads a `--sidebar-width` / `--toc-width` custom property as an
// integer pixel value (the properties are always stored in px).
function panelWidthPx(cssVar: string): number {
  const v = getComputedStyle(document.documentElement).getPropertyValue(cssVar);
  return Math.round(parseFloat(v)) || 0;
}

// persistPanelWidths sends the current side-panel widths to the backend, which
// debounces the write so this can be called freely after each drag/reset.
function persistPanelWidths(): void {
  void api.saveLayout(panelWidthPx("--sidebar-width"), panelWidthPx("--toc-width"));
}

// applyLayout sets the persisted side-panel widths before the UI is revealed so
// the panes appear at their stored size without flashing the default first.
function applyLayout(layout: InitInfo["layout"]): void {
  if (!layout) return;
  const root = document.documentElement;
  if (layout.sidebarWidth > 0) root.style.setProperty("--sidebar-width", `${layout.sidebarWidth}px`);
  if (layout.tocWidth > 0) root.style.setProperty("--toc-width", `${layout.tocWidth}px`);
}

// resetLayout restores the window to its default size and the side panels to
// their default widths, persisting the result via the backend.
async function resetLayout(): Promise<void> {
  const layout = await api.resetLayout();
  const root = document.documentElement;
  root.style.setProperty("--sidebar-width", `${layout.sidebarWidth}px`);
  root.style.setProperty("--toc-width", `${layout.tocWidth}px`);
}


// titleBarAction mirrors the platform's title-bar double-click: it toggles the
// window between maximized and its previous size.
async function titleBarAction(): Promise<void> {
  try {
    if (!isMac) {
      await Window.ToggleMaximise();
      return;
    }
    if (winMaximized) await restoreWindow();
    else await maximizeWindow();
  } catch {
    /* window control unavailable; ignore */
  }
}

// --- maximize state + drag-to-restore (macOS) ------------------------------
// On macOS the native title-bar drag does not restore a maximized window, so we
// track the maximized state ourselves and, when the user starts dragging a
// maximized window, shrink it back to its previous size under the cursor and
// then follow the pointer - matching standard OS behaviour.
const isMac = navigator.userAgent.includes("Macintosh");
let winMaximized = false;
let preMaxBounds: { x: number; y: number; w: number; h: number } | null = null;
let dragGrab: { offX: number; offY: number } | null = null;
let dragRAF = 0;
let dragNext: { x: number; y: number } | null = null;

async function readBounds(): Promise<{ x: number; y: number; w: number; h: number }> {
  const [p, s] = await Promise.all([Window.Position(), Window.Size()]);
  return { x: p.x, y: p.y, w: s.width, h: s.height };
}

// reflectDragRegion enables the native Wails drag region only while the window
// is not maximized; when maximized we drive dragging manually so the native
// drag does not fight our restore logic.
function reflectDragRegion(): void {
  if (winMaximized) els.toolbar.style.setProperty("--wails-draggable", "no-drag");
  else els.toolbar.style.removeProperty("--wails-draggable");
}

async function maximizeWindow(): Promise<void> {
  preMaxBounds = await readBounds();
  await Window.Maximise();
  winMaximized = true;
  reflectDragRegion();
}

async function restoreWindow(): Promise<void> {
  if (preMaxBounds) {
    await Window.SetSize(preMaxBounds.w, preMaxBounds.h);
    await Window.SetPosition(preMaxBounds.x, preMaxBounds.y);
  } else {
    await Window.UnMaximise();
  }
  winMaximized = false;
  reflectDragRegion();
}

// onTitleBarMouseDown starts watching for a drag gesture on a maximized window.
// It only restores once the pointer actually moves, so a plain click or the
// first click of a double-click still toggles via titleBarAction.
function onTitleBarMouseDown(e: MouseEvent): void {
  if (!isMac || !winMaximized || e.button !== 0) return;
  if ((e.target as HTMLElement).closest("button, input, a, .history-menu")) return;

  const start = { cx: e.clientX, cy: e.clientY, sx: e.screenX, sy: e.screenY };
  let armed = true;
  const cleanup = () => {
    armed = false;
    window.removeEventListener("mousemove", move, true);
    window.removeEventListener("mouseup", up, true);
  };
  const move = (me: MouseEvent) => {
    if (!armed) return;
    if (Math.abs(me.screenX - start.sx) < 4 && Math.abs(me.screenY - start.sy) < 4) return;
    cleanup();
    void beginRestoreDrag(start, me);
  };
  const up = () => cleanup();
  window.addEventListener("mousemove", move, true);
  window.addEventListener("mouseup", up, true);
}

// beginRestoreDrag shrinks the maximized window back to its previous size,
// repositioned so the cursor stays at the same relative spot on the title bar,
// then follows the pointer until the mouse button is released.
async function beginRestoreDrag(
  start: { cx: number; cy: number; sx: number; sy: number },
  me: MouseEvent,
): Promise<void> {
  const maxW = window.innerWidth || 1;
  const target = preMaxBounds ?? { x: 0, y: 0, w: 1100, h: 780 };
  const fracX = Math.min(Math.max(start.cx / maxW, 0), 1);
  const offX = Math.round(fracX * target.w);
  const offY = Math.min(start.cy, Math.max(target.h - 1, 0));

  await Window.SetSize(target.w, target.h);
  const px = Math.round(me.screenX - offX);
  const py = Math.round(me.screenY - offY);
  await Window.SetPosition(px, py);
  winMaximized = false;
  reflectDragRegion();

  dragGrab = { offX, offY };
  dragNext = { x: px, y: py };
  window.addEventListener("mousemove", onDragMove, true);
  window.addEventListener("mouseup", onDragUp, true);
}

function onDragMove(e: MouseEvent): void {
  if (!dragGrab) return;
  dragNext = { x: Math.round(e.screenX - dragGrab.offX), y: Math.round(e.screenY - dragGrab.offY) };
  if (!dragRAF) {
    dragRAF = requestAnimationFrame(() => {
      dragRAF = 0;
      if (dragNext) void Window.SetPosition(dragNext.x, dragNext.y);
    });
  }
}

function onDragUp(): void {
  window.removeEventListener("mousemove", onDragMove, true);
  window.removeEventListener("mouseup", onDragUp, true);
  if (dragRAF) {
    cancelAnimationFrame(dragRAF);
    dragRAF = 0;
  }
  if (dragNext) {
    void Window.SetPosition(dragNext.x, dragNext.y);
    dragNext = null;
  }
  dragGrab = null;
}

function toggleLabels(): void {
  labelMode = labelMode === "title" ? "filename" : "title";
  if (contentSearchMode) {
    renderSearchNav();
  } else {
    renderNav(currentFilter());
  }
  applyDocTitle();
  void loadBacklinks();
}

// applyDocTitle sets the toolbar/window title to either the current document's
// filename or its resolved title, following the nav-panel label mode. The title
// is taken from the workspace entry so it matches the navigator exactly (a
// document whose title could not be detected shows its filename in both places);
// documents outside the workspace fall back to the per-document detected title.
function applyDocTitle(): void {
  if (!currentDocName) return;
  const detectedTitle = workspace.find((d) => d.path === currentPath)?.title ?? currentDocTitle;
  els.docTitle.textContent =
    labelMode === "title" && detectedTitle ? detectedTitle : currentDocName;
}

// toggleContentWidth switches between the readable, width-limited layout and a
// full-window-width layout. The active state is reflected on the toolbar button.
function toggleContentWidth(): void {
  const full = document.body.classList.toggle("full-width");
  const btn = $("btn-width");
  btn.classList.toggle("active", full);
  btn.title = full ? "Limit content width" : "Toggle full width";
}

// toggleExtendedSyntax flips the opt-in "extended" inline Markdown syntax
// (math, sub/sup, highlight, inserted), re-renders the current document with
// the new setting, and persists the choice to state.jsonc so it is restored on
// the next launch (and shared with the terminal UI).
async function toggleExtendedSyntax(): Promise<void> {
  extendedSyntax = !extendedSyntax;
  els.btnExtended.classList.toggle("active", extendedSyntax);
  void api.saveExtendedSyntax(extendedSyntax);
  if (lastRendered) {
    const scroll = els.contentWrap.scrollTop;
    await renderInto(lastRendered.markdown, lastRendered.name, lastRendered.path);
    els.contentWrap.scrollTop = scroll;
  }
}

function currentFilter(): DocFileDTO[] {
  const base = visibleWorkspace();
  const q = els.navFilter.value.trim();
  if (!q) return base;
  return base.filter(
    (d) => fuzzyPhraseMatch(d.name, q) || fuzzyPhraseMatch(d.title || "", q)
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
    item.textContent = entry.section
      ? `${baseName(entry.path)} - ${entry.section}`
      : baseName(entry.path);
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
  on("menu:forward", goForward);
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
    const reload = {
      label: "Reload page",
      fn: () => {
        if (currentPath) void openDocument(currentPath, false);
      },
    };
    if (a && isExternalLink(a)) {
      const text = (a.textContent || "").trim();
      const url = externalHref(a);
      openMenu(e, [
        { label: "Copy", fn: () => copyText(text) },
        { label: "Copy hyperlink", fn: () => copyText(url) },
        {
          label: "Open in New Window",
          fn: () => currentPath && api.openNewWindow(currentPath),
        },
        reload,
      ]);
      return;
    }
    const sel = window.getSelection()?.toString() || "";
    openMenu(e, [{ label: "Copy", fn: () => copyText(sel) }, reload]);
  });
  document.addEventListener("click", () => els.contextMenu.classList.add("hidden"));
}

// wireTocContextMenus attaches a custom context menu to each in-page nav entry,
// replacing the default OS menu shown on the underlying anchors.
function wireTocContextMenus(): void {
  for (const a of Array.from(els.tocList.querySelectorAll<HTMLAnchorElement>(".toc-item"))) {
    a.addEventListener("contextmenu", (e) =>
      openMenu(e, [
        { label: "Copy", fn: () => copyText(a.textContent || "") },
        {
          label: "Open in New Window",
          fn: () => currentPath && api.openNewWindow(currentPath, a.dataset.slug || ""),
        },
      ])
    );
  }
}

interface MenuItem {
  label: string;
  fn: () => void;
}

// openMenu renders a custom context menu from the supplied items at the cursor.
function openMenu(e: MouseEvent, items: MenuItem[]): void {
  e.preventDefault();
  const menu = els.contextMenu;
  menu.innerHTML = "";
  if (items.length === 0) return;
  for (const it of items) {
    const el = document.createElement("div");
    el.className = "ctx-item";
    el.textContent = it.label;
    el.addEventListener("click", () => {
      it.fn();
      menu.classList.add("hidden");
    });
    menu.appendChild(el);
  }
  menu.style.left = `${e.clientX}px`;
  menu.style.top = `${e.clientY}px`;
  menu.classList.remove("hidden");
}

function copyText(text: string): void {
  if (text) void navigator.clipboard.writeText(text);
}

// isExternalLink reports whether an anchor points outside the workspace (web,
// mail or external file), as opposed to an internal markdown/anchor link.
function isExternalLink(a: HTMLAnchorElement): boolean {
  const kind = a.dataset.resolvedKind;
  if (kind === "http" || kind === "mailto" || kind === "file") return true;
  const href = a.getAttribute("href") || "";
  return /^(https?:|mailto:|ftp:|file:)/i.test(href);
}

// externalHref returns the actual URL behind an external anchor.
function externalHref(a: HTMLAnchorElement): string {
  const kind = a.dataset.resolvedKind;
  if (a.dataset.resolved && (kind === "http" || kind === "mailto" || kind === "file")) {
    return a.dataset.resolved;
  }
  return a.getAttribute("href") || "";
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

// wireFileDrop listens for files/folders dropped onto the window (delivered by
// the Go side, which alone can resolve real OS paths) and reopens mdv on the
// dropped item in place.
function wireFileDrop(): void {
  Events.On("files:dropped", (ev: { data: string }) => {
    const path = ev?.data;
    if (typeof path === "string" && path) void reopenInput(path);
  });
}

// reopenInput re-initialises the viewer with a newly dropped file or folder.
// The backend re-resolves the input and returns a fresh workspace; live UI
// settings (theme, zoom, panel widths, label mode, exclusions, window geometry,
// sidebar visibility) are intentionally left untouched so the switch feels
// seamless rather than a restart. Only content-scoped state (navigation
// history, the active search) is reset for the new input.
async function reopenInput(path: string): Promise<void> {
  const next = await api.reinit(path);
  if (!next || (next.kind === "file" && !next.path)) return;
  info = next;
  workspace = next.workspace ?? [];

  history = [];
  resetContentSearch();
  clearSearch();
  els.navFilter.value = "";
  currentPath = "";
  currentDir = next.dir;

  // Recompute the navigator exclusion set against the new workspace; the
  // user's patterns (a live setting) are preserved and reapplied.
  excludedPaths = new Set();
  if (excludeEnabled && els.navExclude.value.trim() !== "") {
    await applyExcludes();
  } else {
    renderNav(visibleWorkspace());
  }

  if (next.kind === "file") {
    await openDocument(next.path, false);
    if (next.fragment) scrollToSlug(next.fragment);
  } else {
    // A dropped folder is meant for browsing, so make sure the navigator is
    // visible even if it was collapsed for the previously viewed file.
    els.sidebar.classList.remove("collapsed");
    showFolderWelcome();
    updateChrome();
    highlightActiveNav();
  }
}

// resetContentSearch returns the navigator to plain name-filter mode and drops
// any streamed content-search results, so a freshly opened input starts clean.
function resetContentSearch(): void {
  searchGen++;
  searchResults.clear();
  searchKeywords = [];
  cancelSearchRender();
  if (contentSearchMode) {
    contentSearchMode = false;
    els.btnContentSearch.classList.remove("active");
    els.btnContentSearch.title = "Search document content (off)";
    els.navFilter.placeholder = "Filter documents…";
  }
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

// activeHeading returns the heading element nearest the top of the content
// viewport, used to anchor history entries to a document section. Returns null
// when no heading sits at or above the viewport top (e.g. the document intro).
function activeHeading(): HTMLElement | null {
  const headings = Array.from(
    els.content.querySelectorAll<HTMLElement>("h1[id],h2[id],h3[id],h4[id],h5[id],h6[id]")
  );
  let active: HTMLElement | null = null;
  const top = els.contentWrap.getBoundingClientRect().top + 80;
  for (const h of headings) {
    if (h.getBoundingClientRect().top <= top) active = h;
    else break;
  }
  return active;
}

// headingText extracts a heading's plain text, excluding the injected "#"
// hover-anchor link.
function headingText(h: HTMLElement): string {
  const clone = h.cloneNode(true) as HTMLElement;
  clone.querySelector(".heading-anchor")?.remove();
  return clone.textContent?.trim() ?? "";
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
  } else if (
    e.key === "Backspace" &&
    !e.ctrlKey &&
    !e.metaKey &&
    !e.altKey &&
    !isTypingTarget(e.target)
  ) {
    // Backspace anywhere (outside editable fields, where it deletes text)
    // navigates the content view back to the previous history entry;
    // Shift+Backspace navigates forward again.
    e.preventDefault();
    if (e.shiftKey) goForward();
    else goBack();
  }
});

// Clicking anywhere in the content view (outside the search field) gives it
// keyboard focus so the navigation keys above are delivered to it.
els.contentWrap.addEventListener("pointerdown", (e) => {
  if (!isTypingTarget(e.target)) {
    els.contentWrap.focus({ preventScroll: true });
  }
});

// isTypingTarget reports whether the event originates from an editable field,
// where Home/End must keep their normal caret behavior.
function isTypingTarget(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null;
  if (!el) return false;
  const tag = el.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable;
}

void boot();
