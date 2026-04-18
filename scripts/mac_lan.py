#!/usr/bin/env python3
from __future__ import annotations

import argparse
import ipaddress
import json
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

from patch_apk import patch_apk_dir

DEFAULT_APK_PATH = Path("/Users/axel/Downloads/NieR Re[in]carnation 3.7.1.apk")
DEFAULT_MASTERDATA_PATH = Path.home() / "Downloads" / "20240404193219.bin.e"
DEFAULT_ARCHIVE_PATH = Path.home() / "Downloads" / "resource_dump_android.7z"
DEFAULT_WORKDIR = Path(".local/android-client")
DEFAULT_HTTP_PORT = 8080
DEFAULT_SCENE = 13
DEBUG_KEYSTORE_NAME = "debug.keystore"

DEPENDENCY_HINTS = {
    "python3": "Install Python 3 and ensure `python3` is on PATH.",
    "docker": "Install Docker Desktop for macOS and ensure `docker compose` works.",
    "adb": "Install Android platform-tools and ensure `adb` is on PATH.",
    "apktool": "Install `apktool` and ensure it is on PATH.",
    "apksigner": "Install Android build-tools and ensure `apksigner` is on PATH.",
    "zipalign": "Install Android build-tools and ensure `zipalign` is on PATH.",
    "7zip": "Install `7zz` or `7z` (for example via Homebrew's `sevenzip`).",
    "keytool": "Install a JDK and ensure `keytool` is on PATH.",
}


@dataclass
class DependencyCheck:
    name: str
    ok: bool
    detail: str


def binary_check(name: str, hint: str) -> DependencyCheck:
    path = shutil.which(name)
    return DependencyCheck(name, path is not None, path or hint)


def build_apktool_decode_command(apk_path: Path, out_dir: Path) -> list[str]:
    return ["apktool", "d", "-f", str(apk_path), "-o", str(out_dir)]


def build_apktool_build_command(decoded_dir: Path, out_apk: Path) -> list[str]:
    return ["apktool", "b", str(decoded_dir), "-o", str(out_apk)]


def build_zipalign_command(input_apk: Path, output_apk: Path) -> list[str]:
    return ["zipalign", "-f", "4", str(input_apk), str(output_apk)]


def build_apksigner_command(keystore: Path, apk_path: Path) -> list[str]:
    return [
        "apksigner",
        "sign",
        "--ks",
        str(keystore),
        "--ks-pass",
        "pass:android",
        "--ks-key-alias",
        "androiddebugkey",
        "--key-pass",
        "pass:android",
        str(apk_path),
    ]


def build_adb_install_command(apk_path: Path, serial: str | None = None) -> list[str]:
    cmd = ["adb"]
    if serial:
        cmd.extend(["-s", serial])
    cmd.extend(["install", "-r", str(apk_path)])
    return cmd


def run_command(
    cmd: list[str],
    *,
    cwd: Path | None = None,
    env: dict[str, str] | None = None,
    capture_output: bool = True,
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        cmd,
        cwd=str(cwd) if cwd else None,
        env=env,
        check=True,
        text=True,
        capture_output=capture_output,
    )


def choose_7zip_binary() -> str | None:
    for name in ("7zz", "7z"):
        path = shutil.which(name)
        if path:
            return path
    return None


def detect_private_lan_ip() -> str:
    candidates: list[str] = []

    try:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
            sock.connect(("8.8.8.8", 80))
            candidates.append(sock.getsockname()[0])
    except OSError:
        pass

    hostname = socket.gethostname()
    for family, _, _, _, sockaddr in socket.getaddrinfo(hostname, None):
        if family == socket.AF_INET:
            candidates.append(sockaddr[0])

    seen: set[str] = set()
    for candidate in candidates:
        if candidate in seen:
            continue
        seen.add(candidate)
        try:
            ip = ipaddress.ip_address(candidate)
        except ValueError:
            continue
        if isinstance(ip, ipaddress.IPv4Address) and ip.is_private:
            return candidate

    raise RuntimeError("could not detect a private IPv4 LAN address; pass --host/--server-ip explicitly")


def resolve_server_ip(explicit_ip: str | None) -> str:
    return explicit_ip or detect_private_lan_ip()


def resolve_device_serial(explicit_serial: str | None) -> str:
    if explicit_serial:
        return explicit_serial

    result = run_command(["adb", "devices"])
    serials = []
    for line in result.stdout.splitlines()[1:]:
        parts = line.split()
        if len(parts) >= 2 and parts[1] == "device":
            serials.append(parts[0])

    if not serials:
        raise RuntimeError("no connected adb device found")

    return serials[0]


