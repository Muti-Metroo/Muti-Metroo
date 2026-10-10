# Introduction

## Who This Manual Is For

This manual is written for **operators** -- the people who set up Muti Metroo in a small team and use it during day-to-day work. The typical reader is managing multi-site infrastructure, connecting remote labs, or any other scenario where a few workstations need transparent, encrypted access to remote networks.

It assumes the deployment shape that almost every Muti Metroo user converges on:

- **One shared host** runs the Muti Metroo agent (the mesh entry point) and the Muti Metroo Manager dashboard. This is usually a small VPS, a workstation in the office, or a jump box that everyone on the team can reach.
- **Each operator workstation** -- typically Debian-based Linux -- runs Mutiauk locally, picks up routes from the shared agent, and forwards traffic transparently into the mesh.

If your deployment is more complex (multi-region relays, dedicated exit agents, custom transports, NAT-traversal puzzles), the user manual covers those topologies in detail. This manual sticks to the small-team workflow because that is what most operators actually do.

## What You Will Find Here

Each chapter is organised around a question an operator asks:

- "How do I stand up the shared host?" -- Chapter 3
- "What does each tab in Muti Metroo Manager do?" -- Chapter 4
- "I have a new operator with a fresh Linux laptop -- now what?" -- Chapter 5
- "How do I make 10.20.0.0/16 reachable from my workstation?" -- Chapter 6
- "I want one tool to use the mesh, not the whole machine." -- Chapter 7
- "Does ping work?" -- Chapter 8
- "How do `target.mesh` names resolve?" -- Chapter 9
- "How do I share a folder or expose a port from my workstation?" -- Chapter 10
- "Why isn't my traffic going where I expected?" -- Chapter 11
- "Quick recipes for common situations." -- Chapter 12

A short CLI quick-reference for `mutiauk` is in the appendix. For the `muti-metroo` and `muti-metroo-manager` command surfaces, the user manual is the canonical reference.

## What You Will Not Find Here

- Protocol internals (frame formats, wire layout, stream state machines). See `Architecture.md` in the source tree.
- Every configuration option for every component. See the user manual.
- Cross-platform installation deep-dives. The shared host is assumed Linux; the workstation is assumed Debian-based Linux.
- Translations. English only.

## How to Read This Manual

Front-to-back the first time, then dip into the cookbook (Chapter 12) when you have a specific problem. Most chapters stand alone, but Chapters 3 and 5 set up the deployment that every later chapter assumes.
