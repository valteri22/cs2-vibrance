# CS2 Vibrance

<img src="assets/icon.png" width="56" alt="CS2 Vibrance icon">

Minimal, dark Windows tray app that switches **NVIDIA Digital Vibrance** when `cs2.exe` starts and restores your previous monitor setting when it exits.

- One 0–100% CS2 vibrance slider; 50% is NVIDIA's neutral/default position.
- Optional start with Windows; close the settings window to keep running in the tray.
- Checks for CS2 every 1.5 seconds; no game injection, network access, or third-party Go packages.
- Saves the original NVIDIA driver values before changing them, including a recovery file for unexpected exits.

## Build from source

Requires Windows x64, an NVIDIA-controlled display, and [Go 1.23+](https://go.dev/dl/). Run in PowerShell at the repository root:

```powershell
go run ./tools/packicon
go build -trimpath -ldflags="-H windowsgui" -o CS2Vibrance.exe .
```

The first command generates the Windows icon resource locally from `assets/icon.ico`. **No prebuilt binaries or third-party packages are included.** Review the source before executing it.

## Use

Run `CS2Vibrance.exe`, set vibrance, and close the window to hide it. Click its tray icon to reopen; right-click to exit. Keyboard: Tab selects a setting, arrows adjust vibrance, Space toggles startup.

**Note:** Digital Vibrance is display-wide while CS2 is open, not a true per-game filter. No focus/Alt+Tab detection is performed. If upgrading from CS2 Vibrance Lite, disable the old version's Windows startup and quit it first.

Settings and recovery data: `%APPDATA%\CS2Vibrance\`. The app automatically imports compatible slider and startup settings from an earlier CS2 Vibrance Lite configuration on first run.

## License

MIT — see [LICENSE](LICENSE).
