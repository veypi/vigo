# Doc Middleware

`doc` is a middleware for [vigo](https://github.com/veypi/vigo) that serves embedded markdown documentation as a browsable SPA. It is built on top of the `ufs` module.

## Usage

```go
package main

import (
    "embed"
    "github.com/veypi/vigo"
    "github.com/veypi/vigo/contrib/doc"
)

//go:embed docs/*.md docs/**/*.md
var mdFiles embed.FS

func main() {
    router := vigo.NewRouter()

    // Mount the doc handler
    doc.New(router.SubRouter("/docs"), mdFiles, "docs")

    vigo.Run()
}
```

## How it works

- Non-`.md` requests return `index.html`, a single-page application for browsing docs.
- `.md` file requests return the raw file content (plain text).
- The SPA calls the same endpoint with `X-No-Fallback: true` header and `?depth=N` parameter to get JSON directory listings for building the file tree.
- Search uses ufs's `Searcher` interface: `?glob=` for file matching, `?pattern=` for content regex.
- File content is served raw when `Accept` does not contain `text/html` (e.g., curl), and as the SPA page when it does (browser).
- The SPA switches between directory tree (empty search) and content search results (typing in the search box).

## Options

```go
doc.New(router, mdFiles, "docs", &doc.DocOptions{
    MaxDepth:     5,                     // max directory depth (default 5)
    CacheControl: "public, max-age=0",   // Cache-Control header
})
```

## API

| Path | Description |
|------|-------------|
| `GET /docs/` | Returns `index.html` SPA |
| `GET /docs/readme.md` | Returns file content (plain text) |
| `GET /docs/?depth=5` + `X-No-Fallback` | JSON directory tree |
| `GET /docs/?glob=**/*.md&pattern=keyword&limit=30` + `X-No-Fallback` | JSON content search results |
