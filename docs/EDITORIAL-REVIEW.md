# Editorial review

Page-by-page editorial audit of the documentation against the MDN editorial model in [STYLE.md](STYLE.md). An editor who did not write the page read it in full and checked its purpose, structure, duplication with the canonical page, tone, examples, and See Also section, and verified at least three concrete claims against the source code or a built binary. Problems were fixed in the page.

111 pages reviewed: 61 passed as written, 50 fixed. 443 claims verified against the implementation. The mechanical checks (commands, flags, config examples, HTTP paths, style rules) are in [VERIFICATION.md](VERIFICATION.md) and run in CI.

## `api/agents.md`

**Result:** fixed. Purpose clear; structure fine; examples match live agent output; See Also 4 links with reasons

Claims verified:

- GET /agents shape (display_name only on local) -> live curl localhost:30081/agents
- /agents/{id} fields -> live curl: no 'status' field, exit_running present
- Errors: agent ID required 400, invalid agent ID format 400, 405, 502 failed to fetch: no route to agent -> live curl; 30s timeout -> internal/health/server.go:1466
- /agents/{id}/peers returns full IDs -> live curl

Fixes:

- Corrected /agents/{id} vs /healthz comparison: added that the 'status' field is absent
- Added 502 'remote agent error: <reason>' row (internal/health/server.go:1475)

## `api/dashboard.md`

**Result:** fixed. Purpose clear; reference tables complete; examples match live output; See Also 5 links

Claims verified:

- /api/dashboard shape and peers/routes fields -> live curl localhost:30081/api/dashboard
- /api/setup-defaults sends Cache-Control: no-store and has_* flags instead of keys -> live curl -D-
- /api/mesh-test POST shape and PUT -> 405 -> live curl
- /api/topology roles, connections, shell_enabled only for remote agents -> live curl

Fixes:

- Frontmatter description used ' - ' as a dash separator; rewrote with a colon and quoted it (unquoted colon broke the Docusaurus YAML parse)
- 503 error row now lists both 'provider not configured' and 'remote provider not configured' (internal/health/server.go:2448, meshtest.go:74)

## `api/debugging.md`

**Result:** pass. Purpose and security warning clear; endpoint table before guides; runnable examples; See Also 3 links

Claims verified:

- pprof handlers registered only when http.pprof enabled -> internal/health/server.go:986-994
- Block and mutex profiles empty because sampling not enabled -> grep SetBlockProfileRate/SetMutexProfileFraction found nothing in repo
- goroutine?debug=1 first line format -> live curl returned 'goroutine profile: total 27'

## `api/display-name-management.md`

**Result:** pass. Purpose clear; request/response/errors per endpoint; See Also 3 links

Claims verified:

- set/get responses and message text -> live curl localhost:30081/display-name/manage
- 255-byte limit error, unknown action error, invalid request error -> live curl
- Remote set signed and forwarded, 502 failed to send request on unknown agent -> live curl + internal/health/server.go:2021-2027

## `api/file-transfer.md`

**Result:** pass. Purpose clear; requirements before reference; all actions exercised; See Also 4 links

Claims verified:

- Upload response fields and directory=true tar upload -> live curl to agent C
- Download headers Content-Disposition, X-File-Mode, X-Original-Size; directory returns <name>.tar.gz application/gzip -> live curl -D-
- browse list default limit 100 max 5000 -> internal/filetransfer/browse.go:15-16
- 30 minute transfer timeout -> internal/health/server.go:1594,1671
- delete non-empty dir error and 'path not in allowed list' 400; local ID browse answered locally ('file transfer is disabled') -> live curl

## `api/forward-management.md`

**Result:** fixed. Purpose clear; structure per endpoint; See Also 4 links

Claims verified:

- add/remove/list listener messages and fields -> live curl localhost:30081/forward/manage
- Endpoint add message is JSON-escaped '->' -> live curl on local and remote endpoints
- config endpoint cannot be removed, invalid target error -> live curl; error strings at internal/agent/agent.go:2779-2982

Fixes:

- Replaced garbled sentence ("The `>` escape in `message` is the `>` character") and made the example show the raw '>' the agent actually emits, with an explanation

## `api/health.md`

**Result:** fixed. Purpose clear; table then per-endpoint reference; See Also 4 links

Claims verified:

- /health returns OK text/plain, /ready READY -> live curl -i
- /healthz field set -> live curl
- Non-GET returns 405 method not allowed -> live curl -XPOST /health and /healthz; requireGET at internal/health/server.go:1201

Fixes:

- Container probes section had a bare YAML block; added an intro sentence and the requirement that http.address be reachable by the kubelet (not 127.0.0.1)

## `api/hosts-management.md`

**Result:** fixed. Purpose clear; local-only scope stated; See Also 5 links

Claims verified:

- add/remove/list responses and 409/404/400 errors -> live curl localhost:30081/hosts/manage
- suffix add/remove, suffix-in-use 409 -> live curl /hosts/suffixes/manage
- Not blocked by topology restriction -> internal/health/server.go:1860-1945 has no shouldRestrictTopology call

Fixes:

- Added 400 'invalid request: ...' (invalid JSON) rows to both error tables (internal/health/server.go:1876,1923)

## `api/icmp.md`

**Result:** fixed. Purpose clear; message table before flow; example client runnable (tested with node 24); See Also 3 links

Claims verified:

- init/init_ack/reply exchange and src_ip present -> node WebSocket client against live agent
- Invalid dest_ip: init_ack error then close 1002 -> node client output
- Non-upgrade request returns 426 -> curl
- No message is sent on echo timeout -> internal/icmp/handler.go:300-309; 'error' type only on decrypt failure -> internal/agent/icmp.go:396-413
- src_ip is the exit agent's outbound source address -> internal/icmp/handler.go:319-321,469-483

Fixes:

- Corrected 'error' message meaning (decrypt failure, not timeout) and documented that unanswered echoes produce nothing, so clients need their own timeout
- Corrected src_ip description
- Session-flow step 3 no longer promises a reply or error per echo
- Errors table: added 400 invalid agent ID, 426 non-upgrade, 401 token, close codes 1002/1011, no-answer row; removed unreachable internal 503 row

## `api/listener-management.md`

**Result:** fixed. Purpose clear; request table, per-action examples, errors; See Also 4 links

Claims verified:

- add quic/ws responses and list fields incl. advertise_address -> live curl localhost:30081/listeners/manage
- config listener cannot be removed/replaced, not-found, bind failure 'start listener:' errors -> live curl
- Loopback advertise_address rejected -> live curl

Fixes:

- Remote example added 0.0.0.0:4433 (normally the configured listener, which is rejected); changed to :4434
- Added 'path is required for ws transport' variant and the advertise.address loopback error row

## `api/overview.md`

**Result:** pass. Purpose clear; reference index before narrative; no duplication beyond link-index; neutral tone; ASCII; See Also 5 links with reasons

Claims verified:

- Endpoint groups and default-on remote_api/dashboard/pprof, minimal disables all -> internal/config/config.go:685-706
- Auth-exempt paths /health,/healthz,/ready,/,/logo.png,/notfound.png -> internal/health/server.go:696-703
- 401 body 'unauthorized' with WWW-Authenticate Bearer realm, ?token= fallback -> internal/health/server.go:714-732
- http.enabled default false -> internal/config/config.go:966-967
- Short ID rejected with 400 invalid agent ID format -> curl localhost:30081/agents/eed75a93 returned 400

## `api/peer-management.md`

**Result:** pass. Purpose clear; examples and errors match; See Also 4 links

Claims verified:

- add/remove messages incl. '(disconnected <short>)' -> live curl localhost:30081/peers/manage
- list fields incl. direction inbound and persistent false -> live curl
- Error strings for transport, id, tcp_keepalive, config peer -> live curl; default h2/ws path /mesh -> internal/transport/h2.go:20, ws.go:21

## `api/probe.md`

**Result:** pass. Purpose clear; warning justified; request table and errors; example script runs as written; See Also 4 links

Claims verified:

- Success and failure response shapes, 200 on failed probe -> live curl /api/probe
- All 400 validation messages -> live curl
- Remote probe and the probe-from-every-agent script -> ran against live mesh, printed 'ok 0 ms' per agent

## `api/route-management.md`

**Result:** fixed. Purpose clear; field table, per-action examples, errors; See Also 5 links

Claims verified:

- add/set_comment/set_source_ip/set_interface/remove messages -> live curl localhost:30081/routes/manage
- list scope all with dynamic false for exit.routes -> live curl on agent C
- Error strings for config route, invalid CIDR, scope, action, exit handler not configured -> live curl
- POST /routes/advertise response -> live curl

Fixes:

- Added the address-family mismatch error row for source_ip

## `api/route-trace.md`

**Result:** fixed. Purpose clear; response variants and field tables; See Also 4 links

Claims verified:

- IP trace response shape -> live curl /route/trace
- Mesh registry hostname resolution 'mesh' -> live curl with db.mesh
- Unresolvable hostname returns 200 with error -> live curl nonexistent.invalid
- dest is required 400 -> live curl

Fixes:

- Replaced a real public IP (93.184.215.14) in the domain-route example with documentation address 203.0.113.80

## `api/shell.md`

**Result:** fixed. Purpose clear; message table is the client contract (kept); See Also 4 links

Claims verified:

