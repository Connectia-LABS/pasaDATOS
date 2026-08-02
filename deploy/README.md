# Relay remoto de pasaDATOS

Este directorio contiene un servidor Linux estático, una imagen Docker sin dependencias y dos formas de despliegue:

- `docker-compose.yml`: publica el relay en `127.0.0.1:8088` para usarlo detrás de Nginx o Caddy.
- `docker-compose.traefik.yml`: conecta el relay a una red Traefik existente y crea router, TLS y servicio.

## Inicio con Traefik

```bash
cp .env.example .env
nano .env
./install-traefik.sh
```

Luego ingresá la URL HTTPS en **pasaDATOS para Windows → Ajustes → Modo remoto** y abrí esa misma URL en el celular para vincularlo.

Los archivos temporales se guardan en `deploy/data/` y se eliminan automáticamente al vencer la retención o después de ser recibidos.
