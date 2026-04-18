#!/usr/bin/env python3
"""
patch_apk.py — Static patcher for NieR Re[in]carnation APK.

Patches an apktool-decompiled APK directory so the game connects to a
private server without any runtime (Frida) hooks.

Patches applied:
  1. global-metadata.dat  — rewrite IL2CPP string literals (URLs + hostname)
  2. libil2cpp.so          — ARM64 binary patches (SSL bypass, encryption passthrough,
                             Octo plain list, Google Play billing bypass)
  3. AndroidManifest.xml  — add networkSecurityConfig for cleartext HTTP
  4. res/xml/network_security_config.xml — allow cleartext traffic
"""

from __future__ import annotations

import argparse
import os
import struct
import sys
from dataclasses import dataclass

# ---------------------------------------------------------------------------
# global-metadata.dat string literal patching
# ---------------------------------------------------------------------------

METADATA_MAGIC = 0xFAB11BAF

# Header offsets (v24): each section is a (uint32 offset, uint32 size) pair
HDR_STRING_LITERAL_OFF = 8
HDR_STRING_LITERAL_DATA_OFF = 16


@dataclass
class PatchResult:
    metadata_strings_patched: int
    metadata_strings_expected: int
    il2cpp_patches_applied: int
    il2cpp_patches_expected: int
    manifest_patched: bool
    network_security_config_created: bool


def patch_metadata_strings(meta_path: str, replacements: list[tuple[str, str]]) -> int:
    with open(meta_path, "rb") as f:
        data = bytearray(f.read())

    magic = struct.unpack_from("<I", data, 0)[0]
    if magic != METADATA_MAGIC:
        print(f"  [!] Bad magic 0x{magic:08X}, expected 0x{METADATA_MAGIC:08X}")
        return 0

    version = struct.unpack_from("<i", data, 4)[0]
    print(f"  metadata v{version}, {len(data)} bytes")

    sl_off, sl_size = struct.unpack_from("<II", data, HDR_STRING_LITERAL_OFF)
    sld_off, sld_size = struct.unpack_from("<II", data, HDR_STRING_LITERAL_DATA_OFF)
    n_entries = sl_size // 8
    print(f"  stringLiteral: {n_entries} entries @ 0x{sl_off:X}")
    print(f"  stringLiteralData: {sld_size} bytes @ 0x{sld_off:X}")

    patched = 0
    for old_str, new_str in replacements:
        old_bytes = old_str.encode("utf-8")
        new_bytes = new_str.encode("utf-8")

        if len(new_bytes) > len(old_bytes):
            print(
                f"  [!] SKIP: replacement longer than original "
                f"({len(new_bytes)} > {len(old_bytes)}): {old_str!r}"
            )
            continue

        blob_pos = data.find(old_bytes, sld_off, sld_off + sld_size)
        if blob_pos < 0:
            print(f"  [!] NOT FOUND in blob: {old_str!r}")
            continue

        data_index = blob_pos - sld_off
        entry_found = False
        for i in range(n_entries):
            e_off = sl_off + i * 8
            e_len, e_idx = struct.unpack_from("<II", data, e_off)
            if e_idx == data_index and e_len == len(old_bytes):
                struct.pack_into("<I", data, e_off, len(new_bytes))
                entry_found = True
                print(f"  entry #{i}: length {e_len} -> {len(new_bytes)}")
                break

        if not entry_found:
            print(f"  [!] No table entry found for {old_str!r} (dataIndex=0x{data_index:X})")
            continue

        data[blob_pos: blob_pos + len(old_bytes)] = (
            new_bytes + b"\x00" * (len(old_bytes) - len(new_bytes))
        )

        print(f"  PATCHED: {old_str!r} -> {new_str!r}")
        patched += 1

    with open(meta_path, "wb") as f:
        f.write(data)

    return patched


# ---------------------------------------------------------------------------
# AndroidManifest.xml  — add networkSecurityConfig attribute
# ---------------------------------------------------------------------------

def patch_manifest(manifest_path: str) -> bool:
    with open(manifest_path, "r", encoding="utf-8") as f:
        text = f.read()

    if "networkSecurityConfig" in text:
        print("  already has networkSecurityConfig")
        return False

    new_attr = 'android:networkSecurityConfig="@xml/network_security_config"'
    text = text.replace("<application ", f"<application {new_attr} ", 1)

    with open(manifest_path, "w", encoding="utf-8") as f:
        f.write(text)

    print(f"  added {new_attr}")
    return True


