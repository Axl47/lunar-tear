## Mac LAN Helper

### Canonical Inputs

- APK: `/Users/axel/Downloads/NieR Re[in]carnation 3.7.1.apk`
- Master data: `~/Downloads/20240404193219.bin.e`
- Asset archive: `~/Downloads/resource_dump_android.7z`

### Commands

Check prerequisites and detected LAN state:

```bash
rtk python3 scripts/mac_lan.py doctor
```

Import assets once the archive download is complete:

```bash
rtk python3 scripts/mac_lan.py import-assets \
  --archive ~/Downloads/resource_dump_android.7z \
  --masterdata ~/Downloads/20240404193219.bin.e
```

Start the Dockerized server for LAN access:

```bash
rtk python3 scripts/mac_lan.py start-server --scene 13
```

Build and install the patched client:

```bash
rtk python3 scripts/mac_lan.py prepare-client --install
```

Notes:

- `import-assets` refuses to run if the archive is empty or still downloading.
- `prepare-client` defaults to the local `3.7.1` APK because `patch_apk.py` is version-coupled to that build.
- `start-server` waits for `http://127.0.0.1:8080/healthz` before reporting success.

## How To Patch An APK

### Prerequisites

- Python 3
- `apktool`
- Android build tools (`apksigner`, `zipalign`)
- A decompiled APK directory (from `apktool d`)

### Steps

1. Decompile APK:

```bash
apktool d client/3.7.1.apk -o client/patched
```

2. Run patcher (example for emulator host):

```bash
python3 scripts/patch_apk.py client/patched --server-ip 10.0.2.2 --http-port 8080
```

3. Rebuild:

```bash
apktool b client/patched -o client/patched.apk
```

4. Generate signing key (one-time):

```bash
keytool -genkeypair \
  -v \
  -keystore client/debug.keystore \
  -alias androiddebugkey \
  -storepass android \
  -keypass android \
  -keyalg RSA \
  -keysize 2048 \
  -validity 10000 \
  -dname "CN=Android Debug,O=Android,C=US"
```

5. Align + sign:

```bash
apksigner sign --ks client/debug.keystore --ks-pass pass:android client/patched.apk
```

Notes:

- `--server-ip` replacement must fit existing metadata string lengths (the script validates this).
- The client hardcodes gRPC to port 443. HTTP port is baked into the patched URL.
- `scripts/patch_apk.py` can also be imported and used via `patch_apk_dir(...)` from other Python tooling.

## How To Patch Master Data

### Prerequisites

- Python 3
- `pip install pycryptodome msgpack lz4`

### Steps

1. Basic usage (built-in key/IV, default input path):

```bash
python3 scripts/patch_masterdata.py
```

This reads `server/assets/release/20240404193219.bin.e`, patches it, and overwrites the file.

2. Custom input/output paths:

```bash
python3 scripts/patch_masterdata.py \
    --input original.bin.e --output patched.bin.e
```

3. Dry run (decrypt + patch + report changes, no write):

```bash
python3 scripts/patch_masterdata.py --dry-run
```

4. Override AES key/IV (if the built-in defaults don't match your game version):

```bash
# Via hex strings
python3 scripts/patch_masterdata.py --key 0123...ff --iv abcd...ef

# Via raw binary files (e.g. dumped with Frida)
python3 scripts/patch_masterdata.py --key-file masterdata_key.bin --iv-file masterdata_iv.bin
```

Notes:

- `--key`/`--key-file` and `--iv`/`--iv-file` are mutually exclusive pairs.
- The script patches ~30 tables covering events, quests, gacha, shops, login bonuses, missions, and more. The `m_maintenance` table is emptied to prevent maintenance screens, and `m_omikuji` is skipped.
- The `m_gimmick_sequence_schedule` table is partially patched (only schedules starting before 2023-02) to stay under the client's 1024-entry limit.

## How To Dump Master Data

### Prerequisites

- Python 3
- `pip install pycryptodome msgpack lz4`

### Steps

1. Basic usage (built-in key/IV, default input path, writes to `master_data/`):

```bash
python3 scripts/dump_masterdata.py
```

2. Custom output directory:

```bash
python3 scripts/dump_masterdata.py --output /tmp/dump
```

3. Override AES key/IV:

```bash
# Via hex strings
python3 scripts/dump_masterdata.py --key 0123...ff --iv abcd...ef

# Via raw binary files
python3 scripts/dump_masterdata.py --key-file key.bin --iv-file iv.bin
```

Notes:

- Column names are resolved from the bundled `schemas.json`. Output filenames use the entity class name when a schema match is found (e.g. `EntityMQuestTable.json`), otherwise the raw snake_case table name (e.g. `m_quest.json`).
- `--key`/`--key-file` and `--iv`/`--iv-file` are mutually exclusive pairs.
