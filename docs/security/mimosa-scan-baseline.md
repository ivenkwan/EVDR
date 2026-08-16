# Mimosa Scan Baseline — Phase 0

> **Status:** v1.0 — 2026-08-17. Records the sealed Mimosa scan receipts for Phase 0,
> the engine's terminal coverage state for this repository, and the experiments
> performed to close the git-gate hook's compatibility-policy gaps.

## 1. Sealed scan receipts

| Scan ID | Depth | Seal (SHA-256) | Findings |
|---|---|---|---|
| `scan-2026-08-16T22-27-11.002Z-4e8baaeae1fc` | deep | `09b7b08d…bed6c0` | 0 |
| `scan-2026-08-16T22-33-49.524Z-9961bb7eae03` | deep (full project) | `770c28e9…51fa6971` | 0 |
| `scan-2026-08-16T22-35-40.003Z-106cdb108d6f` | deep (sync) | `34a212a3…337dfca6d1d` | 0 |
| `scan-2026-08-16T22-46-46.271Z-8247537e1ba9` | deep (post-toolchain) | `a521c418…be65705a` | 0 |
| `scan-2026-08-16T22-47-58.599Z-5d7b043d6bfc` | deep (go.exe on PATH) | `ca600ed5…e17713` | 0 |
| `scan-2026-08-16T22-48-43.728Z-59d51573c305` | normal | `f8e2dc58…a54f15f` | 0 |

Artifacts live outside the repo under
`%USERPROFILE%\.mimosa\security-scans\project-4cea4f71d11a95db4d37bc03\<scanId>\`
(manifest, findings, coverage, seal, report projection). Engine producer version: 0.1.0.

## 2. What the engine analyzes in this repo

Every scan selected exactly **9 files, 9/9 parsed, 0 read/parse failures**. Cross-referencing
the git-gate hook's session baseline (13 source-class files) shows the analyzable corpus is
the repo's entire code surface: 6 Go files (`src/spi/*.go`, `samples/hello-service/*.go`)
and 3 shell scripts (`bootstrap-vault.sh`, `install-server.sh`, `install-agent.sh`).
Everything else is Markdown, YAML/HCL configuration, Terraform, or module manifests, which
this engine version does not parse semantically.

Both Go modules have **zero external dependencies** (no `require` directives), so
`dependencySummary.packagesScanned: 0` is factually complete — there is no third-party
package surface to assess.

## 3. The two git-gate gap codes are structural

On every commit the git-gate hook reports the compatibility-policy advisory citing:

- `library_source/library_source_unavailable`
- `callgraph/callgraph_fact_partial`

Attempts to close them (2026-08-17), each followed by a fresh sealed deep scan:

1. **Installed a full Go 1.25.13 toolchain** (portable, `C:\temp\evdr-lab\toolchain\go`)
   and warmed the module/build caches (`go build`, `go test`, `go list -m all` in both
   modules) — *no change* in coverage output.
2. **Exposed `go` on the inherited PATH** (shims in `%USERPROFILE%\bin`, first PATH entry)
   — *no change*.
3. **Made the CreateProcess-resolvable case work**: real `go.exe` in `%USERPROFILE%\bin`
   with a functional home-derived GOROOT (351 stdlib packages resolvable) — *no change*.
4. **Depth variation** (`normal` vs `deep`) — *no change*.

All experiment artifacts were removed afterwards; the toolchain remains at
`C:\temp\evdr-lab\toolchain\go` for future use.

**Conclusion:** the gaps are the engine's conservative terminal state for this repository,
not a missing input. The coverage ledger marks the threatModel/findingDiscovery/pathAnalysis
phases `not_applicable` (zero entry points, principals, call edges, or rule candidates in a
9-file stdlib-only corpus), and per the sealed scan contract a `partial`/`inconclusive`
ledger can never be promoted to a whole-project safety claim. The engine/hook logic is
distributed as encrypted assets, so no further user-side lever exists.

## 4. Standing rules

- The compatibility-policy advisory is **expected on every commit** until the engine changes;
  it is not a regression signal by itself.
- **No whole-project safety claim is made** — for any phase — on the basis of these scans.
  The scans are one input alongside the CI pipeline's Semgrep/Trivy/ZAP gates, whose Phase 0
  evidence lives in `C:\temp\evdr-lab\evidence\phase-0-pipeline-8\`.
- Re-run a sealed deep scan at each phase boundary and after any security-relevant change;
  record the scan ID + seal here (append to §1) and in the phase's audit report.
- If the engine is upgraded, re-run once and re-check `coverage.json`: if `completeness`
  becomes `full`, delete §3's structural conclusion and note the engine version.
