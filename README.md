# Lunar Tear

Private server research project for a certain discontinued mobile game.
Discord server: https://discord.gg/G3anrfcV

## How To Launch The Server

### Prerequisites

- Populated `server/assets/` directory
- Docker Desktop with `docker compose`
- Python 3
- For Android client prep: `adb`, `apktool`, `apksigner`, `zipalign`, `keytool`, and `7zz` or `7z`

### Preferred Mac LAN Flow

Use the helper script from the repository root:

```bash
rtk python3 scripts/mac_lan.py doctor
```

Canonical local inputs:

- APK: `/Users/axel/Downloads/NieR Re[in]carnation 3.7.1.apk`
- Master data: `~/Downloads/20240404193219.bin.e`
- Asset dump archive: `~/Downloads/resource_dump_android.7z`

When the asset archive has finished downloading:

```bash
rtk python3 scripts/mac_lan.py import-assets \
  --archive ~/Downloads/resource_dump_android.7z \
  --masterdata ~/Downloads/20240404193219.bin.e
```

Start the server for LAN access:

```bash
rtk python3 scripts/mac_lan.py start-server --scene 13
```

Prepare and install the patched Android client:

```bash
rtk python3 scripts/mac_lan.py prepare-client --install
```

The helper auto-detects a private LAN IP by default. Override with `--host` or `--server-ip` if needed.

### Health Check

After startup, verify readiness locally:

```bash
rtk curl http://127.0.0.1:8080/healthz
```

Expected JSON shape:

```json
{"ok":true,"host":"192.168.4.21","httpPort":8080,"grpcPort":443,"assetsReady":true}
```

### Regenerate protobuf stubs

```bash
cd server
make proto
```

### Run

```bash
cd server
sudo go run ./cmd/lunar-tear \
  --host 10.0.2.2 \
  --http-port 8080 \
  --scene 13
```

`sudo` is needed because gRPC binds to port 443 (privileged). On Linux you can use `setcap` instead:

```bash
go build -o lunar-tear ./cmd/lunar-tear
sudo setcap cap_net_bind_service=+ep ./lunar-tear
./lunar-tear --host 10.0.2.2 --http-port 8080 --scene 13
```

### Ports

| Protocol | Port | Notes                                                |
| -------- | ---- | ---------------------------------------------------- |
| gRPC     | 443  | hardcoded by the client, not configurable            |
| HTTP     | 8080 | Octo asset API + game web pages (`--http-port` flag) |

### Flags

| Flag                   | Default             | Description                                              |
| ---------------------- | ------------------- | -------------------------------------------------------- |
| `--host`               | `127.0.0.1`         | hostname/IP given to the client                          |
| `--http-port`          | `8080`              | HTTP/Octo server port                                    |
| `--scene`              | `0`                 | bootstrap new users to scene N (0 = fresh start)         |

### Notes

- The helper intentionally refuses `import-assets` while `~/Downloads/resource_dump_android.7z` is empty or still downloading.
- The server now validates `server/assets/` on startup and exits early if `assets/revisions/0/list.bin` or any `assets/release/*.bin.e` file is missing.
- The APK patcher is version-coupled to client `3.7.1`.

## ⚠️ Legal Disclaimer

**Lunar Tear** is a fan-made, non-commercial **preservation and research project** dedicated to keeping a certain discontinued mobile game playable for educational and archival purposes.

- This project is **not affiliated with**, **endorsed by**, or **approved by** the original publisher or any of its subsidiaries.
- All trademarks, copyrights, and intellectual property related to the original game and its associated franchises belong to their respective owners.
- All code in this repository is original work developed through clean-room reverse engineering for interoperability with the game client.
- No copyrighted game assets, binaries, or master data are distributed in this repository.

**Use at your own risk.** The author assumes no liability for any damages or legal consequences that may arise from using this software. By using or contributing to this project, you are solely responsible for ensuring your usage complies with all applicable laws in your jurisdiction.

This project is released under the [MIT License](LICENSE).

**If you are a rights holder with concerns regarding this project**, please contact me directly.