def check_dependencies() -> list[DependencyCheck]:
    checks = [
        binary_check("python3", DEPENDENCY_HINTS["python3"]),
        binary_check("adb", DEPENDENCY_HINTS["adb"]),
        binary_check("apktool", DEPENDENCY_HINTS["apktool"]),
        binary_check("apksigner", DEPENDENCY_HINTS["apksigner"]),
        binary_check("zipalign", DEPENDENCY_HINTS["zipalign"]),
        binary_check("keytool", DEPENDENCY_HINTS["keytool"]),
        binary_check("docker", DEPENDENCY_HINTS["docker"]),
    ]

    seven_zip = choose_7zip_binary()
    checks.append(DependencyCheck("7zip", seven_zip is not None, seven_zip or DEPENDENCY_HINTS["7zip"]))

    if shutil.which("docker") is not None:
        try:
            run_command(["docker", "compose", "version"])
        except subprocess.CalledProcessError as exc:
            checks.append(DependencyCheck("docker compose", False, exc.stderr.strip() or DEPENDENCY_HINTS["docker"]))
        else:
            checks.append(DependencyCheck("docker compose", True, "ok"))

    return checks


def ensure_archive_ready(archive_path: Path) -> None:
    if not archive_path.exists():
        raise RuntimeError(f"asset archive not found: {archive_path}")
    if archive_path.stat().st_size <= 0:
        raise RuntimeError(
            f"asset archive is empty or still downloading: {archive_path}. "
            "Wait for the download to finish before running import-assets."
        )


def find_assets_root(extract_root: Path) -> Path:
    direct = extract_root / "revisions"
    if direct.is_dir():
        return extract_root

    candidates: list[Path] = []
    for dirpath, dirnames, _ in os.walk(extract_root):
        current = Path(dirpath)
        if "revisions" in dirnames:
            candidates.append(current)
        assets_dir = current / "assets"
        if assets_dir.is_dir() and (assets_dir / "revisions").is_dir():
            candidates.append(assets_dir)

    if not candidates:
        raise RuntimeError(f"could not find extracted asset root under {extract_root}")

    candidates.sort(key=lambda path: (len(path.relative_to(extract_root).parts), str(path)))
    return candidates[0]


def validate_asset_tree(dest: Path, masterdata_name: str | None = None) -> None:
    list_path = dest / "revisions" / "0" / "list.bin"
    if not list_path.is_file():
        raise RuntimeError(f"missing imported list.bin: {list_path}")

    release_dir = dest / "release"
    if masterdata_name:
        masterdata_path = release_dir / masterdata_name
        if not masterdata_path.is_file():
            raise RuntimeError(f"missing imported master data: {masterdata_path}")
        return

    if not list(release_dir.glob("*.bin.e")):
        raise RuntimeError(f"missing imported master data under: {release_dir}")


def ensure_debug_keystore(
    keystore_path: Path,
    *,
    runner: Callable[..., subprocess.CompletedProcess[str]] = run_command,
) -> bool:
    if keystore_path.exists():
        return False

    keystore_path.parent.mkdir(parents=True, exist_ok=True)
    runner(
        [
            "keytool",
            "-genkeypair",
            "-v",
            "-keystore",
            str(keystore_path),
            "-alias",
            "androiddebugkey",
            "-storepass",
            "android",
            "-keypass",
            "android",
            "-keyalg",
            "RSA",
            "-keysize",
            "2048",
            "-validity",
            "10000",
            "-dname",
            "CN=Android Debug,O=Android,C=US",
        ]
    )
    return True


def format_missing_dependency_message(names: list[str]) -> str:
    parts = [f"{name}: {DEPENDENCY_HINTS.get(name, 'missing from PATH')}" for name in names]
    return "missing dependencies:\n- " + "\n- ".join(parts)


def cmd_doctor(args: argparse.Namespace) -> int:
    checks = check_dependencies()
    server_ip = resolve_server_ip(args.server_ip)

    apk_exists = args.apk.is_file()
    masterdata_exists = args.masterdata.is_file()
    archive_state = "ready" if args.archive.exists() and args.archive.stat().st_size > 0 else "pending"

    print("Mac LAN doctor")
    print(f"  server_ip: {server_ip}")
    print(f"  apk: {'ok' if apk_exists else 'missing'} ({args.apk})")
    print(f"  masterdata: {'ok' if masterdata_exists else 'missing'} ({args.masterdata})")
    print(f"  archive: {archive_state} ({args.archive})")

    try:
        serial = resolve_device_serial(args.serial)
    except RuntimeError as exc:
        print(f"  adb_device: missing ({exc})")
        adb_device_ok = False
    else:
        print(f"  adb_device: ok ({serial})")
        adb_device_ok = True

    print("  dependencies:")
    for check in checks:
        status = "ok" if check.ok else "missing"
        print(f"    - {check.name}: {status} ({check.detail})")

    missing = [check for check in checks if not check.ok]
    if missing or not apk_exists or not masterdata_exists or not adb_device_ok:
        return 1
    return 0


