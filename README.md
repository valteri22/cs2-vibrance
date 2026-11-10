# CS2 Vibrance

<img src="assets/icon.png" width="64" alt="CS2 Vibrance">

A lightweight Windows tray app that automatically adjusts **NVIDIA Digital Vibrance** for Counter-Strike 2 and restores your original colors when you quit.

### About

Color settings are automatically applied when CS2 launches and unapplied when game closes. Settings are applied just from one slider and you can select an option for the program to start with Windows. It runs in the system tray silently, and checks for CS2 process every 1.5 seconds. Uses NVIDIA's driver API so no injection, networking or external Go dependencies are needed. Safe to use in matchmaking or FaceIT.

### Build

Requires **Windows x64**, an **NVIDIA GPU/display**, and [Go 1.23+](https://go.dev/dl/).

```powershell
go run ./tools/packicon
go build -trimpath -ldflags="-H windowsgui" -o CS2Vibrance.exe .
```

No precompiled binaries included. Fully open source.

### Usage

Run `CS2Vibrance.exe`, choose your vibrance, and close the window to minimize to tray.

- **Left-click tray icon:** Open settings
- **Right-click tray icon:** Exit
- **50%:** NVIDIA's default vibrance

Settings are stored in `%APPDATA%\CS2Vibrance\`.

### License

[MIT](LICENSE)
