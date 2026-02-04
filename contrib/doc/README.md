# Doc Middleware

`doc` is a middleware for [vigo](https://github.com/veypi/vigo) to serve markdown documentation from `embed.FS` or local file system.

It provides recursive file listing, content retrieval, table of contents (TOC) generation, and section filtering.

## Usage

### Initialization

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
    // prefix is the path prefix in the embed.FS (e.g., "docs")
    doc.New(router.SubRouter("/docs"), mdFiles, "docs")
    
    vigo.Run()
}
```

### API Endpoints

The middleware exposes endpoints to navigate and read the documentation.

#### List Directory / Get File Content

*   **Path**: `/` or `/*path`
*   **Method**: `GET`

**Query Parameters:**

| Parameter | Type     | Default | Description                                                                 |
| :-------- | :------- | :------ | :-------------------------------------------------------------------------- |
| `depth`   | `int`    | `1`     | Directory traversal depth. `-1` for unlimited recursive listing.            |
| `toc`     | `bool`   | `false` | If `true`, returns the Table of Contents instead of full content for files. |
| `from`    | `string` | -       | Start section number (inclusive), e.g., `1.2`.                              |
| `to`      | `string` | -       | End section number (inclusive), e.g., `1.3.1`.                              |

### Examples

#### 1. List Files (Recursive)

Get all files under the root directory recursively (directories themselves are excluded from the list).

`GET /docs/?depth=-1`

#### 2. Get File Content

Get the content of `intro.md`.

`GET /docs/intro.md`

#### 3. Get Table of Contents (TOC)

Get the TOC of `guide.md`.

`GET /docs/guide.md?toc=1`

#### 4. Filter Content by Section

Get content from section `1.2` to `1.4` (inclusive).

`GET /docs/manual.md?from=1.2&to=1.4`

#### 5. Filter TOC by Section

Get the TOC for section `2` only.

`GET /docs/manual.md?toc=1&from=2&to=2`
