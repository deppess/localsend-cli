# localsend-cli

A headless [LocalSend](https://localsend.org) v2.1 client for the terminal. Send and receive files over the LAN without a GUI — works alongside the official LocalSend apps on any device on the same network.

## Installation

```sh
go install github.com/deppes/localsend-cli@latest
```

Or build from source:

```sh
# GitHub
git clone https://github.com/deppess/localsend-cli
# Codeberg
git clone https://codeberg.org/deppes/localsend-cli
cd localsend-cli
go build -o localsend-cli .
```

## Configuration

On first run, a config file is created at `~/.config/localsend-cli/config.toml`:

```toml
[device]
  name = "my-laptop"
  port = 53317
  type = "headless"

[receive]
  dir = "~/Downloads"
  prompt_timeout = 30   # seconds to wait before auto-rejecting; 0 = wait forever
  max_file_mb = 0       # maximum file size in MB; 0 = unlimited

[discovery]
  timeout_ms = 500
  upload_concurrency = 4

[whitelist]
  enabled = false
  ips = []

[favorites]
  "Desktop" = "192.168.1.10"
  "phone"   = "192.168.1.20"

[trusted]
```

**Favorites** are auto-accepted without a prompt and used by the `pick` TUI and the [localsend-cli-ui.yazi](https://github.com/deppess/yazi-plugins) plugin.

**Whitelist** (`enabled = true`) restricts incoming transfers and discovery to the listed IPs only. Whitelist filtering is enforced at the HTTP layer — UDP multicast is always sent to the standard multicast group so official LocalSend apps can discover this device normally.

## Commands

### `receive`

Wait for incoming files. Shows a TUI with accept/reject prompt for non-favorites; favorites are auto-accepted.

```sh
localsend-cli receive
localsend-cli receive --dir ~/Desktop
```

**`--headless`** — no TUI; auto-accept all transfers, exit after one complete session, print received filenames to stdout. Designed for use from scripts and plugins:

```sh
localsend-cli receive --headless
# stdout on completion:
# TOTAL:2
# photo.jpg
# document.pdf
```

> **Security note:** `--headless` accepts transfers from any device on the network. Enable the whitelist (`whitelist.enabled = true`) when running in untrusted environments.

### `send`

Send one or more files or directories to a device by IP.

```sh
localsend-cli send --to 192.168.1.10 file.zip
localsend-cli send --to 192.168.1.10 photo.jpg video.mp4 folder/
```

### `pick`

Interactive TUI that scans the network and lets you select a device. Prints the chosen device's IP to stdout. Useful for scripting:

```sh
ip=$(localsend-cli pick)
localsend-cli send --to "$ip" file.zip
```

### `discover`

Scan the network and print found devices as a JSON array, then exit.

```sh
localsend-cli discover
```

**`--stream`** — emit each device as a JSON line as it is found, run until `Ctrl+C`:

```sh
localsend-cli discover --stream
```

### Global flags

```
--port int   override the port from config
```

## Discovery

localsend-cli uses both UDP multicast (`224.0.0.167:53317`) and an HTTP scan loop to find peers:

- **UDP**: listens for announcements and broadcasts its own presence every 2 seconds on all up, multicast-capable interfaces.
- **HTTP**: proactively POSTs to every host on the local subnet (or only whitelisted IPs) every 2 seconds to register with devices that may have missed the UDP broadcast.
- **Re-announce**: periodically re-POSTs to all known peers so devices that refresh their list (e.g. the iOS app) continue to see this device.

TLS is self-signed with TOFU (trust on first use). On first contact with a new peer, the fingerprint is printed to the log and saved to `trusted` in the config — verify it out-of-band if the network is untrusted.

## Yazi plugin

[localsend-cli-ui.yazi](https://github.com/deppess/yazi-plugins) wraps this CLI for use inside [Yazi](https://github.com/sxyazi/yazi): `md` to receive, `mp` to send selected files.

## License

MIT — see [LICENSE](LICENSE).