# ---------------------------------------------------------------------------
# res/xml/network_security_config.xml
# ---------------------------------------------------------------------------

NETWORK_SECURITY_CONFIG = """\
<?xml version="1.0" encoding="utf-8"?>
<network-security-config>
    <base-config cleartextTrafficPermitted="true" />
</network-security-config>
"""


def create_network_security_config(res_xml_dir: str) -> bool:
    os.makedirs(res_xml_dir, exist_ok=True)
    out = os.path.join(res_xml_dir, "network_security_config.xml")
    with open(out, "w", encoding="utf-8") as f:
        f.write(NETWORK_SECURITY_CONFIG)
    print(f"  wrote {out}")
    return True


# ---------------------------------------------------------------------------
# libil2cpp.so ARM64 binary patches
# ---------------------------------------------------------------------------

MOV_X0_0 = struct.pack("<I", 0xD2800000)
MOV_W0_1 = struct.pack("<I", 0x52800020)
MOV_X0_X1 = struct.pack("<I", 0xAA0103E0)
NOP = struct.pack("<I", 0xD503201F)
RET = struct.pack("<I", 0xD65F03C0)

# RVAs from dump.cs (Il2CppDumper output matching client/3.7.1.apk)
IL2CPP_PATCHES = [
    {
        "name": "ToNativeCredentials",
        "desc": "SSL bypass — return NULL to force insecure gRPC channel",
        "rva": 0x35C8670,
        "bytes": MOV_X0_0 + RET,
    },
    {
        "name": "HandleNet.Encrypt",
        "desc": "encryption passthrough — return payload as-is",
        "rva": 0x279410C,
        "bytes": MOV_X0_X1 + RET,
    },
    {
        "name": "HandleNet.Decrypt",
        "desc": "decryption passthrough — return receivedMessage as-is",
        "rva": 0x279420C,
        "bytes": MOV_X0_X1 + RET,
    },
    {
        "name": "OctoManager.Internal.GetListAes",
        "desc": "Octo list: force plain list (return false = no AES); server serves raw list.bin",
        "rva": 0x4C27038,
        "bytes": MOV_X0_0 + RET,
    },
    {
        "name": "PurchaseRealProductAsync.MoveNext (inlined IsInitialized check)",
        "desc": "IAP bypass — NOP the cbz that branches to PurchasingUnavailable when _initialized is false",
        "rva": 0x2831CA8,
        "bytes": NOP,
    },
    {
        "name": "Purchaser.IsExistProduct",
        "desc": "IAP bypass — always report product as existing in store",
        "rva": 0x282CE78,
        "bytes": MOV_W0_1 + RET,
    },
    {
        "name": "Purchaser.<BuyProduct>d__24.MoveNext (null _storeController)",
        "desc": "IAP bypass — redirect null _storeController from NRE to cancelled-return path",
        "rva": 0x2834028,
        "bytes": struct.pack("<I", 0xB4001675),
    },
    {
        "name": "PurchaseRealProductAsync.MoveNext (skip CheckPurchasingAlert)",
        "desc": "Fast purchase — skip CheckPurchasingAlert call, awaiter, and CESA dialog (B to post-alert code)",
        "rva": 0x2831CAC,
        "bytes": struct.pack("<I", 0x14000019),
    },
    {
        "name": "Initialize.MoveNext (skip _initialized check)",
        "desc": "Fast purchase — NOP the cbz so Initialize always returns None via builder (skip ~8s GP timeout)",
        "rva": 0x2830834,
        "bytes": NOP,
    },
]


def patch_libil2cpp(so_path: str) -> int:
    with open(so_path, "r+b") as f:
        file_size = f.seek(0, 2)
        patched = 0
        for patch in IL2CPP_PATCHES:
            rva = patch["rva"]
            if rva + len(patch["bytes"]) > file_size:
                print(f"  [!] SKIP {patch['name']}: RVA 0x{rva:X} beyond file size")
                continue

            f.seek(rva)
            orig = f.read(len(patch["bytes"]))

            f.seek(rva)
            f.write(patch["bytes"])
            patched += 1
            print(f"  {patch['name']} @ 0x{rva:X}: {orig.hex()} -> {patch['bytes'].hex()}")
            print(f"    {patch['desc']}")

    return patched