def cmd_import_assets(args: argparse.Namespace) -> int:
    ensure_archive_ready(args.archive)
    if not args.masterdata.is_file():
        raise RuntimeError(f"master data file not found: {args.masterdata}")

    seven_zip = choose_7zip_binary()
    if not seven_zip:
        raise RuntimeError(DEPENDENCY_HINTS["7zip"])

    args.dest.mkdir(parents=True, exist_ok=True)
    args.dest.joinpath("release").mkdir(parents=True, exist_ok=True)
    temp_parent = Path(".local")
    temp_parent.mkdir(exist_ok=True)

    with tempfile.TemporaryDirectory(prefix="asset-import-", dir=temp_parent) as tmp:
        extract_root = Path(tmp) / "extract"
        extract_root.mkdir(parents=True, exist_ok=True)
        run_command([seven_zip, "x", str(args.archive), f"-o{extract_root}", "-y"])
        assets_root = find_assets_root(extract_root)

        source_revisions = assets_root / "revisions"
        if not source_revisions.is_dir():
            raise RuntimeError(f"archive did not contain a revisions directory at: {source_revisions}")

        dest_revisions = args.dest / "revisions"
        if dest_revisions.exists():
            shutil.rmtree(dest_revisions)
        shutil.copytree(source_revisions, dest_revisions)

        source_release = assets_root / "release"
        if source_release.is_dir():
            shutil.copytree(source_release, args.dest / "release", dirs_exist_ok=True)

        shutil.copy2(args.masterdata, args.dest / "release" / args.masterdata.name)

    validate_asset_tree(args.dest, args.masterdata.name)
    print(f"Imported assets into {args.dest}")
    return 0


def cmd_prepare_client(args: argparse.Namespace) -> int:
    if not args.apk.is_file():
        raise RuntimeError(f"source APK not found: {args.apk}")

    required = {
        "apktool": shutil.which("apktool"),
        "apksigner": shutil.which("apksigner"),
        "zipalign": shutil.which("zipalign"),
        "keytool": shutil.which("keytool"),
    }
    if args.install:
        required["adb"] = shutil.which("adb")
    missing = [name for name, path in required.items() if path is None]
    if missing:
        raise RuntimeError(format_missing_dependency_message(missing))

    server_ip = resolve_server_ip(args.server_ip)
    serial = resolve_device_serial(args.serial) if args.install else None

    workdir = args.workdir
    workdir.mkdir(parents=True, exist_ok=True)
    original_apk = workdir / "original.apk"
    decoded_dir = workdir / "decompiled"
    unsigned_apk = workdir / "patched-unsigned.apk"
    aligned_apk = workdir / "patched-aligned.apk"
    signed_apk = workdir / "patched-signed.apk"
    keystore = workdir / DEBUG_KEYSTORE_NAME

    shutil.copy2(args.apk, original_apk)
    run_command(build_apktool_decode_command(original_apk, decoded_dir))
    patch_result = patch_apk_dir(str(decoded_dir), server_ip, args.http_port)

    for artifact in (unsigned_apk, aligned_apk, signed_apk):
        if artifact.exists():
            artifact.unlink()

    run_command(build_apktool_build_command(decoded_dir, unsigned_apk))
    run_command(build_zipalign_command(unsigned_apk, aligned_apk))
    shutil.copy2(aligned_apk, signed_apk)
    ensure_debug_keystore(keystore)
    run_command(build_apksigner_command(keystore, signed_apk))

    print(
        json.dumps(
            {
                "signedApk": str(signed_apk),
                "serverIp": server_ip,
                "metadataPatched": patch_result.metadata_strings_patched,
                "il2cppPatched": patch_result.il2cpp_patches_applied,
            },
            indent=2,
        )
    )

    if not args.install:
        return 0

    try:
        run_command(build_adb_install_command(signed_apk, serial))
    except subprocess.CalledProcessError as exc:
        output = "\n".join(part for part in (exc.stdout, exc.stderr) if part).strip()
        if "INSTALL_FAILED_UPDATE_INCOMPATIBLE" in output or "INCONSISTENT_CERTIFICATES" in output:
            raise RuntimeError(
                "adb install failed because the existing installed package uses a different signature. "
                "Uninstall the currently installed app from the phone and rerun prepare-client --install."
            ) from exc
        raise RuntimeError(f"adb install failed: {output}") from exc

    print(f"Installed patched APK on device {serial}")
    return 0


