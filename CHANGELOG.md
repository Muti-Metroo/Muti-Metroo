# Changelog

## [4.12.2](https://github.com/Muti-Metroo/Muti-Metroo/compare/muti-metroo-v4.12.1...muti-metroo-v4.12.2) (2026-10-20)


### Bug Fixes

* keep the working directory when detaching with run -D ([f25e99b](https://github.com/Muti-Metroo/Muti-Metroo/commit/f25e99ba2a49c7d3435bdc6e14e28449fca0b8ef))

## [4.12.1](https://github.com/Muti-Metroo/Muti-Metroo/compare/muti-metroo-v4.12.0...muti-metroo-v4.12.1) (2026-10-19)


### Bug Fixes

* add per-stream flow control so slow readers never lose data ([e8a7d26](https://github.com/Muti-Metroo/Muti-Metroo/commit/e8a7d26c44afe25ab28f486ed3bcaaca77a7ad16))
* relay UDP when the mesh has no default route ([4c90b73](https://github.com/Muti-Metroo/Muti-Metroo/commit/4c90b730d86225bbabfaf6c9565a9ffdbe775a77))

## [4.12.0](https://github.com/Muti-Metroo/Muti-Metroo/compare/muti-metroo-v4.11.0...muti-metroo-v4.12.0) (2026-10-19)


### Features

* file API for mounted shares, and sign management requests over their body ([83c3351](https://github.com/Muti-Metroo/Muti-Metroo/commit/83c3351e68e4aca1fdbc056e6d0bf9e097200a44))

## [4.11.0](https://github.com/Muti-Metroo/Muti-Metroo/compare/muti-metroo-v4.10.11...muti-metroo-v4.11.0) (2026-10-19)


### Features

* add UDP STREAM, a SOCKS5 UDP association framed on the connection ([94b179b](https://github.com/Muti-Metroo/Muti-Metroo/commit/94b179b79fc55282aee1974a3e35002ac2856f7d))

## [4.10.11](https://github.com/Muti-Metroo/Muti-Metroo/compare/muti-metroo-v4.10.10...muti-metroo-v4.10.11) (2026-10-12)


### Bug Fixes

* **release:** stop UPX-packing the Windows amd64 EXE and ship all PDFs ([0641660](https://github.com/Muti-Metroo/Muti-Metroo/commit/0641660a64c87980ba6a3a6c0a4517cc19114f44))

## [4.10.10](https://github.com/Muti-Metroo/Muti-Metroo/compare/muti-metroo-v4.10.9...muti-metroo-v4.10.10) (2026-10-11)


### Miscellaneous Chores

* re-cut release on consolidated history ([3589c31](https://github.com/Muti-Metroo/Muti-Metroo/commit/3589c318f4ce7eecb600c71afae2c2e315629b04))

## [4.10.9](https://github.com/Muti-Metroo/Muti-Metroo/compare/muti-metroo-v4.10.8...muti-metroo-v4.10.9) (2026-10-11)


### Miscellaneous Chores

* update repository references to Muti-Metroo org ([996e3e1](https://github.com/Muti-Metroo/Muti-Metroo/commit/996e3e1e38cb6239cf8f19fa29a5f6e9dc2df98e))

## [4.10.8](https://github.com/postalsys/Muti-Metroo/compare/muti-metroo-v4.10.7...muti-metroo-v4.10.8) (2026-10-11)


### Bug Fixes

* **agent:** enforce TTL on STREAM_OPEN/UDP_OPEN/ICMP_OPEN ([b65d8cb](https://github.com/postalsys/Muti-Metroo/commit/b65d8cbd112c271c626eeb470cbffd511fa21d70)), closes [#59](https://github.com/postalsys/Muti-Metroo/issues/59)
* **agent:** fail relayed streams loudly on transit write errors ([9fdaa64](https://github.com/postalsys/Muti-Metroo/commit/9fdaa64e505e0569a972e5dd34b91e2b79da114b)), closes [#50](https://github.com/postalsys/Muti-Metroo/issues/50)
* **agent:** reject unsigned control-channel file browse requests ([1524242](https://github.com/postalsys/Muti-Metroo/commit/1524242aaa27ecd4e6a5dc59b80f086b4d66260c)), closes [#71](https://github.com/postalsys/Muti-Metroo/issues/71)
* **agent:** tear down own streams and UDP/ICMP relay entries on peer disconnect ([15613a3](https://github.com/postalsys/Muti-Metroo/commit/15613a397daffd731f0123ca1df46e4f082a4606)), closes [#56](https://github.com/postalsys/Muti-Metroo/issues/56)
* **cli:** align config validate exit codes with documented contract ([05da97c](https://github.com/postalsys/Muti-Metroo/commit/05da97cdc6a66e3edaadcad3f12a6638b70ce37f)), closes [#73](https://github.com/postalsys/Muti-Metroo/issues/73)
* **peer:** make the app-level keepalive able to detect a dead peer ([833cd97](https://github.com/postalsys/Muti-Metroo/commit/833cd9763ddd5383115ce634d3a54ec13e32a110)), closes [#51](https://github.com/postalsys/Muti-Metroo/issues/51)
* **peer:** resolve duplicate connections deterministically ([d217084](https://github.com/postalsys/Muti-Metroo/commit/d217084fcacb1705f0857a6b916957e2f52f464d)), closes [#52](https://github.com/postalsys/Muti-Metroo/issues/52) [#53](https://github.com/postalsys/Muti-Metroo/issues/53)
* **peer:** validate protocol version in PEER_HELLO_ACK ([e558c1b](https://github.com/postalsys/Muti-Metroo/commit/e558c1b218b7855206d62b7705f0d11442220483)), closes [#62](https://github.com/postalsys/Muti-Metroo/issues/62)
* **protocol:** fail DecodeQueuedState on corrupt sleep command ([b7cbc36](https://github.com/postalsys/Muti-Metroo/commit/b7cbc362507f56fb313d6882158ff73ecd6180e2)), closes [#61](https://github.com/postalsys/Muti-Metroo/issues/61)
* **protocol:** reject unknown route address families loudly ([199f12b](https://github.com/postalsys/Muti-Metroo/commit/199f12beefabb1f5dd5230343fcad12f43b8149d)), closes [#60](https://github.com/postalsys/Muti-Metroo/issues/60)
* remove dead and misleading config, protocol, and stream surface ([b7d2c8f](https://github.com/postalsys/Muti-Metroo/commit/b7d2c8f9ead8b2abf458ccf67f6684cde762a168)), closes [#66](https://github.com/postalsys/Muti-Metroo/issues/66)
* **routing:** deterministic origin tiebreak for equal-metric routes ([1d73591](https://github.com/postalsys/Muti-Metroo/commit/1d735912c0b04bfce778df3688f15bf4e0cf6d95)), closes [#72](https://github.com/postalsys/Muti-Metroo/issues/72)
* **routing:** flood route withdrawals on peer disconnect and guard stale withdraws ([9390505](https://github.com/postalsys/Muti-Metroo/commit/9390505c8e2d844f160ece79726587786c8d66e8)), closes [#55](https://github.com/postalsys/Muti-Metroo/issues/55)
* **stream:** count pending opens against MaxStreamsTotal ([b391d58](https://github.com/postalsys/Muti-Metroo/commit/b391d586cb93ce08a83ff71b3c472a613b5e5647)), closes [#63](https://github.com/postalsys/Muti-Metroo/issues/63)
* **stream:** never block the connection drainer on slow consumers ([74b3bbe](https://github.com/postalsys/Muti-Metroo/commit/74b3bbecc515fba582c25533d200a99c0262ad60)), closes [#57](https://github.com/postalsys/Muti-Metroo/issues/57)
* **transport:** enforce write deadlines on h2/ws peer writes ([3ea5f56](https://github.com/postalsys/Muti-Metroo/commit/3ea5f5691e8aef29063868cdd52e49189f7a2943)), closes [#54](https://github.com/postalsys/Muti-Metroo/issues/54)
* **udp:** drop-tail fast lane, bounded exit DNS, reject oversize at ingress ([1af1488](https://github.com/postalsys/Muti-Metroo/commit/1af1488fd136b19f94966ce446030072bf88547f)), closes [#58](https://github.com/postalsys/Muti-Metroo/issues/58)


### Performance Improvements

* cache AEAD in SessionKey, skip coalescing copy in frame writes ([3bf00bf](https://github.com/postalsys/Muti-Metroo/commit/3bf00bfb343b6e85966a72efffc6dcaa7427191b))

## [4.10.7](https://github.com/postalsys/Muti-Metroo/compare/muti-metroo-v4.10.6...muti-metroo-v4.10.7) (2026-10-11)


### Bug Fixes

* **health:** declare the agent's 404 page as English ([cd49e17](https://github.com/postalsys/Muti-Metroo/commit/cd49e17dd1d9afbc7d19c325e36f3e6421b1496b))

## [4.10.6](https://github.com/postalsys/Muti-Metroo/compare/muti-metroo-v4.10.5...muti-metroo-v4.10.6) (2026-10-11)


### Bug Fixes

* **release:** build and publish the Windows ARM64 DLL ([bac64ac](https://github.com/postalsys/Muti-Metroo/commit/bac64acc608eb47cdd75d3fc99b8dc8e635d85b5))

## [4.10.5](https://github.com/postalsys/Muti-Metroo/compare/muti-metroo-v4.10.4...muti-metroo-v4.10.5) (2026-10-11)


### Bug Fixes

* **crypto:** use curve25519.X25519 which rejects low-order points ([757173e](https://github.com/postalsys/Muti-Metroo/commit/757173e46ade038df5f96019abe224ef2453b6a1))
* **embed:** restore original Muti Metroo embedded-config magic ([1fc5d6c](https://github.com/postalsys/Muti-Metroo/commit/1fc5d6c03bf197d26659fa991a067bac74497d64))

## [4.10.4](https://github.com/postalsys/Muti-Metroo/compare/muti-metroo-v4.10.3...muti-metroo-v4.10.4) (2026-10-10)


### Bug Fixes

* **deps:** patch golang.org/x/crypto, x/net and quic-go vulnerabilities ([69f9ea7](https://github.com/postalsys/Muti-Metroo/commit/69f9ea70047eded966f0edbc7448c4332581a976))
* **deps:** resolve npm vulnerabilities in docs and manual tooling ([8314607](https://github.com/postalsys/Muti-Metroo/commit/83146073a142a615b8472c27e3add274a31e0c4a))

## [4.10.3](https://github.com/postalsys/Muti-Metroo/compare/muti-metroo-v4.10.2...muti-metroo-v4.10.3) (2026-10-10)


### Bug Fixes

* replace hedgehog logo with mole mascot in health page and manuals ([ee5e25a](https://github.com/postalsys/Muti-Metroo/commit/ee5e25acdfbe2ddd7ea492aa37291653807c46e9))

## [4.10.2](https://github.com/postalsys/Muti-Metroo/compare/muti-metroo-v4.10.1...muti-metroo-v4.10.2) (2026-10-10)


### Bug Fixes

* **health:** replace 404 image with mole mascot ([cb92e46](https://github.com/postalsys/Muti-Metroo/commit/cb92e469d382ac0fd88f4dc01a874fb55884de93))
