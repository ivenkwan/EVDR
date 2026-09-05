# EVDR Secure Viewer — core service (`src/services/viewer`)

Wave 3 viewer workstream. Go implementation of the platform's core compliance
property (ADR-0001 view-first DRM): **pages are streamed one at a time; the
full document never crosses the HTTP boundary; the watermark is baked
server-side into the bytes served.** All code in this tree is stdlib-only —
the repo root `go.mod` is untouched.

Traceability: FR-3.1/3.2/3.3 (viewer + watermark), FR-1.3 (watermark token
selection), FR-3.4/TR-4.3 (office conversion seam), TR-4.1/4.2 (page streaming
+ server-side watermark), TR-4.6 (preset plumbing). Wave scope per Todo.md.

---

## Layout

```
src/services/viewer/
├── README.md
├── doc.go                 package overview + wiring example
├── renderer.go            the adapter-facing render pipeline (Renderer)
├── renderer_test.go
├── pdf/                   dependency-free PDF slicing engine
│   ├── pdf.go             public API: Parse, PageCount, ExtractPage
│   ├── parse.go           classic-xref loader + object parser
│   ├── token.go           PDF tokenizer/scanner
│   ├── write.go           deterministic serializer + xref writer
│   ├── stamp.go           StampContent + per-page resource API
│   ├── pdf_test.go
│   └── testpdf/           deterministic in-memory PDF fixture generator
│                          (test support only — not imported by prod code)
├── watermark/             server-side watermark compositors
│   ├── watermark.go       Compositor interface, FR-1.3 token line, escaping
│   ├── pdf.go             PDF compositor (content-stream stamping)
│   ├── text.go            text/plain compositor
│   └── watermark_test.go
├── converter/             office→PDF seam (FR-3.4/TR-4.3)
│   ├── converter.go       Converter interface + LibreOffice stub
│   └── converter_test.go
└── server/                page-streaming HTTP API
    ├── server.go          routes, identity seam, watermark token resolution
    ├── errors.go          SPI-error → HTTP-status mapping
    └── server_test.go     integration suite over the memadapter-backed SPI
```

## How the pieces meet the invariants

The Room SPI (`src/spi`, contract v0.1.0 — read-only) transports
**already-rendered pages**: adapters stream one immutable document version and
delegate rendering to their configured `Renderer` (see `src/spi/README.md`,
"Rendering (GetRenderStream)"). This tree implements the viewer side of that
boundary and the HTTP side that consumes it:

```
doc version content (PDF | text | office)
   │
   ▼  Renderer.Render  (viewer/renderer.go)
   │   • PDF:  pdf.Parse → ExtractPage(page n)   → single-page PDF, per request
   │   • text: one page per non-empty line
   │   • office: converter.Converter seam (worker lab-deferred) → PDF path
   │   • every page: watermark compositor rewrites the page bytes
   ▼
spi.RenderStream — forward-only; Next() honours ctx cancellation
   │                       (the SPI transports one page at a time — FR-3.1)
   ▼
viewer HTTP server (server/) — consumes spi.RoomSPI.GetRenderStream
   GET /v1/viewer/rooms/{roomID}/documents/{documentID}/pages/{page}
   GET /v1/viewer/rooms/{roomID}/documents/{documentID}/pages   → page count
   GET /v1/healthz
```

Production wiring: construct `viewer.NewRenderer(...)`, hand it to the
adapter as its renderer (e.g. `nextcloud.Config{Renderer: renderer}`), and
point a `server.New(...)` at the SPI whose adapter carries that renderer —
both halves then enforce the same property from either side of the SPI
boundary. The HTTP layer also fails closed: if a render stream ever yields
more than the single requested page, the request is aborted before any bytes
are written (adapter contract violation).

**No whole-document endpoint exists by design** — the test
`TestNoWholeDocumentEndpointExists` pins that routes like
`/documents/{id}` and `/documents/{id}/content` return 404.

## Watermarking (FR-3.2, TR-4.2)

- Compositors implement `watermark.Compositor.Apply(ctx, spi.WatermarkSpec,
  contentType, page) ([]byte, error)` and **rewrite the payload bytes** —
  never a client-side overlay, never a side channel.
- `watermark.PDF` stamps a rotated identity band into the page **content
  stream** (font = standard base-14 Helvetica, alpha via an ExtGState
  resource, no font embedding), so the tokens are part of the rendered
  output.
- Tokens follow FR-1.3: viewer identity, timestamp (UTC RFC3339), client
  IP/domain, session ID. The HTTP layer resolves them from the authenticated
  request context (never from client-supplied tenant/identity values); an
  empty spec is a no-op by design — the policy engine upstream decides when
  watermarking is mandatory (FR-3.2/TR-5.x).
