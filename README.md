<p align="center">
  <a href="https://github.com/iamkaf/pastel/actions/workflows/check.yml"><img src="https://img.shields.io/github/actions/workflow/status/iamkaf/pastel/check.yml?style=for-the-badge&label=check&labelColor=201827&color=7dd3fc" alt="Check workflow" /></a>
  <a href="https://github.com/iamkaf/pastel/releases/latest"><img src="https://img.shields.io/github/v/release/iamkaf/pastel?style=for-the-badge&labelColor=201827&color=f9a8d4" alt="Latest release" /></a>
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-c4b5fd?style=for-the-badge&labelColor=201827" alt="Apache 2.0 license" /></a>
</p>

<h1 align="center">Pastel</h1>

<p align="center">
  <strong>A friendly runtime for dedicated Minecraft modpack servers.</strong>
</p>

<p align="center">
  <a href="#for-server-owners">Server owners</a> ·
  <a href="#for-pack-authors">Pack authors</a> ·
  <a href="#contributing">Contributing</a>
</p>

Pastel turns a Modrinth modpack into a managed dedicated server. It downloads and verifies the server-side pack files, installs the required loader and Java runtime, starts Minecraft, exposes a live console, and keeps the server on the pack version you chose.

## For server owners

### Platform support

| Platform | Install and sync | Run | Background console |
| --- | --- | --- | --- |
| macOS | Yes | Yes | Yes |
| Linux | Yes | Yes | Yes |
| Windows | Yes | Foreground with `pastel run -f` | Planned |

Windows release binaries are provided so pack installation and foreground servers can be used and tested today. The default background mode and `pastel console` currently require the Unix FIFO implementation on macOS or Linux.

### Install Pastel

