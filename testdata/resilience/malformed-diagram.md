# Malformed diagram resilience

A broken Mermaid diagram must not stop the rest of this document from rendering.

## Before the diagram

This paragraph appears **above** the diagram and must always render.

## A broken diagram

```mermaid
graph TD
  A --> B -->
  this is not valid mermaid ((( 
```

## A valid diagram

```mermaid
graph LR
  Start --> Middle --> End
```

## After the diagram

This paragraph appears **below** the diagrams and must always render, even
though the first diagram is broken. In the GUI the broken diagram should show a
"Mermaid error" placeholder while the valid one renders normally.
