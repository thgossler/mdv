// Thin wrapper around the generated Wails bindings so the rest of the app has a
// stable, typed surface even if binding output paths change.
import { Bridge } from "../bindings/github.com/thgossler/mdv/gui";
import type {
  InitInfo,
  DocumentDTO,
  LinkTargetDTO,
  DocFileDTO,
  UpdateDTO,
} from "../bindings/github.com/thgossler/mdv/gui/models";
import type { Backlink } from "../bindings/github.com/thgossler/mdv/internal/core/models";

export type { InitInfo, DocumentDTO, LinkTargetDTO, DocFileDTO, UpdateDTO, Backlink };

export const api = {
  init: (): Promise<InitInfo> => Bridge.Init(),
  read: (path: string): Promise<DocumentDTO> => Bridge.ReadDocument(path),
  resolveLink: (raw: string, dir: string): Promise<LinkTargetDTO> => Bridge.ResolveLink(raw, dir),
  resolveAsset: (src: string, dir: string): Promise<string> => Bridge.ResolveAsset(src, dir),
  openExternal: (target: string): Promise<string> => Bridge.OpenExternal(target),
  openNewWindow: (path: string, fragment = ""): Promise<string> =>
    Bridge.OpenInNewWindow(path, fragment),
  backlinks: (path: string): Promise<Backlink[] | null> => Bridge.Backlinks(path),
  watch: (path: string): Promise<void> => Bridge.WatchFile(path),
};