def build_replacements(server_ip: str, http_port: int) -> list[tuple[str, str]]:
    web_url = f"http://{server_ip}:{http_port}"
    grpc_host = server_ip
    return [
        ("api.app.nierreincarnation.com", grpc_host),
        (
            "https://web.app.nierreincarnation.com/assets/release/{0}/database.bin",
            f"{web_url}/assets/release/{{0}}/database.bin",
        ),
        ("https://web.app.nierreincarnation.com", web_url),
        ("https://resources-api.app.nierreincarnation.com/", f"{web_url}/"),
    ]


def validate_replacements(replacements: list[tuple[str, str]]) -> None:
    for old, new in replacements:
        if len(new.encode("utf-8")) > len(old.encode("utf-8")):
            raise ValueError(
                f"Replacement too long ({len(new)} > {len(old)}): "
                f"{old!r} -> {new!r}. Use a shorter server address or omit the port for port 80."
            )


def patch_apk_dir(apk_dir: str, server_ip: str, http_port: int) -> PatchResult:
    apk = apk_dir.rstrip("/")
    meta = os.path.join(apk, "assets/bin/Data/Managed/Metadata/global-metadata.dat")
    so = os.path.join(apk, "lib/arm64-v8a/libil2cpp.so")
    manifest = os.path.join(apk, "AndroidManifest.xml")
    res_xml = os.path.join(apk, "res/xml")

    for path in (meta, so, manifest):
        if not os.path.isfile(path):
            raise FileNotFoundError(path)

    replacements = build_replacements(server_ip, http_port)
    validate_replacements(replacements)

    web_url = f"http://{server_ip}:{http_port}"
    print(f"\n[*] Patching for server {server_ip}:{http_port} (gRPC host={server_ip}, client port from config)")
    print(f"    web URL   = {web_url}")
    print(f"    gRPC host = {server_ip} (ensure server listens on 443 or patch client port)")

    print("\n[1] Patching global-metadata.dat string literals ...")
    metadata_strings_patched = patch_metadata_strings(meta, replacements)
    print(f"    {metadata_strings_patched}/{len(replacements)} strings patched")

    print("\n[2] Patching libil2cpp.so (SSL bypass + encryption passthrough + IAP bypass + fast purchase) ...")
    il2cpp_patches_applied = patch_libil2cpp(so)
    print(f"    {il2cpp_patches_applied}/{len(IL2CPP_PATCHES)} methods patched")

    print("\n[3] Patching AndroidManifest.xml ...")
    manifest_patched = patch_manifest(manifest)

    print("\n[4] Creating network_security_config.xml ...")
    network_security_config_created = create_network_security_config(res_xml)

    return PatchResult(
        metadata_strings_patched=metadata_strings_patched,
        metadata_strings_expected=len(replacements),
        il2cpp_patches_applied=il2cpp_patches_applied,
        il2cpp_patches_expected=len(IL2CPP_PATCHES),
        manifest_patched=manifest_patched,
        network_security_config_created=network_security_config_created,
    )


def main() -> None:
    parser = argparse.ArgumentParser(description="Patch decompiled APK for private server")
    parser.add_argument("apk_dir", help="Path to apktool-decompiled APK directory")
    parser.add_argument("--server-ip", required=True, help="Server IP (e.g. 10.0.2.2)")
    parser.add_argument("--http-port", type=int, default=8080, help="HTTP port (default 8080)")
    args = parser.parse_args()

    try:
        result = patch_apk_dir(args.apk_dir, args.server_ip, args.http_port)
    except FileNotFoundError as exc:
        sys.exit(f"[!] Not found: {exc}")
    except ValueError as exc:
        sys.exit(f"[!] {exc}")

    apk = args.apk_dir.rstrip("/")
    print("\n[+] Done. Rebuild with:")
    print(f"    apktool b {apk} -o client/patched.apk")
    print(
        "    apksigner sign --ks client/debug.keystore --ks-pass pass:android "
        f"{apk.replace('patched/', '')}patched.apk"
    )
    print(
        f"    Summary: metadata={result.metadata_strings_patched}/{result.metadata_strings_expected}, "
        f"libil2cpp={result.il2cpp_patches_applied}/{result.il2cpp_patches_expected}"
    )


if __name__ == "__main__":
    main()
