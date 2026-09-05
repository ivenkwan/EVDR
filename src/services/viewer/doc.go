// Package viewer is the secure viewer render pipeline of EVDR (FR-3.1,
// FR-3.2, TR-4.1, TR-4.2; ADR-0001 view-first DRM).
//
// # Role in the architecture
//
// The Room SPI (src/spi) transports already-rendered pages: adapters stream
// one immutable document version and hand it to their configured Renderer;
// the SPI contract deliberately leaves rendering — page slicing and
// server-side watermark baking — to the viewer pipeline (see src/spi README,
// "Rendering (GetRenderStream)"). This package implements that pipeline:
//
//	doc version content
//	  └─ Renderer.Render (this package)
//	       ├─ PDF input  → pdf.Parse → per-page ExtractPage → watermark
//	       ├─ text input → one page per line                 → watermark
//	       └─ office input → converter.Converter (FR-3.4 seam; worker
//	                         lab-deferred)                   → PDF path
//	  └─ spi.RenderStream: forward-only, one page at a time
//
// Every delivered page satisfies two non-negotiable properties:
//
//  1. The payload is exactly one page — never the full document (FR-3.1).
//  2. The watermark is baked into the page bytes before delivery (FR-3.2) —
//     never a client-side overlay.
//
// The HTTP-facing half of the viewer service (the page-streaming API the
// browser talks to) lives in ./server and consumes spi.RoomSPI; in
// production the SPI implementation it is wired to is constructed with this
// package's Renderer, so the two halves meet the same compliance property
// from both sides of the SPI boundary.
//
// # Wiring
//
//	renderer := viewer.NewRenderer(viewer.RendererConfig{
//	    Converter: converter.NewLibreOffice("/usr/bin/soffice"),
//	})
//	adapter, _ := nextcloud.New(nextcloud.Config{..., Renderer: renderer})
//
// The Renderer is stateless after construction and safe for concurrent use.
package viewer
