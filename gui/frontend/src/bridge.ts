// Thin wrapper around the generated Wails bindings so the rest of the app has a
// stable, typed surface even if binding output paths change.
import { Bridge } from "../bindings/github.com/thgossler/mdv/gui";
import type {
  InitInfo,
  DocumentDTO,
  LinkTargetDTO,
  DocFileDTO,
  UpdateDTO,
  LayoutDTO,
} from "../bindings/github.com/thgossler/mdv/gui/models";
import type { Backlink } from "../bindings/github.com/thgossler/mdv/internal/core/models";

export type { InitInfo, DocumentDTO, LinkTargetDTO, DocFileDTO, UpdateDTO, LayoutDTO, Backlink };

// Content-search result shapes. These mirror core.ContentMatch /
// core.DocSearchResult on the Go side. They are delivered via application
// events (not a binding return value), so they are defined here rather than
// imported from the generated bindings.
export interface ContentMatch {
  line: number;
  col: number;
  text: string;
}

export interface DocSearchResult {
  path: string;
  matches: ContentMatch[];
}

export const api = {
  init: (): Promise<InitInfo> => Bridge.Init(),
  reinit: (path: string): Promise<InitInfo> => Bridge.Reinit(path),
  read: (path: string): Promise<DocumentDTO> => Bridge.ReadDocument(path),
  resolveLink: (raw: string, dir: string): Promise<LinkTargetDTO> => Bridge.ResolveLink(raw, dir),
  resolveAsset: (src: string, dir: string): Promise<string> => Bridge.ResolveAsset(src, dir),
  openExternal: (target: string): Promise<string> => Bridge.OpenExternal(target),
  isDefaultHandler: (path: string): Promise<boolean> => Bridge.IsDefaultHandler(path),
  openNewWindow: (path: string, fragment = ""): Promise<string> =>
    Bridge.OpenInNewWindow(path, fragment),
  backlinks: (path: string): Promise<Backlink[] | null> => Bridge.Backlinks(path),
  watch: (path: string): Promise<void> => Bridge.WatchFile(path),
  refreshWorkspace: (): Promise<DocFileDTO[] | null> => Bridge.RefreshWorkspace(),
  searchContent: (query: string, gen: number): Promise<void> => Bridge.SearchContent(query, gen),
  saveLayout: (sidebarWidth: number, tocWidth: number): Promise<void> =>
    Bridge.SaveLayout(sidebarWidth, tocWidth),
  resetLayout: (): Promise<LayoutDTO> => Bridge.ResetLayout(),
  applyExcludes: (text: string, enabled: boolean): Promise<string[] | null> =>
    Bridge.ApplyExcludes(text, enabled),
  saveExtendedSyntax: (enabled: boolean): Promise<void> => Bridge.SaveExtendedSyntax(enabled),
};
