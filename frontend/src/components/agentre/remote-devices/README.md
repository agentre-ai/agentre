# remote-devices

Desktop UI for pairing and managing agentred LAN devices.

Spec: `docs/superpowers/specs/2026-05-21-desktop-remote-device-mvp-design.md`
Plan: `docs/superpowers/plans/2026-05-21-desktop-remote-device.md`

## Components

| File | Purpose |
|---|---|
| `remote-devices-panel.tsx` | Settings → 远端 主面板，挂载 hook、调度对话框 |
| `device-row.tsx` | 单台 agentred 行卡片 |
| `device-action-menu.tsx` | 行右侧 `…` 菜单（Refresh / Rename / Edit TLS / Remove） |
| `add-device-dialog.tsx` | Add agentred 对话框：地址 + 6 位 code + name + Advanced TLS Trust |
| `tls-trust-dialog.tsx` | 4 模式 radio：default / pin-cert / ca-bundle / skip-verify |
| `use-remote-devices.ts` | hook：list / mutate / 30 s 轮询 / window focus 重新拉 |
| `format.ts` | `relativeTime` / `deriveDeviceName` / `friendlyLastError` |

## Data flow

```
RemoteDevicesPanel
   │
   ├── useRemoteDevices ─── window.setInterval(30s) → RemoteDeviceRefresh(id) for each
   │                       window 'focus' event   → RemoteDeviceList
   │
   ├── AddDeviceDialog ────── onSubmit({URL, code, name, tlsMode, tlsCertPEM})
   │                                                 ↓
   │                                           svc.Add (Go) → daemon auth.pair
   │
   ├── TLSTrustDialog ──── standalone: writes mode+pem into Add form
   │                       edit-row: svc.UpdateTLS → svc.Refresh
   │
   └── DeviceRow → DeviceActionMenu
                       ├── Refresh → svc.Refresh
                       ├── Rename  → window.prompt → svc.Rename
                       ├── Edit TLS → opens TLSTrustDialog
                       └── Remove   → window.confirm → svc.Remove
```

## Manual smoke test (M7)

Requires mac with `agentre` + linux VM (or another machine) running `agentred`.

```bash
# On remote machine
agentred run --port 7456
agentred pair    # copy printed code

# On desktop
agentre   # open Settings → 远端 → + Add agentred
# Paste URL, paste 6-char code, leave TLS = Default, click Pair
# → row appears, status dot turns green within 30s

# Edit TLS → switch to Pin certificate → paste cert → Apply
# → row updates immediately (Refresh runs)

# Stop remote agentred
# → within 30s, row dot turns muted, last_error filled

# Restart remote agentred (same state.json)
# → within 30s, row dot turns green again

# Delete remote state.json + restart agentred
# → Refresh shows tofu_mismatch in red

# Remove device → DB row + keychain token disappear (verify via
# `sqlite3 ~/Library/Application\ Support/agentre/agentre.db
#  "SELECT name, url, status FROM paired_agentreds"`)
```
