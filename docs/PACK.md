# Authoring packs for Pastel

Pastel's maintained public pack format is the standard Modrinth `.mrpack` format. A pack that already describes its dedicated-server files correctly should work without Pastel-specific metadata.

## Archive structure

An `.mrpack` is a zip archive with this shape:

```text
my-pack.mrpack
├── modrinth.index.json
├── overrides/             # shared files
├── server-overrides/      # applied after overrides on a server
└── client-overrides/      # ignored by Pastel
```

Pastel accepts format version 1. `modrinth.index.json` needs a display name, version id, dependencies, and file entries:

```json
{
  "formatVersion": 1,
  "game": "minecraft",
  "versionId": "1.2.0",
  "name": "Example Pack",
  "dependencies": {
    "minecraft": "26.2",
    "fabric-loader": "0.19.3"
  },
  "files": [
    {
      "path": "mods/example-1.0.0.jar",
      "hashes": {
        "sha512": "<hex digest>",
        "sha1": "<hex digest>"
      },
      "downloads": [
        "https://cdn.example.com/example-1.0.0.jar"
      ],
      "fileSize": 123456,
      "env": {
        "client": "required",
        "server": "required"
      }
    }
  ]
}
```

For each file, Pastel requires at least one hash and one download URL. It prefers SHA-512, then SHA-256, then SHA-1 when more than one supported digest is present. `fileSize`, when present, is enforced during download.

Pack paths must be clean, relative, slash-separated paths. Absolute paths, traversal components, backslashes, and ambiguous path components are rejected.

## Dedicated-server filtering

Pastel installs a file unless `env.server` is `unsupported`.

| `env.server` | Installed |
| --- | --- |
| `required` | Yes |
| `optional` | Yes; Pastel does not present an interactive file picker |
| omitted | Yes |
| `unsupported` | No |

Mark client-only mods accurately. A client-only jar left as required or optional is one of the most common reasons a dedicated server fails during startup.

## Overrides

Pastel applies override layers in this order:

1. `overrides/`
2. `server-overrides/`

Later files replace earlier files at the same relative path. `client-overrides/` is not applied to a dedicated server.

Jars delivered through `overrides/mods/` or `server-overrides/mods/` are included in the managed mod set and are not immediately pruned. Prefer normal `files[]` entries when a stable downloadable artifact is available; they carry explicit size and hash metadata and avoid embedding large jars in the pack archive.

Pastel refuses `files[]` entries under `world/`. Pack authors should never distribute or replace a server owner's world.

## Loader dependencies

Declare exactly one loader alongside `minecraft` in `dependencies`.

### Fabric

```json
"dependencies": {
  "minecraft": "26.2",
  "fabric-loader": "0.19.3"
}
```

Pastel asks Fabric Meta for the latest stable installer and downloads a server launcher tied to both dependency versions. A pack upgrade that changes Minecraft or Fabric Loader replaces a stale launcher.

### NeoForge

```json
"dependencies": {
  "minecraft": "26.2",
  "neoforge": "<NeoForge version>"
}
```

Pastel downloads the official NeoForge installer, runs `--installServer`, and launches the generated platform-specific argument file.

### Forge

```json
"dependencies": {
  "minecraft": "1.21.1",
  "forge": "<Forge version>"
}
```

Pastel downloads the official Forge installer and uses its generated launch files. A Forge version without the Minecraft prefix is combined with the declared Minecraft version for the installer artifact coordinate.

### Quilt and vanilla

Quilt automatic installation is not implemented. A Quilt pack must provide `quilt-server-launch.jar` through the pack. A vanilla pack must provide `server.jar`.

## Distribution references

Server owners can install the same `.mrpack` through any of these references:

| Reference | Resolution |
| --- | --- |
| `modrinth:slug` | Latest release with a primary `.mrpack` |
| `modrinth:slug:version` | Matching Modrinth version number or version id |
| Modrinth modpack page | Converted to a Modrinth pin |
| `https://…/pack.mrpack` | Direct HTTPS download |
| `./pack.mrpack` | Local archive relative to `server.pastel` |
| Directory containing `modrinth.index.json` | Local unpacked pack |
| `group:artifact:version` | `.mrpack` from the configured Maven repositories |

Modrinth downloads are checked against the size and checksum returned by the Modrinth API. Direct URL and Maven packs still rely on the transport and archive contents; every file listed inside the pack remains hash-verified during sync.

Pastel does not provide a default Maven host. A Maven pin requires an explicit repository:

```toml
pack = "com.example.modpacks:example-pack:1.2.0"
repositories = ["https://maven.example.com"]
memory = "4G"
```

Repositories are tried in order and the first repository containing the artifact wins.

## Maven layout

Pastel expects the standard group/artifact/version directory shape with an `.mrpack` artifact:

```text
com/example/modpacks/example-pack/1.2.0/
├── example-pack-1.2.0.mrpack
├── example-pack-1.2.0.mrpack.sha512
├── example-pack-1.2.0.pom
└── example-pack-1.2.0.pom.sha512
```

`maven-metadata.xml` at the artifact root is required for `latest` resolution and interactive `pastel update` version discovery. There is no JSON, jar, or `.pastel` fallback for Maven-distributed packs.

## Kaf Maven authoring helpers

The bundled authoring commands are intentionally tailored to Kaf's pack publishing flow. Other authors can build `.mrpack` files with Modrinth's tooling, packwiz, or any spec-compliant zip pipeline and distribute them through Modrinth or their own host.

`pastel pack build` starts with an existing `modrinth.index.json`, updates its display name and version, and adds `config/` and `defaultconfigs/` from a prepared server as server overrides:

```bash
pastel pack build \
  -server ./staging/example-server \
  -mrpack ./staging/example-server/modrinth.index.json \
  -name "Example Pack" \
  -slug example-pack \
  -version 1.2.0 \
  -group com.iamkaf.modpacks \
  -out dist/example-pack/1.2.0
```

The output includes the `.mrpack`, Maven POM, `maven-metadata.xml`, SHA-512 sidecars, and `publish.json`. Artifact identifiers are restricted to safe Maven filename components, and symlinked override inputs are rejected.

Publishing is immutable and uses `MAVEN_PUBLISH_USERNAME` and `MAVEN_PUBLISH_PASSWORD`:

```bash
pastel pack publish -dir dist/example-pack/1.2.0 -dry-run
pastel pack publish -dir dist/example-pack/1.2.0
```

The publisher only sends generated files from the build directory and requires HTTPS for the credentialed endpoint.

## Author checklist

- Test the `.mrpack` in a new empty directory.
- Mark every client-only file as unsupported on the server.
- Include strong hashes and accurate file sizes.
- Keep worlds, secrets, operator lists, and user data out of overrides.
- Test the exact Minecraft and loader dependency pair in the index.
- Run `pastel refresh -dry-run` before applying an upgrade to an existing server.
- Stop the server before install, refresh, or update operations.
