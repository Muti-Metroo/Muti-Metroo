---
title: DLL Mode (Windows)
description: Run Muti Metroo on Windows from a DLL loaded by rundll32.exe, with no console window and no Administrator rights.
sidebar_position: 6
---

# DLL Mode (Windows)

Each release includes a Windows DLL build of the agent. Loaded with `rundll32.exe`, it runs the agent as a background process with no console window. Combined with a Registry Run entry, it starts the agent at every logon without Administrator rights.

The DLL only runs the agent. Use `muti-metroo.exe` for every other command: `status`, `shell`, `upload`, `service`, and so on. Those commands talk to the running agent over its HTTP API, so they work the same against an agent started from the DLL.

## Download

Get the DLL that matches the host architecture from the [Download page](/download):

| File | Architecture |
|------|--------------|
| `muti-metroo-windows-amd64.dll` | x86_64 |
| `muti-metroo-windows-arm64.dll` | ARM64 |

Both are native builds. Load them with the `rundll32.exe` in `C:\Windows\System32`. The amd64 DLL is UPX-compressed; the ARM64 DLL is not.

## Exported functions

| Export | Arguments | Purpose |
|--------|-----------|---------|
| `Run` | `<config-path>` | Load the config and run the agent until the process is terminated. |
| `Install` | `[service-name] <config-path>` | Install the DLL as a user service (Registry Run) and start it. |

### `Run`

```powershell
rundll32.exe C:\Users\alice\muti-metroo\muti-metroo.dll,Run C:\Users\alice\muti-metroo\config.yaml
```

The agent loads the config, replays persisted runtime changes from its data directory, and runs until the `rundll32.exe` process is terminated. It appears in Task Manager as `rundll32.exe`.

If the config cannot be loaded or the agent fails to start, the process exits silently. Test a new config with `muti-metroo.exe run -c <config>` first, where errors are printed to the console.

Config embedding is not supported for DLL files: `muti-metroo embed-config` and `muti-metroo setup` refuse `.dll` targets. Always pass a config path.

### `Install`

```powershell
# Default service name (muti-metroo)
rundll32.exe C:\Users\alice\muti-metroo\muti-metroo.dll,Install C:\Users\alice\muti-metroo\config.yaml

# Custom service name
rundll32.exe C:\Users\alice\muti-metroo\muti-metroo.dll,Install my-tunnel C:\Users\alice\muti-metroo\config.yaml
```

`Install` does the same as `muti-metroo.exe service install --user --dll <dll> -c <config>`, for hosts where only the DLL is available. It:

1. Stops and removes an existing user service with the same name, so it can be used to upgrade.
2. Writes the Registry Run value (`MutiMetroo` for the default name, `MyTunnel` for `my-tunnel`).
3. Writes `%USERPROFILE%\.<name>\service.info`.
4. Starts the agent through a temporary scheduled task.

The arguments are split on the first space: with two arguments, the first is the service name. Keep the config and DLL paths free of spaces. `Install` reports nothing; check the result with `muti-metroo.exe service status`.

## Start at logon

Install the DLL as a user service with the CLI:

```powershell
muti-metroo.exe service install --user --dll C:\Users\alice\muti-metroo\muti-metroo.dll -c C:\Users\alice\muti-metroo\config.yaml
```

See [System Service - Windows user service](/deployment/system-service#windows-user-service-registry-run) for the flags, the registry value it writes, and how to remove it.

To create the Run entry by hand instead:

```powershell
$dll = "C:\Users\alice\muti-metroo\muti-metroo.dll"
$cfg = "C:\Users\alice\muti-metroo\config.yaml"
Set-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run" `
    -Name "MutiMetroo" -Value "C:\Windows\System32\rundll32.exe $dll,Run $cfg"
```

A Run entry created by hand is not known to `muti-metroo service status` or `service uninstall`.

## Stop the agent

The DLL does not handle console signals, and it does not act on [`muti-metroo stop`](/cli/stop): the request is accepted, but the agent keeps running. Terminate the process instead. To stop only the `rundll32.exe` process that loaded the agent DLL:

```powershell
Get-CimInstance Win32_Process -Filter "Name='rundll32.exe'" |
    Where-Object { $_.CommandLine -like "*muti-metroo*" } |
    ForEach-Object { Stop-Process -Id $_.ProcessId -Force }
```

For an agent installed as a user service, `muti-metroo.exe service uninstall` stops the process and removes the Run entry.

Terminating the process closes peer connections without a graceful shutdown. Peers detect the loss and reconnect when the agent starts again.

## Logging

The agent writes its log to stderr, which is discarded when it runs under `rundll32.exe`. There is no option to log to a file. To observe a running DLL agent, enable the [HTTP API](/configuration/http) and use `muti-metroo.exe status`, `peer status`, and `route status`, or the dashboard endpoints.

## EXE and DLL compared

| | `muti-metroo.exe` | DLL via `rundll32.exe` |
|---|---|---|
| Console window | Yes, unless started with `run --detach` | No |
| Log output | Console (stderr) | Discarded |
| Graceful shutdown | Ctrl+C, `muti-metroo stop` | No, terminate the process |
| Embedded config | Supported | Not supported |
| Boot or logon start | Windows Service (Administrator) | Registry Run (no Administrator) |
| Management commands | Yes | No, use the EXE |
| Process name | `muti-metroo.exe` | `rundll32.exe` |

The DLL runs with the privileges of the user who started `rundll32.exe`. Antivirus and application control products that restrict `rundll32.exe` may block this mode; use the EXE where that is the case.

## See Also

- [System Service](/deployment/system-service) for Windows Service, Registry Run, and `run --detach` setups
- [service CLI](/cli/service) for the `service install --user --dll` flags
- [status CLI](/cli/status) to check a running agent from the command line
- [HTTP API](/configuration/http) to enable the API that the EXE uses to reach a DLL agent
- [Download](/download) for the DLL and EXE builds for amd64 and ARM64
