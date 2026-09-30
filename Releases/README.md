# Builds

| Archivo | Plataforma |
|---|---|
| SSneakyNovel-linux-amd64.zip | Linux x86_64 (build antiguo, sin frontend nuevo) |
| SSneakyNovel-macos-arm64-20260930-1951.zip | macOS Apple Silicon (M1–M4) — build 2026-09-30 19:51 UTC |
| SSneakyNovel-macos-x64-20260930-1951.zip | macOS Intel — build 2026-09-30 19:51 UTC |

Los builds de macOS incluyen el frontend completo con la pestaña "Novel parameters", selects de género padre/subgénero, checkboxes de specific settings, campo Theme+Motif integrado, comboboxes editables y chips de audiencia.

## Uso (macOS)
```bash
unzip SSneakyNovel-macos-<arch>-<fecha>.zip
chmod +x SSneakyNovel-macos-*
xattr -d com.apple.quarantine SSneakyNovel-macos-*   # evita bloqueo de Gatekeeper
./SSneakyNovel-macos-*
```
Abrir http://localhost:48090
