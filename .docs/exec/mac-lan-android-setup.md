# Mac LAN Server + Android 3.7.1 Client Setup

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.

This document must be maintained in accordance with `.docs/PLANS.md`.

## Purpose / Big Picture

This change makes Lunar Tear practical to run from a Mac on a home network and connect to from a physical Android phone. After the change, a developer can import the asset dump, start the server in Docker with the Mac's LAN IP, build a patched 3.7.1 client APK from a known-good local source, install it over `adb`, and verify readiness through a dedicated HTTP health endpoint.

## Progress

- [x] (2026-04-18 20:43Z) Gathered current repo/server/tooling state and validated local inputs: Docker present, `adb` device connected, local 3.7.1 APK present, `.bin.e` present, asset archive still incomplete.
- [x] (2026-04-18 20:43Z) Chose Docker as the canonical runtime and local `/Users/axel/Downloads/NieR Re[in]carnation 3.7.1.apk` as the canonical client source.
- [x] (2026-04-18 21:02Z) Implemented server asset validation and `/healthz`, plus Docker Compose environment interpolation for host/port/scene.
- [x] (2026-04-18 21:05Z) Refactored `scripts/patch_apk.py` into reusable functions with a stable CLI wrapper and added `PatchResult`.
- [x] (2026-04-18 21:08Z) Implemented `scripts/mac_lan.py` doctor/import-assets/prepare-client/start-server flows.
- [x] (2026-04-18 21:15Z) Added Go tests for validation and health behavior.
- [x] (2026-04-18 21:15Z) Added Python tests for orchestrator helpers.
- [x] (2026-04-18 21:18Z) Updated docs, `.gitignore`, and `AGENTS.md`.

## Surprises & Discoveries

- Observation: `.gitignore` currently ignores `scripts/` wholesale even though tracked files already exist there.
  Evidence: repository root `.gitignore` ends with `scripts/`, which would hide a new `scripts/mac_lan.py` unless corrected.
- Observation: the existing patcher is version-coupled to 3.7.1 through hardcoded IL2CPP RVAs.
  Evidence: `scripts/patch_apk.py` documents "matching client/3.7.1.apk" above the patch table.
- Observation: the asset archive download is not complete yet, so the implementation must treat extraction as a hard stop condition.
  Evidence: `~/Downloads/resource_dump_android.7z` is `0B` and `resource_dump_android.J0QQ_qdr.7z.part` exists.

## Decision Log

- Decision: Prefer Docker over local Go for macOS bring-up.
  Rationale: Docker is installed; Go is not, and the repo already ships a Dockerfile and Compose file.
  Date/Author: 2026-04-18 / Codex
- Decision: Use the local `/Users/axel/Downloads/NieR Re[in]carnation 3.7.1.apk` as the default client source.
  Rationale: The patcher RVAs are explicitly tied to 3.7.1, making this source safer than the phone-side APK of uncertain provenance.
  Date/Author: 2026-04-18 / Codex
- Decision: Add a dedicated `/healthz` endpoint instead of inferring readiness from Octo routes.
  Rationale: The orchestration script needs a deterministic readiness probe that is independent of game traffic.
  Date/Author: 2026-04-18 / Codex

## Outcomes & Retrospective

The planned implementation is complete. The server now fails early on missing assets, exposes `/healthz`, and can be launched for LAN use through Docker without editing tracked files. The APK patcher is reusable from Python, and the new `scripts/mac_lan.py` command provides doctor/import/build/install/start flows around the known-good 3.7.1 client.

The remaining work is operational rather than code-related: the user still needs the asset dump archive download to finish before `import-assets` and any end-to-end server launch can succeed. The doctor command already reflects that state and intentionally refuses archive-dependent actions while the download is incomplete.

## Context and Orientation

The server entrypoint lives in `server/cmd/lunar-tear/main.go`. It starts the Octo/game HTTP server and the gRPC server used by the client. gRPC binds to port 443 and the client expects that port. The HTTP server currently only serves game routes and has no health endpoint. Runtime asset files are served from `server/assets/`, specifically `assets/revisions/...` for bundle metadata and `assets/release/*.bin.e` for master data.

The current Android patching logic lives in `scripts/patch_apk.py`. It modifies a decompiled 3.7.1 APK directory in place. There is no end-to-end script yet for copying the APK, decompiling it, rebuilding it, signing it, or installing it. The repo also lacks an asset importer for the external archive dump.

## Plan of Work

First, add server-side validation so startup fails clearly when the operator has not imported assets. At the same time, add a dedicated `/healthz` endpoint that reports the configured host, ports, and validation result. Then make `server/docker-compose.yaml` accept environment-provided host/port/scene values so a script can start the server for LAN use without file edits.

Next, refactor `scripts/patch_apk.py` so its current CLI calls a reusable function returning structured results. Add a new Python orchestrator script that performs host checks, imports assets from the external archive, builds/signs the patched APK from the canonical 3.7.1 source, installs it via `adb`, and starts Docker Compose with the proper environment.

Finally, add Go and Python test coverage for the new helpers and update the docs so the workflow is discoverable. Record the `.gitignore` correction and any operator-relevant gotchas in `AGENTS.md`.

## Concrete Steps

Run from repo root:

    rtk git status --short
    rtk python3 scripts/mac_lan.py doctor

After the archive finishes downloading:

    rtk python3 scripts/mac_lan.py import-assets \
      --archive ~/Downloads/resource_dump_android.7z \
      --masterdata ~/Downloads/20240404193219.bin.e

    rtk python3 scripts/mac_lan.py start-server --scene 13
    rtk python3 scripts/mac_lan.py prepare-client --install

Expected readiness check:

    curl http://127.0.0.1:8080/healthz
    {"ok":true,"host":"192.168.4.21","httpPort":8080,"grpcPort":443,"assetsReady":true}

## Validation and Acceptance

Validation requires both automated tests and a manual LAN smoke test. The automated portion is complete when Go tests cover missing-asset failures and the `/healthz` response, and Python tests cover LAN IP detection, dependency checks, import path normalization, and APK build command assembly. Manual acceptance is complete when the server starts through Docker, `/healthz` reports ready, the patched APK installs on the Android phone, and the phone can reach the Mac-hosted server over LAN.

## Idempotence and Recovery

All new scripts should be safe to rerun. `import-assets` should reuse the destination layout after cleaning only temporary extraction directories. `prepare-client` should reuse the debug keystore and only replace generated APK artifacts. If Docker startup fails because assets are missing, the fix is to complete `import-assets` and rerun `start-server`.

## Artifacts and Notes

Current verified local inputs:

    /Users/axel/Downloads/NieR Re[in]carnation 3.7.1.apk
    /Users/axel/Downloads/20240404193219.bin.e

Current blocker:

    /Users/axel/Downloads/resource_dump_android.7z is still incomplete

## Interfaces and Dependencies

In `scripts/patch_apk.py`, define a callable:

    def patch_apk_dir(apk_dir: str, server_ip: str, http_port: int) -> PatchResult

`PatchResult` must expose:

    metadata_strings_patched: int
    metadata_strings_expected: int
    il2cpp_patches_applied: int
    il2cpp_patches_expected: int
    manifest_patched: bool
    network_security_config_created: bool

In `server/cmd/lunar-tear/http.go`, expose:

    GET /healthz

with JSON:

    {"ok":true,"host":"<host>","httpPort":8080,"grpcPort":443,"assetsReady":true}

Revision note: created the initial implementation plan before code changes so the feature can be tracked end to end in-repo.

Revision note: updated after implementation to reflect completed code changes, tests, and the remaining runtime blocker of the unfinished asset archive.