- Message type bytes 0x01-0x0a -> internal/shell/messages.go:15-24
- Error codes 21/22 -> internal/shell/messages.go:34-37
- Stream session ACK/STDOUT/EXIT -> node WebSocket client against live agent
- Shell disabled target yields ERROR without code and close 1011 -> node client against agent B
- Non-upgrade request returns 426 -> curl

Fixes:

- Pre-upgrade errors corrected: 426 for non-upgrade (not 400), 400 for invalid agent ID
- Removed claim of close code 1000 after EXIT (server drops the connection without a close frame; clients see 1006); step 5 now tells clients to treat EXIT as end of session

## `api/sleep.md`

**Result:** fixed. Purpose clear; per-endpoint reference and errors; See Also 3 links

Claims verified:

- POST /sleep response and 'already sleeping' 500 -> live curl
- 'not sleeping' 500 on wake while awake -> live curl
- POST /wake blocks for 2*poll_interval+poll_duration before responding -> internal/agent/agent.go:8344-8371 and a live request hung >5 minutes while the agent had already logged 'exited sleep mode'
- Default poll_interval 5m, duration 30s -> internal/config/config.go:1000-1002; 405 on wrong method -> live curl

Fixes:

- Replaced 'response only confirms the command was sent' with accurate wording and a check via GET /sleep/status
- Documented that POST /wake responds only after the re-send period (10m30s by default), past http.write_timeout
- Status examples now show the default poll_interval_sec 300

## `api/stop.md`

**Result:** pass. Purpose clear; service-manager caveat links to canonical CLI page; See Also 3 links

Claims verified:

- POST /shutdown response 'shutting-down' -> live curl on agent B (agent exited)
- Remote shutdown with {} -> live curl to agent C (agent exited)
- Empty body returns 500 'failed to sign request: parse mutation body: unexpected end of JSON input' -> live curl

## `cli/cert.md`

**Result:** fixed. Synopsis/flags/examples per subcommand; outputs match binary; See Also 4 links

Claims verified:

- Flags and defaults (--cn, --days 365, -o ./certs, --ca, --ca-key, --dns, --ip) -> mm-0 cert {ca,agent,client,info} --help
- Example output formats and file modes 0644/0600 -> ran the examples, ls -l certs
- Status bands and run startup warnings -> cmd/muti-metroo/main.go:2294-2304, 3880-3917
- ECDSA P-256 -> internal/certutil/certutil.go:170; invalid --ip fails with exit 1 -> ran mm-0 cert agent --ip bogus

Fixes:

- Moved the 'Certificate types' summary table ahead of the per-command reference so readers see which command to use first

## `cli/completion.md`

**Result:** pass. Purpose clear in first line; synopsis, flag table, per-shell loading; no duplication; neutral tone; examples match built-in help; See Also has 3 links with reasons.

Claims verified:

- --no-descriptions flag exists on each shell subcommand -> mm-1 completion bash --help
- bash requires bash-completion package and Linux/macOS install paths -> mm-1 completion bash --help
- zsh ${fpath[1]}/_muti-metroo and brew site-functions paths, compinit line -> mm-1 completion zsh --help
- PowerShell Out-String \| Invoke-Expression and profile note -> mm-1 completion powershell --help

## `cli/config.md`

**Result:** pass. Reference first (validate, show), flags tables, real output, exit status; redaction list matches; See Also 4 links with reasons.

Claims verified:

- validate text output 'OK: ok.yaml (file)' and failure message for invalid transport -> mm-1 config validate ok.yaml / bad.yaml
- invalid config exits 5, unreadable file exits 5, --json on invalid exits 0 -> mm-1 config validate bad.yaml; echo $? (5), --json (0), nonexist.yaml (5)
- --json object keys path/ok/embedded/error -> mm-1 config validate --json
- config show --diff-from-default prints only changed keys -> mm-1 config show ok.yaml --diff-from-default
- Redacted keys list -> internal/config/config.go:1821-1822 and mm-1 config show --help

## `cli/display-name.md`

**Result:** pass. Clear purpose; set/get reference with flags, prompt, output, examples; persistence and privilege notes link to canonical config pages; See Also 4 links.

Claims verified:

- 255-byte limit (protocol.MaxStringLen) and journal persistence via OpDisplayNameSet -> internal/agent/agent.go:3489-3517
- Empty-name prompt text and 'Display name reverted to config value' / 'Display name set' output -> cmd/muti-metroo/main.go:7063,7125-7127
- set requires management signing private key; restricted when public_key without private_key -> internal/health/server.go:2261-2290 and requireMutationAuthorization

## `cli/embed-config.md`

**Result:** fixed. Synopsis, arguments, flags, description, examples, exit status present; one inaccurate claim about data_dir; See Also 4 links.

Claims verified:

- Rejects non-.yaml/.yml file names with exit 1 -> mm-1 embed-config ok.txt (exit 1); cmd/muti-metroo/main.go:453-456
- Invalid config leaves binary untouched, exit 1; missing argument exit 2 -> mm-1 embed-config bad.yaml -b src-bin -o out-bin (exit 1); mm-1 embed-config (exit 2)
- Output 'Embedded config into <path>' and resulting binary validates as embedded -> mm-1 embed-config ok.yaml -b src-bin -o out-bin; ./out-bin config validate ./out-bin
- DLL rejection, permissions preserved via chmod srcMode -> cmd/muti-metroo/main.go:464-466,533-535
- agent.data_dir defaults to ./data -> internal/config/config.go:917 and mm-1 config show

Fixes:

- Replaced the claim that data_dir 'must be set' with the actual default (./data, relative to the working directory) and advice to use an absolute path

## `cli/file-transfer.md`

**Result:** pass. Requirements up front, upload/download reference with flags, output, examples, exit status; links to config/API pages instead of duplicating; See Also 5 links.

Claims verified:

- Remote path must be absolute, exit 1 -> mm-1 upload ... f.txt tmp/x (exit 1)
- Unreachable gateway exits 1 for upload and download -> mm-1 upload/download -a 127.0.0.1:31999 (exit 1)
- upload --resume is accepted but has no effect (server ignores it) -> internal/health/server.go:1520-1525
- Default max_file_size 500 MB -> internal/config/config.go:974
- Directory upload resume warning -> cmd/muti-metroo/main.go:2545-2548

## `cli/forward.md`

**Result:** pass. Clear two-halves intro; each subcommand has synopsis, flags, prompt, output, examples; outputs match a live agent; See Also 5 links.

Claims verified:

- forward add/list/list --json output format and MAX_CONN 100 -> live agent on 127.0.0.1:31080: mm-1 forward add api-server ... --max-connections 100; forward list (--json)
- endpoint add/list output -> mm-1 forward endpoint add db 127.0.0.1:5432; forward endpoint list --json
- Non-TTY remove without --yes exits 6 -> mm-1 forward remove api-server </dev/null (exit 6)
- Prompt texts -> cmd/muti-metroo/main.go:5787,6056
- -t accepts unique prefix -> mm-1 forward add x ... -t 2292 succeeded on remote agent

## `cli/hash.md`

**Result:** pass. Purpose clear; config keys table, flags, examples, scripting guidance; See Also 4 links.

Claims verified:

- Argument form prints the shell-history warning to stderr and hash to stdout -> mm-1 hash "correct horse" 2>/dev/null and 2>&1 >/dev/null
- Cost range 4-31 enforced -> mm-1 hash --cost 3 x ('cost must be between 4 and 31')
- Empty password rejected -> mm-1 hash "" ('password cannot be empty')
- Prompt reads terminal only, fails on piped stdin -> echo pw \| mm-1 hash ('inappropriate ioctl for device')
- Default cost 12 -> mm-1 hash --help

## `cli/host.md`

**Result:** pass. Name rules before subcommands; every subcommand has synopsis, flags, output, examples; outputs match a live agent; See Also 5 links.

Claims verified:

- host add / list / list --json output -> live agent: mm-1 host add target-11.mesh --ipv4 10.8.0.11 --comment "edge worker"; host list --json
- Suffix removal blocked while used, exit 5, message 'suffix has registered hosts: alt used by gw.alt' -> mm-1 host suffix remove alt
- Single-label name equal to suffix rejected -> mm-1 host add mesh --ipv4 10.1.1.1
- Suffix list output '.alt' '.mesh' and JSON -> mm-1 host suffix list (--json)
- Last-suffix prompt on non-TTY exits 6 -> mm-1 host suffix remove mesh </dev/null

## `cli/init.md`

**Result:** pass. Purpose and when to use it in first paragraph; flags, description, example output; See Also 3 links.

Claims verified:

- Output 'Agent initialized in' / 'Agent already initialized in' -> mm-1 init -d ./initt twice
- Directory mode 0700 and agent_id mode 0600 -> ls -la initt
- Does not create agent_key -> only agent_id present after init; cmd/muti-metroo/main.go:553-580

## `cli/listener.md`

**Result:** fixed. Prerequisites, per-subcommand reference, persistence, exit status; text vs JSON examples were inconsistent; remote_api wording implied it must be explicitly set; See Also 5 links.

Claims verified:

- Output formats for add, list, list --json, remove -> live agent on 127.0.0.1:31080
- Missing --path for h2, --plaintext on quic, loopback advertise address, removing a config listener all exit 5 -> mm-1 listener add/remove (exit 5 each)
- Remove prompt text -> cmd/muti-metroo/main.go:6822
- /listeners/manage only registered when remote API enabled (default on) -> internal/health/server.go:935-962, internal/config/config.go:702-706