- Deterministic: identical (spec, page) ⇒ identical bytes.
- An ASCII sanitizer plus PDF literal-string escaping keeps hostile
  identities from corrupting or escaping the content stream.

## PDF engine scope (`pdf/`)

A deliberately small, dependency-free subset engine (documented limitation):

- classic (non-stream) xref tables; unencrypted documents; object streams /
  xref streams unsupported → `ErrUnsupported`;
- stream filters: none or `/FlateDecode` (via stdlib `compress/zlib`);
- `ExtractPage` keeps only the objects reachable from the requested page and
  the catalog (sibling pages pruned from the page tree; trailer `/Info`
  metadata dropped), and renumbers/writes a fresh valid PDF;
- `StampContent` handles pages whose `/Contents` is one stream object (or an
  array whose last element is one); inline/multiple-stream layouts →
  `ErrLayoutUnsupported`.

Real-world PDFs from mainstream writers (LibreOffice, Chrome print, qpdf)
fall inside the subset for ordinary page content. A full PDF library can
replace this package behind the same surface later without touching the
renderer or HTTP layers. Fixtures for the suites are generated in-memory by
`pdf/testpdf` (hand-written PDF syntax + xref) — no binary assets, no network.

## Identity & access control posture (`server/`)

- The service is bound to **one tenant at construction** (`Config.TenantID`),
  mirroring the per-tenant adapter topology (TR-2.2). `X-EVDR-Principal` (and
  optional `X-EVDR-Actor` / `X-EVDR-Session` / `X-EVDR-Display-Name` headers)
  carry the gateway-authenticated principal that a future Keycloak/JWT
  middleware populates; **a tenant_id is never read from the client** (SR-2.2).
- Every request resolves through `RoomSPI.GetRenderStream`, whose adapters
  enforce the actor's grant on the document's owning room **before any content
  is fetched**; revoked/expired grants take effect immediately.
- SPI sentinels map to HTTP statuses generically (`server/errors.go`):
  `ErrRoomNotFound`/`ErrDocumentNotFound` → 404, `ErrAccessDenied` → 403,
  `ErrUnsupported` → 501, everything else → 500 with detail only in the
  structured log. The SPI already scopes cross-tenant probes to not-found, so
  the service leaks no existence oracle (verified by
  `TestCrossTenantProbeIsIndistinguishableFromNotFound`).
- The `roomID` path segment is carried for routing/audit; the SPI resolves
  documents directly, so authorization happens against the document's owning
  room inside the adapter. Page responses are `Cache-Control: no-store` +
  `X-Content-Type-Options: nosniff`.
- Every page serve/denial is a structured `slog` event carrying `tenant_id`,
  actor, room/document ids and result (AGENTS.md §6/§11). The client IP token
  honours `X-Forwarded-For` only when `TrustProxy` is set (gateway-terminated
  deployments).

## Lab-deferred (next waves)

- **Office→PDF conversion worker** (FR-3.4/TR-4.3): `converter.Converter` is
  the interface; `converter.LibreOffice` is an inert stub that always returns
  `converter.ErrUnavailable` — `soffice --headless` is not installed on dev
  machines and no test execs it or fakes a conversion result. The real worker
  ships with the conversion service.
- **Real adapter wiring**: integration tests drive the in-memory reference
  adapter (`src/spi/conformance/memadapter`), which renders text lines and
  does not itself watermark; byte-level watermark assurance is therefore
  proven at the pipeline level (`viewer`, `pdf`, `watermark` suites) and the
  memadapter suite proves HTTP/status semantics. End-to-end
  memadapter-with-renderer wiring is a conformance/assembly task for the lab.
- **NATS audit emission** (TR-7.6): page events are structured-logged today;
  publishing `evdr.{cell}.{tenant}.>` events is a wiring wave task.
- **Per-room watermark presets** (TR-4.6): the service applies one configured
  preset; room-level presets arrive with the policy engine (FR-3.6).

## Running

```bash
# From the repo root (Go 1.25+; this repo's toolchain at ~/.local/go/bin):
go test ./src/services/viewer/... -race -count=1
# Whole repo (regression incl. SPI conformance):
go build ./... && go vet ./... && go test ./... -race -count=1
```

Known note: page-count costs one full-range render pass (the frozen SPI
contract has no page-count method); content is discarded inside the service —
only the count crosses the HTTP boundary. A rasterising renderer or an
adapter-level count would change this trade-off later.
