from __future__ import annotations

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))

import mac_lan


class MacLanTests(unittest.TestCase):
    def test_resolve_server_ip_prefers_explicit_override(self) -> None:
        self.assertEqual(mac_lan.resolve_server_ip("192.168.1.10"), "192.168.1.10")

    @mock.patch("mac_lan.socket.getaddrinfo")
    @mock.patch("mac_lan.socket.gethostname", return_value="host")
    def test_detect_private_lan_ip_falls_back_to_hostname(self, _hostname: mock.Mock, getaddrinfo: mock.Mock) -> None:
        getaddrinfo.return_value = [
            (mac_lan.socket.AF_INET, None, None, None, ("192.168.4.21", 0)),
        ]
        with mock.patch("mac_lan.socket.socket", side_effect=OSError("offline")):
            self.assertEqual(mac_lan.detect_private_lan_ip(), "192.168.4.21")

    def test_find_assets_root_normalizes_nested_assets_directory(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            assets_root = root / "payload" / "server" / "assets"
            (assets_root / "revisions" / "0").mkdir(parents=True)
            self.assertEqual(mac_lan.find_assets_root(root), assets_root)

    def test_build_commands(self) -> None:
        apk = Path("/tmp/original.apk")
        out_dir = Path("/tmp/decoded")
        unsigned = Path("/tmp/unsigned.apk")
        aligned = Path("/tmp/aligned.apk")
        signed = Path("/tmp/signed.apk")
        keystore = Path("/tmp/debug.keystore")

        self.assertEqual(mac_lan.build_apktool_decode_command(apk, out_dir), ["apktool", "d", "-f", str(apk), "-o", str(out_dir)])
        self.assertEqual(mac_lan.build_apktool_build_command(out_dir, unsigned), ["apktool", "b", str(out_dir), "-o", str(unsigned)])
        self.assertEqual(mac_lan.build_zipalign_command(unsigned, aligned), ["zipalign", "-f", "4", str(unsigned), str(aligned)])
        self.assertIn(str(keystore), mac_lan.build_apksigner_command(keystore, signed))
        self.assertEqual(mac_lan.build_adb_install_command(signed, "SERIAL123"), ["adb", "-s", "SERIAL123", "install", "-r", str(signed)])

    def test_ensure_debug_keystore_is_idempotent(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            keystore = Path(tmp) / "debug.keystore"
            keystore.write_text("existing", encoding="utf-8")
            runner = mock.Mock(side_effect=AssertionError("runner should not be called"))
            self.assertFalse(mac_lan.ensure_debug_keystore(keystore, runner=runner))

    def test_ensure_debug_keystore_creates_when_missing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            keystore = Path(tmp) / "debug.keystore"
            runner = mock.Mock(return_value=subprocess.CompletedProcess(args=["keytool"], returncode=0, stdout="", stderr=""))
            self.assertTrue(mac_lan.ensure_debug_keystore(keystore, runner=runner))
            runner.assert_called_once()


if __name__ == "__main__":
    unittest.main()