Fixes:

- Reworded prerequisite to say http.remote_api must stay at its default true rather than implying it must be set
- Added the missing dynamic QUIC listener to the --json example so it matches the 4-row text table

## `cli/management-key.md`

**Result:** fixed. Purpose clear; generate/public reference with real output; Configuration section duplicated YAML already shown in output and owned by configuration/management; See Also 4 links.

Claims verified:

- --out writes 0644 public / 0600 private and prints only the public key -> mm-1 management-key generate --out ./keys; ls -l keys
- Refuses to overwrite without --force -> second generate --out ./keys ('already exists; use --force')
- public --stdin derives the key from the file -> mm-1 management-key public --stdin < keys/management-private.key
- Route paths are not encrypted -> internal/flood/flood.go:684
- Validator rejects private_key without public_key -> internal/config/config.go:1369-1370

Fixes:

- Replaced the two duplicated YAML blocks in Configuration with one sentence pointing to the generate snippets, keeping the link to the canonical management configuration page

## `cli/mesh-test.md`

**Result:** pass. Synopsis, description, flags, output, JSON, examples, exit status; output matches live run; See Also 4 links.

Claims verified:

- Text and JSON output format -> mm-1 mesh-test -a 127.0.0.1:31080 (--json)
- Up to 10 in parallel, 10s per agent -> internal/health/meshtest.go:150,216
- CLI POSTs so a fresh test runs every time -> cmd/muti-metroo/main.go:1082; GET is cached 30s internal/health/meshtest.go:46,105
- Endpoint gated by dashboard -> internal/health/server.go:973-977

## `cli/overview.md`

**Result:** fixed. Good hub page: command groups match root help, agent address, global flags, prompts, exit codes, deprecations; was missing which HTTP settings the client commands need, and --yes for default-route prompt; See Also 5 links.

Claims verified:

- Command grouping matches -> mm-1 --help
- Prompts written to stderr, non-TTY without --yes fails with exit 6 -> cmd/muti-metroo/main.go confirmAction
- Exit code mapping 3/4/5/6 -> cmd/muti-metroo/main.go:320-345
- --yes also skips the typed default-route confirmation -> confirmTyped returns true when yes, cmd/muti-metroo/main.go:4823
- Management endpoints only registered with remote API -> internal/health/server.go:935-962

Fixes:

- Added a paragraph under Agent address stating that client commands need http.enabled, and that configuration-changing commands, route trace, display-name, sleep, wake, stop and remote targeting also need http.remote_api (default on), linking to HTTP configuration
- Noted that --yes, not only --allow-default-route, skips the default-route typed confirmation

## `cli/peer.md`

**Result:** fixed. status vs list distinction clear; full reference with outputs; prerequisite wording implied explicit settings; See Also 5 links.

Claims verified:

- peer status --json fields -> mm-1 peer status -a 127.0.0.1:31080 --json
- --id warning text on stderr and 'Peer added' output -> mm-1 peer add 127.0.0.1:31435
- Invalid --tcp-keepalive exits 2, unknown transport exits 5 -> mm-1 peer add ... --tcp-keepalive xx (2), --transport carrier (5)
- peer list text/JSON with inbound direction and '-' REMOTE_ID when disconnected -> mm-1 peer list (--json)
- Deprecated 'peers' warning text and hidden from help -> mm-1 peers; mm-1 --help \| grep -c peers (0)

Fixes:

- Reworded prerequisite: add/remove/list need http.remote_api and status needs http.dashboard, both on by default

## `cli/ping.md`

**Result:** pass. Synopsis, arguments, flags, output, examples, error table, exit status; output matches live run; See Also 5 links.

Claims verified:

- Output format with 12-char ID header, Reply lines and statistics -> mm-1 ping -c 2 -a 127.0.0.1:31080 2292 127.0.0.1
- Hostname rejected 'destination must be a valid IP address' exit 1 -> mm-1 ping ... example.com
- Unreachable gateway 'failed to connect to ICMP endpoint' exit 1 -> mm-1 ping -a 127.0.0.1:31999 ...
- Defaults count 4, interval 1s, timeout 5s -> mm-1 ping --help

## `cli/probe.md`

**Result:** pass. probe and probe listen each fully referenced with flags, outputs, errors, examples, exit status; See Also 5 links.

Claims verified:

- Success output and JSON fields -> mm-1 probe --insecure 127.0.0.1:31433; mm-1 probe --ca certs/ca.crt ... --json
- Failed probe with --json exits 0 and success:false; text failure exits 1 -> mm-1 probe --json 127.0.0.1:31433 (0); mm-1 probe 127.0.0.1:31433 (1)
- --plaintext without -T ws and invalid --timeout exit 1 -> mm-1 probe --plaintext ...; mm-1 probe -w xx ...
- probe listen header output -> mm-1 probe listen -a 127.0.0.1:31501

## `cli/route.md`

**Result:** fixed. status vs list explained; every subcommand fully referenced; outputs match live agent; prerequisite wording implied explicit settings; See Also 5 links.

Claims verified:

- Invalid CIDR and wrong-family --source-ip exit 2 before contacting the agent -> mm-1 route add 300.0.0.0/8 (2); --source-ip ::1 (2)
- Config route removal and unknown route exit 5 -> mm-1 route remove 10.99.0.0/16 -t 2292 (5); 10.55.0.0/16 (5)
- route list --all with SOURCE/INTERFACE columns and JSON -> mm-1 route list --all -t 2292 (--json)
- route trace winner/loser format and JSON resolution field -> mm-1 route trace 10.99.1.3 (--json)
- Remove prompt format -> cmd/muti-metroo/main.go:4935

Fixes:

- Reworded prerequisite: every subcommand except status needs http.remote_api, status needs http.dashboard, both on by default

## `cli/run.md`

**Result:** pass. Purpose, synopsis, flags, startup sequence, shutdown, embedded config, examples; See Also 5 links.

Claims verified:

- Startup stdout/log lines -> a.log from mm-1 run -c a.yaml
- --detach fails on non-Windows with nohup/setsid hint, exit 1 -> mm-1 run -c a.yaml -D
- 10-second shutdown timeout then exit 1 -> cmd/muti-metroo/main.go:717-731
- default_action: help for embedded binary with no args -> cmd/muti-metroo/main.go:306-310

## `cli/service.md`

**Result:** pass. Platform matrix, install/status/uninstall reference, backend details, management commands, comparison table; See Also 5 links.

Claims verified:

- status text and --json when not installed -> mm-1 service status (--json)
- systemd unit Restart=on-failure, RestartSec=5, ProtectSystem=strict, ProtectHome=read-only -> internal/service/service_linux.go:190,216-217
- launchd plist com.<name> in /Library/LaunchDaemons, logs in config dir -> internal/service/service_darwin.go:13,176-184
- Uninstall prompt accepts y/Y/yes, -f skips -> cmd/muti-metroo/main.go:1835-1843,1872-1880
- -c required for install -> service install code 'config file is required: use -c flag'

## `cli/setup.md`

**Result:** pass. Purpose clear; synopsis, flags, ordered wizard steps, delivery options; no duplication; neutral tone; See Also 5 links with reasons

Claims verified:

- Wizard has 11 steps in the documented order (basic, roles, listener, TLS, peers, SOCKS5, exit, monitoring, shell, file transfer, management keys) -> internal/wizard/wizard.go:112-188 (Step 1/11..11/11 comments)
- -c rejects .dll paths -> cmd/muti-metroo/main.go:408-411
- With -c the config path and data dir questions are skipped -> internal/wizard/wizard.go:255-262
- Delivery choices 'Save to config file' / 'Embed in binary' -> internal/wizard/wizard.go:2742-2743

## `cli/shell.md`

**Result:** pass. Requirements up front, synopsis/args/flags/modes/exit status; examples runnable; See Also 5 links

Claims verified:

- Default command is bash -> cmd/muti-metroo/main.go:2390
- TERM defaults to xterm-256color, 'Connected to <name> (<short-id>)' and 'Connection to <name> closed.' on stderr -> internal/shell/client.go:152,466,474
- Startup errors 'failed to connect', 'remote error', 'timeout waiting for shell session to start' return exit 1 -> internal/shell/client.go:126,179,192
- Flags -a/-w/--tty and defaults -> mm-2 shell --help

## `cli/signing-key.md`

**Result:** pass. Reference first, generate/public subcommands with flags and real output; sleep/wake unsigned behavior table correct; See Also 5 links

Claims verified:

- generate --out writes 0644/0600 files, dir 0700, output text matches -> ran mm-2 signing-key generate --out ./keys; ls -la
- Refuses to overwrite without --force -> second run: 'Error: keys/signing-public.key already exists; use --force to overwrite'
- public --stdin derives same key -> mm-2 signing-key public --stdin < keys/signing-private.key
- Sleep/wake accepted unsigned without signing public key, 5 minute timestamp window -> internal/flood/flood.go:172-174,1618-1637; internal/crypto/mgmtauth.go:17

## `cli/sleep.md`

**Result:** pass. Covers sleep, wake, sleep-status with flags, prompts, outputs, JSON fields; See Also 5 links

Claims verified:

- Prompt texts 'Will flood SLEEP ...' and 'Will flood WAKE ...' -> cmd/muti-metroo/main.go:4364,4451
- Outputs 'Sleep mode triggered: sleep command triggered' / wake text -> cmd/muti-metroo/main.go:4410,4497-4499; internal/health/server.go:2364,2388
- ~6m30s from poll_interval 5m and 0.3 jitter defaults -> internal/config/config.go:1000-1001
- 'not sleeping' error -> internal/sleep/sleep.go:57

## `cli/status.md`

**Result:** pass. Synopsis, flags, output with field table, JSON, exit status; See Also 5 links

Claims verified:

- Reads /healthz and prints only the seven documented fields -> cmd/muti-metroo/main.go:745-828
- /healthz exempt from bearer auth -> internal/health/server.go:696-698
- 'unavailable' when agent not running -> internal/health/server.go:1216-1221
- Unreachable agent maps to errAgentUnreachable (exit 3) -> cmd/muti-metroo/main.go status RunE

## `cli/stop.md`

**Result:** pass. Synopsis, flags, authorization, prompt, output, service-manager table; See Also 4 links

Claims verified:

- Prompt text 'Will stop %s.\nThe process will exit; any service manager ...' -> cmd/muti-metroo/main.go:4549-4551
- Output 'Shutdown triggered: agent will exit shortly' -> cmd/muti-metroo/main.go:4601; internal/agent/agent.go:3308
- 10 second drain -> cmd/muti-metroo/main.go:4520
- Flags -a and -t/--target -> mm-2 stop --help

## `comparisons/vs-ssh-jump.md`

**Result:** fixed. Neutral comparison; removed a differentiator that was not a real difference; corrected link for reconnection settings; See Also 5 links

Claims verified:

- Reconnect backoff 1s to 60s with jitter, MaxRetries 0 = forever -> internal/config/config.go:951-957
- peers[].proxy exists for ws and id 'auto' accepts any peer -> internal/config/config.go:388-393; internal/agent/agent.go:1235,1245
- TLS 1.3 minimum on peer links -> internal/transport/tls.go:42,53

Fixes:

- Reconnection settings link pointed to Peers configuration; they live under connections.reconnect, so the link now goes to /configuration/connections
- Removed 'Intermediate hosts should not be able to read the traffic they relay' from 'When to use Muti Metroo', since the page's own security table states SSH jump hosts also cannot read the inner session

## `concepts/agent-roles.md`

**Result:** fixed. Clear purpose and roles table; removed marketing word and an unnecessary admonition; See Also 5 links

Claims verified:

- /api/nodes roles values ingress, exit, transit, forward_ingress, forward_exit -> internal/health/server.go:511,2746-2753
- Ingress: domain routes first, then local resolution (mesh hosts) and CIDR -> internal/agent/agent.go:2256; internal/routing/domain.go:252-274
- Transit has no setting; wizard role list only used for prompts -> internal/wizard/wizard.go:125,459

Fixes:

- Removed 'simply' from the transit paragraph
- Converted the ':::info Agent reachability' admonition (not one of the style guide's admonition types) into a plain paragraph

## `concepts/architecture.md`

**Result:** fixed. Building blocks, connection model, data and route flow, security model; corrected an overstated security claim; See Also 5 links

Claims verified:

- Route metric incremented by one per hop -> internal/routing/manager.go:564; internal/flood/flood.go:378
- TLS 1.3 on every transport -> internal/transport/tls.go:42
- plaintext only for ws -> internal/config/config.go:1543-1546
- Sleep/wake signatures only enforced when signing public key set -> internal/flood/flood.go:1628-1630

Fixes:

- Security model row said sleep/wake always require signed requests; changed it so sleep and wake require a signature only on agents with the signing public key configured, matching flood.go

## `concepts/routing.md`

**Result:** pass. Route types, propagation, selection order, CIDR/domain matching, expiry, redundancy; no internals; See Also 5 links

Claims verified:

- Longest prefix, then metric, then origin agent ID tiebreaker -> internal/routing/table.go:233-260
- Exact domain before single-level wildcard -> internal/routing/domain.go:252-274
- Mesh host registry checked before DNS at ingress -> internal/agent/agent.go:2256,4909
- advertise_interval 2m, route_ttl 5m -> internal/config/config.go:942-943

## `concepts/streams.md`

**Result:** fixed. Concept page with lifecycle, E2E, app-visible behavior, limits summary linking to canonical page; See Also had a wrong canonical link

Claims verified:

- Only max_streams_total caps streams -> internal/stream/manager.go:404,549 (MaxStreamsPerPeer and BufferSize are stored but never compared); max_pending_opens has no reader outside internal/config
- limits.stream_open_timeout is only passed for shell streams -> internal/agent/agent.go:7654; SOCKS and forward opens use a fixed 30 s
- Half-close is supported -> internal/stream/manager.go:24-25 (StateHalfClosedLocal/StateHalfClosedRemote on FIN_WRITE)
- stream_open_timeout default 30s and limits defaults -> internal/config/config.go:959-964
- Exit/forward dial timeout 30s and idle timeout from connections.idle_threshold -> internal/agent/agent.go:488-493,632-637

Fixes:

- See Also linked 'Routing configuration' for stream limits and timeouts; replaced it with Limits configuration and Connections configuration, the canonical pages for those keys
- Limits paragraph said max_streams_per_peer, max_pending_opens, buffer_size, and stream_open_timeout apply to streams; rewrote it to state what is enforced and link to the canonical limits page

## `concepts/transports.md`

**Result:** pass. Comparison table first, per-transport sections, mixed transports, choosing table; See Also 5 links

Claims verified:

- Default path /mesh for h2 and ws -> internal/transport/h2.go:20; internal/transport/ws.go:21
- HTTP/2 uses POST to the path -> internal/transport/h2.go:121,322
- plaintext only valid on ws listeners -> internal/config/config.go:1543-1546
- Proxy field exists only for ws peers -> internal/config/config.go:392

## `configuration/agent.md`

**Result:** pass. Annotated YAML then key table with types/defaults; sections per key group; See Also 5 links

Claims verified:

- Defaults id auto, data_dir ./data, log_level info, log_format text -> internal/config/config.go:915-920
- display_name max 255 bytes -> internal/config/config.go:1177
- data_dir created 0700 -> internal/identity/identity.go:132; internal/identity/keypair.go:124
- data_dir required when id is auto -> internal/config/config.go:1181-1182

## `configuration/connections.md`

**Result:** pass. Annotated YAML, key table, keepalive and reconnection explained; See Also 4 links

Claims verified:

- Defaults idle_threshold 5m, timeout 90s, jitter 0.2, tcp_keepalive 30s, reconnect 1s/60s/2.0/0.2/0 -> internal/config/config.go:946-957
- Keepalive interval = idle_threshold, dead-peer threshold = interval + timeout -> internal/agent/agent.go:390-392; internal/peer/manager.go:320
- Jittered interval never below 1s -> internal/peer/manager.go:455-470
- idle_threshold also used as SOCKS5, exit and forward idle timeout -> internal/agent/agent.go:452,492,636

## `configuration/dynamic-config-persistence.md`

**Result:** pass. What is persisted, file properties, replay, failure, reset; See Also 4 entries

Claims verified:

- Journal file dynamic-config.log, mode 0600 -> internal/dynconfig/dynconfig.go:25,129
- Op names display_name_set, route_add, forward_add, peer_add, host_add -> internal/dynconfig/dynconfig.go:29-46
- Warning text 'persist dynamic config failed; change is ephemeral' -> internal/agent/agent.go:1738

## `configuration/environment-variables.md`

**Result:** pass. Syntax table, unset behavior, multiline PEM, supplying; See Also 4 links

Claims verified:

- ${VAR}, ${VAR:-default}, $VAR regex and name rules -> internal/config/config.go:1127
- Unset ${VAR} left in place; set-but-empty VAR wins over default -> internal/config/config.go:1140-1154 (LookupEnv)
- Expansion over the whole file before YAML parse -> internal/config/config.go:1090,1116

## `configuration/exit.md`

**Result:** pass. Annotated YAML, key table, routes/comments/source IP/interface/domain/DNS sections; See Also 5 links

Claims verified:

- dns.timeout default 5s -> internal/config/config.go:938
- DNS servers tried in order over UDP, IPv4 preferred, 5 minute cache -> internal/exit/dns.go:75-120
- Fixed 30s connect timeout and idle_threshold idle timeout, max connections = limits.max_streams_total -> internal/agent/agent.go:488-494

## `configuration/file-transfer.md`

**Result:** pass. Annotated YAML, key table, allowed-path rules with examples, one justified danger admonition; See Also 5 links

Claims verified:

- max_file_size default 500 MB, 0 = unlimited, directories not checked -> internal/config/config.go:973; internal/filetransfer/stream.go:99,147,172
- Paths must be absolute, no '..', empty allowed_paths allows nothing -> internal/filetransfer/stream.go:330-343
- '/dir/**' recursive form -> internal/filetransfer/stream.go:367-370
- Leading ~/ expansion -> internal/filetransfer/browse.go:109

## `configuration/forward.md`

**Result:** fixed. Annotated YAML, key table, endpoints/listeners, runtime management; vague 'nearest' replaced with the actual selection rule; See Also 4 links

Claims verified:

- Endpoint key required, unique, target required; listener max_connections not negative -> internal/config/config.go:1439-1467
- Lookup returns the lowest-metric route for a key -> internal/routing/forward.go:194-200
- Forward endpoint idle timeout and connect timeout -> internal/agent/agent.go:632-637

Fixes:

- Replaced 'nearest' and 'closest' agent wording with 'lowest metric (fewest hops)', which is what ForwardTable.Lookup selects

## `configuration/hosts.md`

**Result:** pass. Purpose clear in lead; config block then key table; runtime section links to CLI/API instead of duplicating; neutral tone; ASCII; 5 See Also links with reasons.

Claims verified:

- Max name length 253 -> internal/hosts/store.go:36
- Validation error text 'must equal or end with one of .mesh' -> mm-3 config validate on test config printed exactly the documented line
- Config-seeded host removal HTTP 403, suffix in use HTTP 409 -> internal/health/server.go:1888-1894 and 1931-1934

## `configuration/http.md`

**Result:** pass. Purpose clear; config, key table, endpoint groups, auth; links to API reference; tone fine; See Also 5 links.

Claims verified:

- Endpoint group membership for remote_api, dashboard, pprof -> internal/health/server.go:930-1001
- Auth-exempt paths /health /healthz /ready / /logo.png /notfound.png and ?token fallback -> internal/health/server.go:695-733
- Non-loopback without token_hash prints startup warning -> internal/agent/agent.go:751-752
- --token / MUTI_METROO_TOKEN -> mm-3 --help and cmd/muti-metroo/main.go:169-174
- Defaults enabled=false, address :8080, timeouts 10s -> internal/config/config.go:966-971

## `configuration/icmp.md`

**Result:** pass. Purpose clear; reference then OS requirements; tone fine; 4 See Also links.

Claims verified:

- Error strings 'ICMP echo is disabled', 'ICMP session limit reached', 'failed to create ICMP socket' -> internal/icmp/handler.go:107,121,135
- Defaults enabled=true, max_sessions=100, idle 60s, echo 5s -> internal/config/config.go:988-993
- No Windows support -> internal/icmp/socket_windows.go and socket_unix.go build tags
- ping client timeout flag -w/--timeout default 5s -> mm-3 ping --help

## `configuration/limits.md`

**Result:** pass. Purpose clear; key table states which keys are enforced; tone fine; 4 See Also links.

Claims verified:

- max_streams_total caps exit/forward/QUIC streams -> internal/agent/agent.go:493,637,1003,1712; internal/stream/manager.go:404,549
- max_streams_per_peer and buffer_size passed to stream manager but never read -> grep of internal/stream/manager.go shows no cfg.MaxStreamsPerPeer/cfg.BufferSize use
- stream_open_timeout used only for shell sessions -> internal/agent/agent.go:7654 (only reference)
- Validation rules -> internal/config/config.go Validate (limits checks)

## `configuration/listeners.md`

**Result:** fixed. Purpose clear; full annotated YAML and key table; TLS, plaintext, advertise, runtime sections; See Also 5 links.

Claims verified:

- EC-only certificates and self-signed ECDSA generation -> internal/agent/agent.go:1099-1118, internal/transport/tls.go:106
- mTLS client CA always from global tls.ca -> internal/agent/agent.go:1128-1135
- Config listeners cannot be removed/replaced at runtime -> internal/agent/agent.go ManageListener ('is a config listener and cannot be removed')
- advertise rejects loopback/unspecified -> internal/config/config.go isAdvertisableHost

Fixes:

- Noted that tls.key and tls.key_pem are redacted by config show (internal/config/redact.go:36-37)

## `configuration/management.md`

**Result:** fixed. Purpose clear; key table, then topology and signing sections; contained a factual contradiction about route path encryption and an incomplete description of public_key-only restrictions; See Also 5 links.

Claims verified:

- Route paths are always sent plaintext; only display name omitted when management key set -> internal/flood/flood.go:683-698
- public_key-only restrictions: /agents local-only, topology/dashboard/nodes local-only, mesh test local-only, route trace 403, local /routes /peers /listeners /forward /forward/endpoints /display-name manage 403 -> internal/health/server.go:1334,1447,1823,2103,2146,2188,2232,2270,2470,2807,3127; meshtest.go:85; route_trace.go:104
- 5-minute signature timestamp window -> internal/crypto/mgmtauth.go:17
- Missing signing private key gives 403 'management signing private key not configured on this agent' -> internal/health/server.go:1123-1127
- /healthz can_sign_management and accepts_signed_management -> internal/health/server.go:489-494

Fixes:

- Rewrote the lead bullet: topology keys encrypt node information and limit the HTTP API, removing the incorrect implication
- Corrected the topology section, which claimed the agent path of route advertisements is encrypted; paths travel unencrypted and only the display name is omitted
- Rewrote the public_key-only row to list exactly what is local-only, what returns 403 (route trace, remote requests, and the six local runtime-management endpoints)

## `configuration/overview.md`

**Result:** fixed. Purpose clear; section table links each canonical page; validation example matches binary output; See Also 5 links.

Claims verified:

- run default config ./config.yaml and -c ignored with embedded config -> mm-3 run --help
- Validation output block -> mm-3 config validate on a test file printed the same three errors (exit 5)
- $VAR form also expanded -> internal/config/config.go:1127 envVarRegex
- default_action must be run or help -> internal/config/config.go:1163

Fixes:

- Added the $VAR form to the environment-variable sentence
- See Also config CLI link pointed to /cli/overview; now points to the canonical /cli/config page

## `configuration/peers.md`

**Result:** fixed. Purpose clear; key table with honest 'accepted but not applied' notes; address formats; See Also 5 links.

Claims verified:

- peer proxy/proxy_auth never reach the dialer (DialOptions.ProxyURL never set) -> grep 'ProxyURL' in internal and cmd finds only the ws.go reader at internal/transport/ws.go:510
- Default request path /mesh -> internal/transport/h2.go:20, ws.go:21
- 'failed to parse peer ID' and no dial -> internal/agent/agent.go:1235-1243
- id required -> internal/config/config.go validatePeer

Fixes:

- Noted that tls.key and tls.key_pem are redacted by config show (internal/config/redact.go:42-43)

## `configuration/protocol.md`

**Result:** pass. Short reference page; config, keys, example; See Also 3 links.

Claims verified:

- Empty values fall back to defaults -> internal/transport/h2.go:133-139, ws.go:67-69, quic.go:71,121
- h2 header value is the alpn value and listener rejects mismatches -> internal/transport/h2.go:141-142, 328-346
- Defaults -> internal/config/config.go:921-925

## `configuration/routing.md`

**Result:** pass. Purpose clear; key table; tuning table; See Also 5 links.

Claims verified:

- advertise_interval 0 -> 2m and route_ttl 0 -> 5x interval -> internal/agent/agent.go:3605-3614
- node_info_interval falls back to advertise_interval -> internal/agent/agent.go:3699-3702
- max_hops validated but unused -> grep Routing.MaxHops outside internal/config returns nothing

## `configuration/shell.md`

**Result:** fixed. Purpose clear; key table, whitelist rules, platform table; danger admonition justified; See Also 5 links.

Claims verified:

- Argument metacharacter set and absolute-path rejection, skipped for '*' -> internal/shell/executor.go:58,96-110
- Command names with / or \ refused -> internal/shell/executor.go:81
- Client timeout takes precedence over config timeout -> internal/shell/executor.go:195-200

Fixes:

- Replaced a comma splice before the Management link with a semicolon

## `configuration/sleep.md`

**Result:** pass. Purpose clear; full YAML and key table; state file; See Also 4 links.

Claims verified:

- Sleep stops SOCKS, disconnects peers, closes listeners -> internal/agent/agent.go:8039-8064
- sleep_state.json and fields/state values -> internal/sleep/sleep.go:30-34,83-86,142
- window_length >= cycle replaced by cycle/6 -> internal/sleep/window.go:75-76
- max_queued_messages fallback 1000 -> internal/sleep/queue.go:27

## `configuration/socks5.md`

**Result:** pass. Purpose clear; key table; bind/auth/websocket/resolution sections; one warning admonition; See Also 5 links.

Claims verified:

- Hashed credentials take precedence, plaintext users dropped when any hash exists -> internal/socks5/auth.go:209-218, internal/agent/agent.go:670-689
- WebSocket default path /socks5 -> internal/socks5/ws_listener.go:117
- max_connections 0 means unlimited -> internal/socks5/server.go:280

## `configuration/tls-certificates.md`

**Result:** fixed. Purpose clear; key table; default behavior, strict, mTLS, inline PEM, ClientHello; one warning; See Also 5 links.

Claims verified:

- Fingerprint preset list and validation error -> internal/config/config.go validFingerprintPresets
- cert agent flags --cn --dns --ip -o, CA defaults ./certs/ca.crt and ca.key -> mm-3 cert agent --help
- TLS 1.3 minimum -> internal/agent/agent.go:1122 and 1273
- EC validation error text 'EC validation failed' -> internal/agent/agent.go:1102

Fixes:

- Noted that tls.key (file path) is also redacted by config show (internal/config/redact.go:31)

## `configuration/udp.md`

**Result:** pass. Purpose clear; key table; examples; See Also 4 links.

Claims verified:

- 'UDP relay is disabled' -> internal/udp/handler.go:131
- max_associations 0 = unlimited -> internal/udp/handler.go:141
- Oversized datagrams dropped -> internal/udp/handler.go:289
- Defaults -> internal/config/config.go:982-987

## `deployment/dll-mode.md`

**Result:** fixed. Purpose clear; exports, install, stop, logging, comparison table; See Also used ' - ' separators and lacked reasons.

Claims verified:

- Run loads config (or embedded) and blocks forever, exits silently on error -> cmd/muti-dll/main.go Run
- Install splits on first space, upgrades by uninstalling first -> cmd/muti-dll/main.go Install
- Registry value name MutiMetroo/MyTunnel -> internal/service/service.go:106-118
- embed-config and setup refuse .dll -> cmd/muti-metroo/main.go:409,468
- /shutdown only closes shutdownReqCh which the DLL never watches -> internal/agent/agent.go:1415-1419, cmd/muti-dll/main.go select{}

Fixes:

- Clarified that muti-metroo stop is accepted by the API but the DLL agent keeps running
- Rewrote See Also in the site's 'link for reason' form and added the HTTP API configuration link

## `deployment/docker.md`

**Result:** fixed. Purpose clear; build, configure, run, compose, operations, troubleshooting; See Also used ' - ' separators.

Claims verified:

- Repo Dockerfile layout /usr/local/bin/muti-metroo, WORKDIR /app, CMD run -c /app/config.yaml, no USER (root) -> Dockerfile
- Release asset name muti-metroo-linux-<arch> -> release.sh:154
- cert agent output named after CN -> cmd/muti-metroo/main.go:2168
- /health returns OK -> internal/health/server.go:1205-1206

Fixes:

- Rewrote See Also with reasons; replaced the HA link with the HTTP API configuration page, which is relevant to the exposed API port

## `deployment/embedded-config.md`

**Result:** fixed. Purpose clear; behavior, identity, creation, change, service; data_dir guidance was wrong; embed-config linked to overview instead of its own CLI page; See Also used ' - ' separators.

Claims verified:

- Startup output 'Loaded configuration: embedded in binary' -> ran an embed-config output binary from mm-3 (ed-3/runa/out.txt)
- Omitted data_dir defaults to ./data and the agent creates it even with id and private_key embedded -> ran embedded binary in ed-3/runb, ./data/agent_id was created; config show shows data_dir: ./data
- Explicit data_dir: "" runs with no data dir -> mm-3 run -c e3.yaml in ed-3/runc created nothing
- muti-metroo init creates only agent_id -> mm-3 init -d idd2 (only agent_id) and docs/docs/cli/init.md:36
- embed-config flags -b/-o -> mm-3 embed-config --help

Fixes:

- Corrected the identity section: init does not create agent_key; to run without a data directory, set data_dir to an empty string (example added); omitting data_dir still uses ./data
- Corrected the wizard description: it leaves data_dir out of the embedded config, so the ./data default applies
- Linked embed-config to /cli/embed-config and config to /cli/config
- Rewrote See Also with reasons and added the embed-config CLI page

## `deployment/high-availability.md`

**Result:** fixed. Purpose clear; failover mechanics table, patterns, monitoring, testing; See Also used ' - ' separators and advertised max_hops, which is not enforced.

Claims verified:

- Ingress with no route (or next hop disconnected) dials directly -> internal/agent/agent.go:5014-5023
- Route selection: longest prefix, then metric; same-metric across exits not deterministic within a CIDR key -> internal/routing/table.go:150-155,248-266
- /healthz peer_count and route_count -> internal/health/server.go:1228-1230

Fixes:

- Inline link text 'Configuration - Routing' changed and a Connections link added for the keepalive keys used in the example
- Rewrote See Also with reasons and dropped the max_hops mention

## `deployment/pm2.md`

**Result:** pass. Purpose clear in the first paragraph; reference before narrative; no duplication; neutral tone; examples runnable; See Also has 4 links that say why

Claims verified:

- Agent drains for up to 10s after SIGINT -> cmd/muti-metroo/main.go:718 shutdownTimeout := 10 * time.Second
- Log lines go to stderr -> internal/logging/logging.go:15 NewLoggerWithWriter(..., os.Stderr)
- Startup messages such as the agent ID go to stdout -> cmd/muti-metroo/main.go:650 fmt.Printf("Agent ID: ...")
- Relative data_dir resolves against cwd (config loader does not resolve paths relative to the config file) -> grep of internal/config for filepath.Dir(path) found nothing

## `deployment/reverse-proxy.md`

**Result:** pass. Purpose clear; config before proxy recipes; troubleshooting at the end; no duplication beyond short pointers; See Also has 5 links

Claims verified:

- TLS 1.3 minimum -> internal/transport/tls.go:42,53 MinVersion: tls.VersionTLS13
- plaintext only for ws -> internal/config/config.go:1546
- Strict CA must be ECDSA -> internal/agent/agent.go:1292 certutil.ValidateECCertificate(caPEM)
- Listener serves only its path via ServeMux -> internal/transport/ws.go:181 mux.HandleFunc(l.path, ...)

## `deployment/scenarios.md`

**Result:** pass. Purpose and index table up front; complete configs per scenario; neutral; See Also has 5 links

Claims verified:

- peer id "auto" accepted -> internal/agent/agent.go:1235 (cfg.ID != "" && cfg.ID != "auto")
- Listener certs must be ECDSA -> internal/agent/agent.go:1101 certutil.ValidateECKeyPair
- proxy_auth peer key exists -> internal/config/config.go:393
- run warns about certs that expire within 60 days -> cmd/muti-metroo/main.go:624 warnExpiringCerts

## `deployment/system-service.md`

**Result:** fixed. Purpose clear; generated unit and plist match the source; Windows user-mode flags table duplicated the CLI page and was replaced with a link; See Also has 5 links

Claims verified:

- Unit file template (RestartSec=5, TimeoutStopSec=30, ReadWritePaths=workdir + data_dir, no User= unless set) -> internal/service/service_linux.go:179-236
- User unit drops ProtectSystem/ProtectHome and uses default.target -> internal/service/service_linux.go:190-194
- Display name "Muti Metroo Mesh Agent" and description -> internal/service/service.go:69-70
- service uninstall -f skips the prompt -> `mm-4 service uninstall --help`
- Detach prints "Detached agent started (PID n)" and uses the home dir as cwd -> cmd/muti-metroo/detach_windows.go:58,88,150

Fixes:

- Replaced the Windows Registry Run flags table (a copy of the CLI reference) with one sentence saying --dll is required and must match the architecture, plus a link to CLI - service

## `download.mdx`

**Result:** pass. Purpose clear; asset names match the published releases; checksum verification steps correct; 4 admonitions (acceptable); See Also has 5 links

Claims verified:

- Muti Metroo asset names, DLLs, checksums.txt, muti-metroo-user-manual.pdf -> `gh release view -R postalsys/Muti-Metroo` (v4.10.6 assets)
- Manager assets named metroo-manager-* -> `gh release view -R postalsys/Muti-Metroo-Manager` (metroo-manager-v0.2.39)
- Mutiauk releases are Linux-only today -> `gh release view -R postalsys/Mutiauk` (mutiauk-v0.5.2: linux-amd64, linux-arm64)
- checksums.txt generated over dist/* -> release.sh:489

## `features/exit-routing.md`

**Result:** fixed. Purpose clear; route types table first; config reference linked rather than copied; trace output matches the binary; See Also has 5 links

Claims verified:

- Wildcard matches exactly one label, case-insensitive -> internal/routing/domain.go:262-277
- route trace output format -> cmd/muti-metroo/main.go:5534,5596 and live run `mm-4 route trace 10.1.2.3`
- Exit refuses with "destination not allowed" -> internal/exit/handler.go:289
- Unrouted destination is dialed directly by the ingress -> internal/agent/agent.go:5014-5016

Fixes:

- States that the ingress continues with the first resolved address (agent.go uses ips[0]), not "the IP"

## `features/file-transfer.md`

**Result:** fixed. Purpose clear; requirements table, then usage, then troubleshooting; flags linked to the CLI page; See Also has 5 links

Claims verified:

- max_file_size default 500 MB -> internal/config/config.go:974
- Error strings "path not in allowed list" and "file too large: N bytes (max M)" -> internal/filetransfer/stream.go:148,241,355
- Partial files .partial and .partial.json -> internal/filetransfer/partial.go:33,39
- Five-minute signature clock window -> internal/crypto/mgmtauth.go:17

Fixes:

- Corrected the troubleshooting text: an empty allowed_paths gives the separate error 'no paths are allowed (allowed_paths is empty)' (stream.go:341), not 'path not in allowed list'

## `features/icmp-relay.md`

**Result:** pass. Purpose clear; example output matches the CLI format strings; requirements and sysctl precede usage; See Also has 4 links

Claims verified:

- Output format PING/Reply from/statistics/rtt -> cmd/muti-metroo/main.go:3213,3323,3340,3348
- "ICMP echo disabled" error -> internal/agent/icmp.go:116
- "failed to create ICMP socket" -> internal/icmp/handler.go:135
- Defaults -c 4, -i 1s -> cmd/muti-metroo/main.go:3191-3192

## `features/mesh-hostnames.md`

**Result:** pass. Purpose clear; config and CLI linked to their canonical pages; trace output plausible; one note admonition; See Also has 5 links

Claims verified:

- Default suffix "mesh" -> internal/hosts/store.go:33
- host list output format -> cmd/muti-metroo/main.go:7501-7504
- host add/remove require the signing private key -> internal/health/server.go:1881 isMutationAction && requireMutationAuthorization
- Registry consulted before system DNS -> internal/agent/agent.go:4909-4921

## `features/port-forwarding.md`

**Result:** pass. Purpose clear; config keys linked to the forward config page; patterns section is narrative but short; See Also has 5 links

Claims verified:

- Endpoint key and target are carried in route advertisements -> internal/flood/flood.go:663 EncodeForwardKeyWithTarget
- Expose label prefix "expose:" -> internal/expose/protocol.go:52
- forward add <key> <address> synopsis -> `mm-4 forward add --help`
- Dashboard forward_routes field -> internal/health/server.go:606

## `features/runtime-configuration.md`

**Result:** fixed. Purpose clear; command table is a summary that links to CLI pages; outputs match the binary; See Also has 5 entries

Claims verified:

- "Peer added: peer X added (transport=quic)" -> cmd/muti-metroo/main.go:6384 + internal/agent/agent.go:3200
- listener list columns -> cmd/muti-metroo/main.go:6977-6996 and live `mm-4 listener list`
- Warning when --id is omitted -> cmd/muti-metroo/main.go:~6319 'Warning: --id not specified'
- Mutations need the signing private key -> internal/health/server.go:1122-1129

Fixes:

- Corrected 'every command group has a list subcommand': display-name has get, not list

## `features/shell.md`

**Result:** fixed. Purpose clear; whitelist rules match the executor; the 'Other options' flags table copied the CLI page and is now prose plus a link; See Also has 5 links

Claims verified:

- Bare names only, no paths -> internal/shell/executor.go:80-82
- Argument checks and error strings -> internal/shell/executor.go:104,107,179
- Default command bash when none given -> cmd/muti-metroo/main.go:2390
- -w default 0 -> `mm-4 shell --help`

Fixes:

- Replaced the duplicated flags table under 'Other options' with one sentence covering -a and -w and a link to the shell CLI page

## `features/sleep-mode.md`

**Result:** pass. Purpose clear; states table first; config details linked; output matches the binary; See Also has 5 links

Claims verified:

- Defaults poll_interval 5m, jitter 0.3, poll_duration 30s, persist_state true -> internal/config/config.go:1000-1003
- Timestamp window always enforced, signature only when a public key is set -> internal/flood/flood.go:1618-1637
- sleep_state.json in data_dir -> internal/sleep/sleep.go:142
- sleep-status output -> cmd/muti-metroo/main.go:4676-4689 and live `mm-4 sleep-status`

## `features/socks5-proxy.md`

**Result:** fixed. Purpose clear; routing order first; client examples; one warning admonition; See Also has 6 links

Claims verified:

- /healthz JSON keys -> internal/health/server.go:1229-1234
- Domain route checked before DNS, then mesh hosts, then system DNS -> internal/agent/agent.go:4980-5008, 4908-4921
- Unrouted destination dialed directly -> internal/agent/agent.go:5014-5016

Fixes:

- Routing step 3 now says that for a resolved hostname the first returned address is matched (agent.go destIP = ips[0])

## `features/udp-relay.md`

**Result:** pass. Purpose clear; requirements and limits before troubleshooting; example script is runnable; See Also has 5 links

Claims verified:

- Defaults 1472 bytes, 1000 associations, 5m idle -> internal/config/config.go:984-986
- UDP ASSOCIATE refused without a route covering 0.0.0.0 -> internal/agent/udp.go:81
- Relay socket is IPv4 (udp4) -> internal/socks5/udp.go:83-90

## `getting-started/first-mesh.md`

**Result:** fixed. Purpose clear; numbered steps; outputs checked against two live agents; See Also has 6 links

Claims verified:

- Log lines 'connected to peer' and 'peer connected' -> live run of two agents on 127.0.0.1:34433 (a.log, b.log)
- peer status output -> live `mm-4 peer status -a 127.0.0.1:34082`
- Dashboard routes JSON shape (network, route_type, origin, origin_id, hop_count, path_display, path_ids, tcp, udp) -> live `curl /api/dashboard \| jq .routes`
- Ingress uses only the first resolved address -> internal/agent/agent.go:5007 destIP = ips[0]; live `route trace example.com` listed IPv6 addresses first with 'no matching CIDR route'

Fixes:

- 'How the traffic flows' now says Agent A uses the first resolved address, and a new paragraph explains that when that address is IPv6 with no covering route, Agent A connects from its own host outside the mesh. It points to route trace and to adding ::/0 on the exit

## `getting-started/installation.md`

**Result:** pass. Purpose clear; platform table first; service details linked out; See Also has 5 links

Claims verified:

- Go 1.26 required -> go.mod 'go 1.26.0'
- init output and that it creates only agent_id -> live `mm-4 init -d ./initd` printed 'Agent initialized in ./initd' and the directory held only agent_id
- Default data_dir ./data -> internal/config/config.go:917
- Key file names agent_key, agent_key.pub -> internal/identity/keypair.go:21,24

## `getting-started/interactive-setup.md`

**Result:** fixed. Purpose clear; per-step prompt tables before the example session; strings match the wizard; See Also has 6 links

Claims verified:

- Exit DNS defaults 8.8.8.8:53 and 1.1.1.1:53 -> internal/wizard/wizard.go:1205
- Route default text 'Default: 0.0.0.0/0 and ::/0' -> internal/wizard/wizard.go:1230
- SOCKS password min 8 chars, written to the plaintext password field -> internal/wizard/wizard.go:1174-1183
- /health returns OK -> internal/health/server.go:1206

Fixes:

- Removed the false claim that SOCKS5 requires plaintext password storage. The page now says the wizard writes the deprecated plaintext `password` key and that a bcrypt `password_hash` from `muti-metroo hash` can replace it (config.go:553 marks password deprecated)

## `getting-started/overview.md`

**Result:** pass. Purpose clear; demo, then paths table; one tip admonition; See Also has 6 links

Claims verified:

- Docker tryout ports 11080 (SOCKS5) and 18080 (HTTP) -> examples/docker-tryout/docker-compose.yml:18-19
- Six-agent mesh -> examples/docker-tryout/docker-compose.yml services agent1 to agent6
- TLS certs auto-generated when none configured -> internal/transport/tls.go:106 ecdsa.GenerateKey for self-signed

## `getting-started/quick-start.md`

**Result:** pass. Purpose clear in first paragraph; frontmatter accurate; steps ordered; no duplicated reference tables (links to configuration/peers, socks5); neutral tone; examples ran as written against the built binary; See Also has 5 annotated links.

Claims verified:

- init output 'Agent initialized in ./data / Agent ID: <32 hex>' -> ran mm-5 init -d ./data
- run startup lines (Loaded configuration, HTTP server started, listener started, SOCKS5 server started, exit handler started count=1, agent started) -> ran mm-5 run with the page config on 127.0.0.1:3508x
- /healthz JSON body field set and values -> curl http://127.0.0.1:35081/healthz
- muti-metroo status output block -> mm-5 status -a 127.0.0.1:35081
- curl -x socks5h:// through the agent works -> returned HTTP 200 from example.com

## `intro.md`

**Result:** fixed. Clear purpose; feature table at peer level; one inaccurate transport claim fixed; See Also 6 annotated links.

Claims verified:

- Metric is exit metric plus one per hop -> internal/routing/manager.go:564 (Metric: entry.Metric + 1)
- Plaintext/reverse-proxy mode exists only for WebSocket -> internal/config/config.go:1543-1546 and internal/transport/ws.go:109
- WebSocket peers can use an HTTP proxy -> internal/config/config.go:392 (Proxy field, 'HTTP proxy for ws')

Fixes:

- Features table: replaced 'Both TCP transports work behind reverse proxies' (false for HTTP/2) with 'WebSocket can also run behind a TLS-terminating reverse proxy and dial through an HTTP proxy'

## `muti-metroo-manager.mdx`

**Result:** pass. Clear purpose and security warning up front; reference (flags, config, env, data dir) accurate against Manager source; screenshots from capture script; See Also 5 annotated links.

Claims verified:

- Flag defaults (-addr :3000, -agent http://127.0.0.1:8080, -log-level info, -log-format text) -> ../Muti-Metroo-Manager/internal/config/config.go:72-85 and load.go:61-83
- Token env vars METROO_MANAGER_AGENT_TOKEN and deprecated MUTI_METROO_TOKEN -> ../Muti-Metroo-Manager/internal/config/load.go:28-29
- /healthz fields (status, version, agent_reachable, agent_status, agent_latency_ms, agent_checked_ago_s) -> internal/server/health.go:96-104; version 0.2.39 matches latest tag metroo-manager-v0.2.39
- Data dir defaults per OS -> internal/config/datadir.go:24-40
- Lock error text 'another muti-metroo-manager instance is already running' -> internal/store/lock.go:59
- systemd unit runs as dedicated user with check before start -> contrib/systemd/muti-metroo-manager.service:31,37

## `mutiauk.mdx`

**Result:** fixed. Clear purpose; reference tables for config and commands; download section matches what is actually on the latest release; one placeholder value replaced with a real hash; See Also 6 annotated links.

Claims verified:

- status_probe_target default example.com:80 -> ../Mutiauk/internal/config/config.go:338
- expose defaults (max_streams_per_key 256, keepalive_period 25s) -> ../Mutiauk/internal/config/config.go:410-413; admin listen 127.0.0.1:48088 -> config.go:201
- Latest release ships only Linux binaries -> gh release view --repo postalsys/Mutiauk (mutiauk-v0.5.2: checksums.txt, mutiauk-linux-amd64, mutiauk-linux-arm64)
- make build-darwin/-darwin-amd64/-windows/-windows-arm64 targets -> ../Mutiauk/Makefile:31-43
- MUTIAUK_SOCKS5_PASSWORD overrides YAML -> ../Mutiauk/internal/config/config.go:280
- dns status probe lines use ' -- ' -> ../Mutiauk/internal/cli/dns.go:278 (real output, kept)

Fixes:

- Replaced the non-runnable placeholder password_hash "$2a$12$..." in the agent WebSocket SOCKS5 example with a real bcrypt hash of the page's example password generated by muti-metroo hash

## `security/access-control.md`

**Result:** pass. Clear purpose with summary table; sections per control; locked-down exit example; troubleshooting headings use literal error text; See Also 5 annotated links.

Claims verified:

- 'destination not allowed' error -> internal/exit/handler.go:289
- Shell metacharacter set and absolute-path argument rejection, skipped for ["*"] -> internal/shell/executor.go:58,96-110
- Unrouted SOCKS destinations dialed directly from the ingress -> internal/agent/agent.go:5014
- UDP and ICMP enabled by default -> internal/config/config.go:982-989
- max_file_size default 500 MB -> internal/config/config.go:974
- 'path not in allowed list' -> internal/filetransfer/stream.go:241

## `security/authentication.md`

**Result:** fixed. Reference table first, then per-mechanism sections; error message table accurate; removed a YAML block duplicated verbatim on best-practices; See Also 5 annotated links.

Claims verified:

- Signature errors 'management signature outside timestamp window' and 'management signature verification failed' -> internal/crypto/mgmtauth.go:62-63
- Five-minute signature window -> internal/crypto/mgmtauth.go:17
- Auth-exempt paths /health, /healthz, /ready, /, /logo.png, /notfound.png -> internal/health/server.go:695-702
- ?token= query parameter accepted -> internal/health/server.go:732
- CLI token from --token or MUTI_METROO_TOKEN -> cmd/muti-metroo/main.go:169,174
- hash --cost default 12, range 4-31 -> mm-5 hash --help flag line

Fixes:

- Replaced the 'Keep secrets out of configuration files' YAML block (identical to the one on Best Practices) with a short paragraph linking to best-practices#secrets-in-configuration and the Environment Variables page

## `security/best-practices.md`

**Result:** pass. Ranked summary then topical sections; cert commands run as written; incident response actionable; See Also 5 annotated links.

Claims verified:

- cert ca/agent flags (--cn, --dns, --ca, --ca-key, --days, -o) and output names exit-1.crt/exit-1.key -> ran mm-5 cert ca and cert agent with the page's arguments
- systemd unit NoNewPrivileges, ProtectSystem=strict, ProtectHome=read-only, PrivateTmp -> internal/service/service_linux.go:190,221-222
- Unset ${VAR} left as literal text, ${VAR:-default} fallback -> internal/config/config.go:1130-1155
- File transfers are logged with path -> internal/agent/agent.go:6336,6426,6549,6597
- Data dir holds agent_key -> ls of initialized data dir (agent_id, agent_key, agent_key.pub)

## `security/e2e-encryption.md`

**Result:** pass. Purpose clear; what is/is not protected tables; limits stated honestly; level of detail suits a security page; See Also 5 annotated links.

Claims verified:

- 28-byte overhead (12-byte nonce + 16-byte tag) -> internal/crypto/crypto.go:23-30
- HKDF-SHA256 session key derivation -> internal/crypto/crypto.go:15,119
- Persistent agent key in agent_key -> initialized data dir listing

## `security/overview.md`

**Result:** pass. Controls table, protected/not-protected tables, checklist; each row links to canonical page; See Also 5 annotated links.

Claims verified:

- TLS 1.3 minimum on every link -> internal/transport/tls.go:42,53,184
- UDP and ICMP not restricted by exit routes and enabled by default -> internal/config/config.go:982-989
- Unrouted SOCKS destinations dialed directly -> internal/agent/agent.go:5014

## `security/tls-mtls.md`

**Result:** pass. Mode table first, then per-mode sections; cert generation runnable; troubleshooting uses literal errors; See Also 5 annotated links.

Claims verified:

- Default in-memory self-signed ECDSA P-256 cert -> internal/transport/tls.go:106
- RSA rejection message -> internal/certutil/certutil.go:399
- Agent certs include CN, localhost, 127.0.0.1, ::1 and default 365 days -> mm-5 cert agent output (DNS Names: exit-1, localhost, ...; IP Addresses: 127.0.0.1, ::1) and --days default 365

## `suite-overview.md`

**Result:** fixed. Clear purpose and component table; Manager example used double-dash flags and recommended the token flag the Manager page discourages; See Also 4 annotated links.

Claims verified:

- Manager flags use single dash Go flag package (-agent, -agent-token-file, -addr) -> ../Muti-Metroo-Manager/internal/config/load.go:61-83
- Manager default listen :3000 binds all interfaces -> ../Muti-Metroo-Manager/internal/config/config.go:74
- mutiauk daemon start runs in foreground; service install exists -> ../Mutiauk/internal/service/service.go:37 and mutiauk.mdx command reference

Fixes:

- Manager example changed to 'muti-metroo-manager -addr 127.0.0.1:3000 -agent http://192.168.1.10:8080' (single-dash flags, loopback bind)
- Token advice changed from --agent-token to -agent-token-file, and added one sentence that Manager has no login of its own

## `terms-of-service.md`

**Result:** fixed. Legal text left unchanged; admonition type outside the style guide set; See Also had only 2 links.

Claims verified:

- Source license is MIT -> LICENSE in repo root linked
- Download page offers checksums for these binaries -> docs/docs/download.mdx:201-214
- Installation page covers build from source -> docs/docs/getting-started/installation.md:43

Fixes:

- Changed ':::info' to ':::note' (STYLE.md allows note/tip/warning/danger)
- Added a third See Also link to Installation

## `troubleshooting/common-issues.md`

**Result:** fixed. Organized by where the error appears, literal messages verified; See Also and inline link titles used ' - ' as a separator.

Claims verified:

- Missing config message -> mm-5 run -c nope.yaml
- Port-in-use message 'failed to start agent: start HTTP server: listen tcp ...: bind: address already in use' -> mm-5 run against an occupied port
- Validation error format with 'listeners[0]: path is required for ws transport' -> mm-5 run with a ws listener without path
- 'management signing private key not configured (set management.signing_private_key)' -> internal/agent/agent.go:1534
- 'file too large: %d bytes (max %d)' -> internal/filetransfer/stream.go:148
- run --detach / -D exists (Windows only) -> mm-5 run --help

Fixes:

- See Also entries switched from ' - ' separator to ': ' with lowercase descriptions
- Inline link titles 'Configuration - Shell', 'Configuration - File Transfer', 'System Service - Troubleshooting', 'System Service - Windows', 'Connectivity Troubleshooting - Routing' renamed to names without dash separators

## `troubleshooting/connectivity.md`

**Result:** fixed. Ordered diagnosis (agent, peers, routes, path); outputs match binary; link titles used ' - ' separators.

Claims verified:

- Default h2/ws path /mesh -> internal/transport/h2.go:20, ws.go:21 and mm-5 probe --help (--path default "/mesh")
- probe flags --transport, --path, --ca, --cert, --key -> mm-5 probe --help
- route status table header and route trace line format -> mm-5 route status / route trace against a running agent
- 'no matching CIDR route' -> cmd/muti-metroo/main.go:5572
- Route TTL and advertise defaults 5m/2m, max_hops 16 -> internal/config/config.go:944 and CLAUDE.md limits table

Fixes:

- Renamed link titles 'CLI - peer/route/probe', 'Configuration - Connections', 'API - Health', 'Common Issues - Peers do not connect' to dash-free names
- See Also switched to ': ' separators

## `troubleshooting/faq.md`

**Result:** fixed. Short answers each linking to the canonical page; claims accurate; link titles used ' - ' separators; See Also 4 annotated links.

Claims verified:

- routing.max_hops range 1-255, default 16 -> internal/config/config.go:944,1273
- shell --tty flag and service install -n/--name -> mm-5 shell --help, mm-5 service install --help
- macOS launchd logs <name>.err.log in the working directory -> internal/service/service_darwin.go:181-184
- /api/topology and /api/nodes endpoints -> internal/health/server.go:974-976
- Windows DLL for amd64 and arm64 -> release.sh:239-241 and download.mdx:154-166

Fixes:

- Renamed 'Configuration - Agent', 'CLI - cert', 'API - Health', 'API - Dashboard' link titles
- See Also switched to ': ' separators

## `troubleshooting/performance.md`

**Result:** fixed. Measure-first structure; pprof section accurate; one wrong definition of hops fixed; See Also 5 annotated links.

Claims verified:

- pprof enabled unless http.pprof false -> internal/config/config.go:686-690 and curl http://127.0.0.1:35081/debug/pprof/ returned 200
- limits.max_streams_total default 10000 -> CLAUDE.md limits table / internal/config defaults
- hops counts links, not agents between (local route hops=0, direct peer hops=1) -> mm-5 route trace output and connectivity page sample

Fixes:

- Corrected 'the hops value is the number of agents between ingress and exit' to 'the number of peer links between the ingress and the exit (1 when the exit is a direct peer)'
- Renamed 'API - Debugging' and 'Configuration - Limits' link titles; See Also switched to ': ' separators