def wait_for_health(url: str, timeout_seconds: int) -> dict[str, object]:
    deadline = time.time() + timeout_seconds
    last_error = ""
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=2) as response:
                body = response.read().decode("utf-8")
                return json.loads(body)
        except (urllib.error.URLError, json.JSONDecodeError) as exc:
            last_error = str(exc)
            time.sleep(1)
    raise RuntimeError(f"server did not become ready at {url}: {last_error}")


def cmd_start_server(args: argparse.Namespace) -> int:
    server_ip = resolve_server_ip(args.host)
    validate_asset_tree(args.server_dir / "assets")

    env = os.environ.copy()
    env.update(
        {
            "LUNAR_HOST": server_ip,
            "LUNAR_HTTP_PORT": str(args.http_port),
            "LUNAR_SCENE": str(args.scene),
        }
    )

    run_command(["docker", "compose", "up", "--build", "-d"], cwd=args.server_dir, env=env)
    health = wait_for_health(f"http://127.0.0.1:{args.http_port}/healthz", args.timeout)

    print(f"gRPC target: {server_ip}:443")
    print(f"HTTP base: http://{server_ip}:{args.http_port}")
    print(f"Source APK: {args.apk}")
    print(json.dumps(health, indent=2))
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Mac LAN helper for Lunar Tear")
    parser.set_defaults(func=None)

    subparsers = parser.add_subparsers(dest="command")

    doctor = subparsers.add_parser("doctor", help="Check local prerequisites")
    doctor.add_argument("--server-ip", default=None)
    doctor.add_argument("--serial", default=None)
    doctor.add_argument("--apk", type=Path, default=DEFAULT_APK_PATH)
    doctor.add_argument("--masterdata", type=Path, default=DEFAULT_MASTERDATA_PATH)
    doctor.add_argument("--archive", type=Path, default=DEFAULT_ARCHIVE_PATH)
    doctor.set_defaults(func=cmd_doctor)

    import_assets = subparsers.add_parser("import-assets", help="Import the downloaded asset dump")
    import_assets.add_argument("--archive", type=Path, default=DEFAULT_ARCHIVE_PATH)
    import_assets.add_argument("--masterdata", type=Path, default=DEFAULT_MASTERDATA_PATH)
    import_assets.add_argument("--dest", type=Path, default=Path("server/assets"))
    import_assets.set_defaults(func=cmd_import_assets)

    prepare_client = subparsers.add_parser("prepare-client", help="Build and optionally install a patched client")
    prepare_client.add_argument("--apk", type=Path, default=DEFAULT_APK_PATH)
    prepare_client.add_argument("--server-ip", default=None)
    prepare_client.add_argument("--http-port", type=int, default=DEFAULT_HTTP_PORT)
    prepare_client.add_argument("--workdir", type=Path, default=DEFAULT_WORKDIR)
    prepare_client.add_argument("--serial", default=None)
    prepare_client.add_argument("--install", action="store_true")
    prepare_client.set_defaults(func=cmd_prepare_client)

    start_server = subparsers.add_parser("start-server", help="Start Docker Compose and wait for readiness")
    start_server.add_argument("--host", default=None)
    start_server.add_argument("--http-port", type=int, default=DEFAULT_HTTP_PORT)
    start_server.add_argument("--scene", type=int, default=DEFAULT_SCENE)
    start_server.add_argument("--server-dir", type=Path, default=Path("server"))
    start_server.add_argument("--apk", type=Path, default=DEFAULT_APK_PATH)
    start_server.add_argument("--timeout", type=int, default=60)
    start_server.set_defaults(func=cmd_start_server)

    return parser


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    if args.func is None:
        parser.print_help()
        return 1

    try:
        return int(args.func(args) or 0)
    except RuntimeError as exc:
        print(f"[!] {exc}", file=sys.stderr)
        return 1
    except subprocess.CalledProcessError as exc:
        output = "\n".join(part for part in (exc.stdout, exc.stderr) if part).strip()
        print(f"[!] Command failed: {' '.join(exc.cmd)}", file=sys.stderr)
        if output:
            print(output, file=sys.stderr)
        return exc.returncode or 1


if __name__ == "__main__":
    raise SystemExit(main())
