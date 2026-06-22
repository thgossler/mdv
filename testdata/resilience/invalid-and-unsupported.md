# Invalid markdown and unsupported syntax

The renderer should degrade gracefully and keep showing the surrounding content.

## Unbalanced inline markup

This *is **broken _markup [with](unclosed and ` a stray backtick that never ends.

## Invalid / unsafe HTML soup

<div><span><p>unclosed tags <b>bold <img src= <<<>>> &notanentity; <script>
alert('this script must be stripped')</script>

## Unsupported custom directive

:::some-unknown-extension {with="options"}
body of an extension mdv does not implement
:::

## Unterminated code fence

```go
func main() {
    fmt.Println("the fence is never closed")
```

## Still here

This final paragraph must always render. One unsupported extension or malformed
block must not prevent the rest of the document from rendering.