Download the archive for your computer from the [latest GitHub release](https://github.com/iamkaf/pastel/releases/latest), extract it into an empty server folder, and rename the contained executable to `pastel` if you prefer the shorter commands used below.

| System | Release archive |
| --- | --- |
| macOS, Apple silicon | `pastel-darwin-arm64.tar.gz` |
| macOS, Intel | `pastel-darwin-amd64.tar.gz` |
| Linux, x86-64 | `pastel-linux-amd64.tar.gz` |
| Linux, ARM64 | `pastel-linux-arm64.tar.gz` |
| Windows, x86-64 | `pastel-windows-amd64.zip` |
| Windows, ARM64 | `pastel-windows-arm64.zip` |

Each release includes `SHA256SUMS`. On macOS or Linux, make the executable runnable if your extraction tool did not preserve its mode:

```bash
chmod +x pastel
```

### Quick start

From the folder that should become the server:

```bash
./pastel install aristea
./pastel run
./pastel console
```

`install` accepts a Modrinth slug, a Modrinth modpack page, a direct `.mrpack` URL, a local `.mrpack`, or a Maven coordinate with an explicit repository:

```bash
./pastel install aristea@0.1.4
./pastel install https://modrinth.com/modpack/aristea
./pastel install https://example.com/my-pack.mrpack
./pastel install ./my-pack.mrpack
./pastel install com.example:my-pack:1.2.0 -repo https://maven.example.com
```

Pastel writes `server.pastel`, applies the pack, and prepares the loader. The first `run` also chooses a suitable Java runtime. If the system Java is too old, Pastel downloads a Temurin JRE into `.pastel/jre/` and verifies its published SHA-256 checksum before installing it.

By running a Minecraft server with Pastel, you indicate your agreement to [Mojang's Minecraft EULA](https://aka.ms/MinecraftEULA). Pastel writes `eula=true` when it starts the server.

### Everyday commands

| Command | Purpose |
| --- | --- |
| `pastel` | Show the installed pack, server state, and available update |
| `pastel install <pack>` | Create or replace this server's pack pin |
| `pastel refresh` | Reconcile the server with the current pin |
| `pastel refresh -dry-run` | Preview downloads, updates, and pruning |
| `pastel update` | Choose and install another Modrinth or Maven pack version |
| `pastel run` | Refresh when enabled, then start in the background |
| `pastel run -f` | Run in the foreground; required on Windows today |
| `pastel console` | Follow the live log and send server commands |
| `pastel stop` | Ask Minecraft to save and stop, then escalate if necessary |
| `pastel status` | Show detailed state for this server folder |
| `pastel version` | Print the Pastel version |

`sync` remains an alias for `refresh`; `upgrade` remains an alias for `update`.

### `server.pastel`

The generated file is intentionally small:

```toml
pack = "modrinth:aristea"
memory = "4G"
sync_on_run = true
```

| Key | Meaning |
| --- | --- |
| `pack` | Required pack pin: Modrinth, URL, local path, or Maven coordinate |
| `memory` | Java maximum heap, such as `4G`; defaults to `4G` |
| `sync_on_run` | Refresh before each run; defaults to `true` |
| `repositories` | Ordered Maven bases for short coordinates; there is no default |
| `java` | Optional Java executable override |
| `server_dir` | Optional server directory relative to `server.pastel` |
| `extra_java_args` | Additional JVM arguments |
| `nogui` | Pass `nogui` to Minecraft; defaults to `true` |

Paths are resolved relative to the directory containing `server.pastel`.

### What refresh changes

For a dedicated server, Pastel:

1. selects `.mrpack` files whose `env.server` is not `unsupported`;
2. verifies downloads with the strongest hash published by the pack;
3. applies `overrides/`, followed by `server-overrides/`;
4. installs or aligns the loader from the pack dependencies;
5. removes extra jars from `mods/` and stale managed root launcher jars.

Pastel refuses to manage `world/`. It also refuses pack changes while the server is running. Extra jars under `mods/` are pruned during a normal refresh, so preview a change with `pastel refresh -dry-run` or use `-no-prune` when you deliberately maintain local jars.

Set `sync_on_run = false` while debugging local pack changes. `pastel run` will leave pack files alone, while an explicit `pastel refresh` will still reconcile them.

### Pack and loader support

The maintained public pack format is [Modrinth `.mrpack`](https://support.modrinth.com/en/articles/8802351-modrinth-modpack-format-mrpack).

| Loader | Behavior |
| --- | --- |
| Fabric | Downloads a server launcher matching the pack's Minecraft and Fabric Loader versions |
| NeoForge | Runs the official installer and launches its generated argument file |
| Forge | Runs the official installer and launches its generated argument file |
| Quilt | Uses a pack-provided `quilt-server-launch.jar`; automatic installation is planned |
| Vanilla | Uses a pack-provided `server.jar` |

Pastel derives the minimum Java version from Minecraft: Java 25 for 26.1 and newer, Java 21 for 1.20.5 through 1.21.x, Java 17 for 1.17 through 1.20.4, and Java 8 for older releases.

### Troubleshooting

- If `run` restores a jar you removed, set `sync_on_run = false` before trying again.
- If the server exits during startup, read `logs/latest.log`; Pastel also summarizes common wrong-side mod and memory failures.
- If a deleted server folder left a Java process behind on Linux, use `pastel stop -orphans` or `pastel stop -pid <number>`.
- If a Maven coordinate fails, add its repository to `repositories` or pass `-repo` during install. Pastel never silently chooses a Maven host.
- For a reproducible Pastel bug, open a [GitHub issue](https://github.com/iamkaf/pastel/issues). Use [GitHub Discussions](https://github.com/iamkaf/pastel/discussions) for setup help.

## For pack authors

Pastel consumes standard server-capable `.mrpack` files. You do not need a Pastel-specific manifest: publish the pack on Modrinth, provide a direct `.mrpack` URL, or publish the `.mrpack` in a Maven repository.

The short version:

- declare `minecraft` and the loader in `dependencies`;
- give every `files[]` entry a download URL and hash;
- mark client-only files with `env.server = "unsupported"`;
- put shared files in `overrides/` and dedicated-server replacements in `server-overrides/`;
- do not put worlds in the pack.

See [the pack author guide](./docs/PACK.md) for resolution rules, override order, loader behavior, Maven layout, and the bundled Kaf Maven authoring commands.

## Contributing

Pastel requires Go 1.26.5 or the compatible Go version declared by `go.mod`.

```bash
make build
make check
make cross
```

`make check` verifies formatting, modules, vetting, race-enabled tests, and the normal package suite. `make cross` builds the six GitHub release targets.

The source is grouped by responsibility:

| Path | Responsibility |
| --- | --- |
| `cmd/pastel` | Executable entry point and embedded version |
| `internal/cli` | Commands and user flow |
| `internal/pack` | `.mrpack` parsing, resolution, loaders, and launch arguments |
| `internal/sync` | Download, override, and prune reconciliation |
| `internal/runtime` | Server supervision, console, crash reporting, and process recovery |
| `internal/jre` | Java selection and verified managed Temurin installation |
| `internal/author` | Kaf pack build and Maven publication helpers |

Pull requests run the check workflow. Every non-automation change that lands on `main` is automatically tested, assigned the next patch version, committed to `VERSION`, tagged, cross-compiled, checksummed, and published as a GitHub release. Commit subjects should be clear and imperative.

Use the repository's [security policy](./SECURITY.md) instead of a public issue for suspected vulnerabilities. Contributions are licensed under [Apache License 2.0](./LICENSE).
