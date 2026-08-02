# Compilación y release

## Requisitos

- Go 1.22 o superior.
- Node.js únicamente para comprobar sintaxis JavaScript; no es requisito de ejecución.
- Python con Pillow únicamente si se regeneran iconos; los iconos terminados ya están versionados.

## Pruebas

```bash
go fmt ./...
go vet ./...
go test -race ./...
node --check internal/pasadatos/web/assets/desktop.js
node --check internal/pasadatos/web/assets/mobile.js
```

## Compilación manual

### Relay Linux

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" \
  -o deploy/bin/pasadatos-server-linux-amd64 ./cmd/pasadatos
```

### Windows

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w -H=windowsgui" \
  -o release/pasaDATOS-Windows-x64.exe ./cmd/pasadatos
```

### Instalador

Primero copiá el ejecutable actualizado al payload:

```bash
cp release/pasaDATOS-Windows-x64.exe cmd/installer/payload/pasaDATOS.exe
cp internal/pasadatos/web/assets/pasaDATOS.ico cmd/installer/payload/pasaDATOS.ico
```

Después:

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w -H=windowsgui" \
  -o release/pasaDATOS-Setup-Windows-x64.exe ./cmd/installer
```

## Script completo

```bash
./scripts/build-release.sh
```

## Firma recomendada

Con un certificado Authenticode disponible:

```powershell
signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 /a pasaDATOS-Windows-x64.exe
signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 /a pasaDATOS-Setup-Windows-x64.exe
```

La URL de sello de tiempo es solo un ejemplo; debe elegirse según el proveedor del certificado.
