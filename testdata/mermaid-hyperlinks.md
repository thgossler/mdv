# Mermaid hyperlink examples

Open this document in the GUI and click the diagram nodes below:

```sh
./build/mdv testdata/mermaid-hyperlinks.md
```

HTTP(S) links should open in the default browser. Existing Markdown links should
open inside mdv, and non-Markdown local files should open with their associated
application. The missing-file node should report a broken link.

## Mermaid click statements

```mermaid
flowchart LR
  Web[Open Mermaid website]
  Readme[Open README in mdv]
  Root[Open workspace resilience README]
  License[Open LICENSE externally]
  Missing[Open missing file]

  Web --> Readme --> Root
  Readme --> License
  Readme --> Missing

  click Web href "https://mermaid.js.org/?from=mdv&feature=links" _blank
  click Readme href "../README.md#install" _blank
  click Root href "/resilience/README.md" _blank
  click License href "../LICENSE.md" _blank
  click Missing href "does-not-exist.md" _blank
```

## Links embedded in HTML labels

```mermaid
flowchart TD
  A["<a href='https://github.com/thgossler/mdv/issues?q=is%3Aissue&amp;is%3Aopen'>Open GitHub issues</a>"]
  B["<a href='../README.md#running-the-tests'>Open test instructions</a>"]
  C["<a href='../LICENSE.md'>Open the license</a>"]
  A --> B --> C
```

The two diagrams cover multiple links, HTML entities, URL query strings,
relative paths, workspace-root paths, heading fragments, and a local
non-Markdown file.

## Absolute and file URL links

Absolute paths and `file://` URLs are intentionally machine-specific. Replace
these destinations with existing files on the local system to test them:

```mermaid
flowchart LR
  Absolute[Open absolute path]
  FileURL[Open file URL]

  click Absolute href "/absolute/path/to/document.md#heading" _blank
  click FileURL href "file:///absolute/path/to/document.md#heading" _blank
```

Spaces and non-ASCII characters should be percent-encoded in file URLs, for
example:

```text
file:///Users/name/My%20Notes/Guide.md#Setup
```
