# go-pack
A simple CLI tool to package a Go module for **GOPROXY**.  

It generates `<module>/@v/<version>.mod`, `.info`, and `.zip` in the specified GOPROXY root.

## Usage

```bash
gopack -src <module-dir> -version <vX.Y.Z> -out <goproxy-root>
```
**Flags**:
- `-src` — path to the module root (must contain go.mod)
- `-version` — module version (SemVer, e.g. v1.2.3)
- `-out` — GOPROXY root directory

## Result
Creates:
```
<escaped-module>/@v/<version>.mod — original go.mod
<escaped-module>/@v/<version>.info — version + UTC time
<escaped-module>/@v/<version>.zip — canonical source archive
```
