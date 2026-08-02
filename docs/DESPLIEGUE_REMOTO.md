# Despliegue del relay remoto

## Objetivo

El relay permite enviar archivos cuando la PC y el celular están en redes diferentes. Ambos se conectan a una URL pública HTTPS.

El relay:

- registra identidades anónimas;
- administra vínculos y códigos temporales;
- recibe archivos en streaming;
- almacena archivos temporales en disco;
- permite descargas autenticadas o mediante token corto;
- elimina contenido vencido automáticamente.

## Requisitos

- VPS Linux x86‑64.
- Docker Engine y Docker Compose v2, o capacidad para ejecutar un binario estático.
- Dominio o subdominio.
- Certificado HTTPS mediante Traefik, Nginx, Caddy u otro proxy.
- Espacio de disco suficiente.

## Opción A — Docker con Traefik

```bash
cd deploy
cp .env.example .env
nano .env
```

Valores mínimos:

```env
PASADATOS_DOMAIN=pasadatos.tudominio.com
PASADATOS_PUBLIC_URL=https://pasadatos.tudominio.com
TRAEFIK_NETWORK=traefik
TRAEFIK_ENTRYPOINT=websecure
TRAEFIK_CERTRESOLVER=letsencrypt
```

Desplegar:

```bash
./install-traefik.sh
```

Comprobar:

```bash
curl -fsS https://pasadatos.tudominio.com/api/v1/health
```

La respuesta debe incluir `"ok": true`.

## Opción B — Docker detrás de Nginx o Caddy

```bash
cd deploy
cp .env.example .env
nano .env
docker compose up -d --build
```

El servicio queda en `127.0.0.1:8088`. Usá `nginx-pasadatos.conf.example` como referencia.

Ajustes importantes de Nginx:

```nginx
client_max_body_size 0;
proxy_request_buffering off;
proxy_buffering off;
proxy_read_timeout 86400s;
proxy_send_timeout 86400s;
```

Sin estos ajustes, Nginx puede limitar o almacenar temporalmente una carga antes de enviarla al relay.

## Opción C — Binario Linux sin Docker

```bash
sudo install -m 0755 deploy/bin/pasadatos-server-linux-amd64 /usr/local/bin/pasadatos
sudo mkdir -p /var/lib/pasadatos
```

Ejemplo de servicio systemd:

```ini
[Unit]
Description=pasaDATOS relay
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=PASADATOS_LISTEN=127.0.0.1:8088
Environment=PASADATOS_DATA_DIR=/var/lib/pasadatos
Environment=PASADATOS_PUBLIC_URL=https://pasadatos.tudominio.com
Environment=PASADATOS_FILE_TTL_HOURS=24
Environment=PASADATOS_METADATA_TTL_HOURS=720
Environment=PASADATOS_MAX_FILE_BYTES=0
ExecStart=/usr/local/bin/pasadatos --server
Restart=always
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=/var/lib/pasadatos

[Install]
WantedBy=multi-user.target
```

Guardalo como `/etc/systemd/system/pasadatos.service` y ejecutá:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now pasadatos
sudo systemctl status pasadatos
```

## Variables de entorno

| Variable | Predeterminado | Descripción |
|---|---:|---|
| `PASADATOS_LISTEN` | `0.0.0.0:8088` | Dirección interna del servidor |
| `PASADATOS_DATA_DIR` | `/data` | Estado y archivos temporales |
| `PASADATOS_PUBLIC_URL` | vacío | URL usada para enlaces de vinculación |
| `PASADATOS_FILE_TTL_HOURS` | `24` | Retención de archivos no recibidos |
| `PASADATOS_METADATA_TTL_HOURS` | `720` | Retención de metadatos finalizados |
| `PASADATOS_MAX_FILE_BYTES` | `0` | Límite del relay; `0` desactiva el límite de aplicación |

## Directorio de datos

```text
data/
├── state.json
└── files/
    └── tx_*.bin
```

`state.json` contiene dispositivos, hashes de tokens, vínculos y transferencias. No contiene los tokens secretos en texto plano.

## Copias de seguridad

Para conservar vínculos y transferencias pendientes:

```bash
docker compose stop pasadatos
tar -czf pasadatos-backup.tgz data/
docker compose start pasadatos
```

No es necesario respaldar archivos temporales ya entregados.

## Actualización

1. reemplazá `deploy/bin/pasadatos-server-linux-amd64`;
2. ejecutá `docker compose up -d --build`;
3. comprobá `/api/v1/health`;
4. conservá `deploy/data/`.

## Operación segura

- Publicá únicamente el proxy HTTPS, no el puerto `8088` directo.
- Mantené actualizado el sistema y el proxy.
- Supervisá espacio libre e inodos.
- Configurá copias de seguridad de `state.json` si los vínculos son importantes.
- Considerá una VPN o control de acceso adicional si el relay será de uso privado estricto.
